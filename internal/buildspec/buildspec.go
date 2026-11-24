// Package buildspec define los lenguajes soportados sin Dockerfile, detecta el
// lenguaje de un repo por sus archivos y genera el Dockerfile a partir de los
// comandos de build e inicio (al estilo de Render).
package buildspec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Runtime es un lenguaje/entorno disponible para generar el Dockerfile.
type Runtime struct {
	ID               string   `json:"id"`
	Label            string   `json:"label"`
	Versions         []string `json:"versions"` // sugeridas; se acepta cualquier versión válida
	DefaultVersion   string   `json:"defaultVersion"`
	DefaultPort      int      `json:"defaultPort"`
	NeedsBaseImage   bool     `json:"needsBaseImage"` // solo "custom"
	BuildPlaceholder string   `json:"buildPlaceholder"`
	StartPlaceholder string   `json:"startPlaceholder"`

	image      func(version string) string
	buildEnv   []string // antes del build
	runtimeEnv []string // después del build (p. ej. NODE_ENV=production)
	preBuild   []string // instrucciones RUN previas al build
}

var catalog = []Runtime{
	{
		ID: "node", Label: "Node.js", Versions: []string{"20", "22", "24"}, DefaultVersion: "22", DefaultPort: 3000,
		BuildPlaceholder: "npm ci && npm run build", StartPlaceholder: "npm start",
		image: func(v string) string { return "node:" + v + "-slim" },
		// npm y pnpm leen npm_config_*: más reintentos ante cortes de red del registro.
		buildEnv: []string{"COREPACK_ENABLE_DOWNLOAD_PROMPT=0", "NEXT_TELEMETRY_DISABLED=1",
			"npm_config_fetch_retries=5", "npm_config_fetch_retry_mintimeout=20000", "npm_config_fetch_retry_maxtimeout=120000"},
		preBuild:   []string{"corepack enable 2>/dev/null || true"}, // habilita pnpm y yarn
		runtimeEnv: []string{"NODE_ENV=production"},
	},
	{
		ID: "python", Label: "Python", Versions: []string{"3.11", "3.12", "3.13"}, DefaultVersion: "3.12", DefaultPort: 8000,
		BuildPlaceholder: "pip install -r requirements.txt", StartPlaceholder: "gunicorn app:app --bind 0.0.0.0:$PORT",
		image: func(v string) string { return "python:" + v + "-slim" },
		buildEnv: []string{"PYTHONUNBUFFERED=1", "PIP_NO_CACHE_DIR=1", "PIP_DISABLE_PIP_VERSION_CHECK=1",
			"PIP_RETRIES=10", "PIP_DEFAULT_TIMEOUT=60"},
	},
	{
		ID: "java-maven", Label: "Java (Maven)", Versions: []string{"17", "21", "25"}, DefaultVersion: "21", DefaultPort: 8080,
		BuildPlaceholder: "mvn -B -DskipTests package", StartPlaceholder: "java -jar target/app.jar",
		image: func(v string) string { return "maven:3-eclipse-temurin-" + v },
	},
	{
		ID: "java-gradle", Label: "Java (Gradle)", Versions: []string{"17", "21", "25"}, DefaultVersion: "21", DefaultPort: 8080,
		BuildPlaceholder: "gradle build -x test --no-daemon", StartPlaceholder: "java -jar build/libs/app.jar",
		image: func(v string) string { return "gradle:jdk" + v },
	},
	{
		ID: "go", Label: "Go", Versions: []string{"1.25", "1.26", "1.27"}, DefaultVersion: "1.27", DefaultPort: 8080,
		BuildPlaceholder: "go build -o /app/server .", StartPlaceholder: "/app/server",
		image: func(v string) string { return "golang:" + v },
		// Con "|" se descarga directo del repositorio si el proxy falla por cualquier motivo.
		buildEnv: []string{"CGO_ENABLED=0", "GOPROXY=https://proxy.golang.org|direct"},
	},
	{
		ID: "custom", Label: "Otro (imagen base)", NeedsBaseImage: true, DefaultPort: 8080,
		BuildPlaceholder: "bundle install", StartPlaceholder: "bundle exec puma -p $PORT",
	},
}

func Catalog() []Runtime { return catalog }

func Find(id string) (Runtime, bool) {
	for _, r := range catalog {
		if r.ID == id {
			return r, true
		}
	}
	return Runtime{}, false
}

// Spec es la configuración de build de un proyecto sin Dockerfile.
type Spec struct {
	Runtime      string `json:"runtime"`
	Version      string `json:"version"`
	BaseImage    string `json:"baseImage"`
	BuildCommand string `json:"buildCommand"`
	StartCommand string `json:"startCommand"`
	Port         int    `json:"port"`
}

var (
	versionRe = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._-]{0,31}$`)
	imageRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/:@-]{0,254}$`)
)

const maxCommandLen = 2000

// Normalize valida y completa valores por defecto.
func (s *Spec) Normalize() error {
	s.Version = strings.TrimSpace(s.Version)
	s.BaseImage = strings.TrimSpace(s.BaseImage)
	s.BuildCommand = strings.TrimSpace(s.BuildCommand)
	s.StartCommand = strings.TrimSpace(s.StartCommand)

	rt, ok := Find(s.Runtime)
	if !ok {
		return errors.New("elige un lenguaje")
	}
	if rt.NeedsBaseImage {
		s.Version = ""
		if !imageRe.MatchString(s.BaseImage) {
			return errors.New("imagen base inválida (ej. ruby:3.3-slim, php:8.3-cli)")
		}
	} else {
		s.BaseImage = ""
		if s.Version == "" {
			s.Version = rt.DefaultVersion
		}
		if !versionRe.MatchString(s.Version) {
			return errors.New("versión inválida")
		}
	}
	if s.StartCommand == "" {
		return errors.New("el comando de inicio es obligatorio")
	}
	for _, c := range []string{s.BuildCommand, s.StartCommand} {
		if strings.ContainsAny(c, "\r\n") {
			return errors.New("los comandos deben ir en una sola línea (usa && para encadenar)")
		}
		if len(c) > maxCommandLen {
			return fmt.Errorf("los comandos no pueden superar %d caracteres", maxCommandLen)
		}
	}
	if s.Port < 1 || s.Port > 65535 {
		return errors.New("puerto inválido")
	}
	return nil
}

// Image es la imagen base que se usará.
func (s Spec) Image() string {
	rt, _ := Find(s.Runtime)
	if rt.NeedsBaseImage {
		return s.BaseImage
	}
	return rt.image(s.Version)
}

// Label describe el entorno, ej. "Node.js 22".
func (s Spec) Label() string {
	rt, _ := Find(s.Runtime)
	if rt.NeedsBaseImage {
		return s.BaseImage
	}
	return rt.Label + " " + s.Version
}

// Dockerfile genera el Dockerfile. Debe llamarse después de Normalize.
// Es de una sola etapa, igual que Render: el build corre dentro de la imagen
// y el comando de inicio se ejecuta con `exec` para recibir las señales de parada.
func Dockerfile(s Spec) string {
	rt, _ := Find(s.Runtime)
	var b strings.Builder
	fmt.Fprintf(&b, "# Generado por Gestionador (%s)\n", s.Label())
	fmt.Fprintf(&b, "FROM %s\n", s.Image())
	b.WriteString("WORKDIR /app\n")
	writeEnv(&b, rt.buildEnv)
	for _, cmd := range rt.preBuild {
		fmt.Fprintf(&b, "RUN %s\n", cmd)
	}
	b.WriteString("COPY . .\n")
	if s.BuildCommand != "" {
		fmt.Fprintf(&b, "RUN %s\n", s.BuildCommand)
	}
	writeEnv(&b, rt.runtimeEnv)
	fmt.Fprintf(&b, "ENV PORT=%d\n", s.Port)
	fmt.Fprintf(&b, "EXPOSE %d\n", s.Port)
	fmt.Fprintf(&b, "CMD %s\n", jsonArray("sh", "-c", "exec "+s.StartCommand))
	return b.String()
}

func writeEnv(b *strings.Builder, env []string) {
	if len(env) > 0 {
		fmt.Fprintf(b, "ENV %s\n", strings.Join(env, " "))
	}
}

func jsonArray(items ...string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // que && y < se vean tal cual
	_ = enc.Encode(items)
	return strings.TrimSpace(buf.String())
}
