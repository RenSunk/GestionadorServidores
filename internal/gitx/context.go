package gitx

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/moby/patternmatcher"
	"github.com/moby/patternmatcher/ignorefile"
)

// CleanSubPath normaliza una ruta relativa dentro del repo y rechaza las que
// intentan salir de él ("../", rutas absolutas).
func CleanSubPath(p string) (string, error) {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	if p == "" {
		return "", nil
	}
	clean := path.Clean(strings.TrimPrefix(p, "./"))
	if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("ruta inválida %q: debe ser relativa al repositorio", p)
	}
	if clean == "." {
		return "", nil
	}
	return clean, nil
}

// TarContext empaqueta dir como contexto de build de Docker, igual que lo haría
// `docker build`: excluye .git y respeta .dockerignore. El Dockerfile y el
// .dockerignore siempre se incluyen. Escribe el tar en w.
func TarContext(dir, dockerfile string, w io.Writer) error {
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(dockerfile))); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("no se encontró %s en el repositorio", dockerfile)
		}
		return err
	}

	pm, err := loadDockerignore(dir)
	if err != nil {
		return err
	}

	tw := tar.NewWriter(w)
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil || rel == "." {
			return err
		}
		rel = filepath.ToSlash(rel)

		if rel == ".git" || strings.HasPrefix(rel, ".git/") {
			return filepath.SkipDir
		}

		if rel != dockerfile && rel != ".dockerignore" && pm != nil {
			excluded, err := pm.MatchesOrParentMatches(rel)
			if err != nil {
				return err
			}
			if excluded {
				// Se puede saltar la carpeta entera solo si no hay excepciones (!patrón)
				// que podrían reincluir algo de adentro.
				if d.IsDir() && !pm.Exclusions() {
					return filepath.SkipDir
				}
				if !d.IsDir() {
					return nil
				}
			}
		}

		return addToTar(tw, p, rel, d)
	})
	if err != nil {
		return err
	}
	return tw.Close()
}

func loadDockerignore(dir string) (*patternmatcher.PatternMatcher, error) {
	f, err := os.Open(filepath.Join(dir, ".dockerignore"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	patterns, err := ignorefile.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf(".dockerignore: %w", err)
	}
	return patternmatcher.New(patterns)
}

func addToTar(tw *tar.Writer, fullPath, rel string, d fs.DirEntry) error {
	info, err := d.Info()
	if err != nil {
		return err
	}

	var link string
	if info.Mode()&fs.ModeSymlink != 0 {
		if link, err = os.Readlink(fullPath); err != nil {
			return err
		}
	}

	hdr, err := tar.FileInfoHeader(info, link)
	if err != nil {
		return err
	}
	hdr.Name = rel
	if info.IsDir() {
		hdr.Name += "/"
	}
	// Builds reproducibles: sin dueños del host local.
	hdr.Uid, hdr.Gid, hdr.Uname, hdr.Gname = 0, 0, "", ""

	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return nil
	}

	f, err := os.Open(fullPath)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(tw, f)
	return err
}
