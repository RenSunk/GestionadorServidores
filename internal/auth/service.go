package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

const (
	sessionTTL     = 30 * 24 * time.Hour
	sessionRefresh = time.Hour // cada cuánto se extiende una sesión en uso
	inviteTTL      = 7 * 24 * time.Hour
	bcryptCost     = 12

	maxFailures   = 8
	failureWindow = 15 * time.Minute
)

type Service struct {
	store *Store

	mu        sync.Mutex
	setupCode string // vacío cuando ya hay usuarios
	failures  map[string][]time.Time
	dummyHash []byte // para que un email inexistente tarde lo mismo que una contraseña errónea
}

func NewService(ctx context.Context, store *Store) (*Service, error) {
	s := &Service{store: store, failures: map[string][]time.Time{}}
	dummy, err := bcrypt.GenerateFromPassword([]byte("contraseña-que-nadie-usa"), bcryptCost)
	if err != nil {
		return nil, err
	}
	s.dummyHash = dummy

	n, err := store.CountUsers(ctx)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		s.setupCode = randomToken(6)
	}
	return s, nil
}

// SetupCode es el código para crear la primera cuenta desde otra máquina ("" si ya hay usuarios).
func (s *Service) SetupCode() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.setupCode
}

// SetupRequired dice si todavía no hay ninguna cuenta.
func (s *Service) SetupRequired() bool { return s.SetupCode() != "" }

type SetupInput struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
	Code     string `json:"code"`
}

// Setup crea el primer administrador. Desde fuera del propio equipo exige el código del log.
func (s *Service) Setup(ctx context.Context, in SetupInput, local bool) (User, error) {
	code := s.SetupCode()
	if code == "" {
		return User{}, ErrSetupDone
	}
	if !local && subtle.ConstantTimeCompare([]byte(strings.TrimSpace(in.Code)), []byte(code)) != 1 {
		return User{}, ErrBadSetupCode
	}
	u, err := s.newUser(in.Name, in.Email, in.Password, RoleAdmin)
	if err != nil {
		return User{}, err
	}
	if err := s.store.createFirstUser(ctx, u); err != nil {
		return User{}, err
	}
	s.mu.Lock()
	s.setupCode = ""
	s.mu.Unlock()
	slog.Info("primera cuenta creada", "email", u.Email)
	return u, nil
}

func (s *Service) newUser(name, email, password string, role Role) (User, error) {
	var err error
	if name, err = checkName(name); err != nil {
		return User{}, err
	}
	if email, err = normalizeEmail(email); err != nil {
		return User{}, err
	}
	if err := checkPassword(password); err != nil {
		return User{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return User{}, err
	}
	return User{
		ID: uuid.Must(uuid.NewV7()).String(), Email: email, Name: name, Role: role,
		PasswordHash: string(hash), CreatedAt: time.Now().UTC(),
	}, nil
}

// ---------- Login y sesiones ----------

type Client struct {
	IP        string
	UserAgent string
}

// Login verifica la contraseña y abre una sesión. Devuelve el token para la cookie.
func (s *Service) Login(ctx context.Context, email, password string, c Client) (User, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	key := c.IP + "|" + email
	if s.limited(key) {
		return User{}, "", ErrTooManyAttempts
	}
	u, err := s.store.UserByEmail(ctx, email)
	switch {
	case errors.Is(err, ErrUserNotFound):
		_ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(password))
		s.fail(key)
		return User{}, "", ErrInvalidCredentials
	case err != nil:
		return User{}, "", err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil || u.Disabled {
		s.fail(key)
		return User{}, "", ErrInvalidCredentials
	}
	s.clearFailures(key)
	token, err := s.openSession(ctx, u, c)
	return u, token, err
}

func (s *Service) openSession(ctx context.Context, u User, c Client) (string, error) {
	token := randomToken(32)
	now := time.Now().UTC()
	ua := c.UserAgent
	if len(ua) > 300 {
		ua = ua[:300]
	}
	if err := s.store.CreateSession(ctx, session{
		TokenHash: hashToken(token), UserID: u.ID, CreatedAt: now, LastSeenAt: now,
		ExpiresAt: now.Add(sessionTTL), IP: c.IP, UserAgent: ua,
	}); err != nil {
		return "", err
	}
	_ = s.store.TouchLogin(ctx, u.ID, now)
	return token, nil
}

// Authenticate devuelve el usuario de un token de sesión vigente y extiende la sesión si está en uso.
func (s *Service) Authenticate(ctx context.Context, token string) (User, error) {
	if token == "" {
		return User{}, ErrUserNotFound
	}
	now := time.Now().UTC()
	h := hashToken(token)
	u, lastSeen, err := s.store.SessionUser(ctx, h, now)
	if err != nil {
		return User{}, err
	}
	if now.Sub(lastSeen) > sessionRefresh {
		if err := s.store.ExtendSession(ctx, h, now, now.Add(sessionTTL)); err != nil {
			slog.Warn("extender sesión", "err", err)
		}
	}
	return u, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.store.DeleteSession(ctx, hashToken(token))
}

// limited aplica el límite de intentos fallidos por IP+email.
func (s *Service) limited(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	cut := time.Now().Add(-failureWindow)
	recent := s.failures[key][:0]
	for _, t := range s.failures[key] {
		if t.After(cut) {
			recent = append(recent, t)
		}
	}
	if len(recent) == 0 {
		delete(s.failures, key)
	} else {
		s.failures[key] = recent
	}
	return len(recent) >= maxFailures
}

func (s *Service) fail(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.failures) > 10000 { // no crecer sin límite ante un ataque con muchas IPs
		s.failures = map[string][]time.Time{}
	}
	s.failures[key] = append(s.failures[key], time.Now())
}

func (s *Service) clearFailures(key string) {
	s.mu.Lock()
	delete(s.failures, key)
	s.mu.Unlock()
}

// RunCleanup borra sesiones e invitaciones vencidas cada hora.
func (s *Service) RunCleanup(ctx context.Context) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		if err := s.store.DeleteExpired(ctx, time.Now().UTC()); err != nil && ctx.Err() == nil {
			slog.Warn("limpiar sesiones vencidas", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// ---------- Cuenta propia ----------

func (s *Service) UpdateProfile(ctx context.Context, u User, name string) (User, error) {
	n, err := checkName(name)
	if err != nil {
		return User{}, err
	}
	u.Name = n
	return u, s.store.UpdateUser(ctx, u)
}

// ChangePassword cambia la contraseña y cierra las demás sesiones del usuario.
func (s *Service) ChangePassword(ctx context.Context, u User, current, next, keepToken string) error {
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(current)) != nil {
		return ErrWrongPassword
	}
	if err := checkPassword(next); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(next), bcryptCost)
	if err != nil {
		return err
	}
	u.PasswordHash = string(hash)
	if err := s.store.UpdateUser(ctx, u); err != nil {
		return err
	}
	return s.store.DeleteUserSessions(ctx, u.ID, hashToken(keepToken))
}

// ---------- Administración de usuarios ----------

func (s *Service) ListUsers(ctx context.Context) ([]User, error) { return s.store.ListUsers(ctx) }

type UserPatch struct {
	Role     *Role `json:"role"`
	Disabled *bool `json:"disabled"`
}

func (s *Service) UpdateUser(ctx context.Context, actor User, id string, p UserPatch) (User, error) {
	u, err := s.store.GetUser(ctx, id)
	if err != nil {
		return User{}, err
	}
	if id == actor.ID {
		return User{}, ErrSelf // evita quitarse el acceso por accidente
	}
	wasActiveAdmin := u.Role == RoleAdmin && !u.Disabled
	if p.Role != nil {
		if !p.Role.valid() {
			return User{}, invalid("rol inválido")
		}
		u.Role = *p.Role
	}
	if p.Disabled != nil {
		u.Disabled = *p.Disabled
	}
	if wasActiveAdmin && (u.Role != RoleAdmin || u.Disabled) {
		if n, err := s.store.activeAdmins(ctx); err != nil {
			return User{}, err
		} else if n <= 1 {
			return User{}, ErrLastAdmin
		}
	}
	if err := s.store.UpdateUser(ctx, u); err != nil {
		return User{}, err
	}
	if u.Disabled {
		_ = s.store.DeleteUserSessions(ctx, u.ID, "")
	}
	return u, nil
}

func (s *Service) DeleteUser(ctx context.Context, actor User, id string) error {
	if id == actor.ID {
		return ErrSelf
	}
	u, err := s.store.GetUser(ctx, id)
	if err != nil {
		return err
	}
	if u.Role == RoleAdmin && !u.Disabled {
		if n, err := s.store.activeAdmins(ctx); err != nil {
			return err
		} else if n <= 1 {
			return ErrLastAdmin
		}
	}
	return s.store.DeleteUser(ctx, id) // las sesiones se borran en cascada
}

// ---------- Invitaciones ----------

type InviteInput struct {
	Email string `json:"email"`
	Role  Role   `json:"role"`
}

// CreateInvite devuelve la invitación y el token del enlace (solo se muestra una vez).
func (s *Service) CreateInvite(ctx context.Context, actor User, in InviteInput) (Invite, string, error) {
	email, err := normalizeEmail(in.Email)
	if err != nil {
		return Invite{}, "", err
	}
	if !in.Role.valid() {
		return Invite{}, "", invalid("rol inválido")
	}
	if _, err := s.store.UserByEmail(ctx, email); err == nil {
		return Invite{}, "", ErrEmailTaken
	} else if !errors.Is(err, ErrUserNotFound) {
		return Invite{}, "", err
	}
	now := time.Now().UTC()
	inv := Invite{ID: uuid.Must(uuid.NewV7()).String(), Email: email, Role: in.Role, CreatedBy: actor.Name,
		CreatedAt: now, ExpiresAt: now.Add(inviteTTL)}
	token := randomToken(24)
	if err := s.store.CreateInvite(ctx, inv, hashToken(token), actor.ID); err != nil {
		return Invite{}, "", err
	}
	return inv, token, nil
}

func (s *Service) ListInvites(ctx context.Context) ([]Invite, error) {
	return s.store.ListInvites(ctx, time.Now().UTC())
}

func (s *Service) RevokeInvite(ctx context.Context, id string) error {
	ok, err := s.store.DeleteInvite(ctx, id)
	if err == nil && !ok {
		return ErrInviteNotFound
	}
	return err
}

func (s *Service) GetInvite(ctx context.Context, token string) (Invite, error) {
	return s.store.InviteByToken(ctx, hashToken(token), time.Now().UTC())
}

// AcceptInvite crea la cuenta invitada y abre su sesión.
func (s *Service) AcceptInvite(ctx context.Context, token, name, password string, c Client) (User, string, error) {
	inv, err := s.GetInvite(ctx, token)
	if err != nil {
		return User{}, "", err
	}
	u, err := s.newUser(name, inv.Email, password, inv.Role)
	if err != nil {
		return User{}, "", err
	}
	if err := s.store.CreateUser(ctx, u); err != nil {
		return User{}, "", err
	}
	if err := s.store.DeleteInvitesFor(ctx, inv.Email); err != nil {
		return User{}, "", err
	}
	sess, err := s.openSession(ctx, u, c)
	return u, sess, err
}

// ---------- utilidades ----------

func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand no falla en sistemas soportados
	}
	if n <= 8 { // código corto para escribir a mano
		return strings.ToUpper(hex.EncodeToString(b))
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}
