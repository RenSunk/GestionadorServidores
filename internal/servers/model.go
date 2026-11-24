// Package servers gestiona los servidores registrados: alta, conexión SSH,
// verificación de Docker y monitoreo de recursos.
package servers

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

type AuthMethod string

const (
	AuthInstanceKey AuthMethod = "instance_key"
	AuthPrivateKey  AuthMethod = "private_key"
	AuthPassword    AuthMethod = "password"
)

type Status string

const (
	StatusPending Status = "pending"
	StatusOnline  Status = "online"
	StatusOffline Status = "offline"
)

type DockerStatus string

const (
	DockerRunning      DockerStatus = "running"       // instalado y el usuario puede usarlo
	DockerNotInstalled DockerStatus = "not_installed" // no existe el comando docker
	DockerNoPermission DockerStatus = "no_permission" // instalado, pero el usuario no está en el grupo docker
	DockerNotRunning   DockerStatus = "not_running"   // instalado, pero el daemon no responde
)

// Server es un servidor tal como se guarda en la base de datos.
type Server struct {
	ID            string
	Name          string
	Host          string
	Port          int
	Username      string
	AuthMethod    AuthMethod
	CredentialEnc string // llave privada o contraseña cifrada; nunca sale por la API
	HostKey       string
	System        SystemInfo
	Status        Status
	LastError     string
	LastSeenAt    *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// SystemInfo cambia poco; se guarda en la base para mostrarla aunque el servidor esté caído.
type SystemInfo struct {
	OS            string       `json:"os"`
	Kernel        string       `json:"kernel"`
	Arch          string       `json:"arch"`
	Hostname      string       `json:"hostname"`
	CPUCores      int          `json:"cpuCores"`
	DockerStatus  DockerStatus `json:"dockerStatus"`
	DockerVersion string       `json:"dockerVersion"`
}

// Metrics es una muestra puntual de recursos. Solo vive en memoria.
type Metrics struct {
	CollectedAt   time.Time  `json:"collectedAt"`
	CPUPercent    float64    `json:"cpuPercent"`
	Load          [3]float64 `json:"load"`
	Memory        Usage      `json:"memory"`
	Swap          Usage      `json:"swap"`
	Disk          Usage      `json:"disk"`
	UptimeSeconds uint64     `json:"uptimeSeconds"`
}

// Usage en bytes.
type Usage struct {
	Total uint64 `json:"total"`
	Used  uint64 `json:"used"`
}

// CreateInput es el body de POST /api/servers.
type CreateInput struct {
	Name       string     `json:"name"`
	Host       string     `json:"host"`
	Port       int        `json:"port"`
	Username   string     `json:"username"`
	AuthMethod AuthMethod `json:"authMethod"`
	PrivateKey string     `json:"privateKey"`
	Password   string     `json:"password"`
}

// ValidationError se devuelve al cliente como 400 con su mensaje.
type ValidationError struct{ msg string }

func (e *ValidationError) Error() string { return e.msg }

func invalid(format string, args ...any) error {
	return &ValidationError{msg: fmt.Sprintf(format, args...)}
}

func (in *CreateInput) normalize() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Host = strings.TrimSpace(in.Host)
	in.Username = strings.TrimSpace(in.Username)
	if in.Port == 0 {
		in.Port = 22
	}

	switch {
	case in.Name == "":
		return invalid("el nombre es obligatorio")
	case len(in.Name) > 64:
		return invalid("el nombre no puede superar 64 caracteres")
	case in.Host == "":
		return invalid("el host es obligatorio")
	case strings.ContainsAny(in.Host, " /@") && net.ParseIP(in.Host) == nil:
		return invalid("host inválido: usa una IP o un dominio, sin usuario ni protocolo")
	case in.Port < 1 || in.Port > 65535:
		return invalid("el puerto debe estar entre 1 y 65535")
	case in.Username == "":
		return invalid("el usuario es obligatorio")
	}

	switch in.AuthMethod {
	case AuthInstanceKey:
	case AuthPrivateKey:
		if strings.TrimSpace(in.PrivateKey) == "" {
			return invalid("pega la llave privada")
		}
	case AuthPassword:
		if in.Password == "" {
			return invalid("la contraseña es obligatoria")
		}
	default:
		return invalid("método de autenticación inválido")
	}
	return nil
}

var ErrNotFound = errors.New("servidor no encontrado")
