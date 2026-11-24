package projects

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"gestionador/internal/database"
)

type Store struct {
	db *database.DB
}

func NewStore(db *database.DB) *Store {
	return &Store{db: db}
}

// ---------- Proyectos ----------

const projectColumns = `p.id, p.name, p.slug, p.server_id, p.repo_url, p.branch, p.git_username, p.git_token_enc,
	p.root_dir, p.dockerfile, p.container_port, p.host_port, p.build_type, p.runtime, p.runtime_version,
	p.base_image, p.build_command, p.start_command, p.created_at, p.updated_at, s.name, s.host`

const projectFrom = ` FROM projects p JOIN servers s ON s.id = p.server_id`

type scanner interface{ Scan(dest ...any) error }

func scanProject(row scanner) (Project, error) {
	var (
		p        Project
		hostPort sql.NullInt64
	)
	err := row.Scan(&p.ID, &p.Name, &p.Slug, &p.ServerID, &p.RepoURL, &p.Branch, &p.GitUsername, &p.GitTokenEnc,
		&p.RootDir, &p.Dockerfile, &p.ContainerPort, &hostPort, &p.BuildType, &p.Build.Runtime, &p.Build.Version,
		&p.Build.BaseImage, &p.Build.BuildCommand, &p.Build.StartCommand, &p.CreatedAt, &p.UpdatedAt, &p.ServerName, &p.ServerHost)
	p.Build.Port = p.ContainerPort
	if hostPort.Valid {
		v := int(hostPort.Int64)
		p.HostPort = &v
	}
	return p, err
}

func (st *Store) ListProjects(ctx context.Context) ([]Project, error) {
	rows, err := st.db.QueryContext(ctx, `SELECT `+projectColumns+projectFrom+` ORDER BY p.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (st *Store) GetProject(ctx context.Context, id string) (Project, error) {
	p, err := scanProject(st.db.QueryRowContext(ctx, `SELECT `+projectColumns+projectFrom+` WHERE p.id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	return p, err
}

func (st *Store) CreateProject(ctx context.Context, p Project) error {
	_, err := st.db.ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, server_id, repo_url, branch, git_username, git_token_enc,
			root_dir, dockerfile, container_port, host_port, build_type, runtime, runtime_version,
			base_image, build_command, start_command, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)`,
		p.ID, p.Name, p.Slug, p.ServerID, p.RepoURL, p.Branch, p.GitUsername, p.GitTokenEnc,
		p.RootDir, p.Dockerfile, p.ContainerPort, p.HostPort, p.BuildType, p.Build.Runtime, p.Build.Version,
		p.Build.BaseImage, p.Build.BuildCommand, p.Build.StartCommand, p.CreatedAt, p.UpdatedAt)
	if err != nil && isUniqueViolation(err) {
		if strings.Contains(err.Error(), "host_port") && p.HostPort != nil {
			return &PortInUseError{Port: *p.HostPort}
		}
		return ErrDuplicateName
	}
	return err
}

func (st *Store) DeleteProject(ctx context.Context, id string) error {
	res, err := st.db.ExecContext(ctx, `DELETE FROM projects WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------- Deploys ----------

// Las listas no incluyen el log (puede pesar hasta 2 MB por deploy).
const deploymentColumns = `id, project_id, status, commit_sha, commit_message, image, error, created_at, started_at, finished_at`

func scanDeployment(row scanner, withLog bool) (Deployment, error) {
	var (
		d                 Deployment
		started, finished sql.NullTime
	)
	dest := []any{&d.ID, &d.ProjectID, &d.Status, &d.CommitSHA, &d.CommitMessage, &d.Image, &d.Error,
		&d.CreatedAt, &started, &finished}
	if withLog {
		dest = append(dest, &d.Log)
	}
	err := row.Scan(dest...)
	if started.Valid {
		t := started.Time
		d.StartedAt = &t
	}
	if finished.Valid {
		t := finished.Time
		d.FinishedAt = &t
	}
	return d, err
}

func (st *Store) CreateDeployment(ctx context.Context, d Deployment) error {
	_, err := st.db.ExecContext(ctx, `
		INSERT INTO deployments (id, project_id, status, created_at) VALUES ($1, $2, $3, $4)`,
		d.ID, d.ProjectID, d.Status, d.CreatedAt)
	return err
}

func (st *Store) GetDeployment(ctx context.Context, id string) (Deployment, error) {
	d, err := scanDeployment(st.db.QueryRowContext(ctx,
		`SELECT `+deploymentColumns+`, log FROM deployments WHERE id = $1`, id), true)
	if errors.Is(err, sql.ErrNoRows) {
		return Deployment{}, ErrDeploymentNotFound
	}
	return d, err
}

func (st *Store) ListDeployments(ctx context.Context, projectID string, limit int) ([]Deployment, error) {
	rows, err := st.db.QueryContext(ctx, `SELECT `+deploymentColumns+` FROM deployments
		WHERE project_id = $1 ORDER BY created_at DESC LIMIT $2`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Deployment
	for rows.Next() {
		d, err := scanDeployment(rows, false)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// LatestDeployments devuelve el último deploy de cada proyecto.
func (st *Store) LatestDeployments(ctx context.Context) (map[string]Deployment, error) {
	rows, err := st.db.QueryContext(ctx, `SELECT `+deploymentColumns+` FROM deployments d
		WHERE created_at = (SELECT MAX(created_at) FROM deployments WHERE project_id = d.project_id)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Deployment{}
	for rows.Next() {
		d, err := scanDeployment(rows, false)
		if err != nil {
			return nil, err
		}
		out[d.ProjectID] = d
	}
	return out, rows.Err()
}

func (st *Store) MarkStarted(ctx context.Context, id string, at time.Time) error {
	_, err := st.db.ExecContext(ctx, `UPDATE deployments SET status = 'building', started_at = $2 WHERE id = $1`, id, at)
	return err
}

func (st *Store) SetCommit(ctx context.Context, id string, c Deployment) error {
	_, err := st.db.ExecContext(ctx, `UPDATE deployments SET commit_sha = $2, commit_message = $3, image = $4 WHERE id = $1`,
		id, c.CommitSHA, c.CommitMessage, c.Image)
	return err
}

func (st *Store) SetStatus(ctx context.Context, id string, status DeploymentStatus) error {
	_, err := st.db.ExecContext(ctx, `UPDATE deployments SET status = $2 WHERE id = $1`, id, status)
	return err
}

func (st *Store) Finish(ctx context.Context, id string, status DeploymentStatus, errMsg, log string, at time.Time) error {
	_, err := st.db.ExecContext(ctx, `UPDATE deployments SET status = $2, error = $3, log = $4, finished_at = $5 WHERE id = $1`,
		id, status, errMsg, log, at)
	return err
}

// FailInterrupted cierra los deploys que quedaron a medias por un reinicio.
func (st *Store) FailInterrupted(ctx context.Context, at time.Time) (int64, error) {
	res, err := st.db.ExecContext(ctx, `UPDATE deployments
		SET status = 'failed', error = 'Interrumpido: la instancia se reinició durante el deploy', finished_at = $1
		WHERE status IN ('queued', 'building', 'deploying')`, at)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func isUniqueViolation(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") || strings.Contains(msg, "SQLSTATE 23505")
}
