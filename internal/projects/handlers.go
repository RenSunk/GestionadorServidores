package projects

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gestionador/internal/auth"
	"gestionador/internal/buildspec"
	"gestionador/internal/httpx"
	"gestionador/internal/proxy"
)

const ssePingInterval = 20 * time.Second

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/runtimes", h.runtimes)
	mux.HandleFunc("POST /api/projects/detect", h.detect)
	mux.HandleFunc("POST /api/dockerfile-preview", h.dockerfilePreview)

	mux.HandleFunc("GET /api/projects", h.list)
	mux.HandleFunc("POST /api/projects", h.create)
	mux.HandleFunc("GET /api/projects/{id}", h.get)
	mux.HandleFunc("PUT /api/projects/{id}", h.update)
	mux.HandleFunc("DELETE /api/projects/{id}", h.delete)
	mux.HandleFunc("GET /api/projects/{id}/env", h.getEnv)
	mux.HandleFunc("PUT /api/projects/{id}/env", h.setEnv)
	mux.HandleFunc("POST /api/projects/{id}/deploy", h.deploy)
	mux.HandleFunc("POST /api/projects/{id}/restart", h.restart)
	mux.HandleFunc("GET /api/projects/{id}/status", h.status)
	mux.HandleFunc("GET /api/projects/{id}/logs", h.containerLogs)
	mux.HandleFunc("GET /api/projects/{id}/dockerfile", h.projectDockerfile)
	mux.HandleFunc("GET /api/projects/{id}/metrics", h.metrics)

	mux.HandleFunc("GET /api/projects/{id}/domains", h.listDomains)
	mux.HandleFunc("POST /api/projects/{id}/domains", h.addDomain)
	mux.HandleFunc("DELETE /api/domains/{id}", h.removeDomain)
	mux.HandleFunc("POST /api/domains/{id}/check", h.checkDomain)
	mux.HandleFunc("GET /api/servers/{id}/proxy", h.getProxy)
	mux.HandleFunc("PUT /api/servers/{id}/proxy", h.setProxy)

	mux.HandleFunc("GET /api/projects/{id}/autodeploy", h.getAutoDeploy)
	mux.HandleFunc("PUT /api/projects/{id}/autodeploy", h.setAutoDeploy)
	mux.HandleFunc("POST /api/projects/{id}/autodeploy/secret", h.rotateSecret)
	// Público (lo llama el proveedor de Git): se autentica con la firma del secreto.
	mux.HandleFunc("POST /hooks/git/{id}", h.gitHook)

	mux.HandleFunc("GET /api/deployments/{id}", h.getDeployment)
	mux.HandleFunc("POST /api/deployments/{id}/cancel", h.cancelDeployment)
	mux.HandleFunc("POST /api/deployments/{id}/redeploy", h.redeploy)
	mux.HandleFunc("GET /api/deployments/{id}/logs", h.deploymentLogs)
}

// ---------- Vistas JSON ----------

type projectView struct {
	ID                 string      `json:"id"`
	Name               string      `json:"name"`
	Slug               string      `json:"slug"`
	ServerID           string      `json:"serverId"`
	ServerName         string      `json:"serverName"`
	RepoURL            string      `json:"repoUrl"`
	Branch             string      `json:"branch"`
	GitUsername        string      `json:"gitUsername"`
	HasToken           bool        `json:"hasToken"`
	RootDir            string      `json:"rootDir"`
	Dockerfile         string      `json:"dockerfile"`
	ContainerPort      int         `json:"containerPort"`
	HostPort           *int        `json:"hostPort"`
	BuildType          BuildType   `json:"buildType"`
	Runtime            string      `json:"runtime"`
	RuntimeVersion     string      `json:"runtimeVersion"`
	RuntimeLabel       string      `json:"runtimeLabel"`
	BaseImage          string      `json:"baseImage"`
	BuildCommand       string      `json:"buildCommand"`
	StartCommand       string      `json:"startCommand"`
	CPULimit           *float64    `json:"cpuLimit"`
	MemoryLimitMB      *int        `json:"memoryLimitMb"`
	URL                string      `json:"url"`
	Domains            []string    `json:"domains"`
	AutoDeploy         bool        `json:"autoDeploy"`
	ContainerName      string      `json:"containerName"`
	CreatedAt          time.Time   `json:"createdAt"`
	LastDeployment     *Deployment `json:"lastDeployment"`
	ActiveDeploymentID string      `json:"activeDeploymentId"`
}

type projectDetailView struct {
	projectView
	Deployments []Deployment   `json:"deployments"`
	Pending     PendingChanges `json:"pending"`
}

type deploymentDetailView struct {
	Deployment
	Log  string `json:"log"`
	Live bool   `json:"live"`
}

// withDomains completa los dominios y usa el primero como URL principal.
func (h *Handler) withDomains(ctx context.Context, v projectView, serverID string) projectView {
	v.Domains = []string{}
	ds, err := h.svc.store.ListDomains(ctx, v.ID)
	if err != nil || len(ds) == 0 {
		return v
	}
	for _, d := range ds {
		v.Domains = append(v.Domains, d.Hostname)
	}
	v.URL = "https://" + ds[0].Hostname
	if ports, err := h.svc.store.ProxyPorts(ctx, serverID); err == nil && ports.HTTPS != 443 {
		v.URL += ":" + strconv.Itoa(ports.HTTPS)
	}
	return v
}

func (h *Handler) view(p Project, last *Deployment) projectView {
	v := projectView{
		ID: p.ID, Name: p.Name, Slug: p.Slug, ServerID: p.ServerID, ServerName: p.ServerName,
		AutoDeploy: p.AutoDeploy,
		RepoURL:    p.RepoURL, Branch: p.Branch, GitUsername: p.GitUsername, HasToken: p.GitTokenEnc != "",
		RootDir: p.RootDir, Dockerfile: p.Dockerfile, ContainerPort: p.ContainerPort, HostPort: p.HostPort,
		ContainerName: p.containerName(), CreatedAt: p.CreatedAt, LastDeployment: last,
		ActiveDeploymentID: h.svc.ActiveDeploymentID(p.ID),
		BuildType:          p.BuildType,
	}
	if p.BuildType == BuildRuntime {
		v.Runtime, v.RuntimeVersion, v.BaseImage = p.Build.Runtime, p.Build.Version, p.Build.BaseImage
		v.BuildCommand, v.StartCommand, v.RuntimeLabel = p.Build.BuildCommand, p.Build.StartCommand, p.Build.Label()
	}
	if p.CPULimitMilli != nil {
		cpu := float64(*p.CPULimitMilli) / 1000
		v.CPULimit = &cpu
	}
	v.MemoryLimitMB = p.MemoryLimitMB
	if p.HostPort != nil {
		v.URL = fmt.Sprintf("http://%s:%d", hostOnly(p.ServerHost), *p.HostPort)
	}
	return v
}

// ---------- Configuración de build ----------

func (h *Handler) runtimes(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, buildspec.Catalog())
}

func (h *Handler) detect(w http.ResponseWriter, r *http.Request) {
	var in DetectInput
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	d, err := h.svc.Detect(r.Context(), in)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, d)
}

// dockerfilePreview genera el Dockerfile sin guardar nada (vista previa del formulario).
func (h *Handler) dockerfilePreview(w http.ResponseWriter, r *http.Request) {
	var in struct {
		buildspec.Spec
		BuildArgs []string `json:"buildArgs"`
	}
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := in.Spec.Normalize(); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	for _, a := range in.BuildArgs {
		if !envKeyRe.MatchString(a) {
			httpx.WriteError(w, http.StatusBadRequest, "nombre de variable inválido")
			return
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"dockerfile": buildspec.Dockerfile(in.Spec, in.BuildArgs...)})
}

func (h *Handler) projectDockerfile(w http.ResponseWriter, r *http.Request) {
	p, err := h.svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	if p.BuildType != BuildRuntime {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"dockerfile": ""})
		return
	}
	spec := p.Build
	if err := spec.Normalize(); err != nil {
		httpx.WriteError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	env, err := h.svc.GetEnv(r.Context(), p.ID)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"dockerfile": buildspec.Dockerfile(spec, envKeys(buildVars(env))...)})
}

// ---------- Proyectos ----------

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	list, latest, err := h.svc.List(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	scope := auth.ScopeFrom(r.Context())
	out := make([]projectView, 0, len(list))
	for _, p := range list {
		if !scope.Project(p.ID) {
			continue
		}
		var last *Deployment
		if d, ok := latest[p.ID]; ok {
			last = &d
		}
		out = append(out, h.withDomains(r.Context(), h.view(p, last), p.ServerID))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in CreateInput
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !auth.ScopeFrom(r.Context()).Server(in.ServerID) {
		httpx.WriteError(w, http.StatusBadRequest, "servidor no encontrado")
		return
	}
	p, err := h.svc.Create(r.Context(), in)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, h.view(p, nil))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	p, err := h.svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	deps, err := h.svc.ListDeployments(r.Context(), p.ID)
	if err != nil {
		h.fail(w, err)
		return
	}
	var last *Deployment
	if len(deps) > 0 {
		last = &deps[0]
	}
	if deps == nil {
		deps = []Deployment{}
	}
	pending, err := h.svc.Pending(r.Context(), p)
	if err != nil {
		h.fail(w, err)
		return
	}
	v := h.withDomains(r.Context(), h.view(p, last), p.ServerID)
	httpx.WriteJSON(w, http.StatusOK, projectDetailView{projectView: v, Deployments: deps, Pending: pending})
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var in UpdateInput
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	p, err := h.svc.Update(r.Context(), r.PathValue("id"), in)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, h.view(p, nil))
}

func (h *Handler) getEnv(w http.ResponseWriter, r *http.Request) {
	vars, err := h.svc.GetEnv(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string][]EnvVar{"vars": vars})
}

func (h *Handler) setEnv(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Vars []EnvVar `json:"vars"`
	}
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	vars, err := h.svc.SetEnv(r.Context(), r.PathValue("id"), in.Vars)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string][]EnvVar{"vars": vars})
}

// ---------- Dominios ----------

func (h *Handler) listDomains(w http.ResponseWriter, r *http.Request) {
	ds, err := h.svc.Domains(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ds)
}

func (h *Handler) addDomain(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Hostname string        `json:"hostname"`
		TLSMode  proxy.TLSMode `json:"tlsMode"`
	}
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	d, err := h.svc.AddDomain(r.Context(), r.PathValue("id"), in.Hostname, in.TLSMode)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, d)
}

func (h *Handler) removeDomain(w http.ResponseWriter, r *http.Request) {
	warning, err := h.svc.RemoveDomain(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"warning": warning})
}

func (h *Handler) checkDomain(w http.ResponseWriter, r *http.Request) {
	c, err := h.svc.CheckDomain(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, c)
}

func (h *Handler) getProxy(w http.ResponseWriter, r *http.Request) {
	info, err := h.svc.ProxyInfo(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, info)
}

func (h *Handler) setProxy(w http.ResponseWriter, r *http.Request) {
	var in proxy.Ports
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := h.svc.SetProxyPorts(r.Context(), r.PathValue("id"), in)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, info)
}

func (h *Handler) getAutoDeploy(w http.ResponseWriter, r *http.Request) {
	info, err := h.svc.AutoDeployInfo(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, info)
}

func (h *Handler) setAutoDeploy(w http.ResponseWriter, r *http.Request) {
	var in AutoDeployInput
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := h.svc.SetAutoDeploy(r.Context(), r.PathValue("id"), in)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, info)
}

func (h *Handler) rotateSecret(w http.ResponseWriter, r *http.Request) {
	info, err := h.svc.RotateSecret(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, info)
}

const maxHookBody = 10 << 20

func (h *Handler) gitHook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxHookBody))
	if err != nil {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "cuerpo demasiado grande")
		return
	}
	res := h.svc.HandleHook(r.Context(), r.PathValue("id"), r.Header, body)
	httpx.WriteJSON(w, res.Status, res)
}

func (h *Handler) restart(w http.ResponseWriter, r *http.Request) {
	d, err := h.svc.Restart(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, d)
}

func (h *Handler) redeploy(w http.ResponseWriter, r *http.Request) {
	d, err := h.svc.Redeploy(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, d)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	warning, err := h.svc.Delete(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"warning": warning})
}

func (h *Handler) deploy(w http.ResponseWriter, r *http.Request) {
	d, err := h.svc.Deploy(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, d)
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	p, err := h.svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	st, err := h.svc.ContainerStatus(r.Context(), p)
	if err != nil {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"state": "unknown", "error": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, st)
}

// containerLogs transmite los logs del contenedor por SSE hasta que el cliente se desconecte.
func (h *Handler) containerLogs(w http.ResponseWriter, r *http.Request) {
	p, err := h.svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	tail := 200
	if v, err := strconv.Atoi(r.URL.Query().Get("tail")); err == nil && v >= 0 && v <= 5000 {
		tail = v
	}

	sse := httpx.NewSSE(w)
	defer sse.Close()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go keepAlive(ctx, sse)

	lw := httpx.NewLineWriter(func(line string) {
		if sse.Send("", line) != nil {
			cancel() // el cliente se fue
		}
	})
	err = h.svc.StreamLogs(ctx, p, tail, lw)
	lw.Flush()
	if ctx.Err() == nil {
		msg := "El contenedor terminó o no existe"
		if err != nil {
			msg = err.Error()
		}
		_ = sse.Send("end", msg)
	}
}

// ---------- Deploys ----------

func (h *Handler) getDeployment(w http.ResponseWriter, r *http.Request) {
	d, err := h.svc.GetDeployment(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, deploymentDetailView{Deployment: d, Log: d.Log, Live: h.svc.LiveLog(d.ID) != nil})
}

func (h *Handler) cancelDeployment(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Cancel(r.PathValue("id")); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deploymentLogs transmite el log de un deploy por SSE: si está en curso, en vivo;
// si ya terminó, el log guardado. Siempre termina con un evento "end" con el estado.
func (h *Handler) deploymentLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	live := h.svc.LiveLog(id)
	if live == nil {
		d, err := h.svc.GetDeployment(r.Context(), id)
		if err != nil {
			h.fail(w, err)
			return
		}
		sse := httpx.NewSSE(w)
		defer sse.Close()
		if d.Log != "" {
			for line := range strings.SplitSeq(d.Log, "\n") {
				if sse.Send("", line) != nil {
					return
				}
			}
		}
		_ = sse.Send("end", string(d.Status))
		return
	}

	sse := httpx.NewSSE(w)
	defer sse.Close()
	ping := time.NewTicker(ssePingInterval)
	defer ping.Stop()

	cursor := 0
	for {
		lines, next, done, changed := live.Since(cursor)
		for _, line := range lines {
			if sse.Send("", line) != nil {
				return
			}
		}
		cursor = next
		if done {
			break
		}
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			if sse.Ping() != nil {
				return
			}
		case <-changed:
		}
	}

	// El estado final se guarda justo después de cerrar el log; damos un margen breve.
	status := "finished"
	for range 20 {
		if d, err := h.svc.store.GetDeployment(r.Context(), id); err == nil && isFinal(d.Status) {
			status = string(d.Status)
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = sse.Send("end", status)
}

func isFinal(s DeploymentStatus) bool {
	return s == StatusSucceeded || s == StatusFailed || s == StatusCanceled
}

func keepAlive(ctx context.Context, sse *httpx.SSE) {
	t := time.NewTicker(ssePingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if sse.Ping() != nil {
				return
			}
		}
	}
}

func (h *Handler) fail(w http.ResponseWriter, err error) {
	var (
		validation *ValidationError
		repoErr    *RepoError
		portErr    *PortInUseError
		proxyErr   *ProxyError
	)
	switch {
	case errors.As(err, &validation):
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
	case errors.As(err, &repoErr), errors.As(err, &proxyErr):
		httpx.WriteError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrDeploymentNotFound), errors.Is(err, ErrDomainNotFound):
		httpx.WriteError(w, http.StatusNotFound, err.Error())
	case errors.As(err, &portErr), errors.Is(err, ErrDuplicateName),
		errors.Is(err, ErrDeployInProgress), errors.Is(err, ErrNotRunning), errors.Is(err, ErrTooLateToCancel),
		errors.Is(err, ErrNothingToRestore), errors.Is(err, ErrDuplicateDomain):
		httpx.WriteError(w, http.StatusConflict, err.Error())
	default:
		slog.Error("projects: error interno", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "error interno")
	}
}
