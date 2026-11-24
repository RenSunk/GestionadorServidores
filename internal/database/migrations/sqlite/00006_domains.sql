-- +goose Up
CREATE TABLE domains (
    id         TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    hostname   TEXT NOT NULL UNIQUE,
    -- auto: Let's Encrypt (DNS público). internal: CA local de Caddy (pruebas / red interna).
    tls_mode   TEXT NOT NULL DEFAULT 'auto' CHECK (tls_mode IN ('auto', 'internal')),
    created_at TIMESTAMP NOT NULL
);
CREATE INDEX domains_project ON domains (project_id);

-- Deploy cuyo contenedor recibe el tráfico (alias d-<id> en la red "gestionador").
ALTER TABLE projects ADD COLUMN live_deployment_id TEXT;

-- Puertos del servidor donde escucha Caddy (por defecto 80/443).
CREATE TABLE server_proxy (
    server_id  TEXT PRIMARY KEY REFERENCES servers(id) ON DELETE CASCADE,
    http_port  INTEGER NOT NULL DEFAULT 80,
    https_port INTEGER NOT NULL DEFAULT 443
);

-- +goose Down
DROP TABLE server_proxy;
ALTER TABLE projects DROP COLUMN live_deployment_id;
DROP TABLE domains;
