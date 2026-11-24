package gitx

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func tarNames(t *testing.T, data []byte) []string {
	t.Helper()
	var names []string
	tr := tar.NewReader(bytes.NewReader(data))
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag != tar.TypeDir {
			names = append(names, hdr.Name)
		}
	}
	slices.Sort(names)
	return names
}

func TestTarContextRespectsDockerignore(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"Dockerfile":              "FROM scratch",
		".dockerignore":           "node_modules\n*.log\nsecrets/\n!secrets/public.txt\nDockerfile\n",
		"src/index.js":            "x",
		"node_modules/a/index.js": "x",
		"debug.log":               "x",
		"secrets/key.pem":         "x",
		"secrets/public.txt":      "x",
		".git/config":             "x",
		"package.json":            "{}",
	})

	var buf bytes.Buffer
	if err := TarContext(dir, "Dockerfile", &buf); err != nil {
		t.Fatal(err)
	}

	want := []string{".dockerignore", "Dockerfile", "package.json", "secrets/public.txt", "src/index.js"}
	if got := tarNames(t, buf.Bytes()); !slices.Equal(got, want) {
		t.Fatalf("got  %v\nwant %v", got, want)
	}
}

func TestTarContextMissingDockerfile(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"main.go": "package main"})
	if err := TarContext(dir, "Dockerfile", io.Discard); err == nil {
		t.Fatal("esperaba error sin Dockerfile")
	}
}

func TestCleanSubPath(t *testing.T) {
	ok := map[string]string{"": "", ".": "", "./apps/web/": "apps/web", "apps\\api": "apps/api", "docker/Dockerfile.prod": "docker/Dockerfile.prod"}
	for in, want := range ok {
		got, err := CleanSubPath(in)
		if err != nil || got != want {
			t.Errorf("CleanSubPath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"../etc", "/etc/passwd", "a/../../b"} {
		if _, err := CleanSubPath(in); err == nil {
			t.Errorf("CleanSubPath(%q) debería fallar", in)
		}
	}
}

func TestValidateURL(t *testing.T) {
	for _, u := range []string{"https://github.com/a/b.git", "http://gitea.local:3000/a/b"} {
		if err := ValidateURL(u); err != nil {
			t.Errorf("%s: %v", u, err)
		}
	}
	for _, u := range []string{"git@github.com:a/b.git", "https://user:token@github.com/a/b", "ftp://x/y", "nada"} {
		if err := ValidateURL(u); err == nil {
			t.Errorf("%s debería fallar", u)
		}
	}
}
