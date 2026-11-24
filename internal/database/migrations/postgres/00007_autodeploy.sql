-- +goose Up
-- Deploy automático: webhook de Git (con secreto) y/o revisión periódica del repo.
ALTER TABLE projects ADD COLUMN auto_deploy BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE projects ADD COLUMN webhook_secret_enc TEXT NOT NULL DEFAULT '';
ALTER TABLE projects ADD COLUMN poll_interval_min INTEGER NOT NULL DEFAULT 0; -- 0 = sin revisión periódica
-- Último webhook recibido (para diagnosticar desde la interfaz).
ALTER TABLE projects ADD COLUMN last_hook_at TIMESTAMPTZ;
ALTER TABLE projects ADD COLUMN last_hook_result TEXT NOT NULL DEFAULT '';

-- Qué disparó cada deploy: manual, webhook o poll; y quién (ej. autor del push).
ALTER TABLE deployments ADD COLUMN trigger_kind TEXT NOT NULL DEFAULT 'manual';
ALTER TABLE deployments ADD COLUMN triggered_by TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE deployments DROP COLUMN triggered_by;
ALTER TABLE deployments DROP COLUMN trigger_kind;
ALTER TABLE projects DROP COLUMN last_hook_result;
ALTER TABLE projects DROP COLUMN last_hook_at;
ALTER TABLE projects DROP COLUMN poll_interval_min;
ALTER TABLE projects DROP COLUMN webhook_secret_enc;
ALTER TABLE projects DROP COLUMN auto_deploy;
