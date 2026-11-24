// Package projects gestiona los proyectos (apps desplegadas desde Git), sus
// deploys y los logs en vivo.
package projects

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gestionador/internal/buildspec"
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
	BuildType     BuildType
	Build         buildspec.Spec // solo si BuildType == BuildRuntime (Port = ContainerPort)
	EnvEnc        string         // JSON cifrado de []EnvVar; nunca sale por la API así
	CPULimitMilli *int           // nil = sin límite
	MemoryLimitMB *int           // nil = sin límite
	// Último cambio de configuración que requiere reconstruir la imagen / recrear el contenedor.
	BuildConfigUpdatedAt   *time.Time
	RuntimeConfigUpdatedAt *time.Time
	CreatedAt              time.Time
	UpdatedAt              time.Time

	// Solo lectura (JOIN con servers).
	ServerName string
	ServerHost string
}

type BuildType string

const (
	BuildDockerfile BuildType = "dockerfile" // Dockerfile del repositorio
	BuildRuntime    BuildType = "runtime"    // Dockerfile generado (lenguaje + comandos)
)

// generatedDockerfile es el nombre con el que se escribe el Dockerfile generado
// dentro del contexto de build (no choca con uno existente en el repo).
const generatedDockerfile = ".gestionador.Dockerfile"

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
	// SourceDeploymentID: si no está vacío, este deploy reutilizó la imagen de otro
	// (rollback o reinicio) en vez de construir una nueva.
	SourceDeploymentID string     `json:"sourceDeploymentId"`
	Error              string     `json:"error"`
	Log                string     `json:"-"`
	CreatedAt          time.Time  `json:"createdAt"`
	StartedAt          *time.Time `json:"startedAt"`
	FinishedAt         *time.Time `json:"finishedAt"`
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

	BuildType      BuildType `json:"buildType"`
	Runtime        string    `json:"runtime"`
	RuntimeVersion string    `json:"runtimeVersion"`
	BaseImage      string    `json:"baseImage"`
	BuildCommand   string    `json:"buildCommand"`
	StartCommand   string    `json:"startCommand"`
}

// UpdateInput es el body de PUT /api/projects/{id}. El servidor no se puede cambiar
// (el contenedor vive en él); GitToken vacío conserva el actual.
type UpdateInput struct {
	CreateInput
	ClearToken    bool     `json:"clearToken"`
	CPULimit      *float64 `json:"cpuLimit"`      // núcleos, ej. 0.5
	MemoryLimitMB *int     `json:"memoryLimitMb"` // MB
}

const (
	minMemoryMB = 32
	maxMemoryMB = 1 << 20
	maxCPUs     = 256
)

// limits valida los límites y convierte la CPU a milésimas.
func (in UpdateInput) limits() (cpuMilli, memMB *int, err error) {
	if in.CPULimit != nil {
		if *in.CPULimit < 0.01 || *in.CPULimit > maxCPUs {
			return nil, nil, invalid("el límite de CPU debe estar entre 0.01 y %d núcleos", maxCPUs)
		}
		v := int(*in.CPULimit*1000 + 0.5)
		cpuMilli = &v
	}
	if in.MemoryLimitMB != nil {
		if *in.MemoryLimitMB < minMemoryMB || *in.MemoryLimitMB > maxMemoryMB {
			return nil, nil, invalid("el límite de memoria debe ser de al menos %d MB", minMemoryMB)
		}
		v := *in.MemoryLimitMB
		memMB = &v
	}
	return cpuMilli, memMB, nil
}

// spec arma la configuración de build generada a partir del input.
func (in CreateInput) spec() buildspec.Spec {
	return buildspec.Spec{
		Runtime: in.Runtime, Version: in.RuntimeVersion, BaseImage: in.BaseImage,
		BuildCommand: in.BuildCommand, StartCommand: in.StartCommand, Port: in.ContainerPort,
	}
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
	ErrNothingToRestore   = errors.New("este proyecto todavía no tiene un deploy exitoso")
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

	switch in.BuildType {
	case "", BuildDockerfile:
		in.BuildType = BuildDockerfile
		in.Runtime, in.RuntimeVersion, in.BaseImage, in.BuildCommand, in.StartCommand = "", "", "", "", ""
	case BuildRuntime:
		spec := in.spec()
		if err := spec.Normalize(); err != nil {
			return invalid("%s", err.Error())
		}
		in.RuntimeVersion, in.BaseImage, in.BuildCommand, in.StartCommand = spec.Version, spec.BaseImage, spec.BuildCommand, spec.StartCommand
	default:
		return invalid("tipo de build inválido")
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
