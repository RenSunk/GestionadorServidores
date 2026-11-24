package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"gestionador/internal/database"
)

type Store struct{ db *database.DB }

func NewStore(db *database.DB) *Store { return &Store{db: db} }

const userColumns = `id, email, name, password_hash, role, disabled, created_at, last_login_at`

func scanUser(row interface{ Scan(...any) error }) (User, error) {
	var (
		u    User
		last sql.NullTime
	)
	err := row.Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.Role, &u.Disabled, &u.CreatedAt, &last)
	if last.Valid {
		t := last.Time
		u.LastLoginAt = &t
	}
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrUserNotFound
	}
	return u, err
}

func (st *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (st *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := st.db.QueryContext(ctx, `SELECT `+userColumns+` FROM users ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (st *Store) GetUser(ctx context.Context, id string) (User, error) {
	return scanUser(st.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id))
}

func (st *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	return scanUser(st.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE email = $1`, email))
}

func (st *Store) CreateUser(ctx context.Context, u User) error {
	_, err := st.db.ExecContext(ctx, `INSERT INTO users (id, email, name, password_hash, role, disabled, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, u.ID, u.Email, u.Name, u.PasswordHash, u.Role, u.Disabled, u.CreatedAt)
	if err != nil && isUnique(err) {
		return ErrEmailTaken
	}
	return err
}

// createFirstUser inserta el usuario solo si la tabla está vacía (dos instalaciones a la vez: gana una).
func (st *Store) createFirstUser(ctx context.Context, u User) error {
	res, err := st.db.ExecContext(ctx, `INSERT INTO users (id, email, name, password_hash, role, disabled, created_at)
		SELECT $1, $2, $3, $4, $5, $6, $7 WHERE NOT EXISTS (SELECT 1 FROM users)`,
		u.ID, u.Email, u.Name, u.PasswordHash, u.Role, u.Disabled, u.CreatedAt)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrSetupDone
	}
	return nil
}

func (st *Store) UpdateUser(ctx context.Context, u User) error {
	res, err := st.db.ExecContext(ctx, `UPDATE users SET name = $2, role = $3, disabled = $4, password_hash = $5 WHERE id = $1`,
		u.ID, u.Name, u.Role, u.Disabled, u.PasswordHash)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (st *Store) TouchLogin(ctx context.Context, id string, at time.Time) error {
	_, err := st.db.ExecContext(ctx, `UPDATE users SET last_login_at = $2 WHERE id = $1`, id, at)
	return err
}

func (st *Store) DeleteUser(ctx context.Context, id string) error {
	res, err := st.db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrUserNotFound
	}
	return nil
}

// activeAdmins cuenta los administradores habilitados.
func (st *Store) activeAdmins(ctx context.Context) (int, error) {
	var n int
	err := st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role = 'admin' AND disabled = $1`, false).Scan(&n)
	return n, err
}

// ---------- Sesiones ----------

type session struct {
	TokenHash  string
	UserID     string
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
	IP         string
	UserAgent  string
}

func (st *Store) CreateSession(ctx context.Context, s session) error {
	_, err := st.db.ExecContext(ctx, `INSERT INTO sessions (token_hash, user_id, created_at, last_seen_at, expires_at, ip, user_agent)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, s.TokenHash, s.UserID, s.CreatedAt, s.LastSeenAt, s.ExpiresAt, s.IP, s.UserAgent)
	return err
}

// SessionUser devuelve el usuario de una sesión vigente y cuándo se vio por última vez.
func (st *Store) SessionUser(ctx context.Context, tokenHash string, now time.Time) (User, time.Time, error) {
	var (
		lastSeen time.Time
		u        User
		last     sql.NullTime
	)
	err := st.db.QueryRowContext(ctx, `SELECT u.id, u.email, u.name, u.password_hash, u.role, u.disabled, u.created_at, u.last_login_at, s.last_seen_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.expires_at > $2 AND u.disabled = $3`, tokenHash, now, false).
		Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.Role, &u.Disabled, &u.CreatedAt, &last, &lastSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, time.Time{}, ErrUserNotFound
	}
	if last.Valid {
		t := last.Time
		u.LastLoginAt = &t
	}
	return u, lastSeen, err
}

func (st *Store) ExtendSession(ctx context.Context, tokenHash string, now, expires time.Time) error {
	_, err := st.db.ExecContext(ctx, `UPDATE sessions SET last_seen_at = $2, expires_at = $3 WHERE token_hash = $1`, tokenHash, now, expires)
	return err
}

func (st *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := st.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
	return err
}

// DeleteUserSessions cierra las sesiones de un usuario, salvo keep (puede ser "").
func (st *Store) DeleteUserSessions(ctx context.Context, userID, keep string) error {
	_, err := st.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = $1 AND token_hash <> $2`, userID, keep)
	return err
}

func (st *Store) DeleteExpired(ctx context.Context, now time.Time) error {
	if _, err := st.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= $1`, now); err != nil {
		return err
	}
	_, err := st.db.ExecContext(ctx, `DELETE FROM invites WHERE expires_at <= $1`, now)
	return err
}

// ---------- Invitaciones ----------

// CreateInvite reemplaza cualquier invitación pendiente para el mismo email.
func (st *Store) CreateInvite(ctx context.Context, inv Invite, tokenHash, createdBy string) error {
	if err := st.DeleteInvitesFor(ctx, inv.Email); err != nil {
		return err
	}
	_, err := st.db.ExecContext(ctx, `INSERT INTO invites (id, token_hash, email, role, created_by, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, inv.ID, tokenHash, inv.Email, inv.Role, createdBy, inv.CreatedAt, inv.ExpiresAt)
	return err
}

func (st *Store) ListInvites(ctx context.Context, now time.Time) ([]Invite, error) {
	rows, err := st.db.QueryContext(ctx, `SELECT i.id, i.email, i.role, COALESCE(u.name, ''), i.created_at, i.expires_at
		FROM invites i LEFT JOIN users u ON u.id = i.created_by WHERE i.expires_at > $1 ORDER BY i.created_at DESC`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invite{}
	for rows.Next() {
		var inv Invite
		if err := rows.Scan(&inv.ID, &inv.Email, &inv.Role, &inv.CreatedBy, &inv.CreatedAt, &inv.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

func (st *Store) InviteByToken(ctx context.Context, tokenHash string, now time.Time) (Invite, error) {
	var inv Invite
	err := st.db.QueryRowContext(ctx, `SELECT id, email, role, created_at, expires_at FROM invites
		WHERE token_hash = $1 AND expires_at > $2`, tokenHash, now).
		Scan(&inv.ID, &inv.Email, &inv.Role, &inv.CreatedAt, &inv.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return inv, ErrInviteNotFound
	}
	return inv, err
}

func (st *Store) DeleteInvitesFor(ctx context.Context, email string) error {
	_, err := st.db.ExecContext(ctx, `DELETE FROM invites WHERE email = $1`, email)
	return err
}

func (st *Store) DeleteInvite(ctx context.Context, id string) (bool, error) {
	res, err := st.db.ExecContext(ctx, `DELETE FROM invites WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func isUnique(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "unique") || strings.Contains(s, "duplicate key")
}
