package projects

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	q "gestionador/internal/sshx" // q.Quote escapa argumentos de shell
)

// Comandos Docker que se ejecutan por SSH en el servidor. Todo valor que viene
// del usuario pasa por q.Quote; los slugs ya son [a-z0-9-] y los puertos, enteros.

const keepImages = 3 // imágenes por proyecto que se conservan (para rollback en la fase 3)

// buildCmd lee el contexto (tar) por stdin. dockerfile es relativo al contexto.
func buildCmd(p Project, image, dockerfile string) string {
	return fmt.Sprintf("BUILDKIT_PROGRESS=plain docker build --label gestionador.project=%s -t %s -f %s -",
		q.Quote(p.ID), q.Quote(image), q.Quote(dockerfile))
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

func runCmd(p Project, deploymentID, image string) string {
	args := []string{
		"docker run -d",
		"--name " + p.containerName(),
		"--restart unless-stopped",
		"--label gestionador.project=" + q.Quote(p.ID),
		"--label gestionador.deployment=" + q.Quote(deploymentID),
	}
	if p.HostPort != nil {
		args = append(args, fmt.Sprintf("-p %d:%d", *p.HostPort, p.ContainerPort))
	}
	args = append(args, q.Quote(image))
	return strings.Join(args, " ")
}

// inspectCmd imprime "estado reinicios código_salida".
func inspectCmd(p Project) string {
	return fmt.Sprintf("docker inspect -f '{{.State.Status}} {{.RestartCount}} {{.State.ExitCode}}' %s", p.containerName())
}

func failedLogsCmd(p Project) string {
	return fmt.Sprintf("docker logs --tail 50 %s 2>&1", p.containerName())
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
	return fmt.Sprintf(`docker rm -f %s %s >/dev/null 2>&1 || true
docker image ls -q %s | xargs -r docker rmi -f >/dev/null 2>&1 || true`,
		p.containerName(), p.containerName()+"-prev", p.imageRepo())
}

func logsCmd(p Project, tail int) string {
	return fmt.Sprintf("docker logs -f --timestamps --tail %d %s 2>&1", tail, p.containerName())
}

// statusCmd imprime el estado del contenedor y, si corre, su consumo.
func statusCmd(p Project) string {
	name := p.containerName()
	return fmt.Sprintf(`if ! docker container inspect %[1]s >/dev/null 2>&1; then echo missing; exit 0; fi
docker inspect -f '{{.State.Status}}|{{.State.StartedAt}}|{{.RestartCount}}|{{index .Config.Labels "gestionador.deployment"}}' %[1]s
docker stats --no-stream --format '{{.CPUPerc}}|{{.MemUsage}}|{{.MemPerc}}|{{.NetIO}}' %[1]s 2>/dev/null || true`, name)
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
}

func parseStatus(out string) ContainerStatus {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) == 0 || lines[0] == "missing" || lines[0] == "" {
		return ContainerStatus{State: "missing"}
	}

	var st ContainerStatus
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
