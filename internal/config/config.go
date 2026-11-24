// Package config carga la configuración de la instancia desde variables de entorno
// y flags de línea de comandos (los flags tienen prioridad).
package config

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
)

type Config struct {
	// Addr es la dirección donde escucha el servidor HTTP, ej. ":8080".
	Addr string
	// DataDir guarda la base SQLite, la clave maestra y otros archivos locales.
	DataDir string
	// DatabaseURL vacío = SQLite en DataDir. También acepta postgres://...
	DatabaseURL string
	// SecretKey es la clave maestra (base64, 32 bytes) para cifrar secretos.
	// Si está vacía, se lee o genera en DataDir/secret.key.
	// Solo se acepta por variable de entorno, nunca por flag (los flags son visibles en `ps`).
	SecretKey string
	LogLevel  slog.Level
}

func Load(args []string) (Config, error) {
	cfg := Config{
		Addr:        env("GESTIONADOR_ADDR", ":8080"),
		DataDir:     env("GESTIONADOR_DATA_DIR", "./data"),
		DatabaseURL: os.Getenv("GESTIONADOR_DATABASE_URL"),
		SecretKey:   os.Getenv("GESTIONADOR_SECRET_KEY"),
	}
	level := env("GESTIONADOR_LOG_LEVEL", "info")

	fs := flag.NewFlagSet("gestionador", flag.ContinueOnError)
	fs.StringVar(&cfg.Addr, "addr", cfg.Addr, "dirección HTTP (env GESTIONADOR_ADDR)")
	fs.StringVar(&cfg.DataDir, "data-dir", cfg.DataDir, "directorio de datos (env GESTIONADOR_DATA_DIR)")
	fs.StringVar(&cfg.DatabaseURL, "database-url", cfg.DatabaseURL, "URL de base de datos; vacío = SQLite (env GESTIONADOR_DATABASE_URL)")
	fs.StringVar(&level, "log-level", level, "debug, info, warn o error (env GESTIONADOR_LOG_LEVEL)")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	if err := cfg.LogLevel.UnmarshalText([]byte(level)); err != nil {
		return Config{}, fmt.Errorf("log-level inválido %q: %w", level, err)
	}
	return cfg, nil
}

func env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
