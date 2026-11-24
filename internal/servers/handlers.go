package servers

import (
	"errors"
	"gestionador/internal/auth"
	"gestionador/internal/metrics"
	"log/slog"
	"net/http"
	"time"

	"gestionador/internal/httpx"
	"gestionador/internal/sshx"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/ssh-key", h.instanceKey)
	mux.HandleFunc("GET /api/servers", h.list)
	mux.HandleFunc("POST /api/servers", h.create)
	mux.HandleFunc("GET /api/servers/{id}", h.get)
	mux.HandleFunc("POST /api/servers/{id}/check", h.check)
	mux.HandleFunc("DELETE /api/servers/{id}", h.delete)
	mux.HandleFunc("GET /api/servers/{id}/metrics", h.metrics)
}

// --- Vistas JSON (nunca incluyen credenciales) ---

type serverView struct {
	ID                 string     `json:"id"`
	Name               string     `json:"name"`
	Host               string     `json:"host"`
	Port               int        `json:"port"`
	Username           string     `json:"username"`
	AuthMethod         AuthMethod `json:"authMethod"`
	HostKeyFingerprint string     `json:"hostKeyFingerprint"`
	Status             Status     `json:"status"`
	LastError          string     `json:"lastError"`
	LastSeenAt         *time.Time `json:"lastSeenAt"`
	CreatedAt          time.Time  `json:"createdAt"`
	System             SystemInfo `json:"system"`
	Metrics            *Metrics   `json:"metrics"`
}

type historyPoint struct {
	T             time.Time `json:"t"`
	CPUPercent    float64   `json:"cpuPercent"`
	MemoryPercent float64   `json:"memoryPercent"`
}

type serverDetailView struct {
	serverView
	History []historyPoint `json:"history"`
}

func (h *Handler) view(s Server) serverView {
	latest, _ := h.svc.Metrics(s.ID)
	return serverView{
		ID: s.ID, Name: s.Name, Host: s.Host, Port: s.Port, Username: s.Username,
		AuthMethod:         s.AuthMethod,
		HostKeyFingerprint: sshx.Fingerprint(s.HostKey),
		Status:             s.Status, LastError: s.LastError,
		LastSeenAt: s.LastSeenAt, CreatedAt: s.CreatedAt,
		System:  s.System,
		Metrics: latest,
	}
}

func (h *Handler) detailView(s Server) serverDetailView {
	_, history := h.svc.Metrics(s.ID)
	points := make([]historyPoint, len(history))
	for i, m := range history {
		points[i] = historyPoint{T: m.CollectedAt, CPUPercent: m.CPUPercent, MemoryPercent: percent(m.Memory)}
	}
	return serverDetailView{serverView: h.view(s), History: points}
}

func percent(u Usage) float64 {
	if u.Total == 0 {
		return 0
	}
	return round1(float64(u.Used) / float64(u.Total) * 100)
}

// --- Handlers ---

func (h *Handler) instanceKey(w http.ResponseWriter, _ *http.Request) {
	pub := h.svc.InstancePublicKey()
	httpx.WriteJSON(w, http.StatusOK, map[string]string{
		"publicKey":   pub,
		"fingerprint": sshx.Fingerprint(pub),
	})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.List(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	scope := auth.ScopeFrom(r.Context())
	out := make([]serverView, 0, len(list))
	for _, s := range list {
		if scope.Server(s.ID) {
			out = append(out, h.view(s))
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in CreateInput
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	srv, err := h.svc.Create(r.Context(), in)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, h.detailView(srv))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	srv, err := h.svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, h.detailView(srv))
}

func (h *Handler) check(w http.ResponseWriter, r *http.Request) {
	srv, err := h.svc.Check(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, h.detailView(srv))
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Delete(r.Context(), r.PathValue("id")); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) fail(w http.ResponseWriter, err error) {
	var (
		validation *ValidationError
		conn       *ConnectionError
	)
	switch {
	case errors.As(err, &validation):
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
	case errors.As(err, &conn):
		httpx.WriteError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrDuplicateName), errors.Is(err, ErrHasProjects):
		httpx.WriteError(w, http.StatusConflict, err.Error())
	default:
		slog.Error("servers: error interno", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "error interno")
	}
}

func (h *Handler) metrics(w http.ResponseWriter, r *http.Request) {
	rng, ok := metrics.Ranges[r.URL.Query().Get("range")]
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, "rango inválido: usa 1h, 6h, 24h, 7d o 30d")
		return
	}
	if _, err := h.svc.Get(r.Context(), r.PathValue("id")); err != nil {
		h.fail(w, err)
		return
	}
	if h.svc.recorder == nil {
		httpx.WriteJSON(w, http.StatusOK, []metrics.Point{})
		return
	}
	pts, err := h.svc.recorder.Query(r.Context(), metrics.ServerSeries(r.PathValue("id")), rng, time.Now(), 300)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, pts)
}
