// Package database abre la conexión (SQLite o PostgreSQL) y aplica las migraciones.
//
// Ambos motores aceptan placeholders $1, $2..., así que las consultas se pueden
// escribir una sola vez. Lo que difiere (tipos, DDL) vive en migraciones separadas
// por dialecto: migrations/sqlite y migrations/postgres.
package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // driver "pgx"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // driver "sqlite" (Go puro, sin CGO → binario estático)
)

type Dialect string

const (
	SQLite   Dialect = "sqlite"
	Postgres Dialect = "postgres"
)

type DB struct {
	*sql.DB
	Dialect Dialect
}

// Open conecta a la base indicada por rawURL. Si rawURL está vacío usa SQLite
// en dataDir/gestionador.db.
func Open(ctx context.Context, rawURL, dataDir string) (*DB, error) {
	if rawURL == "" {
		rawURL = "sqlite://" + filepath.Join(dataDir, "gestionador.db")
	}

	var (
		db      *sql.DB
		dialect Dialect
		err     error
	)

	switch {
	case strings.HasPrefix(rawURL, "postgres://"), strings.HasPrefix(rawURL, "postgresql://"):
		dialect = Postgres
		db, err = sql.Open("pgx", rawURL)
		if err != nil {
			return nil, fmt.Errorf("abrir postgres: %w", err)
		}
		db.SetMaxOpenConns(10)
		db.SetConnMaxIdleTime(5 * time.Minute)

	case strings.HasPrefix(rawURL, "sqlite://"):
		dialect = SQLite
		path := strings.TrimPrefix(rawURL, "sqlite://")
		q := url.Values{}
		q.Add("_pragma", "foreign_keys(1)")
		q.Add("_pragma", "journal_mode(WAL)")
		q.Add("_pragma", "busy_timeout(5000)")
		db, err = sql.Open("sqlite", "file:"+path+"?"+q.Encode())
		if err != nil {
			return nil, fmt.Errorf("abrir sqlite: %w", err)
		}
		// SQLite admite un solo escritor; una conexión evita errores SQLITE_BUSY.
		db.SetMaxOpenConns(1)

	default:
		return nil, fmt.Errorf("URL de base de datos no soportada (usa sqlite:// o postgres://)")
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, fmt.Errorf("conectar a %s: %w", dialect, err)
	}

	return &DB{DB: db, Dialect: dialect}, nil
}

//go:embed migrations
var migrationsFS embed.FS

// Migrate aplica las migraciones pendientes del dialecto correspondiente.
func Migrate(ctx context.Context, db *DB) error {
	dir, err := fs.Sub(migrationsFS, "migrations/"+string(db.Dialect))
	if err != nil {
		return err
	}

	gooseDialect := goose.DialectSQLite3
	if db.Dialect == Postgres {
		gooseDialect = goose.DialectPostgres
	}

	provider, err := goose.NewProvider(gooseDialect, db.DB, dir)
	if err != nil {
		return fmt.Errorf("preparar migraciones: %w", err)
	}

	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("aplicar migraciones: %w", err)
	}
	for _, r := range results {
		slog.Info("migración aplicada", "version", r.Source.Version, "archivo", filepath.Base(r.Source.Path))
	}
	return nil
}
