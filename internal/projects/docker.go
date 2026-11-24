package projects

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"gestionador/internal/proxy"
	q "gestionador/internal/sshx" // q.Quote escapa argumentos de shell
)

// Comandos Docker que se ejecutan por SSH en el servidor. Todo valor que viene
// del usuario pasa por q.Quote; los slugs ya son [a-z0-9-] y los puertos, enteros.

const keepImages = 3 // imágenes por proyecto que se conservan (para rollback en la fase 3)

// buildCmd lee el contexto (tar) por stdin. dockerfile es relativo al contexto.
// Las variables de build van como --build-arg (quedan en los metadatos de la imagen).
func buildCmd(p Project, image, dockerfile string, args []EnvVar) string {
	var b strings.Builder
	fmt.Fprintf(&b, "BUILDKIT_PROGRESS=plain docker build --label gestionador.project=%s -t %s -f %s",
		q.Quote(p.ID), q.Quote(image), q.Quote(dockerfile))
	for _, a := range args {
		b.WriteString(" --build-arg " + q.Quote(a.Key+"="+a.Value))
	}
	b.WriteString(" -")
	return b.String()
}

// imageExistsCmd falla si la imagen ya no está en el servidor.
func imageExistsCmd(image string) string {
	return "docker image inspect " + q.Quote(image) + " >/dev/null 2>&1"
}

// stopPreviousCmd detiene el contenedor actual y lo renombra a gs-<slug>-prev
// para poder restaurarlo si el nuevo falla.
func stopPreviousCmd(p Project) string {
	name, prev := p.containerName(), p.containerName()+"-prev"
	return fmt.Sprintf(`docker rm -f %[2]s >/dev/null 2>&1 || true
if docker container inspect %[1]s >/dev/null 2>&1; then
  docker stop -t 20 %[1]s >/dev/null && docker rename %[1]s %[2]s && echo "Contenedor anterior detenido"
else
  echo "No hay contenedor anterior"
fi`, name, prev)
}

// runCmd crea el contenedor. Los valores de las variables llegan por stdin como
// `export K='v'` y se cargan en el entorno del cliente docker; en la línea de comandos
// solo van los nombres (`-e K`), así no se ven en `ps`. No se usan archivos: con Docker
// instalado como snap, /tmp es privado y docker no vería un archivo temporal.
// Todo contenedor entra a la red del proxy con el alias d-<deploy>, así Caddy puede
// apuntar a una versión concreta aunque el contenedor se renombre o reinicie.
func runCmd(p Project, name, deploymentID, image string, envKeys []string) string {
	args := []string{
		fmt.Sprintf("docker network inspect %[1]s >/dev/null 2>&1 || docker network create %[1]s >/dev/null;", proxy.Network),
		". /dev/stdin && docker run -d",
		"--name " + name,
		"--network " + proxy.Network,
		"--network-alias " + upstreamAlias(deploymentID),
		"--restart unless-stopped",
		"--label gestionador.project=" + q.Quote(p.ID),
		"--label gestionador.deployment=" + q.Quote(deploymentID),
	}
	for _, k := range envKeys {
		args = append(args, "-e "+k) // nombres ya validados: [A-Za-z_][A-Za-z0-9_]*
	}
	args = append(args, fmt.Sprintf("-e PORT=%d", p.ContainerPort))
	if p.HostPort != nil {
		args = append(args, fmt.Sprintf("-p %d:%d", *p.HostPort, p.ContainerPort))
	}
	if p.CPULimitMilli != nil {
		args = append(args, fmt.Sprintf("--cpus %d.%03d", *p.CPULimitMilli/1000, *p.CPULimitMilli%1000))
	}
	if p.MemoryLimitMB != nil {
		// Igual que memory: sin swap extra, el límite es real.
		args = append(args, fmt.Sprintf("--memory %[1]dm --memory-swap %[1]dm", *p.MemoryLimitMB))
	}
	args = append(args, q.Quote(image))
	return strings.Join(args, " ")
}

// inspectCmd imprime "estado reinicios código_salida".
func inspectCmd(p Project) string {
	return fmt.Sprintf("docker inspect -f '{{.State.Status}} {{.RestartCount}} {{.State.ExitCode}}' %s", p.containerName())
}

func failedLogsCmd(name string) string {
	// Si docker run falló, el contenedor no existe: no hay logs que mostrar.
	return fmt.Sprintf("if docker container inspect %[1]s >/dev/null 2>&1; then docker logs --tail 50 %[1]s 2>&1; fi", name)
}

// rmAll borra contenedores de a uno: con varios nombres, `podman rm -f` no borra
// ninguno si alguno no existe (y aun así sale con 0).
func rmAll(names ...string) string {
	var b strings.Builder
	for _, n := range names {
		fmt.Fprintf(&b, "docker rm -f %s >/dev/null 2>&1 || true\n", n)
	}
	return b.String()
}

// swapCmd: el tráfico ya va al contenedor -next; se apaga el anterior y -next toma el nombre.
func swapCmd(p Project) string {
	name := p.containerName()
	return fmt.Sprintf(`docker stop -t 20 %[1]s >/dev/null 2>&1
`+rmAll("%[1]s", "%[1]s-prev")+`
docker rename %[1]s-next %[1]s && echo "Contenedor anterior detenido"`, name)
}

// rollbackCmd elimina el contenedor nuevo y restaura el anterior si existía.
func rollbackCmd(p Project) string {
	name, prev := p.containerName(), p.containerName()+"-prev"
	return fmt.Sprintf(`docker rm -f %[1]s >/dev/null 2>&1 || true
if docker container inspect %[2]s >/dev/null 2>&1; then
  docker rename %[2]s %[1]s && docker start %[1]s >/dev/null && echo "Se restauró la versión anterior"
fi`, name, prev)
}

// cleanupCmd borra el contenedor anterior y las imágenes viejas (conserva las últimas keepImages).
func cleanupCmd(p Project) string {
	return fmt.Sprintf(`docker rm -f %s >/dev/null 2>&1 || true
docker image ls %s --format '{{.ID}}' | awk '!seen[$0]++' | tail -n +%d | xargs -r docker rmi >/dev/null 2>&1 || true`,
		p.containerName()+"-prev", p.imageRepo(), keepImages+1)
}

// removeAllCmd elimina contenedores e imágenes del proyecto (al borrarlo).
func removeAllCmd(p Project) string {
	return fmt.Sprintf(rmAll("%s", "%s", "%s-next")+`
docker image ls -q %s | xargs -r docker rmi -f >/dev/null 2>&1 || true`,
		p.containerName(), p.containerName()+"-prev", p.containerName(), p.imageRepo())
}

func logsCmd(p Project, tail int) string {
	return fmt.Sprintf("docker logs -f --timestamps --tail %d %s 2>&1", tail, p.containerName())
}

// statusCmd imprime las imágenes disponibles para rollback, el estado del contenedor
// y, si corre, su consumo.
func statusCmd(p Project) string {
	name := p.containerName()
	return fmt.Sprintf(`echo "images:$(docker image ls %[2]s --format '{{.Tag}}' 2>/dev/null | tr '\n' ' ')"
if ! docker container inspect %[1]s >/dev/null 2>&1; then echo missing; exit 0; fi
docker inspect -f '{{.State.Status}}|{{.State.StartedAt}}|{{.RestartCount}}|{{index .Config.Labels "gestionador.deployment"}}' %[1]s
docker stats --no-stream --format '{{.CPUPerc}}|{{.MemUsage}}|{{.MemPerc}}|{{.NetIO}}' %[1]s 2>/dev/null || true`, name, p.imageRepo())
}

// ContainerStatus es el estado actual del contenedor de un proyecto.
type ContainerStatus struct {
	State         string     `json:"state"` // running, exited, restarting, created, paused, missing
	StartedAt     *time.Time `json:"startedAt"`
	RestartCount  int        `json:"restartCount"`
	DeploymentID  string     `json:"deploymentId"`
	CPUPercent    float64    `json:"cpuPercent"`
	MemoryUsage   string     `json:"memoryUsage"`
	MemoryPercent float64    `json:"memoryPercent"`
	NetIO         string     `json:"netIO"`
	// Images son los IDs de deploy cuya imagen sigue en el servidor (se puede volver a ellos).
	Images []string `json:"images"`
}

func parseStatus(out string) ContainerStatus {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	images := []string{}
	if len(lines) > 0 && strings.HasPrefix(lines[0], "images:") {
		images = strings.Fields(strings.TrimPrefix(lines[0], "images:"))
		lines = lines[1:]
	}
	if len(lines) == 0 || lines[0] == "missing" || lines[0] == "" {
		return ContainerStatus{State: "missing", Images: images}
	}

	st := ContainerStatus{Images: images}
	f := strings.Split(lines[0], "|")
	if len(f) >= 4 {
		st.State = f[0]
		if t, err := time.Parse(time.RFC3339Nano, f[1]); err == nil && t.Year() > 1 {
			st.StartedAt = &t
		}
		st.RestartCount, _ = strconv.Atoi(f[2])
		st.DeploymentID = f[3]
	}
	if len(lines) > 1 {
		if s := strings.Split(lines[1], "|"); len(s) >= 4 {
			st.CPUPercent = parsePercent(s[0])
			st.MemoryUsage = strings.TrimSpace(s[1])
			st.MemoryPercent = parsePercent(s[2])
			st.NetIO = strings.TrimSpace(s[3])
		}
	}
	return st
}

func parsePercent(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(s), "%"), 64)
	return v
}
