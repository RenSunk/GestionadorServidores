package auth

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"gestionador/internal/httpx"
)

const cookieName = "gestionador_session"

// ---------- Permisos por ruta ----------

// public: no requieren sesión (se autentican de otra forma o no exponen nada).
var public = map[string]bool{
	"GET /api/health":                  true,
	"GET /api/auth/status":             true,
	"POST /api/auth/setup":             true,
	"POST /api/auth/login":             true,
	"POST /api/auth/logout":            true,
	"GET /api/invites/accept/{token}":  true,
	"POST /api/invites/accept/{token}": true,
	"POST /hooks/git/{id}":             true, // firma del secreto del webhook
}

// developer: operar proyectos. Todo lo que no figura aquí y no es GET exige administrador.
var developer = map[string]bool{
	"POST /api/projects/detect":                 true,
	"POST /api/dockerfile-preview":              true,
	"POST /api/projects":                        true,
	"PUT /api/projects/{id}":                    true,
	"GET /api/projects/{id}/env":                true, // valores secretos
	"PUT /api/projects/{id}/env":                true,
	"POST /api/projects/{id}/deploy":            true,
	"POST /api/projects/{id}/restart":           true,
	"POST /api/projects/{id}/domains":           true,
	"DELETE /api/domains/{id}":                  true,
	"POST /api/domains/{id}/check":              true,
	"GET /api/projects/{id}/autodeploy":         true, // incluye el secreto del webhook
	"PUT /api/projects/{id}/autodeploy":         true,
	"POST /api/projects/{id}/autodeploy/secret": true,
	"POST /api/deployments/{id}/cancel":         true,
	"POST /api/deployments/{id}/redeploy":       true,
	"POST /api/servers/{id}/check":              true, // solo vuelve a leer el estado
}

// adminGET: lecturas reservadas a administradores.
var adminGET = map[string]bool{
	"GET /api/users":   true,
	"GET /api/invites": true,
	"GET /api/teams":   true,
}

// Required devuelve el rol mínimo para un patrón del router, o "" si es público.
func Required(pattern string) Role {
	method, path, ok := strings.Cut(pattern, " ")
	if !ok { // patrón sin método, ej. "/api/"
		method, path = "", pattern
	}
	switch {
	case public[pattern], !strings.HasPrefix(path, "/api/") && path != "/api":
		return ""
	case developer[pattern]:
		return RoleDeveloper
	case adminGET[pattern]:
		return RoleAdmin
	case method == http.MethodGet || method == "":
		return RoleViewer
	}
	return RoleAdmin
}

// Middleware exige sesión y el rol adecuado según la ruta que atenderá mux.
func (s *Service) Middleware(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, pattern := mux.Handler(r)
		need := Required(pattern)
		if need == "" {
			mux.ServeHTTP(w, r)
			return
		}
		if !sameOrigin(r) {
			httpx.WriteError(w, http.StatusForbidden, "petición de otro sitio rechazada")
			return
		}
		var token string
		if c, err := r.Cookie(cookieName); err == nil {
			token = c.Value
		}
		u, err := s.Authenticate(r.Context(), token)
		if err != nil {
			if !errors.Is(err, ErrUserNotFound) {
				slog.Error("autenticar", "err", err)
			}
			httpx.WriteError(w, http.StatusUnauthorized, "inicia sesión para continuar")
			return
		}
		if !u.Role.Can(need) {
			httpx.WriteError(w, http.StatusForbidden, "tu rol no permite esta acción")
			return
		}
		scope, err := s.scopeFor(r, u, pattern)
		if err != nil {
			slog.Error("cargar equipos", "err", err)
			httpx.WriteError(w, http.StatusInternalServerError, "error interno")
			return
		}
		if scope == nil {
			// Fuera del alcance del usuario: se responde como si no existiera.
			httpx.WriteError(w, http.StatusNotFound, "no encontrado")
			return
		}
		ctx := WithScope(WithUser(r.Context(), u), *scope)
		mux.ServeHTTP(w, r.WithContext(ctx))
	})
}

// scopeFor carga el alcance del usuario y comprueba el recurso de la ruta.
// Devuelve nil si el recurso existe pero el usuario no puede verlo.
func (s *Service) scopeFor(r *http.Request, u User, pattern string) (*Scope, error) {
	if u.Role == RoleAdmin || s.scopes == nil {
		full := FullScope()
		return &full, nil
	}
	scope, err := s.scopes.LoadScope(r.Context(), u.ID)
	if err != nil {
		return nil, err
	}
	kind, id := resourceOf(pattern, r.URL.Path)
	ok := true
	switch kind {
	case "projects":
		ok = scope.Project(id)
	case "servers":
		ok = scope.Server(id)
	case "deployments", "domains":
		pid, err := s.scopes.ProjectOf(r.Context(), kind, id)
		if err != nil {
			return nil, err
		}
		ok = pid == "" || scope.Project(pid)
	}
	if !ok {
		return nil, nil
	}
	return &scope, nil
}

// sameOrigin rechaza escrituras iniciadas desde otro sitio (CSRF). La cookie además es SameSite=Lax.
func sameOrigin(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		return site == "same-origin" || site == "none"
	}
	if o := r.Header.Get("Origin"); o != "" {
		_, host, _ := strings.Cut(o, "://")
		return host == r.Host
	}
	return true // clientes que no son navegadores (curl): no envían la cookie por sí solos
}

// ---------- Handlers ----------

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/auth/status", h.status)
	mux.HandleFunc("POST /api/auth/setup", h.setup)
	mux.HandleFunc("POST /api/auth/login", h.login)
	mux.HandleFunc("POST /api/auth/logout", h.logout)
	mux.HandleFunc("GET /api/auth/me", h.me)
	mux.HandleFunc("PUT /api/auth/me", h.updateMe)
	mux.HandleFunc("POST /api/auth/password", h.changePassword)

	mux.HandleFunc("GET /api/users", h.listUsers)
	mux.HandleFunc("PATCH /api/users/{id}", h.updateUser)
	mux.HandleFunc("DELETE /api/users/{id}", h.deleteUser)

	mux.HandleFunc("GET /api/invites", h.listInvites)
	mux.HandleFunc("POST /api/invites", h.createInvite)
	mux.HandleFunc("DELETE /api/invites/{id}", h.revokeInvite)
	mux.HandleFunc("GET /api/invites/accept/{token}", h.getInvite)
	mux.HandleFunc("POST /api/invites/accept/{token}", h.acceptInvite)
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{
		"setupRequired": h.svc.SetupRequired(),
		"needsCode":     !isLocal(r), // desde otra máquina hace falta el código del log
	})
}

func (h *Handler) setup(w http.ResponseWriter, r *http.Request) {
	var in SetupInput
	if !decode(w, r, &in) {
		return
	}
	u, err := h.svc.Setup(r.Context(), in, isLocal(r))
	if err != nil {
		fail(w, err)
		return
	}
	token, err := h.svc.openSession(r.Context(), u, client(r))
	if err != nil {
		fail(w, err)
		return
	}
	setCookie(w, r, token)
	httpx.WriteJSON(w, http.StatusCreated, u)
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	u, token, err := h.svc.Login(r.Context(), in.Email, in.Password, client(r))
	if err != nil {
		fail(w, err)
		return
	}
	setCookie(w, r, token)
	httpx.WriteJSON(w, http.StatusOK, u)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		_ = h.svc.Logout(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	httpx.WriteJSON(w, http.StatusOK, u)
}

func (h *Handler) updateMe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &in) {
		return
	}
	u, _ := UserFrom(r.Context())
	u, err := h.svc.UpdateProfile(r.Context(), u, in.Name)
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, u)
}

func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if !decode(w, r, &in) {
		return
	}
	u, _ := UserFrom(r.Context())
	c, _ := r.Cookie(cookieName)
	if err := h.svc.ChangePassword(r.Context(), u, in.Current, in.New, c.Value); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.svc.ListUsers(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, users)
}

func (h *Handler) updateUser(w http.ResponseWriter, r *http.Request) {
	var in UserPatch
	if !decode(w, r, &in) {
		return
	}
	actor, _ := UserFrom(r.Context())
	u, err := h.svc.UpdateUser(r.Context(), actor, r.PathValue("id"), in)
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, u)
}

func (h *Handler) deleteUser(w http.ResponseWriter, r *http.Request) {
	actor, _ := UserFrom(r.Context())
	if err := h.svc.DeleteUser(r.Context(), actor, r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listInvites(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.ListInvites(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, list)
}

func (h *Handler) createInvite(w http.ResponseWriter, r *http.Request) {
	var in InviteInput
	if !decode(w, r, &in) {
		return
	}
	actor, _ := UserFrom(r.Context())
	inv, token, err := h.svc.CreateInvite(r.Context(), actor, in)
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, struct {
		Invite
		Path string `json:"path"` // el enlace completo lo arma el frontend con su origen
	}{inv, "/invite/" + token})
}

func (h *Handler) revokeInvite(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.RevokeInvite(r.Context(), r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) getInvite(w http.ResponseWriter, r *http.Request) {
	inv, err := h.svc.GetInvite(r.Context(), r.PathValue("token"))
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"email": inv.Email, "role": inv.Role})
}

func (h *Handler) acceptInvite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	u, token, err := h.svc.AcceptInvite(r.Context(), r.PathValue("token"), in.Name, in.Password, client(r))
	if err != nil {
		fail(w, err)
		return
	}
	setCookie(w, r, token)
	httpx.WriteJSON(w, http.StatusCreated, u)
}

// ---------- utilidades ----------

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := httpx.DecodeJSON(w, r, dst); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return false
	}
	return true
}

func fail(w http.ResponseWriter, err error) {
	var v *ValidationError
	switch {
	case errors.As(err, &v):
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrInvalidCredentials), errors.Is(err, ErrWrongPassword), errors.Is(err, ErrBadSetupCode):
		httpx.WriteError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, ErrTooManyAttempts):
		httpx.WriteError(w, http.StatusTooManyRequests, err.Error())
	case errors.Is(err, ErrUserNotFound), errors.Is(err, ErrInviteNotFound):
		httpx.WriteError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrEmailTaken), errors.Is(err, ErrLastAdmin), errors.Is(err, ErrSelf), errors.Is(err, ErrSetupDone):
		httpx.WriteError(w, http.StatusConflict, err.Error())
	default:
		slog.Error("auth: error interno", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "error interno")
	}
}

func setCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: token, Path: "/",
		MaxAge:   int(sessionTTL / time.Second),
		HttpOnly: true,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteLaxMode,
	})
}

func client(r *http.Request) Client {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return Client{IP: host, UserAgent: r.UserAgent()}
}

// isLocal: la petición viene del mismo equipo y no a través de un proxy.
func isLocal(r *http.Request) bool {
	for _, h := range []string{"X-Forwarded-For", "X-Real-Ip", "Forwarded"} {
		if r.Header.Get(h) != "" {
			return false
		}
	}
	ip := net.ParseIP(client(r).IP)
	return ip != nil && ip.IsLoopback()
}
