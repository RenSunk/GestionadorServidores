-- +goose Up
CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE, -- en minúsculas
    name          TEXT NOT NULL,
    password_hash TEXT NOT NULL,         -- bcrypt
    role          TEXT NOT NULL CHECK (role IN ('admin', 'developer', 'viewer')),
    disabled      INTEGER NOT NULL DEFAULT 0,
    created_at    TIMESTAMP NOT NULL,
    last_login_at TIMESTAMP
);

-- La cookie lleva un token aleatorio; aquí solo se guarda su SHA-256.
CREATE TABLE sessions (
    token_hash   TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at   TIMESTAMP NOT NULL,
    last_seen_at TIMESTAMP NOT NULL,
    expires_at   TIMESTAMP NOT NULL,
    ip           TEXT NOT NULL DEFAULT '',
    user_agent   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX sessions_user ON sessions (user_id);

-- Invitaciones: el enlace lleva un token aleatorio; aquí solo su SHA-256.
CREATE TABLE invites (
    id         TEXT PRIMARY KEY,
    token_hash TEXT NOT NULL UNIQUE,
    email      TEXT NOT NULL,
    role       TEXT NOT NULL CHECK (role IN ('admin', 'developer', 'viewer')),
    created_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMP NOT NULL,
    expires_at TIMESTAMP NOT NULL
);

-- +goose Down
DROP TABLE invites;
DROP TABLE sessions;
DROP TABLE users;
