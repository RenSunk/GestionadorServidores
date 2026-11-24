package teams

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gestionador/internal/auth"
	"gestionador/internal/database"
)

type fixture struct {
	db    *database.DB
	auth  *auth.Service
	teams *Service
	admin auth.User
}

func setup(t *testing.T) fixture {
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
	now := time.Now().UTC()
	for _, q := range []string{
		`INSERT INTO servers (id, name, host, username, auth_method, created_at, updated_at) VALUES ('s1','s1','h','u','instance_key',$1,$1)`,
		`INSERT INTO servers (id, name, host, username, auth_method, created_at, updated_at) VALUES ('s2','s2','h','u','instance_key',$1,$1)`,
		`INSERT INTO projects (id, name, slug, server_id, repo_url, branch, container_port, created_at, updated_at) VALUES ('p1','p1','p1','s1','r','main',80,$1,$1)`,
		`INSERT INTO projects (id, name, slug, server_id, repo_url, branch, container_port, created_at, updated_at) VALUES ('p2','p2','p2','s2','r','main',80,$1,$1)`,
		`INSERT INTO deployments (id, project_id, status, created_at) VALUES ('d1','p1','succeeded',$1)`,
	} {
		if _, err := db.ExecContext(ctx, q, now); err != nil {
			t.Fatal(err)
		}
	}
	a, err := auth.NewService(ctx, auth.NewStore(db))
	if err != nil {
		t.Fatal(err)
	}
	tm := NewService(db)
	a.SetScopes(tm)
	admin, err := a.Setup(ctx, auth.SetupInput{Name: "Ana", Email: "a@x.io", Password: "contraseña-larga"}, true)
	if err != nil {
		t.Fatal(err)
	}
	return fixture{db, a, tm, admin}
}

// user crea un usuario por invitación y devuelve su id y token de sesión.
func (f fixture) user(t *testing.T, email string, role auth.Role) (string, string) {
	t.Helper()
	ctx := context.Background()
	_, tok, err := f.auth.CreateInvite(ctx, f.admin, auth.InviteInput{Email: email, Role: role})
	if err != nil {
		t.Fatal(err)
	}
	u, sess, err := f.auth.AcceptInvite(ctx, tok, email, "contraseña-larga", auth.Client{})
	if err != nil {
		t.Fatal(err)
	}
	return u.ID, sess
}

func TestScope(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	u1, _ := f.user(t, "u1@x.io", auth.RoleDeveloper)
	u2, _ := f.user(t, "u2@x.io", auth.RoleDeveloper)

	// Sin equipos: todos ven todo.
	sc, _ := f.teams.LoadScope(ctx, u2)
	if !sc.Project("p1") || !sc.Server("s1") {
		t.Fatal("sin equipos todo debería ser visible")
	}

	team, err := f.teams.Create(ctx, Input{Name: "Pagos", UserIDs: []string{u1, u1}, ProjectIDs: []string{"p1"}, ServerIDs: []string{"s1"}})
	if err != nil || len(team.UserIDs) != 1 {
		t.Fatalf("crear: %+v %v", team, err)
	}
	if _, err := f.teams.Create(ctx, Input{Name: "Pagos"}); err != ErrDuplicateName {
		t.Fatalf("nombre repetido: %v", err)
	}
	if _, err := f.teams.Create(ctx, Input{Name: "X", ProjectIDs: []string{"nope"}}); err == nil {
		t.Fatal("proyecto inexistente aceptado")
	}

	sc1, _ := f.teams.LoadScope(ctx, u1)
	sc2, _ := f.teams.LoadScope(ctx, u2)
	for _, c := range []struct {
		name string
		got  bool
		want bool
	}{
		{"u1 ve p1 (su equipo)", sc1.Project("p1"), true},
		{"u1 ve p2 (sin equipo)", sc1.Project("p2"), true},
		{"u1 ve s1", sc1.Server("s1"), true},
		{"u2 no ve p1", sc2.Project("p1"), false},
		{"u2 ve p2", sc2.Project("p2"), true},
		{"u2 no ve s1", sc2.Server("s1"), false},
		{"u2 ve s2", sc2.Server("s2"), true},
	} {
		if c.got != c.want {
			t.Errorf("%s: %v", c.name, c.got)
		}
	}
	if pid, _ := f.teams.ProjectOf(ctx, "deployments", "d1"); pid != "p1" {
		t.Errorf("ProjectOf(d1) = %q", pid)
	}

	// Al borrar el equipo, todo vuelve a ser visible.
	if err := f.teams.Delete(ctx, team.ID); err != nil {
		t.Fatal(err)
	}
	if sc2, _ = f.teams.LoadScope(ctx, u2); !sc2.Project("p1") {
		t.Fatal("tras borrar el equipo p1 debería ser visible")
	}
}

func TestMiddlewareScope(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	u1, tok1 := f.user(t, "u1@x.io", auth.RoleDeveloper)
	_, tok2 := f.user(t, "u2@x.io", auth.RoleViewer)
	if _, err := f.teams.Create(ctx, Input{Name: "Pagos", UserIDs: []string{u1}, ProjectIDs: []string{"p1"}}); err != nil {
		t.Fatal(err)
	}
	_, adminTok, _ := f.auth.Login(ctx, "a@x.io", "contraseña-larga", auth.Client{})

	mux := http.NewServeMux()
	ok := func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }
	mux.HandleFunc("GET /api/projects/{id}", ok)
	mux.HandleFunc("GET /api/deployments/{id}/logs", ok)
	mux.HandleFunc("GET /api/teams", ok)
	h := f.auth.Middleware(mux)
	do := func(path, tok string) int {
		r := httptest.NewRequest("GET", path, nil)
		r.AddCookie(&http.Cookie{Name: "gestionador_session", Value: tok})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	for _, c := range []struct {
		name, path, tok string
		want            int
	}{
		{"miembro ve p1", "/api/projects/p1", tok1, 200},
		{"no miembro: p1 no existe", "/api/projects/p1", tok2, 404},
		{"no miembro: deploy de p1 no existe", "/api/deployments/d1/logs", tok2, 404},
		{"no miembro ve p2", "/api/projects/p2", tok2, 200},
		{"admin ve todo", "/api/projects/p1", adminTok, 200},
		{"equipos: solo admin", "/api/teams", tok1, 403},
		{"equipos: admin", "/api/teams", adminTok, 200},
	} {
		if got := do(c.path, c.tok); got != c.want {
			t.Errorf("%s: %d (want %d)", c.name, got, c.want)
		}
	}
}
