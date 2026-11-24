package projects

import (
	"context"
	"net/http"
	"time"

	"gestionador/internal/httpx"
	"gestionador/internal/metrics"
)

// Expected es un contenedor que debería estar corriendo (el proyecto tuvo un deploy exitoso).
type Expected struct {
	ProjectID string
	Name      string
	ServerID  string
	Container string
}

// ExpectedContainers lista los contenedores de apps que deberían estar corriendo en un servidor.
func (s *Service) ExpectedContainers(ctx context.Context, serverID string) ([]Expected, error) {
	rows, err := s.store.db.QueryContext(ctx, `SELECT p.id, p.name, p.slug FROM projects p
		WHERE p.server_id = $1 AND EXISTS (SELECT 1 FROM deployments d WHERE d.project_id = p.id AND d.status = 'succeeded')`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Expected
	for rows.Next() {
		var e Expected
		var slug string
		if err := rows.Scan(&e.ProjectID, &e.Name, &slug); err != nil {
			return nil, err
		}
		e.ServerID, e.Container = serverID, Project{Slug: slug}.containerName()
		out = append(out, e)
	}
	return out, rows.Err()
}

// OnDeployFinished registra una función que recibe cada deploy terminado (ej. alertas).
func (s *Service) OnDeployFinished(fn func(p Project, d Deployment)) {
	s.mu.Lock()
	s.deployHooks = append(s.deployHooks, fn)
	s.mu.Unlock()
}

func (s *Service) notifyDeploy(p Project, d Deployment) {
	s.mu.Lock()
	hooks := append([]func(Project, Deployment){}, s.deployHooks...)
	s.mu.Unlock()
	for _, fn := range hooks {
		fn(p, d)
	}
}

// SetRecorder conecta el historial de métricas (consumo de cada contenedor).
func (s *Service) SetRecorder(r *metrics.Recorder) { s.recorder = r }

const maxChartPoints = 300

func (h *Handler) metrics(w http.ResponseWriter, r *http.Request) {
	rng, ok := metrics.Ranges[r.URL.Query().Get("range")]
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, "rango inválido: usa 1h, 6h, 24h, 7d o 30d")
		return
	}
	p, err := h.svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	if h.svc.recorder == nil {
		httpx.WriteJSON(w, http.StatusOK, []metrics.Point{})
		return
	}
	pts, err := h.svc.recorder.Query(r.Context(), metrics.ContainerSeries(p.ServerID, p.containerName()), rng, time.Now(), maxChartPoints)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, pts)
}
