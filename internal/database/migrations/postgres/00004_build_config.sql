-- +goose Up
-- dockerfile: se usa el Dockerfile del repositorio.
-- runtime:    Gestionador genera el Dockerfile a partir del lenguaje y los comandos.
ALTER TABLE projects ADD COLUMN build_type TEXT NOT NULL DEFAULT 'dockerfile' CHECK (build_type IN ('dockerfile', 'runtime'));
ALTER TABLE projects ADD COLUMN runtime TEXT NOT NULL DEFAULT '';
ALTER TABLE projects ADD COLUMN runtime_version TEXT NOT NULL DEFAULT '';
ALTER TABLE projects ADD COLUMN base_image TEXT NOT NULL DEFAULT '';
ALTER TABLE projects ADD COLUMN build_command TEXT NOT NULL DEFAULT '';
ALTER TABLE projects ADD COLUMN start_command TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE projects DROP COLUMN start_command;
ALTER TABLE projects DROP COLUMN build_command;
ALTER TABLE projects DROP COLUMN base_image;
ALTER TABLE projects DROP COLUMN runtime_version;
ALTER TABLE projects DROP COLUMN runtime;
ALTER TABLE projects DROP COLUMN build_type;
