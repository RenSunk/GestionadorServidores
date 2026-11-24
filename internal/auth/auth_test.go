package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gestionador/internal/database"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(ctx, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	s, err := NewService(ctx, NewStore(db))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRequired(t *testing.T) {
	for pattern, want := range map[string]Role{
		"GET /api/health":                   "",
		"POST /api/auth/login":              "",
		"POST /hooks/git/{id}":              "",
		"/":                                 "",
		"GET /api/projects":                 RoleViewer,
		"GET /api/projects/{id}/env":        RoleDeveloper,
		"GET /api/projects/{id}/autodeploy": RoleDeveloper,
		"POST /api/projects/{id}/deploy":    RoleDeveloper,
		"DELETE /api/projects/{id}":         RoleAdmin,
		"POST /api/servers":                 RoleAdmin,
		"PUT /api/servers/{id}/proxy":       RoleAdmin,
		"GET /api/users":                    RoleAdmin,
		"POST /api/rutas/nuevas":            RoleAdmin, // lo no listado que escribe: solo admin
		"/api/":                             RoleViewer,
	} {
		if got := Required(pattern); got != want {
			t.Errorf("%s: %q, want %q", pattern, got, want)
		}
	}
}

func TestSetupAndLogin(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	code := s.SetupCode()
	if code == "" {
		t.Fatal("sin usuarios debe haber código de instalación")
	}
	in := SetupInput{Name: "Ana", Email: "Ana@Example.com", Password: "contraseña-larga"}
	if _, err := s.Setup(ctx, in, false); !errors.Is(err, ErrBadSetupCode) {
		t.Fatalf("desde fuera sin código: %v", err)
	}
	in.Code = code
	u, err := s.Setup(ctx, in, false)
	if err != nil || u.Email != "ana@example.com" || u.Role != RoleAdmin {
		t.Fatalf("setup: %+v %v", u, err)
	}
	if _, err := s.Setup(ctx, in, true); !errors.Is(err, ErrSetupDone) {
		t.Fatalf("segundo setup: %v", err)
	}

	c := Client{IP: "1.2.3.4"}
	if _, _, err := s.Login(ctx, "ana@example.com", "mal", c); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("contraseña mala: %v", err)
	}
	_, token, err := s.Login(ctx, " ANA@example.com ", "contraseña-larga", c)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.Authenticate(ctx, token); err != nil || got.ID != u.ID {
		t.Fatalf("authenticate: %v", err)
	}
	if _, err := s.Authenticate(ctx, token+"x"); err == nil {
		t.Fatal("token alterado aceptado")
	}
	_ = s.Logout(ctx, token)
	if _, err := s.Authenticate(ctx, token); err == nil {
		t.Fatal("sesión cerrada sigue valiendo")
	}
}

func TestLoginRateLimit(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	if _, err := s.Setup(ctx, SetupInput{Name: "Ana", Email: "a@x.io", Password: "contraseña-larga"}, true); err != nil {
		t.Fatal(err)
	}
	c := Client{IP: "9.9.9.9"}
	for range maxFailures {
		_, _, _ = s.Login(ctx, "a@x.io", "mal", c)
	}
	if _, _, err := s.Login(ctx, "a@x.io", "contraseña-larga", c); !errors.Is(err, ErrTooManyAttempts) {
		t.Fatalf("debería bloquear: %v", err)
	}
	if _, _, err := s.Login(ctx, "a@x.io", "contraseña-larga", Client{IP: "8.8.8.8"}); err != nil {
		t.Fatalf("otra IP no debería estar bloqueada: %v", err)
	}
}

func TestInvitesAndAdminGuards(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	admin, err := s.Setup(ctx, SetupInput{Name: "Ana", Email: "a@x.io", Password: "contraseña-larga"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreateInvite(ctx, admin, InviteInput{Email: "a@x.io", Role: RoleViewer}); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("invitar a un email existente: %v", err)
	}
	_, old, _ := s.CreateInvite(ctx, admin, InviteInput{Email: "dev@x.io", Role: RoleViewer})
	_, token, err := s.CreateInvite(ctx, admin, InviteInput{Email: "dev@x.io", Role: RoleDeveloper})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetInvite(ctx, old); !errors.Is(err, ErrInviteNotFound) {
		t.Fatalf("la invitación anterior debería reemplazarse: %v", err)
	}
	if list, _ := s.ListInvites(ctx); len(list) != 1 {
		t.Fatalf("invitaciones pendientes: %d", len(list))
	}
	dev, sess, err := s.AcceptInvite(ctx, token, "Dev", "otra-contraseña", Client{})
	if err != nil || dev.Role != RoleDeveloper || sess == "" {
		t.Fatalf("aceptar: %+v %v", dev, err)
	}
	if _, _, err := s.AcceptInvite(ctx, token, "Dev", "otra-contraseña", Client{}); !errors.Is(err, ErrInviteNotFound) {
		t.Fatalf("invitación reutilizada: %v", err)
	}

	// No se puede dejar la instancia sin administradores ni tocarse a uno mismo.
	if _, err := s.UpdateUser(ctx, admin, admin.ID, UserPatch{Role: new(RoleViewer)}); !errors.Is(err, ErrSelf) {
		t.Fatalf("auto-degradarse: %v", err)
	}
	promoted, err := s.UpdateUser(ctx, admin, dev.ID, UserPatch{Role: new(RoleAdmin)})
	if err != nil || promoted.Role != RoleAdmin {
		t.Fatal(err)
	}
	if _, err := s.UpdateUser(ctx, promoted, admin.ID, UserPatch{Disabled: new(true)}); err != nil {
		t.Fatalf("deshabilitar con otro admin activo: %v", err)
	}
	if err := s.DeleteUser(ctx, admin, promoted.ID); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("borrar al último admin activo: %v", err)
	}
	// Deshabilitar cierra sus sesiones.
	if _, err := s.UpdateUser(ctx, admin, dev.ID, UserPatch{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, sess); err != nil {
		t.Fatalf("la sesión del dev debería seguir: %v", err)
	}
}

func TestChangePasswordClosesOtherSessions(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	u, _ := s.Setup(ctx, SetupInput{Name: "Ana", Email: "a@x.io", Password: "contraseña-larga"}, true)
	_, t1, _ := s.Login(ctx, "a@x.io", "contraseña-larga", Client{})
	_, t2, _ := s.Login(ctx, "a@x.io", "contraseña-larga", Client{})
	if err := s.ChangePassword(ctx, u, "mal", "nueva-contraseña", t1); !errors.Is(err, ErrWrongPassword) {
		t.Fatal(err)
	}
	if err := s.ChangePassword(ctx, u, "contraseña-larga", "nueva-contraseña", t1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, t1); err != nil {
		t.Fatal("la sesión actual debe seguir")
	}
	if _, err := s.Authenticate(ctx, t2); err == nil {
		t.Fatal("las otras sesiones deben cerrarse")
	}
}

func TestMiddleware(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	if _, err := s.Setup(ctx, SetupInput{Name: "Ana", Email: "a@x.io", Password: "contraseña-larga"}, true); err != nil {
		t.Fatal(err)
	}
	admin, adminTok, _ := s.Login(ctx, "a@x.io", "contraseña-larga", Client{})
	_, tok, _ := s.CreateInvite(ctx, admin, InviteInput{Email: "v@x.io", Role: RoleViewer})
	_, viewerTok, _ := s.AcceptInvite(ctx, tok, "Vera", "contraseña-larga", Client{})

	mux := http.NewServeMux()
	ok := func(w http.ResponseWriter, r *http.Request) {
		u, _ := UserFrom(r.Context())
		w.Write([]byte("ok " + u.Name))
	}
	mux.HandleFunc("GET /api/projects", ok)
	mux.HandleFunc("POST /api/projects/{id}/deploy", ok)
	mux.HandleFunc("GET /api/health", ok)
	h := s.Middleware(mux)

	do := func(method, path, token string, hdr ...string) (int, string) {
		r := httptest.NewRequest(method, path, nil)
		if token != "" {
			r.AddCookie(&http.Cookie{Name: cookieName, Value: token})
		}
		for i := 0; i+1 < len(hdr); i += 2 {
			r.Header.Set(hdr[i], hdr[i+1])
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code, strings.TrimSpace(w.Body.String())
	}
	cases := []struct {
		name, method, path, token string
		hdr                       []string
		want                      int
	}{
		{"público sin sesión", "GET", "/api/health", "", nil, 200},
		{"sin sesión", "GET", "/api/projects", "", nil, 401},
		{"viewer lee", "GET", "/api/projects", viewerTok, nil, 200},
		{"viewer no despliega", "POST", "/api/projects/1/deploy", viewerTok, nil, 403},
		{"admin despliega", "POST", "/api/projects/1/deploy", adminTok, []string{"Sec-Fetch-Site", "same-origin"}, 200},
		{"CSRF por Sec-Fetch-Site", "POST", "/api/projects/1/deploy", adminTok, []string{"Sec-Fetch-Site", "cross-site"}, 403},
		{"CSRF por Origin", "POST", "/api/projects/1/deploy", adminTok, []string{"Origin", "https://malo.example"}, 403},
	}
	for _, c := range cases {
		if code, body := do(c.method, c.path, c.token, c.hdr...); code != c.want {
			t.Errorf("%s: %d %s (want %d)", c.name, code, body, c.want)
		}
	}
	if _, body := do("GET", "/api/projects", viewerTok); body != "ok Vera" {
		t.Errorf("el usuario no llegó al handler: %q", body)
	}
}

func TestResourceOf(t *testing.T) {
	for _, c := range []struct{ pattern, path, kind, id string }{
		{"GET /api/projects/{id}/env", "/api/projects/abc/env", "projects", "abc"},
		{"GET /api/servers/{id}", "/api/servers/s1", "servers", "s1"},
		{"GET /api/deployments/{id}/logs", "/api/deployments/d1/logs", "deployments", "d1"},
		{"DELETE /api/domains/{id}", "/api/domains/x", "domains", "x"},
		{"POST /api/projects/detect", "/api/projects/detect", "", ""},
		{"GET /api/projects", "/api/projects", "", ""},
		{"PATCH /api/users/{id}", "/api/users/u", "", ""},
	} {
		if k, id := resourceOf(c.pattern, c.path); k != c.kind || id != c.id {
			t.Errorf("%s: (%q, %q)", c.pattern, k, id)
		}
	}
}
