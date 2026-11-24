package buildspec

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func repoWith(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestDetect(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  Detection
	}{
		{
			name: "Next.js con pnpm",
			files: map[string]string{
				"package.json":   `{"scripts":{"build":"next build","start":"next start"},"dependencies":{"next":"15"},"engines":{"node":">=20"}}`,
				"pnpm-lock.yaml": "",
			},
			want: Detection{Runtime: "node", Framework: "Next.js", Version: "20", PackageManager: "pnpm",
				BuildCommand: "pnpm install --frozen-lockfile && pnpm run build", StartCommand: "pnpm start", Port: 3000},
		},
		{
			name: "NestJS con npm",
			files: map[string]string{
				"package.json":      `{"scripts":{"build":"nest build","start:prod":"node dist/main"},"dependencies":{"@nestjs/core":"10"}}`,
				"package-lock.json": "{}",
			},
			want: Detection{Runtime: "node", Framework: "NestJS", Version: "22", PackageManager: "npm",
				BuildCommand: "npm ci && npm run build", StartCommand: "npm run start:prod", Port: 3000},
		},
		{
			name:  "Express sin script start",
			files: map[string]string{"package.json": `{"main":"src/server.js","dependencies":{"express":"4"}}`},
			want: Detection{Runtime: "node", Framework: "Express", Version: "22", PackageManager: "npm",
				BuildCommand: "npm install", StartCommand: "node src/server.js", Port: 3000},
		},
		{
			name: "Vite estático",
			files: map[string]string{
				"package.json": `{"scripts":{"build":"vite build"},"devDependencies":{"vite":"6","react":"19"}}`,
				"yarn.lock":    "",
			},
			want: Detection{Runtime: "node", Framework: "Vite (sitio estático)", Version: "22", PackageManager: "yarn",
				BuildCommand: "yarn install && yarn run build", StartCommand: "npx --yes serve -s dist -l $PORT", Port: 3000},
		},
		{
			name: "Django sin gunicorn",
			files: map[string]string{
				"manage.py":          "",
				"requirements.txt":   "Django==5.1\npsycopg[binary]\n",
				"mysite/wsgi.py":     "",
				"mysite/settings.py": "",
				".python-version":    "3.11.9\n",
			},
			want: Detection{Runtime: "python", Framework: "Django", Version: "3.11",
				BuildCommand: "pip install -r requirements.txt && pip install gunicorn",
				StartCommand: "gunicorn mysite.wsgi:application --bind 0.0.0.0:$PORT", Port: 8000},
		},
		{
			name:  "FastAPI en app/main.py",
			files: map[string]string{"requirements.txt": "fastapi\nuvicorn[standard]\n", "app/main.py": ""},
			want: Detection{Runtime: "python", Framework: "FastAPI", Version: "3.12",
				BuildCommand: "pip install -r requirements.txt",
				StartCommand: "uvicorn app.main:app --host 0.0.0.0 --port $PORT", Port: 8000},
		},
		{
			name:  "Spring Boot con Maven",
			files: map[string]string{"pom.xml": "<project><parent><artifactId>spring-boot-starter-parent</artifactId></parent><properties><java.version>17</java.version></properties></project>", "package.json": "{}"},
			want: Detection{Runtime: "java-maven", Framework: "Spring Boot (Maven)", Version: "17",
				BuildCommand: "mvn -B -DskipTests package",
				StartCommand: "java -Dserver.port=$PORT -jar $(ls target/*.jar | grep -v original | head -n 1)", Port: 8080},
		},
		{
			name:  "Gradle con wrapper",
			files: map[string]string{"build.gradle.kts": "java { toolchain { languageVersion = JavaLanguageVersion.of(21) } }", "gradlew": ""},
			want: Detection{Runtime: "java-gradle", Framework: "Java (Gradle)", Version: "21",
				BuildCommand: "chmod +x gradlew && ./gradlew build -x test --no-daemon",
				StartCommand: "java -Dserver.port=$PORT -jar $(ls build/libs/*.jar | grep -v plain | head -n 1)", Port: 8080},
		},
		{
			name:  "Go con cmd/",
			files: map[string]string{"go.mod": "module x\n\ngo 1.14\n", "cmd/api/main.go": "package main"},
			want: Detection{Runtime: "go", Framework: "Go", Version: "1.27",
				BuildCommand: "go build -o /app/server ./cmd/api", StartCommand: "/app/server", Port: 8080},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Detect(repoWith(t, c.files), "Dockerfile")
			got.Notes = nil
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("\ngot  %+v\nwant %+v", got, c.want)
			}
		})
	}
}

func TestDetectUnknownAndDockerfile(t *testing.T) {
	d := Detect(repoWith(t, map[string]string{"Dockerfile": "FROM x", "index.php": ""}), "Dockerfile")
	if !d.HasDockerfile || d.Runtime != "" {
		t.Fatalf("%+v", d)
	}
	d = Detect(repoWith(t, map[string]string{"docker/Dockerfile.prod": "FROM x"}), "docker/Dockerfile.prod")
	if !d.HasDockerfile {
		t.Fatal("debería encontrar el Dockerfile en la ruta configurada")
	}
}

func TestDockerfile(t *testing.T) {
	s := Spec{Runtime: "node", BuildCommand: "npm ci && npm run build", StartCommand: "npm start", Port: 3000}
	if err := s.Normalize(); err != nil {
		t.Fatal(err)
	}
	got := Dockerfile(s)
	for _, want := range []string{
		"FROM node:22-slim\n",
		"RUN corepack enable",
		"COPY . .\nRUN npm ci && npm run build\n",
		"ENV NODE_ENV=production\n",
		"ENV PORT=3000\nEXPOSE 3000\n",
		`CMD ["sh","-c","exec npm start"]`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("falta %q en:\n%s", want, got)
		}
	}
	// NODE_ENV=production debe ir DESPUÉS del build (si no, npm no instala devDependencies).
	if strings.Index(got, "NODE_ENV") < strings.Index(got, "RUN npm ci") {
		t.Error("NODE_ENV debe definirse después del build")
	}

	custom := Spec{Runtime: "custom", BaseImage: "ruby:3.3-slim", StartCommand: `ruby app.rb -p "$PORT"`, Port: 4567}
	if err := custom.Normalize(); err != nil {
		t.Fatal(err)
	}
	if got := Dockerfile(custom); !strings.Contains(got, "FROM ruby:3.3-slim\n") || !strings.Contains(got, `"exec ruby app.rb -p \"$PORT\""`) {
		t.Errorf("custom:\n%s", got)
	}
}

func TestNormalizeRejects(t *testing.T) {
	bad := []Spec{
		{Runtime: "cobol", StartCommand: "x", Port: 80},
		{Runtime: "node", StartCommand: "", Port: 80},
		{Runtime: "node", StartCommand: "npm start\nrm -rf /", Port: 80},
		{Runtime: "node", Version: "22; rm -rf /", StartCommand: "x", Port: 80},
		{Runtime: "custom", BaseImage: "ruby 3\nRUN evil", StartCommand: "x", Port: 80},
		{Runtime: "custom", BaseImage: "", StartCommand: "x", Port: 80},
	}
	for _, s := range bad {
		if err := s.Normalize(); err == nil {
			t.Errorf("esperaba error: %+v", s)
		}
	}
}
