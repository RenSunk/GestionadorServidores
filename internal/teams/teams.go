// Package teams agrupa usuarios y limita qué proyectos y servidores ven.
// El rol (auth) define qué puede hacer cada usuario; los equipos, sobre qué.
package teams

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"gestionador/internal/auth"
	"gestionador/internal/database"
	"gestionador/internal/httpx"
)

var (
	ErrNotFound      = errors.New("equipo no encontrado")
	ErrDuplicateName = errors.New("ya existe un equipo con ese nombre")
)

type Team struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	CreatedAt  time.Time `json:"createdAt"`
	UserIDs    []string  `json:"userIds"`
	ProjectIDs []string  `json:"projectIds"`
	ServerIDs  []string  `json:"serverIds"`
}

type Input struct {
	Name       string   `json:"name"`
	UserIDs    []string `json:"userIds"`
	ProjectIDs []string `json:"projectIds"`
	ServerIDs  []string `json:"serverIds"`
}

type Service struct{ db *database.DB }

func NewService(db *database.DB) *Service { return &Service{db: db} }

// ---------- auth.ScopeSource ----------

func (s *Service) LoadScope(ctx context.Context, userID string) (auth.Scope, error) {
	q := func(query string, args ...any) ([]string, error) { return s.strings(ctx, query, args...) }
	rp, err := q(`SELECT DISTINCT project_id FROM team_projects`)
	if err != nil {
		return auth.Scope{}, err
	}
	mp, err := q(`SELECT DISTINCT tp.project_id FROM team_projects tp
		JOIN team_members tm ON tm.team_id = tp.team_id WHERE tm.user_id = $1`, userID)
	if err != nil {
		return auth.Scope{}, err
	}
	rs, err := q(`SELECT DISTINCT server_id FROM team_servers`)
	if err != nil {
		return auth.Scope{}, err
	}
	ms, err := q(`SELECT DISTINCT ts.server_id FROM team_servers ts
		JOIN team_members tm ON tm.team_id = ts.team_id WHERE tm.user_id = $1`, userID)
	if err != nil {
		return auth.Scope{}, err
	}
	return auth.NewScope(rp, mp, rs, ms), nil
}

func (s *Service) ProjectOf(ctx context.Context, kind, id string) (string, error) {
	table := map[string]string{"deployments": "deployments", "domains": "domains"}[kind]
	if table == "" {
		return "", nil
	}
	var pid string
	err := s.db.QueryRowContext(ctx, `SELECT project_id FROM `+table+` WHERE id = $1`, id).Scan(&pid)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return pid, err
}

// ---------- CRUD ----------

func (s *Service) List(ctx context.Context) ([]Team, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, created_at FROM teams ORDER BY name`)
	if err != nil {
		return nil, err
	}
	var out []Team
	for rows.Next() {
		var t Team
		if err := rows.Scan(&t.ID, &t.Name, &t.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if err := s.fill(ctx, &out[i]); err != nil {
			return nil, err
		}
	}
	if out == nil {
		out = []Team{}
	}
	return out, nil
}

func (s *Service) fill(ctx context.Context, t *Team) error {
	var err error
	if t.UserIDs, err = s.strings(ctx, `SELECT user_id FROM team_members WHERE team_id = $1 ORDER BY user_id`, t.ID); err != nil {
		return err
	}
	if t.ProjectIDs, err = s.strings(ctx, `SELECT project_id FROM team_projects WHERE team_id = $1 ORDER BY project_id`, t.ID); err != nil {
		return err
	}
	t.ServerIDs, err = s.strings(ctx, `SELECT server_id FROM team_servers WHERE team_id = $1 ORDER BY server_id`, t.ID)
	return err
}

func (s *Service) Get(ctx context.Context, id string) (Team, error) {
	var t Team
	err := s.db.QueryRowContext(ctx, `SELECT id, name, created_at FROM teams WHERE id = $1`, id).Scan(&t.ID, &t.Name, &t.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	if err != nil {
		return t, err
	}
	return t, s.fill(ctx, &t)
}

func (s *Service) Create(ctx context.Context, in Input) (Team, error) {
	return s.save(ctx, "", in)
}

func (s *Service) Update(ctx context.Context, id string, in Input) (Team, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return Team{}, err
	}
	return s.save(ctx, id, in)
}

// save crea o reemplaza el equipo completo (nombre, miembros y recursos) en una transacción.
func (s *Service) save(ctx context.Context, id string, in Input) (Team, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 60 {
		return Team{}, &httpError{http.StatusBadRequest, "el nombre del equipo es obligatorio (máximo 60 caracteres)"}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Team{}, err
	}
	defer tx.Rollback()

	if id == "" {
		id = uuid.Must(uuid.NewV7()).String()
		_, err = tx.ExecContext(ctx, `INSERT INTO teams (id, name, created_at) VALUES ($1, $2, $3)`, id, name, time.Now().UTC())
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE teams SET name = $2 WHERE id = $1`, id, name)
	}
	if err != nil {
		if s := strings.ToLower(err.Error()); strings.Contains(s, "unique") || strings.Contains(s, "duplicate key") {
			return Team{}, ErrDuplicateName
		}
		return Team{}, err
	}
	for _, link := range []struct {
		table, column string
		ids           []string
	}{
		{"team_members", "user_id", in.UserIDs},
		{"team_projects", "project_id", in.ProjectIDs},
		{"team_servers", "server_id", in.ServerIDs},
	} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+link.table+` WHERE team_id = $1`, id); err != nil {
			return Team{}, err
		}
		ids := slices.Compact(slices.Sorted(slices.Values(link.ids)))
		for _, ref := range ids {
			if _, err := tx.ExecContext(ctx, `INSERT INTO `+link.table+` (team_id, `+link.column+`) VALUES ($1, $2)`, id, ref); err != nil {
				// Clave foránea: el usuario, proyecto o servidor no existe.
				return Team{}, &httpError{http.StatusBadRequest, "algún usuario, proyecto o servidor elegido ya no existe; recarga la página"}
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return Team{}, err
	}
	return s.Get(ctx, id)
}

func (s *Service) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM teams WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) strings(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ---------- HTTP (solo administradores: ver auth.Required) ----------

type httpError struct {
	status int
	msg    string
}

func (e *httpError) Error() string { return e.msg }

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/teams", h.list)
	mux.HandleFunc("POST /api/teams", h.create)
	mux.HandleFunc("PUT /api/teams/{id}", h.update)
	mux.HandleFunc("DELETE /api/teams/{id}", h.delete)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.List(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, list)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in Input
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	t, err := h.svc.Create(r.Context(), in)
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, t)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var in Input
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	t, err := h.svc.Update(r.Context(), r.PathValue("id"), in)
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, t)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Delete(r.Context(), r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func fail(w http.ResponseWriter, err error) {
	var he *httpError
	switch {
	case errors.As(err, &he):
		httpx.WriteError(w, he.status, he.msg)
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrDuplicateName):
		httpx.WriteError(w, http.StatusConflict, err.Error())
	default:
		slog.Error("teams: error interno", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "error interno")
	}
}
