-- +goose Up
-- Configuración general de la instancia (estado del asistente inicial, ajustes globales, etc.).
CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE settings;
