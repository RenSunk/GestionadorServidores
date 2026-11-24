// Package alerts detecta problemas (servidor caído, recursos al límite, app caída,
// deploy fallido), guarda su historial y avisa por webhook (Slack, Discord o JSON).
package alerts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"gestionador/internal/database"
	"gestionador/internal/metrics"
	"gestionador/internal/projects"
	"gestionador/internal/secrets"
	"gestionador/internal/servers"
	"gestionador/internal/settings"
)

type Kind string

const (
	ServerDown   Kind = "server_down"
	CPUHigh      Kind = "cpu_high"
	MemHigh      Kind = "mem_high"
	DiskFull     Kind = "disk_full"
	AppDown      Kind = "app_down"
	DeployFailed Kind = "deploy_failed" // evento: se abre y se cierra a la vez
)

const (
	settingsKey      = "alerts"
	downAfter        = 2 // sondeos fallidos seguidos (~30 s) para dar un servidor por caído
	hysteresis       = 5 // puntos por debajo del umbral para resolver CPU/RAM
	diskHysteresis   = 2
	reconcileEvery   = 5 * time.Minute
	historyRetention = 90 * 24 * time.Hour
)

type Alert struct {
	ID         string     `json:"id"`
	Kind       Kind       `json:"kind"`
	TargetType string     `json:"targetType"` // server, project
	TargetID   string     `json:"targetId"`
	TargetName string     `json:"targetName"`
	Message    string     `json:"message"`
	StartedAt  time.Time  `json:"startedAt"`
	ResolvedAt *time.Time `json:"resolvedAt"`
}

// Settings son los umbrales y el canal de aviso de la instancia.
type Settings struct {
	Enabled        bool    `json:"enabled"`
	CPUPercent     float64 `json:"cpuPercent"`  // 0 = sin alerta
	MemPercent     float64 `json:"memPercent"`  // 0 = sin alerta
	DiskPercent    float64 `json:"diskPercent"` // 0 = sin alerta
	SustainMinutes int     `json:"sustainMinutes"`
	ServerDown     bool    `json:"serverDown"`
	AppDown        bool    `json:"appDown"`
	DeployFailed   bool    `json:"deployFailed"`
	WebhookURL     string  `json:"webhookUrl"`
}

var defaults = Settings{
	Enabled: true, CPUPercent: 90, MemPercent: 90, DiskPercent: 90, SustainMinutes: 5,
	ServerDown: true, AppDown: true, DeployFailed: true,
}

func (s *Settings) validate() error {
	for _, v := range []float64{s.CPUPercent, s.MemPercent, s.DiskPercent} {
		if v < 0 || v > 100 {
			return errors.New("los umbrales van de 0 a 100 % (0 desactiva la alerta)")
		}
	}
	if s.SustainMinutes < 1 || s.SustainMinutes > 60 {
		return errors.New("la duración sostenida va de 1 a 60 minutos")
	}
	s.WebhookURL = strings.TrimSpace(s.WebhookURL)
	if s.WebhookURL != "" && !strings.HasPrefix(s.WebhookURL, "https://") && !strings.HasPrefix(s.WebhookURL, "http://") {
		return errors.New("la URL del webhook debe empezar con https://")
	}
	return nil
}

// Projects es lo que las alertas necesitan del módulo de proyectos.
type Projects interface {
	ExpectedContainers(ctx context.Context, serverID string) ([]projects.Expected, error)
	ActiveDeploymentID(projectID string) string
	Get(ctx context.Context, id string) (projects.Project, error)
}

type Servers interface {
	Get(ctx context.Context, id string) (servers.Server, error)
}

type Service struct {
	db       *database.DB
	settings *settings.Store
	box      *secrets.Box
	projects Projects
	servers  Servers
	notifier *notifier

	mu       sync.Mutex
	cfg      Settings
	failures map[string]int             // sondeos fallidos seguidos por servidor
	names    map[string]string          // nombre de cada servidor
	minutes  map[string][]metrics.Point // últimos minutos por servidor
}

func NewService(ctx context.Context, db *database.DB, st *settings.Store, box *secrets.Box, p Projects, s Servers) (*Service, error) {
	svc := &Service{
		db: db, settings: st, box: box, projects: p, servers: s, notifier: newNotifier(),
		failures: map[string]int{}, names: map[string]string{}, minutes: map[string][]metrics.Point{},
	}
	cfg, err := svc.load(ctx)
	if err != nil {
		return nil, err
	}
	svc.cfg = cfg
	return svc, nil
}

// ---------- Configuración ----------

type storedSettings struct {
	Settings
	WebhookURL string `json:"webhookUrl,omitempty"` // vacío en lo guardado: va cifrado
	WebhookEnc string `json:"webhookEnc,omitempty"`
}

func (s *Service) load(ctx context.Context) (Settings, error) {
	raw, err := s.settings.Get(ctx, settingsKey)
	if errors.Is(err, settings.ErrNotFound) {
		return defaults, nil
	}
	if err != nil {
		return Settings{}, err
	}
	var st storedSettings
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		return Settings{}, err
	}
	cfg := st.Settings
	if st.WebhookEnc != "" {
		url, err := s.box.Decrypt(st.WebhookEnc)
		if err != nil {
			return Settings{}, fmt.Errorf("descifrar webhook de alertas: %w", err)
		}
		cfg.WebhookURL = string(url)
	}
	return cfg, nil
}

func (s *Service) Settings() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

func (s *Service) SaveSettings(ctx context.Context, cfg Settings) (Settings, error) {
	if err := cfg.validate(); err != nil {
		return Settings{}, &ValidationError{err.Error()}
	}
	st := storedSettings{Settings: cfg}
	st.Settings.WebhookURL = ""
	if cfg.WebhookURL != "" {
		enc, err := s.box.Encrypt([]byte(cfg.WebhookURL))
		if err != nil {
			return Settings{}, err
		}
		st.WebhookEnc = enc
	}
	raw, err := json.Marshal(st)
	if err != nil {
		return Settings{}, err
	}
	if err := s.settings.Set(ctx, settingsKey, string(raw)); err != nil {
		return Settings{}, err
	}
	s.mu.Lock()
	s.cfg = cfg
	s.mu.Unlock()
	return cfg, nil
}

type ValidationError struct{ msg string }

func (e *ValidationError) Error() string { return e.msg }

// Test envía un aviso de prueba al webhook configurado (o al indicado).
func (s *Service) Test(ctx context.Context, url string) error {
	if url == "" {
		url = s.Settings().WebhookURL
	}
	if url == "" {
		return &ValidationError{"configura primero la URL del webhook"}
	}
	return s.notifier.send(ctx, url, message{Event: "test", Text: "🔔 Prueba de Gestionador: las alertas llegarán a este canal."})
}

// ---------- Ciclo de vida ----------

func (s *Service) open(ctx context.Context, kind Kind, targetID string) (Alert, bool, error) {
	a, err := scanAlert(s.db.QueryRowContext(ctx, `SELECT `+alertColumns+` FROM alerts
		WHERE kind = $1 AND target_id = $2 AND resolved_at IS NULL LIMIT 1`, kind, targetID))
	if errors.Is(err, sql.ErrNoRows) {
		return Alert{}, false, nil
	}
	return a, err == nil, err
}

// fire abre una alerta si no hay otra igual abierta, y avisa.
func (s *Service) fire(ctx context.Context, kind Kind, targetType, targetID, targetName, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok, err := s.open(ctx, kind, targetID); err != nil || ok {
		return
	}
	now := time.Now().UTC()
	a := Alert{ID: uuid.Must(uuid.NewV7()).String(), Kind: kind, TargetType: targetType, TargetID: targetID,
		TargetName: targetName, Message: msg, StartedAt: now}
	if kind == DeployFailed {
		a.ResolvedAt = &now
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO alerts (`+alertColumns+`) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		a.ID, a.Kind, a.TargetType, a.TargetID, a.TargetName, a.Message, a.StartedAt, a.ResolvedAt); err != nil {
		slog.Error("guardar alerta", "err", err)
		return
	}
	slog.Warn("alerta", "tipo", kind, "objetivo", targetName, "mensaje", msg)
	s.notify(message{Event: "alert.firing", Text: "🔴 " + msg, Alert: &a})
}

// resolve cierra la alerta abierta de ese tipo y objetivo, si hay una.
func (s *Service) resolve(ctx context.Context, kind Kind, targetID, why string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok, err := s.open(ctx, kind, targetID)
	if err != nil || !ok {
		return
	}
	now := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx, `UPDATE alerts SET resolved_at = $2 WHERE id = $1`, a.ID, now); err != nil {
		slog.Error("resolver alerta", "err", err)
		return
	}
	a.ResolvedAt = &now
	slog.Info("alerta resuelta", "tipo", kind, "objetivo", a.TargetName)
	s.notify(message{Event: "alert.resolved", Text: "✅ Resuelto: " + why + " (" + a.Message + ")", Alert: &a})
}

// notify envía en segundo plano (no frena al monitor). Requiere s.mu tomado.
func (s *Service) notify(m message) {
	if url := s.cfg.WebhookURL; url != "" {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := s.notifier.send(ctx, url, m); err != nil {
				slog.Warn("enviar alerta por webhook", "err", err)
			}
		}()
	}
}

// ---------- Observadores ----------

// ServerProbed (servers.Observer): servidor caído o recuperado.
func (s *Service) ServerProbed(srv servers.Server, err error) {
	ctx := context.Background()
	s.mu.Lock()
	s.names[srv.ID] = srv.Name
	cfg := s.cfg
	if err != nil {
		s.failures[srv.ID]++
	} else {
		s.failures[srv.ID] = 0
	}
	n := s.failures[srv.ID]
	s.mu.Unlock()

	switch {
	case err == nil:
		s.resolve(ctx, ServerDown, srv.ID, srv.Name+" vuelve a responder")
	case cfg.Enabled && cfg.ServerDown && n >= downAfter:
		s.fire(ctx, ServerDown, "server", srv.ID, srv.Name, fmt.Sprintf("El servidor %s no responde: %s", srv.Name, err))
	}
}

// ContainersProbed (servers.Observer): apps que deberían estar corriendo y no lo están.
func (s *Service) ContainersProbed(srv servers.Server, list []servers.Container) {
	ctx := context.Background()
	cfg := s.Settings()
	expected, err := s.projects.ExpectedContainers(ctx, srv.ID)
	if err != nil {
		slog.Error("alertas: contenedores esperados", "err", err)
		return
	}
	state := map[string]string{}
	for _, c := range list {
		state[c.Name] = c.State
	}
	for _, e := range expected {
		if s.projects.ActiveDeploymentID(e.ProjectID) != "" {
			continue // durante un deploy el contenedor se reemplaza
		}
		st, exists := state[e.Container]
		switch {
		case st == "running":
			s.resolve(ctx, AppDown, e.ProjectID, e.Name+" vuelve a estar en ejecución")
		case !cfg.Enabled || !cfg.AppDown:
		case !exists:
			s.fire(ctx, AppDown, "project", e.ProjectID, e.Name, fmt.Sprintf("La app %s no está en ejecución en %s: el contenedor no existe", e.Name, srv.Name))
		case st == "restarting":
			s.fire(ctx, AppDown, "project", e.ProjectID, e.Name, fmt.Sprintf("La app %s se reinicia una y otra vez en %s (se cae al arrancar)", e.Name, srv.Name))
		default:
			s.fire(ctx, AppDown, "project", e.ProjectID, e.Name, fmt.Sprintf("La app %s no está en ejecución en %s (estado: %s)", e.Name, srv.Name, st))
		}
	}
}

// MinuteRecorded (metrics.OnMinute): CPU y RAM sostenidas, disco lleno.
func (s *Service) MinuteRecorded(series string, p metrics.Point) {
	id, ok := strings.CutPrefix(series, "server:")
	if !ok {
		return
	}
	ctx := context.Background()
	s.mu.Lock()
	cfg := s.cfg
	name := s.names[id]
	pts := append(s.minutes[id], p)
	if len(pts) > 60 {
		pts = pts[len(pts)-60:]
	}
	s.minutes[id] = pts
	s.mu.Unlock()
	if name == "" {
		name = id
	}

	window := recent(pts, cfg.SustainMinutes, p.At)
	check := func(kind Kind, threshold float64, avg float64, label string) {
		if threshold <= 0 || !cfg.Enabled {
			s.resolve(ctx, kind, id, "alerta desactivada")
			return
		}
		switch {
		case window != nil && avg >= threshold:
			s.fire(ctx, kind, "server", id, name, fmt.Sprintf("%s de %s al %.0f %% (promedio de %d min; umbral %.0f %%)",
				label, name, avg, cfg.SustainMinutes, threshold))
		case avg < threshold-hysteresis:
			s.resolve(ctx, kind, id, fmt.Sprintf("%s de %s bajó al %.0f %%", label, name, avg))
		}
	}
	if window != nil {
		var cpu, mem float64
		for _, w := range window {
			cpu += w.CPU
			mem += w.Mem
		}
		check(CPUHigh, cfg.CPUPercent, cpu/float64(len(window)), "CPU")
		check(MemHigh, cfg.MemPercent, mem/float64(len(window)), "RAM")
	}
	if p.Disk != nil {
		switch {
		case !cfg.Enabled || cfg.DiskPercent <= 0:
			s.resolve(ctx, DiskFull, id, "alerta desactivada")
		case *p.Disk >= cfg.DiskPercent:
			s.fire(ctx, DiskFull, "server", id, name, fmt.Sprintf("Disco de %s al %.0f %% (umbral %.0f %%)", name, *p.Disk, cfg.DiskPercent))
		case *p.Disk < cfg.DiskPercent-diskHysteresis:
			s.resolve(ctx, DiskFull, id, fmt.Sprintf("disco de %s al %.0f %%", name, *p.Disk))
		}
	}
}

// recent devuelve los últimos n minutos si están todos (sin huecos); si no, nil.
func recent(pts []metrics.Point, n int, now time.Time) []metrics.Point {
	if len(pts) < n {
		return nil
	}
	w := pts[len(pts)-n:]
	if now.Sub(w[0].At) > time.Duration(n)*time.Minute+30*time.Second {
		return nil // faltan minutos (el servidor estuvo caído)
	}
	return w
}

// DeployFinished (projects.OnDeployFinished): deploy fallido.
func (s *Service) DeployFinished(p projects.Project, d projects.Deployment) {
	cfg := s.Settings()
	if d.Status != projects.StatusFailed || !cfg.Enabled || !cfg.DeployFailed {
		return
	}
	msg := fmt.Sprintf("Falló el deploy de %s", p.Name)
	if d.Error != "" {
		msg += ": " + d.Error
	}
	s.fire(context.Background(), DeployFailed, "project", p.ID, p.Name, msg)
}

// RunReconcile cierra alertas de servidores o proyectos que ya no existen y borra
// el historial viejo, hasta que ctx se cancele.
func (s *Service) RunReconcile(ctx context.Context) {
	t := time.NewTicker(reconcileEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		list, err := s.List(ctx, true, 500)
		if err != nil {
			continue
		}
		for _, a := range list {
			var gone bool
			if a.TargetType == "server" {
				_, err = s.servers.Get(ctx, a.TargetID)
				gone = errors.Is(err, servers.ErrNotFound)
			} else {
				_, err = s.projects.Get(ctx, a.TargetID)
				gone = errors.Is(err, projects.ErrNotFound)
			}
			if gone {
				s.resolve(ctx, a.Kind, a.TargetID, a.TargetName+" fue eliminado")
			}
		}
		_, _ = s.db.ExecContext(ctx, `DELETE FROM alerts WHERE resolved_at IS NOT NULL AND started_at < $1`, time.Now().UTC().Add(-historyRetention))
	}
}

// ---------- Consultas ----------

const alertColumns = `id, kind, target_type, target_id, target_name, message, started_at, resolved_at`

func scanAlert(row interface{ Scan(...any) error }) (Alert, error) {
	var (
		a   Alert
		res sql.NullTime
	)
	err := row.Scan(&a.ID, &a.Kind, &a.TargetType, &a.TargetID, &a.TargetName, &a.Message, &a.StartedAt, &res)
	if res.Valid {
		t := res.Time
		a.ResolvedAt = &t
	}
	return a, err
}

// List devuelve alertas recientes (las abiertas primero).
func (s *Service) List(ctx context.Context, openOnly bool, limit int) ([]Alert, error) {
	q := `SELECT ` + alertColumns + ` FROM alerts`
	if openOnly {
		q += ` WHERE resolved_at IS NULL`
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY started_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Alert{}
	for rows.Next() {
		a, err := scanAlert(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
