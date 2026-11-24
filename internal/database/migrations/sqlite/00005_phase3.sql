-- +goose Up
-- Variables de entorno: JSON cifrado [{key, value, build}].
ALTER TABLE projects ADD COLUMN env_enc TEXT NOT NULL DEFAULT '';
-- Límites del contenedor. NULL = sin límite. CPU en milésimas de núcleo (500 = 0,5 CPU).
ALTER TABLE projects ADD COLUMN cpu_limit_milli INTEGER;
ALTER TABLE projects ADD COLUMN memory_limit_mb INTEGER;
-- Cuándo cambió por última vez la configuración que exige reconstruir la imagen (build)
-- o solo recrear el contenedor (runtime). Sirve para avisar de cambios pendientes.
ALTER TABLE projects ADD COLUMN build_config_updated_at TIMESTAMP;
ALTER TABLE projects ADD COLUMN runtime_config_updated_at TIMESTAMP;

-- Redeploy de una imagen ya construida (rollback o reinicio con la configuración nueva).
ALTER TABLE deployments ADD COLUMN source_deployment_id TEXT REFERENCES deployments(id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE deployments DROP COLUMN source_deployment_id;
ALTER TABLE projects DROP COLUMN runtime_config_updated_at;
ALTER TABLE projects DROP COLUMN build_config_updated_at;
ALTER TABLE projects DROP COLUMN memory_limit_mb;
ALTER TABLE projects DROP COLUMN cpu_limit_milli;
ALTER TABLE projects DROP COLUMN env_enc;
