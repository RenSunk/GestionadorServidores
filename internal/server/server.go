// Package server arma el router HTTP: la API bajo /api y el frontend embebido en el resto.
package server

import (
	"log/slog"
	"net/http"
	"time"

	"gestionador/internal/database"
	"gestionador/internal/httpx"
	"gestionador/web"
)

// Module es un módulo de la API (servidores, proyectos...) que registra sus rutas.
type Module interface {
	Register(mux *http.ServeMux)
}

type Deps struct {
	DB      *database.DB
	Version string
	Modules []Module
}

type Server struct {
	deps Deps
}

func New(deps Deps) *Server {
	return &Server{deps: deps}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// --- API ---
	mux.HandleFunc("GET /api/health", s.handleHealth)
	for _, m := range s.deps.Modules {
		m.Register(mux)
	}
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteError(w, http.StatusNotFound, "ruta no encontrada")
	})

	// --- Frontend (SPA) ---
	mux.Handle("/", web.Handler())

	return recoverPanics(logRequests(mux))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	status, code := "ok", http.StatusOK
	if err := s.deps.DB.PingContext(r.Context()); err != nil {
		slog.Error("health: base de datos no responde", "err", err)
		status, code = "degraded", http.StatusServiceUnavailable
	}
	httpx.WriteJSON(w, code, map[string]any{
		"status":   status,
		"version":  s.deps.Version,
		"database": s.deps.DB.Dialect,
	})
}

// --- Middlewares ---

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Unwrap permite que http.ResponseController llegue al writer original (flush, websockets).
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		slog.Debug("http", "method", r.Method, "path", r.URL.Path, "status", rec.status, "dur", time.Since(start))
	})
}

func recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				slog.Error("panic en handler", "panic", v, "path", r.URL.Path)
				httpx.WriteError(w, http.StatusInternalServerError, "error interno")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
