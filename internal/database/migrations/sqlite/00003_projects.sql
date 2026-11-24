-- +goose Up
CREATE TABLE projects (
    id             TEXT PRIMARY KEY,
    name           TEXT NOT NULL UNIQUE,
    -- slug: nombre seguro para Docker (contenedor gs-<slug>, imagen gestionador/<slug>).
    slug           TEXT NOT NULL UNIQUE,
    -- RESTRICT: no se puede borrar un servidor que todavía tiene proyectos.
    server_id      TEXT NOT NULL REFERENCES servers(id) ON DELETE RESTRICT,

    repo_url       TEXT NOT NULL,
    branch         TEXT NOT NULL,
    git_username   TEXT NOT NULL DEFAULT '',
    git_token_enc  TEXT NOT NULL DEFAULT '',

    -- Subcarpeta del repo usada como contexto de build (monorepos). Vacío = raíz.
    root_dir       TEXT NOT NULL DEFAULT '',
    -- Ruta del Dockerfile relativa a root_dir.
    dockerfile     TEXT NOT NULL DEFAULT 'Dockerfile',

    container_port INTEGER NOT NULL,
    -- Puerto publicado en el servidor. NULL = no se publica (útil con dominios, fase 4).
    host_port      INTEGER,

    created_at     TIMESTAMP NOT NULL,
    updated_at     TIMESTAMP NOT NULL
);

-- Dos proyectos no pueden publicar el mismo puerto en el mismo servidor (los NULL no chocan).
CREATE UNIQUE INDEX projects_server_host_port ON projects (server_id, host_port);

CREATE TABLE deployments (
    id             TEXT PRIMARY KEY,
    project_id     TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    status         TEXT NOT NULL CHECK (status IN ('queued', 'building', 'deploying', 'succeeded', 'failed', 'canceled')),
    commit_sha     TEXT NOT NULL DEFAULT '',
    commit_message TEXT NOT NULL DEFAULT '',
    image          TEXT NOT NULL DEFAULT '',
    error          TEXT NOT NULL DEFAULT '',
    -- Log completo del deploy (se guarda al terminar; mientras corre vive en memoria).
    log            TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMP NOT NULL,
    started_at     TIMESTAMP,
    finished_at    TIMESTAMP
);

CREATE INDEX deployments_project_created ON deployments (project_id, created_at);

-- +goose Down
DROP TABLE deployments;
DROP TABLE projects;
