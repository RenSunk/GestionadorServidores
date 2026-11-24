-- +goose Up
-- Configuración general de la instancia (estado del asistente inicial, ajustes globales, etc.).
CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE settings;
