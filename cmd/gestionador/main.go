// Comando gestionador: arranca la instancia completa (API + frontend + base de datos)
// en un solo proceso.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gestionador/internal/config"
	"gestionador/internal/database"
	"gestionador/internal/secrets"
	"gestionador/internal/server"
	"gestionador/internal/servers"
	"gestionador/internal/settings"
)

// version se inyecta al compilar: -ldflags "-X main.version=v1.2.3"
var version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error("error fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		return err
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel})))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return fmt.Errorf("crear directorio de datos: %w", err)
	}

	db, err := database.Open(ctx, cfg.DatabaseURL, cfg.DataDir)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := database.Migrate(ctx, db); err != nil {
		return err
	}

	box, err := secrets.LoadOrCreate(cfg.SecretKey, cfg.DataDir)
	if err != nil {
		return err
	}

	serversSvc, err := servers.NewService(ctx, servers.NewStore(db), settings.NewStore(db), box)
	if err != nil {
		return err
	}

	// El monitor corre en segundo plano y se detiene al cancelar ctx.
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		serversSvc.RunMonitor(ctx)
	}()
	defer func() { stop(); <-monitorDone }()

	srv := server.New(server.Deps{
		DB:      db,
		Version: version,
		Modules: []server.Module{
			servers.NewHandler(serversSvc),
		},
	})

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("servidor iniciado", "addr", cfg.Addr, "version", version, "database", db.Dialect)
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		slog.Info("apagando...")
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("servidor http: %w", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}
