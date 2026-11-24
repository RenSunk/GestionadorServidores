// Package metrics guarda el historial de uso de servidores y contenedores:
// agregados por minuto (48 h) y por hora (90 días), en SQLite o PostgreSQL.
package metrics

import (
	"context"
	"log/slog"
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	"gestionador/internal/database"
)

const (
	minuteRetention = 48 * time.Hour
	hourRetention   = 90 * 24 * time.Hour
	// Rangos de hasta este largo se leen de la tabla por minuto.
	minuteRangeMax  = 26 * time.Hour
	maintenanceTick = 5 * time.Minute
)

// Sample es una medición puntual en porcentaje (Disk nil = no aplica).
type Sample struct {
	CPU  float64
	Mem  float64
	Disk *float64
}

// Point es un agregado: promedio y máximo del período.
type Point struct {
	At     time.Time `json:"t"`
	CPU    float64   `json:"cpu"`
	CPUMax float64   `json:"cpuMax"`
	Mem    float64   `json:"mem"`
	MemMax float64   `json:"memMax"`
	Disk   *float64  `json:"disk"`
}

func ServerSeries(serverID string) string { return "server:" + serverID }
func ContainerSeries(serverID, container string) string {
	return "container:" + serverID + ":" + container
}

// acc acumula las muestras de un minuto.
type acc struct {
	minute         time.Time
	n, nDisk       int
	cpu, mem, disk float64
	cpuMax, memMax float64
}

func (a *acc) add(s Sample) {
	a.n++
	a.cpu += s.CPU
	a.mem += s.Mem
	a.cpuMax = math.Max(a.cpuMax, s.CPU)
	a.memMax = math.Max(a.memMax, s.Mem)
	if s.Disk != nil {
		a.nDisk++
		a.disk += *s.Disk
	}
}

func (a *acc) point() Point {
	p := Point{At: a.minute, CPU: a.cpu / float64(a.n), CPUMax: a.cpuMax, Mem: a.mem / float64(a.n), MemMax: a.memMax}
	if a.nDisk > 0 {
		d := a.disk / float64(a.nDisk)
		p.Disk = &d
	}
	return p
}

// Recorder recibe muestras, escribe un agregado por minuto y avisa a los observadores.
type Recorder struct {
	db *database.DB

	mu         sync.Mutex
	accs       map[string]*acc
	onMinute   []func(series string, p Point)
	lastRolled time.Time
}

func NewRecorder(db *database.DB) *Recorder {
	return &Recorder{db: db, accs: map[string]*acc{}}
}

// OnMinute registra una función que recibe cada agregado por minuto (ej. alertas).
func (r *Recorder) OnMinute(fn func(series string, p Point)) {
	r.mu.Lock()
	r.onMinute = append(r.onMinute, fn)
	r.mu.Unlock()
}

// Add suma una muestra. Al empezar un minuto nuevo se guarda el anterior.
func (r *Recorder) Add(ctx context.Context, series string, at time.Time, s Sample) {
	minute := at.UTC().Truncate(time.Minute)
	r.mu.Lock()
	a := r.accs[series]
	var done *acc
	if a == nil || !a.minute.Equal(minute) {
		done = a
		a = &acc{minute: minute}
		r.accs[series] = a
	}
	a.add(s)
	r.mu.Unlock()
	if done != nil {
		r.flush(ctx, series, done)
	}
}

func (r *Recorder) flush(ctx context.Context, series string, a *acc) {
	p := a.point()
	_, err := r.db.ExecContext(context.WithoutCancel(ctx), `INSERT INTO metrics_minute (series, at, cpu, cpu_max, mem, mem_max, disk)
		VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT (series, at) DO NOTHING`,
		series, p.At, p.CPU, p.CPUMax, p.Mem, p.MemMax, p.Disk)
	if err != nil {
		slog.Error("guardar métricas", "series", series, "err", err)
		return
	}
	r.mu.Lock()
	obs := slices.Clone(r.onMinute)
	r.mu.Unlock()
	for _, fn := range obs {
		fn(series, p)
	}
}

// RunMaintenance guarda minutos sin muestras nuevas (servidor caído), resume por
// hora y borra lo vencido, hasta que ctx se cancele.
func (r *Recorder) RunMaintenance(ctx context.Context) {
	t := time.NewTicker(maintenanceTick)
	defer t.Stop()
	for {
		r.maintain(ctx, time.Now().UTC())
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (r *Recorder) maintain(ctx context.Context, now time.Time) {
	// 1. Minutos que quedaron abiertos (no llegaron más muestras).
	r.mu.Lock()
	stale := map[string]*acc{}
	for s, a := range r.accs {
		if now.Sub(a.minute) > 2*time.Minute {
			stale[s] = a
			delete(r.accs, s)
		}
	}
	r.mu.Unlock()
	for s, a := range stale {
		r.flush(ctx, s, a)
	}

	// 2. Resumen por hora de las horas completas.
	if err := r.rollup(ctx, now); err != nil && ctx.Err() == nil {
		slog.Error("resumir métricas por hora", "err", err)
	}

	// 3. Retención.
	for table, keep := range map[string]time.Duration{"metrics_minute": minuteRetention, "metrics_hour": hourRetention} {
		if _, err := r.db.ExecContext(ctx, `DELETE FROM `+table+` WHERE at < $1`, now.Add(-keep)); err != nil && ctx.Err() == nil {
			slog.Error("borrar métricas vencidas", "tabla", table, "err", err)
		}
	}
}

func (r *Recorder) rollup(ctx context.Context, now time.Time) error {
	current := now.Truncate(time.Hour)
	if r.lastRolled.IsZero() {
		var last *time.Time
		var t time.Time
		err := r.db.QueryRowContext(ctx, `SELECT at FROM metrics_hour ORDER BY at DESC LIMIT 1`).Scan(&t)
		if err == nil {
			last = &t
		}
		if last != nil {
			r.lastRolled = last.UTC()
		} else {
			r.lastRolled = current.Add(-minuteRetention)
		}
	}
	for h := r.lastRolled.Add(time.Hour); h.Before(current); h = h.Add(time.Hour) {
		rows, err := r.read(ctx, "metrics_minute", "", h, h.Add(time.Hour))
		if err != nil {
			return err
		}
		for series, pts := range rows {
			p := merge(h, pts)
			if _, err := r.db.ExecContext(ctx, `INSERT INTO metrics_hour (series, at, cpu, cpu_max, mem, mem_max, disk)
				VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT (series, at) DO NOTHING`,
				series, p.At, p.CPU, p.CPUMax, p.Mem, p.MemMax, p.Disk); err != nil {
				return err
			}
		}
		r.lastRolled = h
	}
	return nil
}

// read devuelve los puntos de [from, to) agrupados por serie (series "" = todas).
func (r *Recorder) read(ctx context.Context, table, series string, from, to time.Time) (map[string][]Point, error) {
	q := `SELECT series, at, cpu, cpu_max, mem, mem_max, disk FROM ` + table + ` WHERE at >= $1 AND at < $2`
	args := []any{from, to}
	if series != "" {
		q += ` AND series = $3`
		args = append(args, series)
	}
	rows, err := r.db.QueryContext(ctx, q+` ORDER BY at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]Point{}
	for rows.Next() {
		var (
			s string
			p Point
		)
		if err := rows.Scan(&s, &p.At, &p.CPU, &p.CPUMax, &p.Mem, &p.MemMax, &p.Disk); err != nil {
			return nil, err
		}
		p.At = p.At.UTC()
		out[s] = append(out[s], p)
	}
	return out, rows.Err()
}

// merge resume varios puntos en uno: promedio de promedios y máximo de máximos.
func merge(at time.Time, pts []Point) Point {
	out := Point{At: at}
	var disk float64
	nDisk := 0
	for _, p := range pts {
		out.CPU += p.CPU
		out.Mem += p.Mem
		out.CPUMax = math.Max(out.CPUMax, p.CPUMax)
		out.MemMax = math.Max(out.MemMax, p.MemMax)
		if p.Disk != nil {
			disk += *p.Disk
			nDisk++
		}
	}
	out.CPU /= float64(len(pts))
	out.Mem /= float64(len(pts))
	if nDisk > 0 {
		d := disk / float64(nDisk)
		out.Disk = &d
	}
	return out
}

// Ranges son los rangos que ofrece la interfaz.
var Ranges = map[string]time.Duration{
	"1h": time.Hour, "6h": 6 * time.Hour, "24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour, "30d": 30 * 24 * time.Hour,
}

// Query devuelve como mucho maxPoints puntos de la serie en el rango, del más antiguo al más reciente.
func (r *Recorder) Query(ctx context.Context, series string, rng time.Duration, now time.Time, maxPoints int) ([]Point, error) {
	from := now.UTC().Add(-rng)
	table := "metrics_minute"
	if rng > minuteRangeMax {
		table = "metrics_hour"
	}
	rows, err := r.read(ctx, table, series, from, now.UTC().Add(time.Minute))
	if err != nil {
		return nil, err
	}
	pts := downsample(rows[series], from, rng, maxPoints)
	for i := range pts {
		pts[i].round()
	}
	return pts, nil
}

// round deja dos decimales (la interfaz muestra uno; no hace falta enviar más).
func (p *Point) round() {
	r := func(v float64) float64 { return math.Round(v*100) / 100 }
	p.CPU, p.CPUMax, p.Mem, p.MemMax = r(p.CPU), r(p.CPUMax), r(p.Mem), r(p.MemMax)
	if p.Disk != nil {
		d := r(*p.Disk)
		p.Disk = &d
	}
}

// downsample agrupa en intervalos iguales; los intervalos sin datos se omiten
// (la interfaz corta la línea ahí, en vez de inventar valores).
func downsample(pts []Point, from time.Time, rng time.Duration, maxPoints int) []Point {
	if len(pts) <= maxPoints {
		if pts == nil {
			return []Point{}
		}
		return pts
	}
	step := rng / time.Duration(maxPoints)
	out := []Point{}
	var bucket []Point
	var cur time.Time
	for _, p := range pts {
		b := from.Add(p.At.Sub(from) / step * step)
		if len(bucket) > 0 && !b.Equal(cur) {
			out = append(out, merge(cur, bucket))
			bucket = bucket[:0]
		}
		cur = b
		bucket = append(bucket, p)
	}
	if len(bucket) > 0 {
		out = append(out, merge(cur, bucket))
	}
	return out
}

// DeleteSeries borra el historial de una serie (ej. el contenedor de un proyecto eliminado).
func (r *Recorder) DeleteSeries(ctx context.Context, series string) error {
	for _, table := range []string{"metrics_minute", "metrics_hour"} {
		if _, err := r.db.ExecContext(ctx, `DELETE FROM `+table+` WHERE series = $1`, series); err != nil {
			return err
		}
	}
	r.mu.Lock()
	delete(r.accs, series)
	r.mu.Unlock()
	return nil
}

// DeleteServer borra el historial de un servidor y de sus contenedores.
func (r *Recorder) DeleteServer(ctx context.Context, serverID string) error {
	for _, table := range []string{"metrics_minute", "metrics_hour"} {
		if _, err := r.db.ExecContext(ctx, `DELETE FROM `+table+` WHERE series = $1 OR series LIKE $2`,
			ServerSeries(serverID), "container:"+strings.ReplaceAll(serverID, "%", "")+":%"); err != nil {
			return err
		}
	}
	r.mu.Lock()
	for s := range r.accs {
		if s == ServerSeries(serverID) || strings.HasPrefix(s, "container:"+serverID+":") {
			delete(r.accs, s)
		}
	}
	r.mu.Unlock()
	return nil
}
