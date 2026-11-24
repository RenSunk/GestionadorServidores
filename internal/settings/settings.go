// Package settings guarda pares clave/valor de la instancia en la tabla settings.
package settings

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"gestionador/internal/database"
)

var ErrNotFound = errors.New("settings: clave no encontrada")

type Store struct {
	db *database.DB
}

func NewStore(db *database.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Get(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = $1`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return value, err
}

func (s *Store) Set(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, $3)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, time.Now().UTC())
	return err
}
