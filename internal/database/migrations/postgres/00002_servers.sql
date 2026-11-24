-- +goose Up
CREATE TABLE servers (
    id             TEXT PRIMARY KEY,
    name           TEXT NOT NULL UNIQUE,
    host           TEXT NOT NULL,
    port           INTEGER NOT NULL DEFAULT 22,
    username       TEXT NOT NULL,
    -- instance_key: llave SSH generada por la instancia (credential_enc vacío)
    -- private_key:  llave privada propia, cifrada en credential_enc
    -- password:     contraseña, cifrada en credential_enc
    auth_method    TEXT NOT NULL CHECK (auth_method IN ('instance_key', 'private_key', 'password')),
    credential_enc TEXT NOT NULL DEFAULT '',
    -- Llave del host (formato authorized_keys) fijada en la primera conexión (TOFU).
    host_key       TEXT NOT NULL DEFAULT '',

    -- Información del sistema detectada en la última verificación.
    os             TEXT NOT NULL DEFAULT '',
    kernel         TEXT NOT NULL DEFAULT '',
    arch           TEXT NOT NULL DEFAULT '',
    hostname       TEXT NOT NULL DEFAULT '',
    cpu_cores      INTEGER NOT NULL DEFAULT 0,
    docker_status  TEXT NOT NULL DEFAULT '',
    docker_version TEXT NOT NULL DEFAULT '',

    status         TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'online', 'offline')),
    last_error     TEXT NOT NULL DEFAULT '',
    last_seen_at   TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL,
    updated_at     TIMESTAMPTZ NOT NULL
);

-- +goose Down
DROP TABLE servers;
