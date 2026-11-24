// Package auth gestiona usuarios, sesiones, invitaciones y los permisos de cada rol.
package auth

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"
)

type Role string

const (
	RoleAdmin     Role = "admin"     // todo, incluidos servidores y usuarios
	RoleDeveloper Role = "developer" // proyectos: deploys, configuración, variables, dominios
	RoleViewer    Role = "viewer"    // solo lectura, sin secretos
)

// rank ordena los roles: un rol incluye los permisos de los de menor rango.
func (r Role) rank() int {
	switch r {
	case RoleAdmin:
		return 3
	case RoleDeveloper:
		return 2
	case RoleViewer:
		return 1
	}
	return 0
}

// Can dice si el rol alcanza el mínimo pedido.
func (r Role) Can(min Role) bool { return r.rank() >= min.rank() && r.rank() > 0 }

func (r Role) valid() bool { return r.rank() > 0 }

type User struct {
	ID           string     `json:"id"`
	Email        string     `json:"email"`
	Name         string     `json:"name"`
	Role         Role       `json:"role"`
	Disabled     bool       `json:"disabled"`
	CreatedAt    time.Time  `json:"createdAt"`
	LastLoginAt  *time.Time `json:"lastLoginAt"`
	PasswordHash string     `json:"-"`
}

type Invite struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Role      Role      `json:"role"`
	CreatedBy string    `json:"createdBy"` // nombre de quien invitó
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

var (
	ErrInvalidCredentials = errors.New("email o contraseña incorrectos")
	ErrTooManyAttempts    = errors.New("demasiados intentos fallidos; espera unos minutos")
	ErrSetupDone          = errors.New("la instancia ya tiene usuarios")
	ErrBadSetupCode       = errors.New("código de instalación incorrecto; está en el log de Gestionador al arrancar")
	ErrUserNotFound       = errors.New("usuario no encontrado")
	ErrInviteNotFound     = errors.New("la invitación no existe, ya se usó o venció")
	ErrEmailTaken         = errors.New("ya existe un usuario con ese email")
	ErrLastAdmin          = errors.New("debe quedar al menos un administrador activo")
	ErrSelf               = errors.New("no puedes hacer eso con tu propia cuenta")
	ErrWrongPassword      = errors.New("la contraseña actual no es correcta")
)

type ValidationError struct{ msg string }

func (e *ValidationError) Error() string { return e.msg }

func invalid(msg string) error { return &ValidationError{msg} }

const minPasswordLen = 10

func normalizeEmail(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s {
		return "", invalid("email inválido")
	}
	return s, nil
}

func checkPassword(p string) error {
	switch {
	case len(p) < minPasswordLen:
		return invalid("la contraseña debe tener al menos 10 caracteres")
	case len(p) > 72: // límite de bcrypt
		return invalid("la contraseña no puede superar 72 caracteres")
	}
	return nil
}

func checkName(n string) (string, error) {
	n = strings.TrimSpace(n)
	if n == "" || len(n) > 80 {
		return "", invalid("escribe tu nombre (máximo 80 caracteres)")
	}
	return n, nil
}

// ---------- Contexto ----------

type ctxKey struct{}

// WithUser guarda el usuario autenticado en el contexto de la petición.
func WithUser(ctx context.Context, u User) context.Context {
	return context.WithValue(ctx, ctxKey{}, u)
}

// UserFrom devuelve el usuario de la petición (ok=false en rutas públicas).
func UserFrom(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(ctxKey{}).(User)
	return u, ok
}
