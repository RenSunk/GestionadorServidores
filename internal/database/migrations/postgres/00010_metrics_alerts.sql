-- +goose Up
-- Métricas históricas. series: "server:<id>" o "container:<serverID>:<nombre>".
-- Valores en porcentaje; disk solo en servidores. Minuto: 48 h; hora: 90 días.
CREATE TABLE metrics_minute (
    series  TEXT NOT NULL,
    at      TIMESTAMPTZ NOT NULL,
    cpu     DOUBLE PRECISION NOT NULL,
    cpu_max DOUBLE PRECISION NOT NULL,
    mem     DOUBLE PRECISION NOT NULL,
    mem_max DOUBLE PRECISION NOT NULL,
    disk    DOUBLE PRECISION,
    PRIMARY KEY (series, at)
);
CREATE INDEX metrics_minute_at ON metrics_minute (at);

CREATE TABLE metrics_hour (
    series  TEXT NOT NULL,
    at      TIMESTAMPTZ NOT NULL,
    cpu     DOUBLE PRECISION NOT NULL,
    cpu_max DOUBLE PRECISION NOT NULL,
    mem     DOUBLE PRECISION NOT NULL,
    mem_max DOUBLE PRECISION NOT NULL,
    disk    DOUBLE PRECISION,
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
    started_at  TIMESTAMPTZ NOT NULL,
    resolved_at TIMESTAMPTZ
);
CREATE INDEX alerts_open ON alerts (resolved_at, kind, target_id);
CREATE INDEX alerts_started ON alerts (started_at);

-- +goose Down
DROP TABLE alerts;
DROP TABLE metrics_hour;
DROP TABLE metrics_minute;
