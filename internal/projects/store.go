package projects

import (
	"cmp"
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
	p.base_image, p.build_command, p.start_command, p.env_enc, p.cpu_limit_milli, p.memory_limit_mb,
	p.build_config_updated_at, p.runtime_config_updated_at, COALESCE(p.live_deployment_id, ''),
	p.auto_deploy, p.webhook_secret_enc, p.poll_interval_min, p.last_hook_at, p.last_hook_result, p.created_at, p.updated_at, s.name, s.host`

const projectFrom = ` FROM projects p JOIN servers s ON s.id = p.server_id`

type scanner interface{ Scan(dest ...any) error }

func scanProject(row scanner) (Project, error) {
	var (
		p                  Project
		hostPort, cpu, mem sql.NullInt64
		buildAt, runtimeAt sql.NullTime
		hookAt             sql.NullTime
	)
	err := row.Scan(&p.ID, &p.Name, &p.Slug, &p.ServerID, &p.RepoURL, &p.Branch, &p.GitUsername, &p.GitTokenEnc,
		&p.RootDir, &p.Dockerfile, &p.ContainerPort, &hostPort, &p.BuildType, &p.Build.Runtime, &p.Build.Version,
		&p.Build.BaseImage, &p.Build.BuildCommand, &p.Build.StartCommand, &p.EnvEnc, &cpu, &mem,
		&buildAt, &runtimeAt, &p.LiveDeploymentID,
		&p.AutoDeploy, &p.WebhookSecretEnc, &p.PollIntervalMin, &hookAt, &p.LastHookResult, &p.CreatedAt, &p.UpdatedAt, &p.ServerName, &p.ServerHost)
	p.Build.Port = p.ContainerPort
	p.HostPort, p.CPULimitMilli, p.MemoryLimitMB = nullInt(hostPort), nullInt(cpu), nullInt(mem)
	p.BuildConfigUpdatedAt, p.RuntimeConfigUpdatedAt = nullTime(buildAt), nullTime(runtimeAt)
	p.LastHookAt = nullTime(hookAt)
	return p, err
}

func nullInt(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	i := int(v.Int64)
	return &i
}

func nullTime(v sql.NullTime) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time
	return &t
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

// UpdateProject guarda todos los campos editables (no cambia slug ni servidor).
func (st *Store) UpdateProject(ctx context.Context, p Project) error {
	res, err := st.db.ExecContext(ctx, `
		UPDATE projects SET name = $2, repo_url = $3, branch = $4, git_username = $5, git_token_enc = $6,
			root_dir = $7, dockerfile = $8, container_port = $9, host_port = $10, build_type = $11, runtime = $12,
			runtime_version = $13, base_image = $14, build_command = $15, start_command = $16, env_enc = $17,
			cpu_limit_milli = $18, memory_limit_mb = $19, build_config_updated_at = $20,
			runtime_config_updated_at = $21, updated_at = $22
		WHERE id = $1`,
		p.ID, p.Name, p.RepoURL, p.Branch, p.GitUsername, p.GitTokenEnc,
		p.RootDir, p.Dockerfile, p.ContainerPort, p.HostPort, p.BuildType, p.Build.Runtime,
		p.Build.Version, p.Build.BaseImage, p.Build.BuildCommand, p.Build.StartCommand, p.EnvEnc,
		p.CPULimitMilli, p.MemoryLimitMB, p.BuildConfigUpdatedAt, p.RuntimeConfigUpdatedAt, p.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			if strings.Contains(err.Error(), "host_port") && p.HostPort != nil {
				return &PortInUseError{Port: *p.HostPort}
			}
			return ErrDuplicateName
		}
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
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
const deploymentColumns = `id, project_id, status, commit_sha, commit_message, image, source_deployment_id, trigger_kind, triggered_by, error, created_at, started_at, finished_at`

func scanDeployment(row scanner, withLog bool) (Deployment, error) {
	var (
		d                 Deployment
		started, finished sql.NullTime
		source            sql.NullString
	)
	dest := []any{&d.ID, &d.ProjectID, &d.Status, &d.CommitSHA, &d.CommitMessage, &d.Image, &source, &d.Trigger, &d.TriggeredBy, &d.Error,
		&d.CreatedAt, &started, &finished}
	if withLog {
		dest = append(dest, &d.Log)
	}
	err := row.Scan(dest...)
	d.SourceDeploymentID = source.String
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
		INSERT INTO deployments (id, project_id, status, commit_sha, commit_message, image, source_deployment_id,
			trigger_kind, triggered_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		d.ID, d.ProjectID, d.Status, d.CommitSHA, d.CommitMessage, d.Image, nullString(d.SourceDeploymentID),
		cmp.Or(d.Trigger, TriggerManual), d.TriggeredBy, d.CreatedAt)
	return err
}

func nullString(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }

// LatestSucceeded devuelve el último deploy exitoso del proyecto (el que está en ejecución,
// porque un deploy fallido restaura el anterior). ErrNothingToRestore si no hay ninguno.
func (st *Store) LatestSucceeded(ctx context.Context, projectID string) (Deployment, error) {
	d, err := scanDeployment(st.db.QueryRowContext(ctx, `SELECT `+deploymentColumns+` FROM deployments
		WHERE project_id = $1 AND status = 'succeeded' ORDER BY created_at DESC LIMIT 1`, projectID), false)
	if errors.Is(err, sql.ErrNoRows) {
		return Deployment{}, ErrNothingToRestore
	}
	return d, err
}

// LastSuccessTimes devuelve cuándo se creó el último deploy exitoso (cualquiera) y el
// último exitoso que construyó imagen. Sirve para saber si hay cambios sin aplicar.
func (st *Store) LastSuccessTimes(ctx context.Context, projectID string) (anyAt, buildAt *time.Time, err error) {
	// Se lee la columna directamente (no MAX()): en SQLite un agregado pierde el tipo TIMESTAMP.
	last := func(filter string) (*time.Time, error) {
		var t time.Time
		err := st.db.QueryRowContext(ctx, `SELECT created_at FROM deployments
			WHERE project_id = $1 AND status = 'succeeded'`+filter+` ORDER BY created_at DESC LIMIT 1`, projectID).Scan(&t)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return &t, err
	}
	if anyAt, err = last(""); err != nil {
		return nil, nil, err
	}
	buildAt, err = last(" AND source_deployment_id IS NULL")
	return anyAt, buildAt, err
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
