package projects

import (
	"fmt"
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
