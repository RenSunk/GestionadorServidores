package buildspec

import (
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Detection es lo que se pudo deducir de los archivos del repo.
type Detection struct {
	HasDockerfile  bool     `json:"hasDockerfile"`
	Runtime        string   `json:"runtime"` // "" = no se detectó
	Framework      string   `json:"framework"`
	Version        string   `json:"version"`
	PackageManager string   `json:"packageManager"`
	BuildCommand   string   `json:"buildCommand"`
	StartCommand   string   `json:"startCommand"`
	Port           int      `json:"port"`
	Notes          []string `json:"notes"`
}

// Detect analiza dir (la carpeta raíz del proyecto dentro del repo).
// dockerfile es la ruta esperada del Dockerfile, relativa a dir.
func Detect(dir, dockerfile string) Detection {
	r := repo{dir: dir}
	d := Detection{HasDockerfile: r.exists(dockerfile)}

	// Orden: un proyecto Java o Python puede traer un package.json para assets;
	// en ese caso el lenguaje principal es el backend.
	switch {
	case r.exists("pom.xml"):
		detectMaven(r, &d)
	case r.exists("build.gradle") || r.exists("build.gradle.kts"):
		detectGradle(r, &d)
	case r.exists("go.mod"):
		detectGo(r, &d)
	case r.exists("requirements.txt") || r.exists("pyproject.toml") || r.exists("Pipfile") || r.exists("manage.py"):
		detectPython(r, &d)
	case r.exists("package.json"):
		detectNode(r, &d)
	}

	if d.Runtime != "" && d.Port == 0 {
		rt, _ := Find(d.Runtime)
		d.Port = rt.DefaultPort
	}
	if d.Notes == nil {
		d.Notes = []string{}
	}
	return d
}

// ---------- Node.js ----------

type packageJSON struct {
	Main            string            `json:"main"`
	Scripts         map[string]string `json:"scripts"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
	Engines         struct {
		Node string `json:"node"`
	} `json:"engines"`
	PackageManager string `json:"packageManager"`
}

func (p packageJSON) has(dep string) bool {
	_, a := p.Dependencies[dep]
	_, b := p.DevDependencies[dep]
	return a || b
}

func detectNode(r repo, d *Detection) {
	d.Runtime = "node"
	var pkg packageJSON
	if err := json.Unmarshal(r.read("package.json"), &pkg); err != nil {
		d.Notes = append(d.Notes, "No se pudo leer package.json; revisa los comandos.")
	}

	// Gestor de paquetes
	pm := "npm"
	switch {
	case strings.HasPrefix(pkg.PackageManager, "pnpm@") || r.exists("pnpm-lock.yaml"):
		pm = "pnpm"
	case strings.HasPrefix(pkg.PackageManager, "yarn@") || r.exists("yarn.lock"):
		pm = "yarn"
	case r.exists("bun.lockb") || r.exists("bun.lock"):
		d.Notes = append(d.Notes, "Bun no está soportado todavía; se usará npm.")
	}
	d.PackageManager = pm

	install := map[string]string{"npm": "npm install", "pnpm": "pnpm install", "yarn": "yarn install"}[pm]
	switch {
	case pm == "npm" && r.exists("package-lock.json"):
		install = "npm ci"
	case pm == "pnpm" && r.exists("pnpm-lock.yaml"):
		install = "pnpm install --frozen-lockfile"
	}
	run := pm + " run "

	d.BuildCommand = install
	if _, ok := pkg.Scripts["build"]; ok {
		d.BuildCommand += " && " + run + "build"
	}

	start := ""
	if _, ok := pkg.Scripts["start"]; ok {
		start = pm + " start"
	}

	switch {
	case pkg.has("next"):
		d.Framework = "Next.js"
		if start == "" {
			start = "npx next start"
		}
	case pkg.has("@nestjs/core"):
		d.Framework = "NestJS"
		if _, ok := pkg.Scripts["start:prod"]; ok {
			start = run + "start:prod"
		} else {
			start = "node dist/main"
		}
	case pkg.has("nuxt"):
		d.Framework = "Nuxt"
		start = "node .output/server/index.mjs"
	case pkg.has("vite") && !pkg.has("express") && !pkg.has("fastify"):
		d.Framework = "Vite (sitio estático)"
		start = "npx --yes serve -s dist -l $PORT"
	case pkg.has("express"):
		d.Framework = "Express"
	case pkg.has("fastify"):
		d.Framework = "Fastify"
	case pkg.has("hono"):
		d.Framework = "Hono"
	default:
		d.Framework = "Node.js"
	}

	if start == "" {
		entry := pkg.Main
		for _, f := range []string{"index.js", "server.js", "app.js", "main.js"} {
			if entry == "" && r.exists(f) {
				entry = f
			}
		}
		if entry == "" {
			entry = "index.js"
			d.Notes = append(d.Notes, "No se encontró un script \"start\"; revisa el comando de inicio.")
		}
		start = "node " + entry
	}
	d.StartCommand = start
	d.Version = pickVersion(firstNumber(pkg.Engines.Node), []string{"20", "22", "24"}, "22")
}

// ---------- Python ----------

func detectPython(r repo, d *Detection) {
	d.Runtime = "python"
	deps := strings.ToLower(string(r.read("requirements.txt")) + "\n" + string(r.read("pyproject.toml")) + "\n" + string(r.read("Pipfile")))
	has := func(pkg string) bool {
		return regexp.MustCompile(`(?m)(^|["'\s])` + regexp.QuoteMeta(pkg) + `([\s=<>~!\[;"',]|$)`).MatchString(deps)
	}

	install := ""
	switch {
	case r.exists("requirements.txt"):
		install = "pip install -r requirements.txt"
	case r.exists("Pipfile"):
		install = "pip install pipenv && pipenv install --system --deploy"
	case r.exists("pyproject.toml"):
		install = "pip install ."
	}
	ensure := func(pkg string) {
		if !has(pkg) {
			install = joinCmd(install, "pip install "+pkg)
		}
	}

	switch {
	case r.exists("manage.py") || has("django"):
		d.Framework = "Django"
		mod := r.findWSGI()
		if mod == "" {
			mod = "config.wsgi"
			d.Notes = append(d.Notes, "No se encontró wsgi.py; ajusta el módulo en el comando de inicio.")
		}
		ensure("gunicorn")
		d.StartCommand = "gunicorn " + mod + ":application --bind 0.0.0.0:$PORT"
	case has("fastapi"):
		d.Framework = "FastAPI"
		ensure("uvicorn")
		d.StartCommand = "uvicorn " + r.pythonModule("main.py", "app/main.py", "src/main.py", "app.py") + ":app --host 0.0.0.0 --port $PORT"
	case has("flask"):
		d.Framework = "Flask"
		ensure("gunicorn")
		d.StartCommand = "gunicorn " + r.pythonModule("app.py", "wsgi.py", "main.py", "app/__init__.py") + ":app --bind 0.0.0.0:$PORT"
	default:
		d.Framework = "Python"
		entry := "main.py"
		if !r.exists(entry) && r.exists("app.py") {
			entry = "app.py"
		}
		d.StartCommand = "python " + entry
	}
	d.BuildCommand = install

	v := ""
	if b := r.read(".python-version"); len(b) > 0 {
		v = majorMinor(string(b))
	} else if b := r.read("runtime.txt"); len(b) > 0 { // "python-3.11.4"
		v = majorMinor(strings.TrimPrefix(strings.TrimSpace(string(b)), "python-"))
	}
	d.Version = pickVersion(v, []string{"3.11", "3.12", "3.13"}, "3.12")
}

// ---------- Java ----------

var (
	mavenJavaRe  = regexp.MustCompile(`<(?:java\.version|maven\.compiler\.release|maven\.compiler\.source)>\s*(?:1\.)?(\d+)`)
	gradleJavaRe = regexp.MustCompile(`(?:JavaLanguageVersion\.of\(\s*|VERSION_(?:1_)?|sourceCompatibility\s*=\s*['"]?)(\d+)`)
)

func detectMaven(r repo, d *Detection) {
	pom := string(r.read("pom.xml"))
	d.Runtime = "java-maven"
	d.Framework = javaFramework(pom, "Maven")
	d.BuildCommand = "mvn -B -DskipTests package"
	d.StartCommand = "java -Dserver.port=$PORT -jar $(ls target/*.jar | grep -v original | head -n 1)"
	d.Version = javaVersion(mavenJavaRe.FindStringSubmatch(pom))
}

func detectGradle(r repo, d *Detection) {
	file := string(r.read("build.gradle")) + string(r.read("build.gradle.kts"))
	d.Runtime = "java-gradle"
	d.Framework = javaFramework(file, "Gradle")
	d.BuildCommand = "gradle build -x test --no-daemon"
	if r.exists("gradlew") {
		d.BuildCommand = "chmod +x gradlew && ./gradlew build -x test --no-daemon"
	}
	d.StartCommand = "java -Dserver.port=$PORT -jar $(ls build/libs/*.jar | grep -v plain | head -n 1)"
	d.Version = javaVersion(gradleJavaRe.FindStringSubmatch(file))
}

func javaFramework(buildFile, tool string) string {
	if strings.Contains(buildFile, "spring-boot") {
		return "Spring Boot (" + tool + ")"
	}
	return "Java (" + tool + ")"
}

// javaVersion lleva la versión del proyecto a la LTS más cercana igual o superior.
func javaVersion(m []string) string {
	if len(m) < 2 {
		return "21"
	}
	v, _ := strconv.Atoi(m[1])
	switch {
	case v <= 17:
		return "17"
	case v <= 21:
		return "21"
	default:
		return "25"
	}
}

// ---------- Go ----------

var goVersionRe = regexp.MustCompile(`(?m)^go\s+(\d+\.\d+)`)

func detectGo(r repo, d *Detection) {
	d.Runtime = "go"
	d.Framework = "Go"
	// La directiva "go" de go.mod es la versión MÍNIMA. Go es compatible hacia atrás,
	// así que usamos la más reciente salvo que el proyecto pida una todavía más nueva.
	d.Version = "1.27"
	if m := goVersionRe.FindStringSubmatch(string(r.read("go.mod"))); len(m) == 2 && newerVersion(m[1], d.Version) {
		d.Version = m[1]
	}

	pkg := "."
	if !r.exists("main.go") {
		if matches, _ := filepath.Glob(filepath.Join(r.dir, "cmd", "*", "main.go")); len(matches) > 0 {
			rel, _ := filepath.Rel(r.dir, filepath.Dir(matches[0]))
			pkg = "./" + filepath.ToSlash(rel)
		} else {
			d.Notes = append(d.Notes, "No se encontró main.go; ajusta el paquete en el comando de build.")
		}
	}
	d.BuildCommand = "go build -o /app/server " + pkg
	d.StartCommand = "/app/server"
}

// ---------- utilidades ----------

type repo struct{ dir string }

func (r repo) exists(name string) bool {
	if name == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(r.dir, filepath.FromSlash(name)))
	return err == nil && !info.IsDir()
}

func (r repo) read(name string) []byte {
	b, err := os.ReadFile(filepath.Join(r.dir, filepath.FromSlash(name)))
	if err != nil || len(b) > 1<<20 {
		return nil
	}
	return b
}

// findWSGI busca <paquete>/wsgi.py (hasta 2 niveles) y devuelve "paquete.wsgi".
func (r repo) findWSGI() string {
	found := ""
	_ = filepath.WalkDir(r.dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil || found != "" {
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(r.dir, p)
		rel = filepath.ToSlash(rel)
		if e.IsDir() && (strings.Count(rel, "/") >= 2 || strings.HasPrefix(e.Name(), ".") || e.Name() == "node_modules" || e.Name() == "venv") && rel != "." {
			return filepath.SkipDir
		}
		if !e.IsDir() && e.Name() == "wsgi.py" && strings.Contains(rel, "/") {
			found = strings.ReplaceAll(strings.TrimSuffix(rel, ".py"), "/", ".")
		}
		return nil
	})
	return found
}

// pythonModule devuelve el módulo del primer archivo que exista ("app/main.py" → "app.main").
func (r repo) pythonModule(candidates ...string) string {
	for _, c := range candidates {
		if r.exists(c) {
			mod := strings.TrimSuffix(strings.TrimSuffix(c, ".py"), "/__init__")
			return strings.ReplaceAll(path.Clean(mod), "/", ".")
		}
	}
	return strings.TrimSuffix(candidates[0], ".py")
}

var numberRe = regexp.MustCompile(`\d+`)

func firstNumber(s string) string { return numberRe.FindString(s) }

var majorMinorRe = regexp.MustCompile(`\d+\.\d+`)

func majorMinor(s string) string { return majorMinorRe.FindString(s) }

// pickVersion usa la detectada si está entre las soportadas; si no, la por defecto.
func pickVersion(detected string, supported []string, def string) string {
	for _, v := range supported {
		if v == detected {
			return v
		}
	}
	return def
}

// newerVersion indica si a > b comparando "mayor.menor".
func newerVersion(a, b string) bool {
	pa, pb := strings.SplitN(a, ".", 2), strings.SplitN(b, ".", 2)
	for i := range 2 {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			return x > y
		}
	}
	return false
}

func joinCmd(a, b string) string {
	if a == "" {
		return b
	}
	return a + " && " + b
}
