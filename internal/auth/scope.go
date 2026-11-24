package auth

import (
	"context"
	"strings"
)

// Scope dice qué proyectos y servidores puede ver el usuario de una petición.
// Regla: un recurso sin equipos es visible para todos; uno asignado a equipos,
// solo para sus miembros. Los administradores ven todo.
type Scope struct {
	all                bool
	restrictedProjects map[string]bool
	memberProjects     map[string]bool
	restrictedServers  map[string]bool
	memberServers      map[string]bool
}

// FullScope ve todo (administradores y tareas internas).
func FullScope() Scope { return Scope{all: true} }

// NewScope arma el alcance de un usuario: recursos restringidos y los que ve por sus equipos.
func NewScope(restrictedProjects, memberProjects, restrictedServers, memberServers []string) Scope {
	set := func(ids []string) map[string]bool {
		m := make(map[string]bool, len(ids))
		for _, id := range ids {
			m[id] = true
		}
		return m
	}
	return Scope{
		restrictedProjects: set(restrictedProjects), memberProjects: set(memberProjects),
		restrictedServers: set(restrictedServers), memberServers: set(memberServers),
	}
}

func (s Scope) All() bool { return s.all }

func (s Scope) Project(id string) bool {
	return s.all || !s.restrictedProjects[id] || s.memberProjects[id]
}

func (s Scope) Server(id string) bool {
	return s.all || !s.restrictedServers[id] || s.memberServers[id]
}

type scopeKey struct{}

func WithScope(ctx context.Context, s Scope) context.Context {
	return context.WithValue(ctx, scopeKey{}, s)
}

// ScopeFrom devuelve el alcance de la petición. Fuera de una petición (deploys
// automáticos, revisión periódica) no hay restricción.
func ScopeFrom(ctx context.Context) Scope {
	if s, ok := ctx.Value(scopeKey{}).(Scope); ok {
		return s
	}
	return FullScope()
}

// ScopeSource carga los alcances desde la base (lo implementa el paquete teams).
type ScopeSource interface {
	LoadScope(ctx context.Context, userID string) (Scope, error)
	// ProjectOf devuelve el proyecto de un deploy o dominio ("" si no existe).
	ProjectOf(ctx context.Context, kind, id string) (string, error)
}

// resourceOf extrae el recurso de la ruta según el patrón: "/api/projects/{id}/env"
// con "/api/projects/abc/env" → ("projects", "abc").
func resourceOf(pattern, path string) (kind, id string) {
	_, pat, ok := strings.Cut(pattern, " ")
	if !ok {
		pat = pattern
	}
	ps, xs := strings.Split(pat, "/"), strings.Split(path, "/")
	if len(ps) < 4 || len(xs) < 4 || ps[1] != "api" || ps[3] != "{id}" {
		return "", ""
	}
	switch ps[2] {
	case "projects", "servers", "deployments", "domains":
		return ps[2], xs[3]
	}
	return "", ""
}
