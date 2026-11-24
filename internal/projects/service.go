package projects

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"gestionador/internal/gitx"
	"gestionador/internal/secrets"
	"gestionador/internal/servers"
	"gestionador/internal/sshx"
)

const (
	repoCheckTimeout = 20 * time.Second
	deployTimeout    = 30 * time.Minute
	healthWait       = 5 * time.Second // tiempo que el contenedor nuevo debe seguir vivo
	publishTimeout   = 3 * time.Minute
)

// Servers es lo que este módulo necesita del módulo de servidores.
type Servers interface {
	Get(ctx context.Context, id string) (servers.Server, error)
	Exec(ctx context.Context, serverID, cmd string, opts sshx.ExecOptions) (int, error)
	Run(ctx context.Context, serverID, cmd string) (sshx.Result, error)
}

type Service struct {
	store    *Store
	servers  Servers
	box      *secrets.Box
	buildDir string

	rootCtx context.Context // se cancela al apagar la instancia
	wg      sync.WaitGroup

	mu      sync.Mutex
	running map[string]*activeDeploy // por ID de proyecto
}

type activeDeploy struct {
	deploymentID string
	log          *LogBuffer
	cancel       context.CancelFunc
	publishing   bool // protegido por Service.mu; a partir de aquí no se puede cancelar
}

func NewService(ctx context.Context, store *Store, srv Servers, box *secrets.Box, dataDir string) (*Service, error) {
	if n, err := store.FailInterrupted(ctx, time.Now().UTC()); err != nil {
		return nil, err
	} else if n > 0 {
		slog.Warn("deploys interrumpidos por un reinicio marcados como fallidos", "cantidad", n)
	}

	buildDir := filepath.Join(dataDir, "builds")
	// Restos de builds anteriores (si la instancia se cortó a mitad de un clon).
	_ = os.RemoveAll(buildDir)
	if err := os.MkdirAll(buildDir, 0o700); err != nil {
		return nil, err
	}

	return &Service{
		store:    store,
		servers:  srv,
		box:      box,
		buildDir: buildDir,
		rootCtx:  ctx,
		running:  make(map[string]*activeDeploy),
	}, nil
}

// Wait espera a que terminen los deploys en curso (se cancelan al apagar).
func (s *Service) Wait() { s.wg.Wait() }

// ---------- Proyectos ----------

func (s *Service) List(ctx context.Context) ([]Project, map[string]Deployment, error) {
	list, err := s.store.ListProjects(ctx)
	if err != nil {
		return nil, nil, err
	}
	latest, err := s.store.LatestDeployments(ctx)
	return list, latest, err
}

func (s *Service) Get(ctx context.Context, id string) (Project, error) {
	return s.store.GetProject(ctx, id)
}

func (s *Service) Create(ctx context.Context, in CreateInput) (Project, error) {
	if err := in.normalize(); err != nil {
		return Project{}, err
	}
	if _, err := s.servers.Get(ctx, in.ServerID); err != nil {
		if errors.Is(err, servers.ErrNotFound) {
			return Project{}, invalid("el servidor elegido no existe")
		}
		return Project{}, err
	}

	checkCtx, cancel := context.WithTimeout(ctx, repoCheckTimeout)
	defer cancel()
	repo := gitx.Repo{URL: in.RepoURL, Branch: in.Branch, Username: in.GitUsername, Token: in.GitToken}
	if err := gitx.Check(checkCtx, repo); err != nil {
		return Project{}, &RepoError{Err: err}
	}

	now := time.Now().UTC()
	p := Project{
		ID:            uuid.Must(uuid.NewV7()).String(),
		Name:          in.Name,
		Slug:          slugify(in.Name),
		ServerID:      in.ServerID,
		RepoURL:       in.RepoURL,
		Branch:        in.Branch,
		GitUsername:   in.GitUsername,
		RootDir:       in.RootDir,
		Dockerfile:    in.Dockerfile,
		ContainerPort: in.ContainerPort,
		HostPort:      in.HostPort,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if in.GitToken != "" {
		enc, err := s.box.Encrypt([]byte(in.GitToken))
		if err != nil {
			return Project{}, err
		}
		p.GitTokenEnc = enc
	}

	if err := s.store.CreateProject(ctx, p); err != nil {
		return Project{}, err
	}
	slog.Info("proyecto creado", "id", p.ID, "name", p.Name)
	return s.store.GetProject(ctx, p.ID)
}

// Delete borra el proyecto y, si el servidor responde, su contenedor e imágenes.
// Devuelve un aviso (no un error) si no se pudo limpiar el servidor.
func (s *Service) Delete(ctx context.Context, id string) (warning string, err error) {
	p, err := s.store.GetProject(ctx, id)
	if err != nil {
		return "", err
	}
	if s.activeFor(id) != nil {
		return "", ErrDeployInProgress
	}

	cleanCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if res, err := s.servers.Run(cleanCtx, p.ServerID, removeAllCmd(p)); err != nil || res.ExitCode != 0 {
		warning = "No se pudo limpiar el servidor (¿está apagado?). Revisa manualmente el contenedor " + p.containerName()
		slog.Warn("borrar proyecto: limpieza en servidor falló", "project", p.Name, "err", err)
	}

	if err := s.store.DeleteProject(ctx, id); err != nil {
		return "", err
	}
	slog.Info("proyecto eliminado", "id", id, "name", p.Name)
	return warning, nil
}

func (s *Service) ListDeployments(ctx context.Context, projectID string) ([]Deployment, error) {
	return s.store.ListDeployments(ctx, projectID, 30)
}

// ContainerStatus consulta el estado del contenedor en el servidor.
func (s *Service) ContainerStatus(ctx context.Context, p Project) (ContainerStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	res, err := s.servers.Run(ctx, p.ServerID, statusCmd(p))
	if err != nil {
		return ContainerStatus{}, err
	}
	return parseStatus(res.Stdout), nil
}

// StreamLogs sigue los logs del contenedor hasta que ctx se cancele.
func (s *Service) StreamLogs(ctx context.Context, p Project, tail int, w io.Writer) error {
	// PTY: al cerrar la conexión, `docker logs -f` muere en el servidor.
	_, err := s.servers.Exec(ctx, p.ServerID, logsCmd(p, tail), sshx.ExecOptions{Stdout: w, Stderr: w, PTY: true})
	return err
}

// ---------- Deploys ----------

func (s *Service) activeFor(projectID string) *activeDeploy {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running[projectID]
}

// ActiveDeploymentID devuelve el deploy en curso del proyecto, o "".
func (s *Service) ActiveDeploymentID(projectID string) string {
	if a := s.activeFor(projectID); a != nil {
		return a.deploymentID
	}
	return ""
}

// LiveLog devuelve el log en vivo de un deploy en curso (nil si ya terminó).
func (s *Service) LiveLog(deploymentID string) *LogBuffer {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.running {
		if a.deploymentID == deploymentID {
			return a.log
		}
	}
	return nil
}

func (s *Service) GetDeployment(ctx context.Context, id string) (Deployment, error) {
	d, err := s.store.GetDeployment(ctx, id)
	if err != nil {
		return d, err
	}
	if live := s.LiveLog(id); live != nil {
		d.Log = live.String()
	}
	return d, nil
}

// Deploy inicia un deploy en segundo plano y devuelve el registro creado.
func (s *Service) Deploy(ctx context.Context, projectID string) (Deployment, error) {
	p, err := s.store.GetProject(ctx, projectID)
	if err != nil {
		return Deployment{}, err
	}

	d := Deployment{
		ID:        uuid.Must(uuid.NewV7()).String(),
		ProjectID: p.ID,
		Status:    StatusQueued,
		CreatedAt: time.Now().UTC(),
	}

	// Reservamos el lugar antes de tocar la base: un solo deploy por proyecto.
	runCtx, cancel := context.WithTimeout(s.rootCtx, deployTimeout)
	active := &activeDeploy{deploymentID: d.ID, log: newLogBuffer(), cancel: cancel}
	s.mu.Lock()
	if _, busy := s.running[p.ID]; busy {
		s.mu.Unlock()
		cancel()
		return Deployment{}, ErrDeployInProgress
	}
	s.running[p.ID] = active
	s.mu.Unlock()

	if err := s.store.CreateDeployment(ctx, d); err != nil {
		s.release(p.ID)
		cancel()
		return Deployment{}, err
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.release(p.ID)
		defer cancel()
		s.run(runCtx, p, d, active)
	}()
	return d, nil
}

func (s *Service) release(projectID string) {
	s.mu.Lock()
	delete(s.running, projectID)
	s.mu.Unlock()
}

// Cancel detiene un deploy en curso.
func (s *Service) Cancel(deploymentID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.running {
		if a.deploymentID == deploymentID {
			if a.publishing {
				return ErrTooLateToCancel
			}
			a.cancel()
			return nil
		}
	}
	return ErrNotRunning
}

// run ejecuta el deploy completo y guarda el resultado.
func (s *Service) run(ctx context.Context, p Project, d Deployment, active *activeDeploy) {
	start := time.Now()
	log := active.log
	db := context.WithoutCancel(ctx) // las escrituras finales deben ocurrir aunque se cancele

	err := s.execute(ctx, db, p, &d, active)

	status, msg := StatusSucceeded, ""
	switch {
	case err == nil:
		log.Step("Deploy completado en %s", time.Since(start).Round(time.Second))
	case errors.Is(err, context.Canceled) && s.rootCtx.Err() != nil:
		status, msg = StatusFailed, "La instancia se apagó durante el deploy"
	case errors.Is(err, context.Canceled):
		status, msg = StatusCanceled, "Cancelado por el usuario"
	case errors.Is(err, context.DeadlineExceeded):
		status, msg = StatusFailed, fmt.Sprintf("Se superó el tiempo máximo de deploy (%s)", deployTimeout)
	default:
		status, msg = StatusFailed, err.Error()
	}
	if status != StatusSucceeded {
		log.Step("ERROR: %s", msg)
	}
	log.Close()

	if err := s.store.Finish(db, d.ID, status, msg, log.String(), time.Now().UTC()); err != nil {
		slog.Error("guardar resultado del deploy", "deployment", d.ID, "err", err)
	}
	slog.Info("deploy terminado", "project", p.Name, "deployment", d.ID, "status", status, "dur", time.Since(start).Round(time.Second))
}

func (s *Service) execute(ctx, db context.Context, p Project, d *Deployment, active *activeDeploy) error {
	log := active.log
	if err := s.store.MarkStarted(db, d.ID, time.Now().UTC()); err != nil {
		return err
	}

	srv, err := s.servers.Get(ctx, p.ServerID)
	if err != nil {
		return err
	}
	if srv.System.DockerStatus != servers.DockerRunning {
		return fmt.Errorf("docker no está disponible en el servidor %s; revísalo en la página del servidor", srv.Name)
	}

	// 1. Clonar
	log.Step("Clonando %s (rama %s)", p.RepoURL, p.Branch)
	repo := gitx.Repo{URL: p.RepoURL, Branch: p.Branch, Username: p.GitUsername}
	if p.GitTokenEnc != "" {
		token, err := s.box.Decrypt(p.GitTokenEnc)
		if err != nil {
			return fmt.Errorf("no se pudo descifrar el token de git: %w", err)
		}
		repo.Token = string(token)
	}
	dir := filepath.Join(s.buildDir, d.ID)
	defer os.RemoveAll(dir)

	commit, err := gitx.Clone(ctx, repo, dir)
	if err != nil {
		return err
	}
	d.CommitSHA, d.CommitMessage = commit.SHA, commit.Message
	d.Image = fmt.Sprintf("%s:%s", p.imageRepo(), d.ID)
	if err := s.store.SetCommit(db, d.ID, *d); err != nil {
		return err
	}
	fmt.Fprintf(log, "Commit %s: %s\n", shortSHA(commit.SHA), commit.Message)

	// 2. Build: el contexto viaja como tar por stdin de `docker build -`.
	log.Step("Construyendo imagen en %s", srv.Name)
	contextDir := filepath.Join(dir, filepath.FromSlash(p.RootDir))
	pr, pw := io.Pipe()
	tarErr := make(chan error, 1)
	go func() {
		err := gitx.TarContext(contextDir, p.Dockerfile, pw)
		tarErr <- err
		pw.CloseWithError(err)
	}()
	code, err := s.servers.Exec(ctx, p.ServerID, buildCmd(p, d.Image), sshx.ExecOptions{Stdin: pr, Stdout: log, Stderr: log})
	pr.CloseWithError(io.ErrClosedPipe) // desbloquea al escritor si el build terminó antes de leer todo
	if tErr := <-tarErr; tErr != nil && !errors.Is(tErr, io.ErrClosedPipe) {
		return fmt.Errorf("preparar el contexto de build: %w", tErr)
	}
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("el build falló (código %d); revisa el log", code)
	}

	// 3. Publicar el contenedor nuevo. Desde aquí el deploy no se puede cancelar:
	// cortarlo a la mitad podría dejar la app caída.
	s.mu.Lock()
	active.publishing = true
	s.mu.Unlock()
	if err := ctx.Err(); err != nil { // se canceló justo antes
		return err
	}
	if err := s.store.SetStatus(db, d.ID, StatusDeploying); err != nil {
		return err
	}
	pctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), publishTimeout)
	defer cancel()
	return s.publish(pctx, p, d, log)
}

// publish reemplaza el contenedor y vuelve al anterior si algo falla.
// Recibe un contexto que no se cancela por el usuario (solo por timeout).
func (s *Service) publish(ctx context.Context, p Project, d *Deployment, log *LogBuffer) error {
	log.Step("Publicando contenedor %s", p.containerName())

	err := s.runStep(ctx, p, stopPreviousCmd(p), log, "no se pudo detener el contenedor anterior")
	if err == nil {
		err = s.runStep(ctx, p, runCmd(p, d.ID, d.Image), log, "no se pudo iniciar el contenedor; revisa el error de Docker en el log")
	}
	if err == nil {
		fmt.Fprintf(log, "Esperando %s para verificar que el contenedor siga en ejecución...\n", healthWait)
		time.Sleep(healthWait)
		err = s.checkHealthy(ctx, p, log)
	}

	if err != nil {
		log.Step("El contenedor nuevo no arrancó; revirtiendo")
		rbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
		defer cancel()
		_, _ = s.servers.Exec(rbCtx, p.ServerID, failedLogsCmd(p), sshx.ExecOptions{Stdout: log, Stderr: log})
		_ = s.runStep(rbCtx, p, rollbackCmd(p), log, "no se pudo restaurar la versión anterior")
		return err
	}

	if p.HostPort != nil {
		fmt.Fprintf(log, "Disponible en http://%s:%d\n", hostOnly(p.ServerHost), *p.HostPort)
	}
	log.Step("Limpiando imágenes antiguas (se conservan %d)", keepImages)
	return s.runStep(ctx, p, cleanupCmd(p), log, "no se pudieron limpiar las imágenes antiguas")
}

func (s *Service) checkHealthy(ctx context.Context, p Project, log *LogBuffer) error {
	res, err := s.servers.Run(ctx, p.ServerID, inspectCmd(p))
	if err != nil {
		return err
	}
	f := strings.Fields(res.Stdout)
	if len(f) < 3 {
		return fmt.Errorf("no se pudo inspeccionar el contenedor: %s", strings.TrimSpace(res.Stderr))
	}
	state, restarts, exitCode := f[0], f[1], f[2]
	if state != "running" || restarts != "0" {
		return fmt.Errorf("el contenedor se detuvo (estado %s, código de salida %s, reinicios %s)", state, exitCode, restarts)
	}
	fmt.Fprintln(log, "El contenedor está en ejecución")
	return nil
}

// runStep ejecuta un comando y vuelca su salida en el log. what describe el fallo.
func (s *Service) runStep(ctx context.Context, p Project, cmd string, log *LogBuffer, what string) error {
	code, err := s.servers.Exec(ctx, p.ServerID, cmd, sshx.ExecOptions{Stdout: log, Stderr: log})
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if code != 0 {
		return fmt.Errorf("%s (código %d)", what, code)
	}
	return nil
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func hostOnly(h string) string {
	if strings.Contains(h, ":") { // IPv6
		return "[" + h + "]"
	}
	return h
}
