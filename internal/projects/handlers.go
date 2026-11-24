package projects

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gestionador/internal/httpx"
)

const ssePingInterval = 20 * time.Second

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/projects", h.list)
	mux.HandleFunc("POST /api/projects", h.create)
	mux.HandleFunc("GET /api/projects/{id}", h.get)
	mux.HandleFunc("DELETE /api/projects/{id}", h.delete)
	mux.HandleFunc("POST /api/projects/{id}/deploy", h.deploy)
	mux.HandleFunc("GET /api/projects/{id}/status", h.status)
	mux.HandleFunc("GET /api/projects/{id}/logs", h.containerLogs)

	mux.HandleFunc("GET /api/deployments/{id}", h.getDeployment)
	mux.HandleFunc("POST /api/deployments/{id}/cancel", h.cancelDeployment)
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
	URL                string      `json:"url"`
	ContainerName      string      `json:"containerName"`
	CreatedAt          time.Time   `json:"createdAt"`
	LastDeployment     *Deployment `json:"lastDeployment"`
	ActiveDeploymentID string      `json:"activeDeploymentId"`
}

type projectDetailView struct {
	projectView
	Deployments []Deployment `json:"deployments"`
}

type deploymentDetailView struct {
	Deployment
	Log  string `json:"log"`
	Live bool   `json:"live"`
}

func (h *Handler) view(p Project, last *Deployment) projectView {
	v := projectView{
		ID: p.ID, Name: p.Name, Slug: p.Slug, ServerID: p.ServerID, ServerName: p.ServerName,
		RepoURL: p.RepoURL, Branch: p.Branch, GitUsername: p.GitUsername, HasToken: p.GitTokenEnc != "",
		RootDir: p.RootDir, Dockerfile: p.Dockerfile, ContainerPort: p.ContainerPort, HostPort: p.HostPort,
		ContainerName: p.containerName(), CreatedAt: p.CreatedAt, LastDeployment: last,
		ActiveDeploymentID: h.svc.ActiveDeploymentID(p.ID),
	}
	if p.HostPort != nil {
		v.URL = fmt.Sprintf("http://%s:%d", hostOnly(p.ServerHost), *p.HostPort)
	}
	return v
}

// ---------- Proyectos ----------

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	list, latest, err := h.svc.List(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	out := make([]projectView, len(list))
	for i, p := range list {
		var last *Deployment
		if d, ok := latest[p.ID]; ok {
			last = &d
		}
		out[i] = h.view(p, last)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in CreateInput
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
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
	httpx.WriteJSON(w, http.StatusOK, projectDetailView{projectView: h.view(p, last), Deployments: deps})
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
	)
	switch {
	case errors.As(err, &validation):
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
	case errors.As(err, &repoErr):
		httpx.WriteError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrDeploymentNotFound):
		httpx.WriteError(w, http.StatusNotFound, err.Error())
	case errors.As(err, &portErr), errors.Is(err, ErrDuplicateName),
		errors.Is(err, ErrDeployInProgress), errors.Is(err, ErrNotRunning), errors.Is(err, ErrTooLateToCancel):
		httpx.WriteError(w, http.StatusConflict, err.Error())
	default:
		slog.Error("projects: error interno", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "error interno")
	}
}
