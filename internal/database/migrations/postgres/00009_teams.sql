-- +goose Up
-- Equipos: limitan qué proyectos y servidores ve cada usuario (el rol sigue
-- definiendo qué puede hacer). Un recurso sin equipos es visible para todos.
CREATE TABLE teams (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE team_members (
    team_id TEXT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (team_id, user_id)
);
CREATE INDEX team_members_user ON team_members (user_id);

CREATE TABLE team_projects (
    team_id    TEXT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    PRIMARY KEY (team_id, project_id)
);
CREATE INDEX team_projects_project ON team_projects (project_id);

CREATE TABLE team_servers (
    team_id   TEXT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    PRIMARY KEY (team_id, server_id)
);
CREATE INDEX team_servers_server ON team_servers (server_id);

-- +goose Down
DROP TABLE team_servers;
DROP TABLE team_projects;
DROP TABLE team_members;
DROP TABLE teams;
