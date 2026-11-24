package servers

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"gestionador/internal/database"
)

var ErrDuplicateName = errors.New("ya existe un servidor con ese nombre")

type Store struct {
	db *database.DB
}

func NewStore(db *database.DB) *Store {
	return &Store{db: db}
}

const columns = `id, name, host, port, username, auth_method, credential_enc, host_key,
	os, kernel, arch, hostname, cpu_cores, docker_status, docker_version,
	status, last_error, last_seen_at, created_at, updated_at`

type scanner interface{ Scan(dest ...any) error }

func scanServer(row scanner) (Server, error) {
	var (
		s        Server
		lastSeen sql.NullTime
	)
	err := row.Scan(&s.ID, &s.Name, &s.Host, &s.Port, &s.Username, &s.AuthMethod, &s.CredentialEnc, &s.HostKey,
		&s.System.OS, &s.System.Kernel, &s.System.Arch, &s.System.Hostname, &s.System.CPUCores,
		&s.System.DockerStatus, &s.System.DockerVersion,
		&s.Status, &s.LastError, &lastSeen, &s.CreatedAt, &s.UpdatedAt)
	if lastSeen.Valid {
		t := lastSeen.Time
		s.LastSeenAt = &t
	}
	return s, err
}

func (st *Store) List(ctx context.Context) ([]Server, error) {
	rows, err := st.db.QueryContext(ctx, `SELECT `+columns+` FROM servers ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Server
	for rows.Next() {
		s, err := scanServer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (st *Store) Get(ctx context.Context, id string) (Server, error) {
	s, err := scanServer(st.db.QueryRowContext(ctx, `SELECT `+columns+` FROM servers WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Server{}, ErrNotFound
	}
	return s, err
}

func (st *Store) Create(ctx context.Context, s Server) error {
	_, err := st.db.ExecContext(ctx, `
		INSERT INTO servers (`+columns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)`,
		s.ID, s.Name, s.Host, s.Port, s.Username, s.AuthMethod, s.CredentialEnc, s.HostKey,
		s.System.OS, s.System.Kernel, s.System.Arch, s.System.Hostname, s.System.CPUCores,
		s.System.DockerStatus, s.System.DockerVersion,
		s.Status, s.LastError, s.LastSeenAt, s.CreatedAt, s.UpdatedAt)
	if err != nil && isUniqueViolation(err) {
		return ErrDuplicateName
	}
	return err
}

// RecordCheckOK guarda el resultado de una verificación exitosa.
func (st *Store) RecordCheckOK(ctx context.Context, id string, info SystemInfo, at time.Time) error {
	_, err := st.db.ExecContext(ctx, `
		UPDATE servers SET
			os = $2, kernel = $3, arch = $4, hostname = $5, cpu_cores = $6,
			docker_status = $7, docker_version = $8,
			status = 'online', last_error = '', last_seen_at = $9, updated_at = $9
		WHERE id = $1`,
		id, info.OS, info.Kernel, info.Arch, info.Hostname, info.CPUCores,
		info.DockerStatus, info.DockerVersion, at)
	return err
}

// RecordCheckFailed marca el servidor como offline con el motivo.
func (st *Store) RecordCheckFailed(ctx context.Context, id, reason string, at time.Time) error {
	_, err := st.db.ExecContext(ctx, `
		UPDATE servers SET status = 'offline', last_error = $2, updated_at = $3 WHERE id = $1`,
		id, reason, at)
	return err
}

func (st *Store) Delete(ctx context.Context, id string) error {
	res, err := st.db.ExecContext(ctx, `DELETE FROM servers WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// isUniqueViolation reconoce el error de clave única en SQLite y PostgreSQL
// sin importar los tipos de cada driver.
func isUniqueViolation(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") || strings.Contains(msg, "SQLSTATE 23505")
}
