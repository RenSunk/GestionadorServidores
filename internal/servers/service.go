package servers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"

	"gestionador/internal/metrics"
	"gestionador/internal/secrets"
	"gestionador/internal/settings"
	"gestionador/internal/sshx"
)

const (
	monitorInterval    = 15 * time.Second
	probeTimeout       = 25 * time.Second
	maxParallelProbes  = 8
	historySize        = 120 // 120 muestras × 15 s = 30 minutos
	instanceKeySetting = "ssh_instance_key"
)

// ConnectionError indica que no se pudo conectar o verificar el servidor.
type ConnectionError struct{ Err error }

func (e *ConnectionError) Error() string { return e.Err.Error() }
func (e *ConnectionError) Unwrap() error { return e.Err }

type Service struct {
	store       *Store
	box         *secrets.Box
	pool        *sshx.Pool
	instanceKey ssh.Signer

	recorder *metrics.Recorder // nil = sin historial
	observer Observer          // nil = sin alertas

	mu           sync.RWMutex
	snapshots    map[string]*snapshot
	containersAt map[string]time.Time // último sondeo de contenedores por servidor
}

type snapshot struct {
	latest  *Metrics
	history []Metrics // ordenadas de más antigua a más reciente
}

func NewService(ctx context.Context, store *Store, settingsStore *settings.Store, box *secrets.Box) (*Service, error) {
	key, err := loadOrCreateInstanceKey(ctx, settingsStore, box)
	if err != nil {
		return nil, fmt.Errorf("llave SSH de la instancia: %w", err)
	}
	return &Service{
		store:        store,
		box:          box,
		pool:         sshx.NewPool(),
		instanceKey:  key,
		snapshots:    make(map[string]*snapshot),
		containersAt: make(map[string]time.Time),
	}, nil
}

// loadOrCreateInstanceKey genera (una sola vez) la llave SSH que la instancia usa
// por defecto. Se guarda cifrada en settings; el usuario solo copia la pública.
func loadOrCreateInstanceKey(ctx context.Context, st *settings.Store, box *secrets.Box) (ssh.Signer, error) {
	enc, err := st.Get(ctx, instanceKeySetting)
	if errors.Is(err, settings.ErrNotFound) {
		pemBytes, err := sshx.GenerateKey("gestionador")
		if err != nil {
			return nil, err
		}
		if enc, err = box.Encrypt(pemBytes); err != nil {
			return nil, err
		}
		if err := st.Set(ctx, instanceKeySetting, enc); err != nil {
			return nil, err
		}
		slog.Info("se generó la llave SSH de la instancia")
	} else if err != nil {
		return nil, err
	}

	pemBytes, err := box.Decrypt(enc)
	if err != nil {
		return nil, fmt.Errorf("no se pudo descifrar (¿cambió la clave maestra?): %w", err)
	}
	return sshx.ParsePrivateKey(pemBytes)
}

// InstancePublicKey es la línea que el usuario agrega a ~/.ssh/authorized_keys.
func (s *Service) InstancePublicKey() string {
	return sshx.MarshalAuthorizedKey(s.instanceKey.PublicKey()) + " gestionador"
}

func (s *Service) List(ctx context.Context) ([]Server, error) {
	return s.store.List(ctx)
}

func (s *Service) Get(ctx context.Context, id string) (Server, error) {
	return s.store.Get(ctx, id)
}

// Create valida la conexión antes de guardar: si no podemos entrar, no se registra.
func (s *Service) Create(ctx context.Context, in CreateInput) (Server, error) {
	if err := in.normalize(); err != nil {
		return Server{}, err
	}

	srv := Server{
		ID:         uuid.Must(uuid.NewV7()).String(),
		Name:       in.Name,
		Host:       in.Host,
		Port:       in.Port,
		Username:   in.Username,
		AuthMethod: in.AuthMethod,
	}

	var credential string
	switch in.AuthMethod {
	case AuthPrivateKey:
		credential = strings.TrimSpace(in.PrivateKey) + "\n"
		if _, err := sshx.ParsePrivateKey([]byte(credential)); err != nil {
			return Server{}, invalid("%s", err.Error())
		}
	case AuthPassword:
		credential = in.Password
	}
	if credential != "" {
		enc, err := s.box.Encrypt([]byte(credential))
		if err != nil {
			return Server{}, err
		}
		srv.CredentialEnc = enc
	}

	target, err := s.target(srv)
	if err != nil {
		return Server{}, err
	}

	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	client, hostKey, err := sshx.Dial(probeCtx, target)
	if err != nil {
		return Server{}, &ConnectionError{Err: err}
	}
	defer client.Close()

	info, metrics, err := runProbe(func(cmd string) (sshx.Result, error) { return sshx.Run(probeCtx, client, cmd) })
	if err != nil {
		return Server{}, &ConnectionError{Err: err}
	}

	now := time.Now().UTC()
	srv.HostKey = sshx.MarshalAuthorizedKey(hostKey) // TOFU: fijamos la llave del host
	srv.System = info
	srv.Status = StatusOnline
	srv.LastSeenAt = &now
	srv.CreatedAt = now
	srv.UpdatedAt = now

	if err := s.store.Create(ctx, srv); err != nil {
		return Server{}, err
	}
	s.recordMetrics(srv.ID, metrics)
	slog.Info("servidor registrado", "id", srv.ID, "name", srv.Name, "host", srv.Host)
	return srv, nil
}

// Check verifica un servidor ahora mismo y devuelve su estado actualizado.
func (s *Service) Check(ctx context.Context, id string) (Server, error) {
	srv, err := s.store.Get(ctx, id)
	if err != nil {
		return Server{}, err
	}
	// Conexión nueva: cambios como agregar el usuario al grupo docker solo
	// aplican a sesiones SSH nuevas.
	s.pool.Forget(id)
	s.probe(ctx, srv)
	return s.store.Get(ctx, id)
}

func (s *Service) Delete(ctx context.Context, id string) error {
	if err := s.store.Delete(ctx, id); err != nil {
		return err
	}
	s.pool.Forget(id)
	s.mu.Lock()
	delete(s.snapshots, id)
	delete(s.containersAt, id)
	s.mu.Unlock()
	if s.recorder != nil {
		if err := s.recorder.DeleteServer(ctx, id); err != nil {
			slog.Warn("borrar historial del servidor", "err", err)
		}
	}
	return nil
}

// Metrics devuelve la última muestra y el historial reciente (nil si aún no hay datos).
func (s *Service) Metrics(id string) (*Metrics, []Metrics) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap, ok := s.snapshots[id]
	if !ok {
		return nil, nil
	}
	return snap.latest, append([]Metrics(nil), snap.history...)
}

// RunMonitor verifica todos los servidores periódicamente hasta que ctx se cancele.
func (s *Service) RunMonitor(ctx context.Context) {
	defer s.pool.CloseAll()
	ticker := time.NewTicker(monitorInterval)
	defer ticker.Stop()

	for {
		s.probeAll(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) probeAll(ctx context.Context) {
	list, err := s.store.List(ctx)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("monitor: listar servidores", "err", err)
		}
		return
	}

	sem := make(chan struct{}, maxParallelProbes)
	var wg sync.WaitGroup
	for _, srv := range list {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			s.probe(ctx, srv)
		}()
	}
	wg.Wait()
}

// probe verifica un servidor y guarda el resultado (online u offline).
func (s *Service) probe(ctx context.Context, srv Server) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	info, metrics, err := s.runOnServer(ctx, srv)
	now := time.Now().UTC()
	if ctx.Err() != nil && errors.Is(context.Cause(ctx), context.Canceled) {
		return // la instancia se está apagando
	}

	if err != nil {
		if srv.Status != StatusOffline || srv.LastError != err.Error() {
			slog.Warn("servidor no disponible", "name", srv.Name, "err", err)
		}
		if dbErr := s.store.RecordCheckFailed(context.WithoutCancel(ctx), srv.ID, err.Error(), now); dbErr != nil {
			slog.Error("guardar estado del servidor", "err", dbErr)
		}
		s.mu.Lock()
		if snap, ok := s.snapshots[srv.ID]; ok {
			snap.latest = nil // no mostramos métricas viejas como si fueran actuales
		}
		s.mu.Unlock()
		s.afterProbe(ctx, srv, info, nil, err)
		return
	}

	if err := s.store.RecordCheckOK(context.WithoutCancel(ctx), srv.ID, info, now); err != nil {
		slog.Error("guardar estado del servidor", "err", err)
	}
	s.recordMetrics(srv.ID, metrics)
	srv.Status, srv.System = StatusOnline, info
	s.afterProbe(ctx, srv, info, &metrics, nil)
}

func (s *Service) runOnServer(ctx context.Context, srv Server) (SystemInfo, Metrics, error) {
	dial, err := s.dialer(srv)
	if err != nil {
		return SystemInfo{}, Metrics{}, err
	}
	return runProbe(func(cmd string) (sshx.Result, error) {
		return s.pool.Run(ctx, srv.ID, dial, cmd)
	})
}

func (s *Service) dialer(srv Server) (func(context.Context) (*ssh.Client, error), error) {
	target, err := s.target(srv)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) (*ssh.Client, error) {
		c, _, err := sshx.Dial(ctx, target)
		return c, err
	}, nil
}

// Exec ejecuta un comando en el servidor en streaming (lo usan otros módulos,
// p. ej. proyectos para hacer build y ver logs). Reutiliza la conexión del pool.
func (s *Service) Exec(ctx context.Context, serverID, cmd string, opts sshx.ExecOptions) (int, error) {
	srv, err := s.store.Get(ctx, serverID)
	if err != nil {
		return -1, err
	}
	dial, err := s.dialer(srv)
	if err != nil {
		return -1, err
	}
	session, err := s.pool.Session(ctx, srv.ID, dial)
	if err != nil {
		return -1, err
	}
	return sshx.Exec(ctx, session, cmd, opts)
}

// Run ejecuta un comando en el servidor y devuelve la salida completa.
func (s *Service) Run(ctx context.Context, serverID, cmd string) (sshx.Result, error) {
	var stdout, stderr strings.Builder
	code, err := s.Exec(ctx, serverID, cmd, sshx.ExecOptions{Stdout: &stdout, Stderr: &stderr})
	return sshx.Result{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: code}, err
}

func runProbe(run func(cmd string) (sshx.Result, error)) (SystemInfo, Metrics, error) {
	res, err := run(probeScript)
	if err != nil {
		return SystemInfo{}, Metrics{}, err
	}
	return parseProbe(res.Stdout, time.Now().UTC())
}

// target arma los datos de conexión descifrando la credencial guardada.
func (s *Service) target(srv Server) (sshx.Target, error) {
	t := sshx.Target{Host: srv.Host, Port: srv.Port, User: srv.Username, HostKey: srv.HostKey}

	if srv.AuthMethod == AuthInstanceKey {
		t.Signer = s.instanceKey
		return t, nil
	}

	cred, err := s.box.Decrypt(srv.CredentialEnc)
	if err != nil {
		return t, fmt.Errorf("no se pudo descifrar la credencial: %w", err)
	}
	switch srv.AuthMethod {
	case AuthPrivateKey:
		signer, err := sshx.ParsePrivateKey(cred)
		if err != nil {
			return t, err
		}
		t.Signer = signer
	case AuthPassword:
		t.Password = string(cred)
	}
	return t, nil
}

func (s *Service) recordMetrics(id string, m Metrics) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, ok := s.snapshots[id]
	if !ok {
		snap = &snapshot{}
		s.snapshots[id] = snap
	}
	snap.latest = &m
	snap.history = append(snap.history, m)
	if len(snap.history) > historySize {
		snap.history = snap.history[len(snap.history)-historySize:]
	}
}
