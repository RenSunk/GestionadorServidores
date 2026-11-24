// Package projects gestiona los proyectos (apps desplegadas desde Git), sus
// deploys y los logs en vivo.
package projects

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gestionador/internal/gitx"
)

type Project struct {
	ID            string
	Name          string
	Slug          string
	ServerID      string
	RepoURL       string
	Branch        string
	GitUsername   string
	GitTokenEnc   string // nunca sale por la API
	RootDir       string
	Dockerfile    string
	ContainerPort int
	HostPort      *int
	CreatedAt     time.Time
	UpdatedAt     time.Time

	// Solo lectura (JOIN con servers).
	ServerName string
	ServerHost string
}

func (p Project) containerName() string { return "gs-" + p.Slug }
func (p Project) imageRepo() string     { return "gestionador/" + p.Slug }

type DeploymentStatus string

const (
	StatusQueued    DeploymentStatus = "queued"
	StatusBuilding  DeploymentStatus = "building"
	StatusDeploying DeploymentStatus = "deploying"
	StatusSucceeded DeploymentStatus = "succeeded"
	StatusFailed    DeploymentStatus = "failed"
	StatusCanceled  DeploymentStatus = "canceled"
)

type Deployment struct {
	ID            string           `json:"id"`
	ProjectID     string           `json:"projectId"`
	Status        DeploymentStatus `json:"status"`
	CommitSHA     string           `json:"commitSha"`
	CommitMessage string           `json:"commitMessage"`
	Image         string           `json:"image"`
	Error         string           `json:"error"`
	Log           string           `json:"-"`
	CreatedAt     time.Time        `json:"createdAt"`
	StartedAt     *time.Time       `json:"startedAt"`
	FinishedAt    *time.Time       `json:"finishedAt"`
}

// CreateInput es el body de POST /api/projects.
type CreateInput struct {
	Name          string `json:"name"`
	ServerID      string `json:"serverId"`
	RepoURL       string `json:"repoUrl"`
	Branch        string `json:"branch"`
	GitUsername   string `json:"gitUsername"`
	GitToken      string `json:"gitToken"`
	RootDir       string `json:"rootDir"`
	Dockerfile    string `json:"dockerfile"`
	ContainerPort int    `json:"containerPort"`
	HostPort      *int   `json:"hostPort"`
}

// ValidationError se devuelve al cliente como 400.
type ValidationError struct{ msg string }

func (e *ValidationError) Error() string { return e.msg }

func invalid(format string, args ...any) error {
	return &ValidationError{msg: fmt.Sprintf(format, args...)}
}

// RepoError: el repositorio no es accesible (422).
type RepoError struct{ Err error }

func (e *RepoError) Error() string { return e.Err.Error() }

var (
	ErrNotFound           = errors.New("proyecto no encontrado")
	ErrDeploymentNotFound = errors.New("deploy no encontrado")
	ErrDuplicateName      = errors.New("ya existe un proyecto con ese nombre")
	ErrDeployInProgress   = errors.New("ya hay un deploy en curso para este proyecto")
	ErrNotRunning         = errors.New("el deploy ya terminó")
	ErrTooLateToCancel    = errors.New("el deploy ya está publicando el contenedor; no se puede cancelar en este punto")
)

// PortInUseError: otro proyecto ya publica ese puerto en el mismo servidor.
type PortInUseError struct{ Port int }

func (e *PortInUseError) Error() string {
	return fmt.Sprintf("el puerto %d ya lo usa otro proyecto en ese servidor", e.Port)
}

func (in *CreateInput) normalize() error {
	in.Name = strings.TrimSpace(in.Name)
	in.RepoURL = strings.TrimSpace(in.RepoURL)
	in.Branch = strings.TrimSpace(in.Branch)
	in.GitUsername = strings.TrimSpace(in.GitUsername)
	in.GitToken = strings.TrimSpace(in.GitToken)
	if in.Branch == "" {
		in.Branch = "main"
	}

	switch {
	case in.Name == "":
		return invalid("el nombre es obligatorio")
	case len(in.Name) > 64:
		return invalid("el nombre no puede superar 64 caracteres")
	case slugify(in.Name) == "":
		return invalid("el nombre debe contener letras o números")
	case in.ServerID == "":
		return invalid("elige un servidor")
	case in.ContainerPort < 1 || in.ContainerPort > 65535:
		return invalid("el puerto del contenedor debe estar entre 1 y 65535")
	case in.HostPort != nil && (*in.HostPort < 1 || *in.HostPort > 65535):
		return invalid("el puerto publicado debe estar entre 1 y 65535")
	}

	if err := gitx.ValidateURL(in.RepoURL); err != nil {
		return invalid("%s", err.Error())
	}

	var err error
	if in.RootDir, err = gitx.CleanSubPath(in.RootDir); err != nil {
		return invalid("carpeta raíz: %s", err.Error())
	}
	if in.Dockerfile == "" {
		in.Dockerfile = "Dockerfile"
	}
	if in.Dockerfile, err = gitx.CleanSubPath(in.Dockerfile); err != nil || in.Dockerfile == "" {
		return invalid("ruta del Dockerfile inválida")
	}
	return nil
}

var accents = strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n")

// slugify genera un nombre válido para contenedores e imágenes Docker: [a-z0-9-].
func slugify(name string) string {
	var b strings.Builder
	dash := true // evita guiones al inicio y repetidos
	for _, r := range accents.Replace(strings.ToLower(name)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	return s
}
