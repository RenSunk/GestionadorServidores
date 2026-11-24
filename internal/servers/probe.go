package servers

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// probeScript se ejecuta por SSH en cada verificación. Usa solo sh POSIX y /proc,
// así funciona en cualquier Linux (Debian, Ubuntu, RHEL, Alpine...) sin instalar nada.
// Cada sección empieza con una línea "@@nombre".
const probeScript = `
echo '@@os'; (. /etc/os-release 2>/dev/null && echo "$PRETTY_NAME")
echo '@@kernel'; uname -r
echo '@@arch'; uname -m
echo '@@hostname'; hostname 2>/dev/null || cat /etc/hostname
echo '@@nproc'; nproc 2>/dev/null || grep -c ^processor /proc/cpuinfo
echo '@@loadavg'; cat /proc/loadavg
echo '@@uptime'; cat /proc/uptime
echo '@@meminfo'; grep -E '^(MemTotal|MemAvailable|SwapTotal|SwapFree):' /proc/meminfo
echo '@@disk'; df -Pk / | tail -n 1
echo '@@cpu1'; head -n 1 /proc/stat
sleep 1
echo '@@cpu2'; head -n 1 /proc/stat
echo '@@docker'
if ! command -v docker >/dev/null 2>&1; then
  echo not_installed
else
  # stderr aparte: podman-docker imprime "Emulate Docker CLI using podman..." en cada comando.
  out=$(docker version --format '{{.Server.Version}}' 2>/dev/null | tail -n 1)
  if [ -n "$out" ]; then
    if docker --version 2>/dev/null | grep -qi podman; then echo "running podman-$out"; else echo "running $out"; fi
  elif docker version 2>&1 | grep -qi 'permission denied'; then echo no_permission
  else echo not_running
  fi
fi
`

// parseProbe convierte la salida de probeScript en SystemInfo y Metrics.
func parseProbe(out string, now time.Time) (SystemInfo, Metrics, error) {
	sec := splitSections(out)
	var (
		info SystemInfo
		m    = Metrics{CollectedAt: now}
	)

	info.OS = first(sec["os"])
	info.Kernel = first(sec["kernel"])
	info.Arch = first(sec["arch"])
	info.Hostname = first(sec["hostname"])
	info.CPUCores, _ = strconv.Atoi(first(sec["nproc"]))

	if f := strings.Fields(first(sec["loadavg"])); len(f) >= 3 {
		for i := range 3 {
			m.Load[i], _ = strconv.ParseFloat(f[i], 64)
		}
	}

	if f := strings.Fields(first(sec["uptime"])); len(f) >= 1 {
		up, _ := strconv.ParseFloat(f[0], 64)
		m.UptimeSeconds = uint64(up)
	}

	mem := map[string]uint64{}
	for _, line := range sec["meminfo"] {
		f := strings.Fields(line) // "MemTotal:  16314128 kB"
		if len(f) >= 2 {
			v, _ := strconv.ParseUint(f[1], 10, 64)
			mem[strings.TrimSuffix(f[0], ":")] = v * 1024
		}
	}
	if mem["MemTotal"] == 0 {
		return info, m, fmt.Errorf("no se pudo leer /proc/meminfo; ¿es un servidor Linux?")
	}
	m.Memory = Usage{Total: mem["MemTotal"], Used: mem["MemTotal"] - min(mem["MemAvailable"], mem["MemTotal"])}
	m.Swap = Usage{Total: mem["SwapTotal"], Used: mem["SwapTotal"] - min(mem["SwapFree"], mem["SwapTotal"])}

	// df -Pk: Filesystem 1024-blocks Used Available Capacity Mounted
	if f := strings.Fields(first(sec["disk"])); len(f) >= 4 {
		used, _ := strconv.ParseUint(f[2], 10, 64)
		avail, _ := strconv.ParseUint(f[3], 10, 64)
		// Total = usado + disponible: excluye los bloques reservados para root, igual que el % de df.
		m.Disk = Usage{Total: (used + avail) * 1024, Used: used * 1024}
	}

	m.CPUPercent = cpuPercent(first(sec["cpu1"]), first(sec["cpu2"]))

	switch d := strings.Fields(first(sec["docker"])); {
	case len(d) == 0:
	case d[0] == string(DockerRunning):
		info.DockerStatus = DockerRunning
		if len(d) > 1 {
			info.DockerVersion = d[1]
		}
	default:
		info.DockerStatus = DockerStatus(d[0])
	}

	return info, m, nil
}

func splitSections(out string) map[string][]string {
	sections := map[string][]string{}
	current := ""
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if name, ok := strings.CutPrefix(line, "@@"); ok {
			current = name
			continue
		}
		if current != "" && line != "" {
			sections[current] = append(sections[current], line)
		}
	}
	return sections
}

func first(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return lines[0]
}

// cpuPercent calcula el uso de CPU entre dos lecturas de la línea "cpu" de /proc/stat:
// cpu  user nice system idle iowait irq softirq steal ...
func cpuPercent(a, b string) float64 {
	idleA, totalA, okA := parseCPULine(a)
	idleB, totalB, okB := parseCPULine(b)
	if !okA || !okB || totalB <= totalA {
		return 0
	}
	dTotal := float64(totalB - totalA)
	dIdle := float64(idleB - idleA)
	return round1((dTotal - dIdle) / dTotal * 100)
}

func parseCPULine(line string) (idle, total uint64, ok bool) {
	f := strings.Fields(line)
	if len(f) < 5 || f[0] != "cpu" {
		return 0, 0, false
	}
	// Solo los primeros 8 campos: guest/guest_nice ya están incluidos en user/nice.
	for i := 1; i < len(f) && i <= 8; i++ {
		v, err := strconv.ParseUint(f[i], 10, 64)
		if err != nil {
			return 0, 0, false
		}
		total += v
		if i == 4 || i == 5 { // idle + iowait
			idle += v
		}
	}
	return idle, total, true
}

func round1(v float64) float64 {
	return float64(int64(v*10+0.5)) / 10
}
