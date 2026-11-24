package metrics

import (
	"context"
	"os"
	"testing"
	"time"

	"gestionador/internal/database"
)

func newRecorder(t *testing.T) *Recorder {
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
	return NewRecorder(db)
}

func f(v float64) *float64 { return &v }

func TestMinuteAggregationAndRollup(t *testing.T) {
	r := newRecorder(t)
	ctx := context.Background()
	var got []Point
	r.OnMinute(func(s string, p Point) { got = append(got, p) })

	base := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour) // hora completa en el pasado
	// 60 minutos con 4 muestras cada uno: CPU 10, 20, 30, 40 (promedio 25, máximo 40).
	for m := 0; m < 60; m++ {
		for i, cpu := range []float64{10, 20, 30, 40} {
			r.Add(ctx, "server:s1", base.Add(time.Duration(m)*time.Minute+time.Duration(i)*15*time.Second), Sample{CPU: cpu, Mem: 50, Disk: f(70)})
		}
	}
	r.Add(ctx, "server:s1", base.Add(time.Hour), Sample{CPU: 99, Mem: 99}) // cierra el último minuto
	if len(got) != 60 || got[0].CPU != 25 || got[0].CPUMax != 40 || *got[0].Disk != 70 {
		t.Fatalf("minutos: %d, primero %+v", len(got), got[0])
	}

	r.maintain(ctx, base.Add(2*time.Hour+time.Minute))
	pts, err := r.read(ctx, "metrics_hour", "server:s1", base, base.Add(time.Hour))
	if err != nil || len(pts["server:s1"]) != 1 {
		t.Fatalf("hora: %v %v", pts, err)
	}
	h := pts["server:s1"][0]
	if h.CPU != 25 || h.CPUMax != 40 || h.Mem != 50 || h.Disk == nil || *h.Disk != 70 {
		t.Fatalf("resumen por hora: %+v", h)
	}
	// Un segundo mantenimiento no duplica.
	r.lastRolled = time.Time{}
	r.maintain(ctx, base.Add(2*time.Hour+2*time.Minute))
	pts, _ = r.read(ctx, "metrics_hour", "server:s1", base, base.Add(3*time.Hour))
	// Dos horas: la completa y la del minuto que cerró la serie; ninguna repetida.
	if hs := pts["server:s1"]; len(hs) != 2 || !hs[0].At.Equal(base) || !hs[1].At.Equal(base.Add(time.Hour)) {
		t.Fatalf("resumen por hora repetido o faltante: %+v", hs)
	}
}

func TestQueryDownsample(t *testing.T) {
	r := newRecorder(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Minute)
	for m := 360; m >= 1; m-- { // 6 h de minutos
		at := now.Add(-time.Duration(m) * time.Minute)
		_, err := r.db.ExecContext(ctx, `INSERT INTO metrics_minute (series, at, cpu, cpu_max, mem, mem_max) VALUES ('server:s1', $1, $2, $2, 10, 10)`, at, float64(m%10))
		if err != nil {
			t.Fatal(err)
		}
	}
	pts, err := r.Query(ctx, "server:s1", 6*time.Hour, now, 120)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) < 110 || len(pts) > 121 {
		t.Fatalf("puntos: %d", len(pts))
	}
	for i := 1; i < len(pts); i++ {
		if !pts[i].At.After(pts[i-1].At) {
			t.Fatal("puntos desordenados")
		}
	}
	if pts[0].CPUMax < pts[0].CPU {
		t.Fatal("el máximo debe ser >= promedio")
	}
	if all, _ := r.Query(ctx, "server:s1", time.Hour, now, 300); len(all) != 60 {
		t.Fatalf("1 h sin reducir: %d", len(all))
	}
	if none, _ := r.Query(ctx, "server:otro", time.Hour, now, 300); none == nil || len(none) != 0 {
		t.Fatal("serie vacía debe ser []")
	}
}
