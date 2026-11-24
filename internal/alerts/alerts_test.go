package alerts

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"gestionador/internal/database"
	"gestionador/internal/metrics"
	"gestionador/internal/projects"
	"gestionador/internal/secrets"
	"gestionador/internal/servers"
	"gestionador/internal/settings"
)

type fakeProjects struct {
	expected []projects.Expected
	active   map[string]string
}

func (f *fakeProjects) ExpectedContainers(context.Context, string) ([]projects.Expected, error) {
	return f.expected, nil
}
func (f *fakeProjects) ActiveDeploymentID(id string) string { return f.active[id] }
func (f *fakeProjects) Get(_ context.Context, id string) (projects.Project, error) {
	for _, e := range f.expected {
		if e.ProjectID == id {
			return projects.Project{ID: id, Name: e.Name}, nil
		}
	}
	return projects.Project{}, projects.ErrNotFound
}

type fakeServers struct{}

func (fakeServers) Get(context.Context, string) (servers.Server, error) {
	return servers.Server{}, servers.ErrNotFound
}

func newService(t *testing.T) (*Service, *fakeProjects) {
	t.Helper()
	ctx := context.Background()
	// TEST_DATABASE_URL=postgres://... corre las mismas pruebas sobre PostgreSQL.
	db, err := database.Open(ctx, os.Getenv("TEST_DATABASE_URL"), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"metrics_minute", "metrics_hour", "alerts", "settings"} {
		if _, err := db.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			t.Fatal(err)
		}
	}
	box, err := secrets.New(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	fp := &fakeProjects{active: map[string]string{}}
	svc, err := NewService(ctx, db, settings.NewStore(db), box, fp, fakeServers{})
	if err != nil {
		t.Fatal(err)
	}
	return svc, fp
}

func open(t *testing.T, s *Service) []Alert {
	t.Helper()
	list, err := s.List(context.Background(), true, 100)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func TestServerDownNeedsTwoFailures(t *testing.T) {
	s, _ := newService(t)
	srv := servers.Server{ID: "s1", Name: "web-1"}
	s.ServerProbed(srv, errors.New("timeout"))
	if len(open(t, s)) != 0 {
		t.Fatal("una falla aislada no debe alertar")
	}
	s.ServerProbed(srv, errors.New("timeout"))
	s.ServerProbed(srv, errors.New("timeout"))
	got := open(t, s)
	if len(got) != 1 || got[0].Kind != ServerDown || !strings.Contains(got[0].Message, "web-1") {
		t.Fatalf("%+v", got)
	}
	s.ServerProbed(srv, nil)
	if len(open(t, s)) != 0 {
		t.Fatal("debió resolverse")
	}
	all, _ := s.List(context.Background(), false, 100)
	if len(all) != 1 || all[0].ResolvedAt == nil {
		t.Fatalf("historial: %+v", all)
	}
}

func minute(at time.Time, cpu, disk float64) metrics.Point {
	return metrics.Point{At: at, CPU: cpu, CPUMax: cpu, Mem: 20, MemMax: 20, Disk: &disk}
}

func TestCPUSustainedWithHysteresis(t *testing.T) {
	s, _ := newService(t)
	s.ServerProbed(servers.Server{ID: "s1", Name: "web-1"}, nil)
	base := time.Now().UTC().Truncate(time.Minute)
	at := func(i int) time.Time { return base.Add(time.Duration(i) * time.Minute) }

	for i := range 4 { // 4 min por encima: aún no (hacen falta 5)
		s.MinuteRecorded("server:s1", minute(at(i), 95, 40))
	}
	if len(open(t, s)) != 0 {
		t.Fatal("alertó antes de 5 minutos")
	}
	s.MinuteRecorded("server:s1", minute(at(4), 95, 40))
	if got := open(t, s); len(got) != 1 || got[0].Kind != CPUHigh {
		t.Fatalf("%+v", got)
	}
	// Bajar a 88 (promedio entre 85 y 90): sigue abierta por la histéresis.
	for i := 5; i < 10; i++ {
		s.MinuteRecorded("server:s1", minute(at(i), 88, 40))
	}
	if len(open(t, s)) != 1 {
		t.Fatal("se resolvió dentro de la banda de histéresis")
	}
	for i := 10; i < 15; i++ {
		s.MinuteRecorded("server:s1", minute(at(i), 30, 40))
	}
	if len(open(t, s)) != 0 {
		t.Fatal("no se resolvió")
	}
}

func TestCPUGapDoesNotFire(t *testing.T) {
	s, _ := newService(t)
	base := time.Now().UTC().Truncate(time.Minute)
	for _, i := range []int{0, 1, 2, 8, 9} { // 5 puntos, pero con un hueco de 6 minutos
		s.MinuteRecorded("server:s1", minute(base.Add(time.Duration(i)*time.Minute), 99, 40))
	}
	if len(open(t, s)) != 0 {
		t.Fatal("alertó con minutos faltantes")
	}
}

func TestDiskFullAndDisabled(t *testing.T) {
	s, _ := newService(t)
	now := time.Now().UTC()
	s.MinuteRecorded("server:s1", minute(now, 10, 93))
	if got := open(t, s); len(got) != 1 || got[0].Kind != DiskFull {
		t.Fatalf("%+v", got)
	}
	s.MinuteRecorded("server:s1", minute(now.Add(time.Minute), 10, 95))
	if len(open(t, s)) != 1 {
		t.Fatal("abrió una segunda alerta igual")
	}
	cfg := s.Settings()
	cfg.DiskPercent = 0
	if _, err := s.SaveSettings(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	s.MinuteRecorded("server:s1", minute(now.Add(2*time.Minute), 10, 95))
	if len(open(t, s)) != 0 {
		t.Fatal("desactivar el umbral debe cerrar la alerta")
	}
	// Las contenedores no generan alertas de servidor.
	s.MinuteRecorded("container:s1:gs-web", minute(now, 99, 99))
	if len(open(t, s)) != 0 {
		t.Fatal("serie de contenedor no debe alertar")
	}
}

func TestAppDown(t *testing.T) {
	s, fp := newService(t)
	fp.expected = []projects.Expected{{ProjectID: "p1", Name: "Tienda", ServerID: "s1", Container: "gs-tienda"}}
	srv := servers.Server{ID: "s1", Name: "web-1"}

	s.ContainersProbed(srv, []servers.Container{{Name: "gs-tienda", State: "exited"}})
	if got := open(t, s); len(got) != 1 || got[0].Kind != AppDown || got[0].TargetType != "project" {
		t.Fatalf("%+v", got)
	}
	s.ContainersProbed(srv, []servers.Container{{Name: "gs-tienda", State: "running"}})
	if len(open(t, s)) != 0 {
		t.Fatal("no se resolvió")
	}
	// Durante un deploy no se alerta aunque el contenedor no exista.
	fp.active["p1"] = "d1"
	s.ContainersProbed(srv, nil)
	if len(open(t, s)) != 0 {
		t.Fatal("alertó durante un deploy")
	}
}

func TestDeployFailedAndWebhook(t *testing.T) {
	s, _ := newService(t)
	var (
		mu  sync.Mutex
		got []map[string]any
	)
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]any
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &m)
		mu.Lock()
		got = append(got, m)
		mu.Unlock()
	}))
	defer hook.Close()

	cfg := s.Settings()
	cfg.WebhookURL = hook.URL
	if _, err := s.SaveSettings(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	// La URL se guarda cifrada y se recupera al recargar.
	if reloaded, err := s.load(context.Background()); err != nil || reloaded.WebhookURL != hook.URL {
		t.Fatalf("recargar: %+v %v", reloaded, err)
	}
	raw, _ := s.settings.Get(context.Background(), settingsKey)
	if strings.Contains(raw, hook.URL) {
		t.Fatal("la URL del webhook quedó en claro")
	}

	s.DeployFinished(projects.Project{ID: "p1", Name: "Tienda"}, projects.Deployment{Status: projects.StatusSucceeded})
	s.DeployFinished(projects.Project{ID: "p1", Name: "Tienda"}, projects.Deployment{Status: projects.StatusFailed, Error: "el build falló"})
	s.DeployFinished(projects.Project{ID: "p1", Name: "Tienda"}, projects.Deployment{Status: projects.StatusFailed, Error: "otra vez"})
	all, _ := s.List(context.Background(), false, 100)
	if len(all) != 2 || all[0].ResolvedAt == nil || len(open(t, s)) != 0 {
		t.Fatalf("cada deploy fallido es un evento cerrado: %+v", all)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n == 2 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 || got[0]["event"] != "alert.firing" || !strings.Contains(got[0]["text"].(string), "Tienda") {
		t.Fatalf("webhook: %+v", got)
	}
}

func TestPayloadFormats(t *testing.T) {
	m := message{Event: "test", Text: "hola"}
	if p := payload("https://hooks.slack.com/services/x", m).(map[string]string); p["text"] != "hola" {
		t.Error("slack")
	}
	if p := payload("https://discord.com/api/webhooks/1/x", m).(map[string]string); p["content"] != "hola" {
		t.Error("discord")
	}
	if _, ok := payload("https://example.com/hook", m).(message); !ok {
		t.Error("genérico")
	}
}

func TestSettingsValidation(t *testing.T) {
	s, _ := newService(t)
	for _, bad := range []Settings{
		{CPUPercent: 120, SustainMinutes: 5},
		{SustainMinutes: 0},
		{SustainMinutes: 5, WebhookURL: "ftp://x"},
	} {
		if _, err := s.SaveSettings(context.Background(), bad); err == nil {
			t.Errorf("debió rechazar %+v", bad)
		}
	}
}
