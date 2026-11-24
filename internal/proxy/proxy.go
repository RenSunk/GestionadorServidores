// Package proxy gestiona Caddy en cada servidor: un contenedor gestionador-caddy
// conectado a la red Docker "gestionador", con un Caddyfile que Gestionador
// genera completo desde la base de datos y recarga sin cortar conexiones.
package proxy

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"gestionador/internal/sshx"
)

const (
	Network       = "gestionador"
	ContainerName = "gestionador-caddy"
	// Nombre completo: Podman (que a veces se hace pasar por docker) no resuelve nombres cortos.
	Image = "docker.io/library/caddy:2-alpine"
)

type TLSMode string

const (
	TLSAuto     TLSMode = "auto"     // Let's Encrypt / ZeroSSL (necesita DNS público y puertos 80/443)
	TLSInternal TLSMode = "internal" // CA local de Caddy: pruebas y redes internas
)

// Route envía uno o varios dominios a un contenedor de la red "gestionador".
type Route struct {
	Hosts    []string
	TLS      TLSMode
	Upstream string // alias en la red, ej. "d-<deploy>"
	Port     int
}

// Ports son los puertos del servidor donde Caddy escucha.
type Ports struct {
	HTTP  int `json:"httpPort"`
	HTTPS int `json:"httpsPort"`
}

var DefaultPorts = Ports{HTTP: 80, HTTPS: 443}

// Runner es lo que el proxy necesita para ejecutar comandos en un servidor.
type Runner interface {
	Run(ctx context.Context, serverID, cmd string) (sshx.Result, error)
	Exec(ctx context.Context, serverID, cmd string, opts sshx.ExecOptions) (int, error)
}

type Manager struct {
	run Runner

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func NewManager(r Runner) *Manager {
	return &Manager{run: r, locks: map[string]*sync.Mutex{}}
}

// Lock serializa los cambios de proxy de un servidor. Quien calcula las rutas
// debe tenerlo tomado hasta aplicarlas, para no pisar un cambio más reciente.
func (m *Manager) Lock(serverID string) func() {
	m.mu.Lock()
	l, ok := m.locks[serverID]
	if !ok {
		l = &sync.Mutex{}
		m.locks[serverID] = l
	}
	m.mu.Unlock()
	l.Lock()
	return l.Unlock
}

// Apply deja Caddy con exactamente estas rutas. Si no hay rutas y Caddy no
// existe, no lo crea.
func (m *Manager) Apply(ctx context.Context, serverID string, ports Ports, routes []Route) error {
	if len(routes) == 0 {
		st, err := m.Status(ctx, serverID)
		if err != nil {
			return err
		}
		if st.State == "missing" {
			return nil
		}
	}
	if err := m.Ensure(ctx, serverID, ports); err != nil {
		return err
	}
	var out strings.Builder
	code, err := m.run.Exec(ctx, serverID, reloadCmd(), sshx.ExecOptions{
		Stdin: strings.NewReader(Caddyfile(routes)), Stdout: &out, Stderr: &out,
	})
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("caddy rechazó la configuración: %s", lastLines(out.String(), 6))
	}
	return nil
}

// Ensure crea la red y el contenedor de Caddy si faltan, y lo recrea si cambiaron los puertos.
func (m *Manager) Ensure(ctx context.Context, serverID string, ports Ports) error {
	res, err := m.run.Run(ctx, serverID, ensureCmd(ports))
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return ensureError(res.Stdout+res.Stderr, ports)
	}
	return nil
}

// Status del contenedor de Caddy en el servidor.
type Status struct {
	State string `json:"state"` // running, exited, missing, ...
	Ports string `json:"ports"` // "80,443" según la etiqueta del contenedor
}

func (m *Manager) Status(ctx context.Context, serverID string) (Status, error) {
	res, err := m.run.Run(ctx, serverID, fmt.Sprintf(
		`docker inspect -f '{{.State.Status}} {{index .Config.Labels "gestionador.ports"}}' %s 2>/dev/null || echo missing`, ContainerName))
	if err != nil {
		return Status{}, err
	}
	f := strings.Fields(res.Stdout)
	st := Status{State: "missing"}
	if len(f) > 0 {
		st.State = f[0]
	}
	if len(f) > 1 {
		st.Ports = f[1]
	}
	return st, nil
}

// WaitReady espera a que algo responda en alias:port desde la red de Caddy.
// Cualquier respuesta HTTP cuenta (incluso 404 o 500): solo importa que la app escuche.
func (m *Manager) WaitReady(ctx context.Context, serverID, container, alias string, port int, w interface{ Write([]byte) (int, error) }) error {
	code, err := m.run.Exec(ctx, serverID, probeCmd(container, alias, port), sshx.ExecOptions{Stdout: w, Stderr: w})
	if err != nil {
		return err
	}
	switch code {
	case 0:
		return nil
	case 2:
		return errors.New("el contenedor nuevo se detuvo antes de responder")
	default:
		return fmt.Errorf("el contenedor nuevo no respondió en el puerto %d a tiempo; revisa que la app escuche en 0.0.0.0:$PORT", port)
	}
}

// Caddyfile genera la configuración completa. Es determinista (ordenada) para
// que el mismo estado produzca siempre el mismo archivo.
func Caddyfile(routes []Route) string {
	var b strings.Builder
	b.WriteString("# Generado por Gestionador. No editar: se reescribe en cada cambio.\n")
	b.WriteString("{\n\tadmin localhost:2019\n}\n")

	sorted := slices.Clone(routes)
	slices.SortFunc(sorted, func(a, b Route) int { return strings.Compare(a.Hosts[0], b.Hosts[0]) })
	for _, r := range sorted {
		hosts := slices.Clone(r.Hosts)
		slices.Sort(hosts)
		fmt.Fprintf(&b, "\n%s {\n", strings.Join(hosts, ", "))
		if r.TLS == TLSInternal {
			b.WriteString("\ttls internal\n")
		}
		b.WriteString("\tencode zstd gzip\n")
		// lb_try_duration: durante el cambio de contenedor reintenta en vez de devolver 502.
		fmt.Fprintf(&b, "\treverse_proxy %s:%d {\n\t\tlb_try_duration 10s\n\t}\n}\n", r.Upstream, r.Port)
	}
	return b.String()
}

// ---------- comandos ----------

func ensureCmd(p Ports) string {
	label := fmt.Sprintf("%d,%d", p.HTTP, p.HTTPS)
	return fmt.Sprintf(`docker network inspect %[1]s >/dev/null 2>&1 || docker network create %[1]s >/dev/null || exit 1
cur=$(docker inspect -f '{{index .Config.Labels "gestionador.ports"}}' %[2]s 2>/dev/null || echo missing)
if [ "$cur" != "missing" ] && [ "$cur" != "%[3]s" ]; then
  echo "Recreando Caddy con los puertos nuevos"
  docker rm -f %[2]s >/dev/null || exit 1
  cur=missing
fi
if [ "$cur" = "missing" ]; then
  docker run -d --name %[2]s --restart unless-stopped --network %[1]s \
    -p %[4]d:80 -p %[5]d:443 -p %[5]d:443/udp \
    -v gestionador-caddy-data:/data -v gestionador-caddy-config:/config -v gestionador-caddy-etc:/etc/caddy \
    --label gestionador.role=proxy --label gestionador.ports=%[3]s %[6]s >/dev/null 2>/tmp/gestionador-caddy.err \
    || { cat /tmp/gestionador-caddy.err; rm -f /tmp/gestionador-caddy.err; exit 1; }
  rm -f /tmp/gestionador-caddy.err
elif [ "$(docker inspect -f '{{.State.Running}}' %[2]s)" != "true" ]; then
  docker start %[2]s >/dev/null || exit 1
fi`, Network, ContainerName, label, p.HTTP, p.HTTPS, Image)
}

// reloadCmd recibe el Caddyfile por stdin, lo valida y lo recarga sin cortar conexiones.
func reloadCmd() string {
	return fmt.Sprintf(`docker exec -i %s sh -c 'cat > /etc/caddy/Caddyfile.new &&
caddy validate --config /etc/caddy/Caddyfile.new --adapter caddyfile >/dev/null 2>/tmp/validate.err || { cat /tmp/validate.err; exit 1; }
mv /etc/caddy/Caddyfile.new /etc/caddy/Caddyfile &&
for i in 1 2 3 4 5 6 7 8 9 10; do caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile 2>/tmp/reload.err && exit 0; sleep 1; done
cat /tmp/reload.err; exit 1'`, ContainerName)
}

// probeCmd sale con 0 si la app responde, 2 si el contenedor murió, 1 si no respondió en 90 s.
func probeCmd(container, alias string, port int) string {
	return fmt.Sprintf(`i=0
while [ $i -lt 90 ]; do
  out=$(docker exec %[1]s wget -q -T 3 -O /dev/null "http://%[2]s:%[3]d/" 2>&1)
  case "$out" in
    *"can't connect"*|*"bad address"*|*"timed out"*|*"Connection refused"*) ;;
    *) echo "La app responde en el puerto %[3]d"; exit 0 ;;
  esac
  # Con --restart, un proceso que muere vuelve a "running": los reinicios también cuentan como caída.
  [ "$(docker inspect -f '{{.State.Running}} {{.RestartCount}}' %[4]s 2>/dev/null)" = "true 0" ] || exit 2
  [ $i -eq 0 ] && echo "Esperando a que la app escuche en el puerto %[3]d..."
  i=$((i+1)); sleep 1
done
exit 1`, ContainerName, alias, port, container)
}

func ensureError(out string, p Ports) error {
	switch {
	case strings.Contains(out, "privileged port"), strings.Contains(out, "permission denied") && strings.Contains(out, "bind"):
		return fmt.Errorf("el servidor usa Podman sin root y no puede abrir los puertos %d/%d. Conéctalo con el usuario root, "+
			"o permite puertos bajos con: sudo sysctl -w net.ipv4.ip_unprivileged_port_start=80 (y agrégalo a /etc/sysctl.conf)", p.HTTP, p.HTTPS)
	case strings.Contains(out, "address already in use"), strings.Contains(out, "port is already allocated"):
		return fmt.Errorf("los puertos %d/%d del servidor están ocupados (¿hay un nginx o apache instalado?); libéralos o cambia los puertos del proxy en la página del servidor", p.HTTP, p.HTTPS)
	case strings.TrimSpace(out) == "":
		return errors.New("no se pudo iniciar Caddy en el servidor")
	}
	return fmt.Errorf("no se pudo iniciar Caddy: %s", lastLines(out, 4))
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " / ")
}
