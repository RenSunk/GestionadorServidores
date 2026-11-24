// Package gitx clona repositorios con go-git (Git en Go puro: no requiere tener
// git instalado) y empaqueta el contexto de build de Docker.
package gitx

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"
)

// Repo identifica un repositorio y cómo acceder a él.
type Repo struct {
	URL    string
	Branch string
	// Username/Token para repos privados por HTTPS. Con GitHub, GitLab y Gitea
	// basta el token (el usuario puede ser cualquiera); Bitbucket exige el usuario real.
	Username string
	Token    string
}

func (r Repo) auth() transport.AuthMethod {
	if r.Token == "" {
		return nil
	}
	user := r.Username
	if user == "" {
		user = "git"
	}
	return &githttp.BasicAuth{Username: user, Password: r.Token}
}

// ValidateURL acepta solo http(s) sin credenciales embebidas.
func ValidateURL(raw string) error {
	if strings.HasPrefix(raw, "git@") || strings.HasPrefix(raw, "ssh://") {
		return errors.New("por ahora solo se admiten URLs HTTPS (ej. https://github.com/usuario/repo.git)")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return errors.New("URL del repositorio inválida")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return errors.New("usa la URL HTTPS del repositorio (ej. https://github.com/usuario/repo.git)")
	}
	if u.User != nil {
		return errors.New("no incluyas usuario ni token en la URL; usa el campo de token")
	}
	return nil
}

// Check verifica que el repo es accesible y que la rama existe, sin clonar.
func Check(ctx context.Context, r Repo) error {
	remote := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{Name: "origin", URLs: []string{r.URL}})
	refs, err := remote.ListContext(ctx, &git.ListOptions{Auth: r.auth()})
	if err != nil {
		return friendly(err, r.Token != "")
	}
	want := plumbing.NewBranchReferenceName(r.Branch)
	for _, ref := range refs {
		if ref.Name() == want {
			return nil
		}
	}
	return fmt.Errorf("la rama %q no existe en el repositorio", r.Branch)
}

// Commit describe el commit desplegado.
type Commit struct {
	SHA     string
	Message string
}

// Clone hace un clon superficial (solo el último commit) de la rama en dir.
func Clone(ctx context.Context, r Repo, dir string) (Commit, error) {
	repo, err := git.PlainCloneContext(ctx, dir, false, &git.CloneOptions{
		URL:           r.URL,
		Auth:          r.auth(),
		ReferenceName: plumbing.NewBranchReferenceName(r.Branch),
		SingleBranch:  true,
		Depth:         1,
		Tags:          git.NoTags,
	})
	if err != nil {
		var noRef git.NoMatchingRefSpecError
		if errors.Is(err, plumbing.ErrReferenceNotFound) || errors.As(err, &noRef) {
			return Commit{}, fmt.Errorf("la rama %q no existe en el repositorio", r.Branch)
		}
		return Commit{}, friendly(err, r.Token != "")
	}
	head, err := repo.Head()
	if err != nil {
		return Commit{}, err
	}
	c, err := repo.CommitObject(head.Hash())
	if err != nil {
		return Commit{}, err
	}
	msg, _, _ := strings.Cut(strings.TrimSpace(c.Message), "\n")
	return Commit{SHA: head.Hash().String(), Message: msg}, nil
}

func friendly(err error, hasToken bool) error {
	switch {
	case errors.Is(err, transport.ErrAuthenticationRequired), errors.Is(err, transport.ErrAuthorizationFailed):
		// GitHub responde "autenticación requerida" también para repos que no existen.
		if hasToken {
			return errors.New("el token no es válido o no tiene acceso de lectura a este repositorio")
		}
		return errors.New("no se pudo acceder: el repositorio no existe o es privado (si es privado, agrega un token con permiso de lectura)")
	case errors.Is(err, transport.ErrRepositoryNotFound):
		return errors.New("repositorio no encontrado (si es privado, agrega un token con permiso de lectura)")
	case errors.Is(err, transport.ErrEmptyRemoteRepository):
		return errors.New("el repositorio está vacío")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	}
	return fmt.Errorf("git: %w", err)
}
