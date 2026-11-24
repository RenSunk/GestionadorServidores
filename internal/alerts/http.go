package alerts

import (
	"errors"
	"log/slog"
	"net/http"

	"gestionador/internal/auth"
	"gestionador/internal/httpx"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/alerts", h.list)
	mux.HandleFunc("GET /api/alert-settings", h.getSettings)
	mux.HandleFunc("PUT /api/alert-settings", h.putSettings)
	mux.HandleFunc("POST /api/alert-settings/test", h.test)
}

// list: ?state=open para solo las activas. Se filtran por el alcance del usuario (equipos).
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.List(r.Context(), r.URL.Query().Get("state") == "open", 200)
	if err != nil {
		fail(w, err)
		return
	}
	scope := auth.ScopeFrom(r.Context())
	out := make([]Alert, 0, len(list))
	for _, a := range list {
		if (a.TargetType == "server" && scope.Server(a.TargetID)) || (a.TargetType == "project" && scope.Project(a.TargetID)) {
			out = append(out, a)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) getSettings(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, h.svc.Settings())
}

func (h *Handler) putSettings(w http.ResponseWriter, r *http.Request) {
	var in Settings
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg, err := h.svc.SaveSettings(r.Context(), in)
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, cfg)
}

func (h *Handler) test(w http.ResponseWriter, r *http.Request) {
	var in struct {
		WebhookURL string `json:"webhookUrl"`
	}
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.Test(r.Context(), in.WebhookURL); err != nil {
		var v *ValidationError
		if errors.As(err, &v) {
			httpx.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		httpx.WriteError(w, http.StatusBadGateway, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func fail(w http.ResponseWriter, err error) {
	var v *ValidationError
	if errors.As(err, &v) {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	slog.Error("alerts: error interno", "err", err)
	httpx.WriteError(w, http.StatusInternalServerError, "error interno")
}
