-- +goose Up
-- Métricas históricas. series: "server:<id>" o "container:<serverID>:<nombre>".
-- Valores en porcentaje; disk solo en servidores. Minuto: 48 h; hora: 90 días.
CREATE TABLE metrics_minute (
    series  TEXT NOT NULL,
    at      TIMESTAMP NOT NULL,
    cpu     REAL NOT NULL,
    cpu_max REAL NOT NULL,
    mem     REAL NOT NULL,
    mem_max REAL NOT NULL,
    disk    REAL,
    PRIMARY KEY (series, at)
);
CREATE INDEX metrics_minute_at ON metrics_minute (at);

CREATE TABLE metrics_hour (
    series  TEXT NOT NULL,
    at      TIMESTAMP NOT NULL,
    cpu     REAL NOT NULL,
    cpu_max REAL NOT NULL,
    mem     REAL NOT NULL,
    mem_max REAL NOT NULL,
    disk    REAL,
    PRIMARY KEY (series, at)
);
CREATE INDEX metrics_hour_at ON metrics_hour (at);

-- Alertas: una abierta por (kind, target); resolved_at NULL = activa.
CREATE TABLE alerts (
    id          TEXT PRIMARY KEY,
    kind        TEXT NOT NULL,
    target_type TEXT NOT NULL CHECK (target_type IN ('server', 'project')),
    target_id   TEXT NOT NULL,
    target_name TEXT NOT NULL,
    message     TEXT NOT NULL,
    started_at  TIMESTAMP NOT NULL,
    resolved_at TIMESTAMP
);
CREATE INDEX alerts_open ON alerts (resolved_at, kind, target_id);
CREATE INDEX alerts_started ON alerts (started_at);

-- +goose Down
DROP TABLE alerts;
DROP TABLE metrics_hour;
DROP TABLE metrics_minute;
