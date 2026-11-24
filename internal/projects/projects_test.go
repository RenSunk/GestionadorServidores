package projects

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Mi API":                "mi-api",
		"  Producción / Web  ":  "produccion-web",
		"next.js__app":          "next-js-app",
		"---":                   "",
		strings.Repeat("a", 50): strings.Repeat("a", 40),
		"Año 2026 ñandú":        "ano-2026-nandu",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLogBufferFollow(t *testing.T) {
	b := newLogBuffer()
	b.Step("Clonando")
	fmt.Fprint(b, "parcial")
	lines, cursor, done, changed := b.Since(0)
	if len(lines) != 1 || lines[0] != "==> Clonando" || done {
		t.Fatalf("lines=%q done=%v", lines, done)
	}

	// Una escritura nueva despierta al lector.
	go func() { fmt.Fprint(b, " y fin\n50%\r100%\n") }()
	select {
	case <-changed:
	case <-time.After(time.Second):
		t.Fatal("el lector no fue notificado")
	}

	b.Close()
	lines, _, done, _ = b.Since(cursor)
	if want := []string{"parcial y fin", "100%"}; strings.Join(lines, "|") != strings.Join(want, "|") || !done {
		t.Fatalf("lines=%q done=%v", lines, done)
	}
}

func TestLogBufferDropsOldLines(t *testing.T) {
	b := newLogBuffer()
	line := strings.Repeat("x", 1023) + "\n"
	for range 3000 { // ~3 MB > límite de 2 MB
		fmt.Fprint(b, line)
	}
	b.Close()
	if !strings.HasPrefix(b.String(), "[... ") {
		t.Fatal("debería indicar líneas omitidas")
	}
	lines, next, _, _ := b.Since(0)
	if next != 3000 || len(lines) >= 3000 {
		t.Fatalf("next=%d len=%d", next, len(lines))
	}
}

func TestParseStatus(t *testing.T) {
	out := "running|2026-11-24T17:00:00.123456789Z|0|dep-1\n0.52%|45.3MiB / 7.6GiB|0.58%|1.2kB / 3.4kB\n"
	st := parseStatus(out)
	if st.State != "running" || st.DeploymentID != "dep-1" || st.CPUPercent != 0.52 ||
		st.MemoryPercent != 0.58 || st.MemoryUsage != "45.3MiB / 7.6GiB" || st.StartedAt == nil {
		t.Fatalf("%+v", st)
	}
	if parseStatus("missing\n").State != "missing" {
		t.Fatal("esperaba missing")
	}
}

func TestNormalizeInput(t *testing.T) {
	port := 8080
	in := CreateInput{Name: "api", ServerID: "s1", RepoURL: "https://github.com/a/b", ContainerPort: 3000, HostPort: &port, RootDir: "./apps/api/"}
	if err := in.normalize(); err != nil {
		t.Fatal(err)
	}
	if in.Branch != "main" || in.Dockerfile != "Dockerfile" || in.RootDir != "apps/api" {
		t.Fatalf("%+v", in)
	}

	bad := []CreateInput{
		{Name: "", ServerID: "s", RepoURL: "https://x/y", ContainerPort: 80},
		{Name: "a", ServerID: "s", RepoURL: "git@github.com:a/b", ContainerPort: 80},
		{Name: "a", ServerID: "s", RepoURL: "https://x/y", ContainerPort: 0},
		{Name: "a", ServerID: "s", RepoURL: "https://x/y", ContainerPort: 80, RootDir: "../x"},
		{Name: "a", ServerID: "", RepoURL: "https://x/y", ContainerPort: 80},
	}
	for _, b := range bad {
		if err := b.normalize(); err == nil {
			t.Errorf("esperaba error: %+v", b)
		}
	}
}

func TestNetworkFailure(t *testing.T) {
	if !networkFailure("#8 5.024 npm error code ECONNRESET\n#8 ERROR: process ...") {
		t.Error("ECONNRESET de npm debería detectarse")
	}
	if !networkFailure(`read "https://proxy.golang.org/x.zip": stream error: stream ID 97; INTERNAL_ERROR`) {
		t.Error("el corte del proxy de Go debería detectarse")
	}
	if networkFailure("src/app.ts(3,1): error TS2304: Cannot find name 'foo'") {
		t.Error("un error de compilación no es de red")
	}
}

func TestNormalizeEnv(t *testing.T) {
	got, err := normalizeEnv([]EnvVar{{Key: " Z_KEY ", Value: "a=b c"}, {}, {Key: "API_URL", Value: "https://x", Build: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Key != "API_URL" || got[1].Key != "Z_KEY" || got[1].Value != "a=b c" {
		t.Fatalf("%+v", got)
	}
	for _, bad := range [][]EnvVar{
		{{Key: "1ABC", Value: "x"}},
		{{Key: "A-B", Value: "x"}},
		{{Key: "PORT", Value: "80"}},
		{{Key: "A", Value: "1"}, {Key: "A", Value: "2"}},
		{{Key: "HOME", Value: "/x"}},
		{{Key: "DOCKER_HOST", Value: "tcp://evil"}},
	} {
		if _, err := normalizeEnv(bad); err == nil {
			t.Errorf("esperaba error: %+v", bad)
		}
	}
}

func TestRunCmdKeepsSecretsOffCommandLine(t *testing.T) {
	cpu, mem, hp := 1500, 512, 8080
	p := Project{ID: "p1", Slug: "api", ContainerPort: 3000, HostPort: &hp, CPULimitMilli: &cpu, MemoryLimitMB: &mem}
	cmd := runCmd(p, "d1", "gestionador/api:d1", []string{"DB_PASS"})
	for _, want := range []string{". /dev/stdin && docker run -d", "-e DB_PASS ", "-e PORT=3000", "-p 8080:3000", "--cpus 1.500", "--memory 512m --memory-swap 512m"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("falta %q en %s", want, cmd)
		}
	}
	if strings.Contains(cmd, "mktemp") || strings.Contains(cmd, "/tmp") {
		t.Errorf("no debe usar archivos temporales (snap tiene /tmp privado): %s", cmd)
	}
	// El script se carga en un shell real: el valor debe llegar sin expandirse ni cortarse.
	tricky := "s3cr3t 'x' \"y\" $HOME `id` \\n\nsegunda línea"
	sh := exec.Command("sh", "-c", `. /dev/stdin && printf %s "$DB_PASS"`)
	sh.Stdin = strings.NewReader(envScript([]EnvVar{{Key: "DB_PASS", Value: tricky}}))
	out, err := sh.Output()
	if err != nil || string(out) != tricky {
		t.Errorf("envScript: got %q (%v), want %q", out, err, tricky)
	}

	build := buildCmd(p, "img", "Dockerfile", []EnvVar{{Key: "NEXT_PUBLIC_API", Value: "https://a b"}})
	if !strings.Contains(build, `--build-arg 'NEXT_PUBLIC_API=https://a b' -`) {
		t.Errorf("build args mal escapados: %s", build)
	}
}

func TestParseStatusImages(t *testing.T) {
	st := parseStatus("images:d3 d2 d1 \nmissing\n")
	if st.State != "missing" || len(st.Images) != 3 || st.Images[0] != "d3" {
		t.Fatalf("%+v", st)
	}
	st = parseStatus("images:\nrunning|2026-01-01T00:00:00Z|0|d3\n1.5%|10MiB / 1GiB|1%|1kB / 2kB\n")
	if st.State != "running" || st.DeploymentID != "d3" || len(st.Images) != 0 || st.MemoryUsage != "10MiB / 1GiB" {
		t.Fatalf("%+v", st)
	}
}

func TestLimits(t *testing.T) {
	cpu, mem := 0.25, 256
	c, m, err := UpdateInput{CPULimit: &cpu, MemoryLimitMB: &mem}.limits()
	if err != nil || *c != 250 || *m != 256 {
		t.Fatalf("%v %v %v", c, m, err)
	}
	if formatCPU(250) != "0.25" || formatCPU(2000) != "2" {
		t.Errorf("formatCPU: %s %s", formatCPU(250), formatCPU(2000))
	}
	tiny := 8
	if _, _, err := (UpdateInput{MemoryLimitMB: &tiny}).limits(); err == nil {
		t.Error("8 MB debería rechazarse")
	}
}
