package servers

import (
	"context"
	"strconv"
	"strings"
	"time"

	"gestionador/internal/metrics"
)

// Observer recibe los resultados del monitor (lo implementan las alertas).
type Observer interface {
	ServerProbed(srv Server, err error)
	ContainersProbed(srv Server, containers []Container)
}

// Container es el estado de un contenedor de app (gs-*) en un servidor.
type Container struct {
	Name    string
	State   string // running, exited, restarting, ...
	CPU     float64
	Mem     float64
	Sampled bool // hay CPU/RAM (solo si está corriendo)
}

const containerInterval = time.Minute

// SetRecorder conecta el historial de métricas.
func (s *Service) SetRecorder(r *metrics.Recorder) { s.recorder = r }

// SetObserver conecta las alertas.
func (s *Service) SetObserver(o Observer) { s.observer = o }

func usagePct(u Usage) float64 {
	if u.Total == 0 {
		return 0
	}
	return float64(u.Used) / float64(u.Total) * 100
}

// afterProbe registra la muestra y, una vez por minuto, el estado de los contenedores.
func (s *Service) afterProbe(ctx context.Context, srv Server, info SystemInfo, m *Metrics, err error) {
	if s.observer != nil {
		s.observer.ServerProbed(srv, err)
	}
	if err != nil {
		return
	}
	if s.recorder != nil {
		disk := usagePct(m.Disk)
		s.recorder.Add(ctx, metrics.ServerSeries(srv.ID), m.CollectedAt, metrics.Sample{CPU: m.CPUPercent, Mem: usagePct(m.Memory), Disk: &disk})
	}
	if info.DockerStatus != DockerRunning {
		return
	}
	s.mu.Lock()
	due := time.Since(s.containersAt[srv.ID]) >= containerInterval-monitorInterval/2
	if due {
		s.containersAt[srv.ID] = time.Now()
	}
	s.mu.Unlock()
	if !due {
		return
	}
	res, err := s.Run(ctx, srv.ID, containersCmd)
	if err != nil || res.ExitCode != 0 {
		return
	}
	list := parseContainers(res.Stdout)
	now := time.Now().UTC()
	if s.recorder != nil {
		for _, c := range list {
			if c.Sampled && !strings.HasSuffix(c.Name, "-next") { // -next: contenedor temporal de un deploy
				s.recorder.Add(ctx, metrics.ContainerSeries(srv.ID, c.Name), now, metrics.Sample{CPU: c.CPU, Mem: c.Mem})
			}
		}
	}
	if s.observer != nil {
		s.observer.ContainersProbed(srv, list)
	}
}

// containersCmd lista los contenedores de apps (los que creó un deploy llevan la
// etiqueta gestionador.project) y su consumo, en un solo comando por servidor.
const containersCmd = `docker ps -a --filter label=gestionador.project --format '{{.Names}}|{{.State}}' 2>/dev/null
echo @@stats
ids=$(docker ps -q --filter label=gestionador.project 2>/dev/null)
[ -n "$ids" ] && docker stats --no-stream --format '{{.Name}}|{{.CPUPerc}}|{{.MemPerc}}' $ids 2>/dev/null
true`

func parseContainers(out string) []Container {
	ps, stats, _ := strings.Cut(out, "@@stats")
	byName := map[string]*Container{}
	var list []*Container
	for _, line := range strings.Split(ps, "\n") {
		name, state, ok := strings.Cut(strings.TrimSpace(line), "|")
		if !ok || !strings.HasPrefix(name, "gs-") {
			continue
		}
		c := &Container{Name: name, State: strings.ToLower(state)}
		byName[name] = c
		list = append(list, c)
	}
	pct := func(s string) (float64, bool) {
		v, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(s), "%"), 64)
		return v, err == nil
	}
	for _, line := range strings.Split(stats, "\n") {
		f := strings.Split(strings.TrimSpace(line), "|")
		if len(f) != 3 {
			continue
		}
		c := byName[f[0]]
		cpu, ok1 := pct(f[1])
		mem, ok2 := pct(f[2])
		if c != nil && ok1 && ok2 {
			c.CPU, c.Mem, c.Sampled = cpu, mem, true
		}
	}
	out2 := make([]Container, len(list))
	for i, c := range list {
		out2[i] = *c
	}
	return out2
}
