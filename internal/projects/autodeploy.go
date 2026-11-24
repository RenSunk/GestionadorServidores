package projects

import (
	"cmp"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"

	"gestionador/internal/gitx"
)

// Deploy automático: un webhook del proveedor de Git (instantáneo, requiere que
// Gestionador sea accesible desde internet) o una revisión periódica de la rama
// (funciona detrás de NAT). Si llega un push durante un deploy, queda uno en cola.

const (
	TriggerManual  = "manual"
	TriggerWebhook = "webhook"
	TriggerPoll    = "poll"

	pollTick    = 20 * time.Second
	pollTimeout = 20 * time.Second
)

// PollIntervals son las frecuencias de revisión que se ofrecen (minutos; 0 = desactivada).
var PollIntervals = []int{0, 1, 5, 15, 60}

// Trigger dice qué disparó un deploy.
type Trigger struct {
	Kind string
	By   string
}

type pollState struct {
	At     time.Time
	Result string
	Failed bool
}

// AutoDeploy es la configuración y el estado del deploy automático de un proyecto.
type AutoDeploy struct {
	Enabled        bool       `json:"enabled"`
	PollMinutes    int        `json:"pollMinutes"`
	HookPath       string     `json:"hookPath"`
	Secret         string     `json:"secret"`
	LastHookAt     *time.Time `json:"lastHookAt"`
	LastHookResult string     `json:"lastHookResult"`
	LastPollAt     *time.Time `json:"lastPollAt"`
	LastPollResult string     `json:"lastPollResult"`
	LastPollFailed bool       `json:"lastPollFailed"`
}

type AutoDeployInput struct {
	Enabled     bool `json:"enabled"`
	PollMinutes int  `json:"pollMinutes"`
}

func hookPath(projectID string) string { return "/hooks/git/" + projectID }

// ---------- Store ----------

func (st *Store) SetAutoDeploy(ctx context.Context, id string, enabled bool, secretEnc string, pollMin int) error {
	res, err := st.db.ExecContext(ctx, `UPDATE projects SET auto_deploy = $2, webhook_secret_enc = $3, poll_interval_min = $4,
		updated_at = $5 WHERE id = $1`, id, enabled, secretEnc, pollMin, time.Now().UTC())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (st *Store) RecordHook(ctx context.Context, id, result string, at time.Time) error {
	_, err := st.db.ExecContext(ctx, `UPDATE projects SET last_hook_at = $2, last_hook_result = $3 WHERE id = $1`, id, at, result)
	return err
}

// LastBuildCommit es el commit del último deploy que construyó imagen (sin importar si
// terminó bien): la revisión periódica no reintenta un commit que ya falló.
func (st *Store) LastBuildCommit(ctx context.Context, projectID string) (string, error) {
	var sha string
	err := st.db.QueryRowContext(ctx, `SELECT commit_sha FROM deployments
		WHERE project_id = $1 AND source_deployment_id IS NULL AND commit_sha <> ''
		ORDER BY created_at DESC LIMIT 1`, projectID).Scan(&sha)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return sha, err
}

// ---------- Servicio ----------

func (s *Service) AutoDeployInfo(ctx context.Context, id string) (AutoDeploy, error) {
	p, err := s.store.GetProject(ctx, id)
	if err != nil {
		return AutoDeploy{}, err
	}
	info := AutoDeploy{
		Enabled: p.AutoDeploy, PollMinutes: p.PollIntervalMin, HookPath: hookPath(p.ID),
		LastHookAt: p.LastHookAt, LastHookResult: p.LastHookResult,
	}
	if p.WebhookSecretEnc != "" {
		raw, err := s.box.Decrypt(p.WebhookSecretEnc)
		if err != nil {
			return AutoDeploy{}, fmt.Errorf("no se pudo descifrar el secreto del webhook: %w", err)
		}
		info.Secret = string(raw)
	}
	s.mu.Lock()
	if ps, ok := s.polls[id]; ok {
		at := ps.At
		info.LastPollAt, info.LastPollResult, info.LastPollFailed = &at, ps.Result, ps.Failed
	}
	s.mu.Unlock()
	return info, nil
}

// SetAutoDeploy activa o desactiva el deploy automático. El secreto del webhook se
// genera la primera vez y se conserva al desactivar (la URL configurada sigue valiendo).
func (s *Service) SetAutoDeploy(ctx context.Context, id string, in AutoDeployInput) (AutoDeploy, error) {
	if !slices.Contains(PollIntervals, in.PollMinutes) {
		return AutoDeploy{}, invalid("frecuencia de revisión inválida")
	}
	p, err := s.store.GetProject(ctx, id)
	if err != nil {
		return AutoDeploy{}, err
	}
	secretEnc := p.WebhookSecretEnc
	if in.Enabled && secretEnc == "" {
		if secretEnc, err = s.newSecret(); err != nil {
			return AutoDeploy{}, err
		}
	}
	if err := s.store.SetAutoDeploy(ctx, id, in.Enabled, secretEnc, in.PollMinutes); err != nil {
		return AutoDeploy{}, err
	}
	if !in.Enabled || in.PollMinutes == 0 {
		s.mu.Lock()
		delete(s.polls, id)
		s.mu.Unlock()
	}
	return s.AutoDeployInfo(ctx, id)
}

// RotateSecret genera un secreto nuevo: el anterior deja de funcionar de inmediato.
func (s *Service) RotateSecret(ctx context.Context, id string) (AutoDeploy, error) {
	p, err := s.store.GetProject(ctx, id)
	if err != nil {
		return AutoDeploy{}, err
	}
	enc, err := s.newSecret()
	if err != nil {
		return AutoDeploy{}, err
	}
	if err := s.store.SetAutoDeploy(ctx, id, p.AutoDeploy, enc, p.PollIntervalMin); err != nil {
		return AutoDeploy{}, err
	}
	return s.AutoDeployInfo(ctx, id)
}

func (s *Service) newSecret() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return s.box.Encrypt([]byte(base64.RawURLEncoding.EncodeToString(b)))
}

// deployOrQueue inicia un deploy automático; si ya hay uno en curso, deja este en
// cola (uno solo: el siguiente deploy toma el último commit de la rama).
func (s *Service) deployOrQueue(ctx context.Context, p Project, t Trigger) (deploymentID string, queued bool, err error) {
	for range 2 {
		d, err := s.DeployWith(ctx, p.ID, t)
		if !errors.Is(err, ErrDeployInProgress) {
			return d.ID, false, err
		}
		// release() quita el deploy de running y lee la cola con el mismo lock:
		// si sigue en curso, este push no se pierde.
		s.mu.Lock()
		if _, busy := s.running[p.ID]; busy {
			s.queued[p.ID] = t
			s.mu.Unlock()
			return "", true, nil
		}
		s.mu.Unlock() // terminó justo ahora: se reintenta
	}
	return "", false, ErrDeployInProgress
}

// ---------- Revisión periódica ----------

// RunPoller revisa las ramas de los proyectos con revisión periódica hasta que se cancele ctx.
func (s *Service) RunPoller(ctx context.Context) {
	t := time.NewTicker(pollTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.pollOnce(ctx, time.Now())
		}
	}
}

func (s *Service) pollOnce(ctx context.Context, now time.Time) {
	list, err := s.store.ListProjects(ctx)
	if err != nil {
		slog.Error("revisión periódica: listar proyectos", "err", err)
		return
	}
	for _, p := range list {
		if ctx.Err() != nil {
			return
		}
		if !p.AutoDeploy || p.PollIntervalMin == 0 {
			continue
		}
		s.mu.Lock()
		last, seen := s.polls[p.ID]
		_, busy := s.running[p.ID]
		s.mu.Unlock()
		if busy || (seen && now.Sub(last.At) < time.Duration(p.PollIntervalMin)*time.Minute-pollTick/2) {
			continue
		}
		result, failed := s.pollProject(ctx, p)
		s.mu.Lock()
		s.polls[p.ID] = pollState{At: now, Result: result, Failed: failed}
		s.mu.Unlock()
	}
}

func (s *Service) pollProject(ctx context.Context, p Project) (result string, failed bool) {
	repo := gitx.Repo{URL: p.RepoURL, Branch: p.Branch, Username: p.GitUsername}
	if p.GitTokenEnc != "" {
		token, err := s.box.Decrypt(p.GitTokenEnc)
		if err != nil {
			return "no se pudo descifrar el token de git", true
		}
		repo.Token = string(token)
	}
	cctx, cancel := context.WithTimeout(ctx, pollTimeout)
	defer cancel()
	head, err := gitx.RemoteHead(cctx, repo)
	if err != nil {
		slog.Warn("revisión periódica", "project", p.Name, "err", err)
		return "No se pudo consultar el repositorio: " + err.Error(), true
	}
	last, err := s.store.LastBuildCommit(ctx, p.ID)
	if err != nil {
		return "error interno al leer el último deploy", true
	}
	if head == last {
		return "Sin cambios (" + shortSHA(head) + ")", false
	}
	if _, err := s.DeployWith(ctx, p.ID, Trigger{Kind: TriggerPoll, By: shortSHA(head)}); err != nil {
		if errors.Is(err, ErrDeployInProgress) {
			return "Commit nuevo " + shortSHA(head) + "; se revisará de nuevo al terminar el deploy en curso", false
		}
		return "No se pudo iniciar el deploy: " + err.Error(), true
	}
	slog.Info("revisión periódica: commit nuevo", "project", p.Name, "commit", shortSHA(head))
	return "Commit nuevo " + shortSHA(head) + ": deploy iniciado", false
}

// ---------- Webhooks ----------

// HookResult es la respuesta al proveedor de Git (se ve en el historial de entregas).
type HookResult struct {
	Status       int    `json:"-"`
	Message      string `json:"message"`
	DeploymentID string `json:"deploymentId,omitempty"`
}

var errBadSignature = errors.New("firma inválida")

// HandleHook procesa un webhook de GitHub, GitLab, Gitea/Forgejo o Bitbucket.
func (s *Service) HandleHook(ctx context.Context, projectID string, h hookHeaders, body []byte) HookResult {
	p, err := s.store.GetProject(ctx, projectID)
	if err != nil {
		return HookResult{Status: 404, Message: "proyecto no encontrado"}
	}
	if p.WebhookSecretEnc == "" {
		return HookResult{Status: 403, Message: "el deploy automático no está configurado en este proyecto"}
	}
	secret, err := s.box.Decrypt(p.WebhookSecretEnc)
	if err != nil {
		return HookResult{Status: 500, Message: "error interno"}
	}

	res := func() HookResult {
		provider, err := verifyHook(h, body, string(secret))
		if err != nil {
			return HookResult{Status: 401, Message: "Rechazado: " + err.Error()}
		}
		ev, err := parseHook(provider, h, body)
		if err != nil {
			return HookResult{Status: 400, Message: "Rechazado: " + err.Error()}
		}
		switch {
		case ev.Ping:
			return HookResult{Status: 200, Message: fmt.Sprintf("Prueba de %s recibida: el webhook está bien configurado", provider)}
		case ev.Ignore != "":
			return HookResult{Status: 200, Message: "Ignorado: " + ev.Ignore}
		case !p.AutoDeploy:
			return HookResult{Status: 200, Message: "Ignorado: el deploy automático está desactivado"}
		case ev.Branch != "" && ev.Branch != p.Branch:
			return HookResult{Status: 200, Message: fmt.Sprintf("Ignorado: push a la rama %s (el proyecto despliega %s)", ev.Branch, p.Branch)}
		}
		desc := describePush(provider, ev)
		depID, queued, err := s.deployOrQueue(ctx, p, Trigger{Kind: TriggerWebhook, By: ev.Pusher})
		switch {
		case err != nil:
			return HookResult{Status: 500, Message: "No se pudo iniciar el deploy: " + err.Error()}
		case queued:
			return HookResult{Status: 202, Message: "En cola: " + desc + "; se desplegará al terminar el deploy en curso"}
		}
		return HookResult{Status: 202, Message: "Deploy iniciado: " + desc, DeploymentID: depID}
	}()

	if err := s.store.RecordHook(context.WithoutCancel(ctx), p.ID, res.Message, time.Now().UTC()); err != nil {
		slog.Error("registrar webhook", "project", p.ID, "err", err)
	}
	slog.Info("webhook", "project", p.Name, "status", res.Status, "result", res.Message)
	return res
}

func describePush(provider string, ev pushEvent) string {
	var b strings.Builder
	b.WriteString("push")
	if ev.Pusher != "" {
		fmt.Fprintf(&b, " de %s", ev.Pusher)
	}
	if ev.Branch != "" {
		fmt.Fprintf(&b, " a %s", ev.Branch)
	}
	if ev.SHA != "" {
		fmt.Fprintf(&b, " (%s)", shortSHA(ev.SHA))
	}
	fmt.Fprintf(&b, " desde %s", provider)
	return b.String()
}

// hookHeaders es lo que se lee de la petición (independiente de net/http para testear).
type hookHeaders interface{ Get(string) string }

type pushEvent struct {
	Branch string
	SHA    string
	Pusher string
	Ping   bool
	Ignore string // motivo para no desplegar (tag, rama borrada, otro evento)
}

// verifyHook comprueba la firma o el token según el proveedor y devuelve su nombre.
func verifyHook(h hookHeaders, body []byte, secret string) (string, error) {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sum := hex.EncodeToString(mac.Sum(nil))
	sigOK := func(got string) bool { return hmac.Equal([]byte(strings.ToLower(got)), []byte(sum)) }
	tokOK := func(got string) bool { return subtle.ConstantTimeCompare([]byte(got), []byte(secret)) == 1 }

	provider := "Git"
	switch {
	case h.Get("X-Gitea-Event") != "", h.Get("X-Gogs-Event") != "":
		provider = "Gitea"
	case h.Get("X-Forgejo-Event") != "":
		provider = "Forgejo"
	case h.Get("X-GitHub-Event") != "":
		provider = "GitHub"
	case h.Get("X-Gitlab-Event") != "":
		provider = "GitLab"
	case h.Get("X-Event-Key") != "":
		provider = "Bitbucket"
	}

	switch {
	case h.Get("X-Hub-Signature-256") != "":
		if v, ok := strings.CutPrefix(h.Get("X-Hub-Signature-256"), "sha256="); ok && sigOK(v) {
			return provider, nil
		}
	case h.Get("X-Gitea-Signature") != "" || h.Get("X-Forgejo-Signature") != "" || h.Get("X-Gogs-Signature") != "":
		if sigOK(h.Get("X-Gitea-Signature") + h.Get("X-Forgejo-Signature") + h.Get("X-Gogs-Signature")) {
			return provider, nil
		}
	case h.Get("X-Gitlab-Token") != "":
		if tokOK(h.Get("X-Gitlab-Token")) {
			return provider, nil
		}
	case h.Get("X-Hub-Signature") != "": // Bitbucket Cloud
		if v, ok := strings.CutPrefix(h.Get("X-Hub-Signature"), "sha256="); ok && sigOK(v) {
			return provider, nil
		}
	case h.Get("X-Gestionador-Token") != "": // disparo manual (CI, curl)
		if tokOK(h.Get("X-Gestionador-Token")) {
			return "token", nil
		}
	default:
		return provider, errors.New("falta la firma: configura el secreto en el webhook")
	}
	return provider, fmt.Errorf("%w: el secreto del webhook no coincide con el de Gestionador", errBadSignature)
}

// parseHook interpreta el evento push de cada proveedor.
func parseHook(provider string, h hookHeaders, body []byte) (pushEvent, error) {
	if strings.HasPrefix(h.Get("Content-Type"), "application/x-www-form-urlencoded") {
		// GitHub con "Content type: application/x-www-form-urlencoded" (su valor por defecto).
		q, err := url.ParseQuery(string(body))
		if err != nil {
			return pushEvent{}, errors.New("cuerpo inválido")
		}
		body = []byte(q.Get("payload"))
	}

	switch provider {
	case "token":
		return pushEvent{Pusher: "token"}, nil
	case "GitHub", "Gitea", "Forgejo":
		event := cmp.Or(h.Get("X-Gitea-Event"), h.Get("X-Forgejo-Event"), h.Get("X-Gogs-Event"), h.Get("X-GitHub-Event"))
		switch event {
		case "ping":
			return pushEvent{Ping: true}, nil
		case "push":
		default:
			return pushEvent{Ignore: fmt.Sprintf("evento %q (solo se despliega con push)", event)}, nil
		}
		var v struct {
			Ref     string `json:"ref"`
			After   string `json:"after"`
			Deleted bool   `json:"deleted"`
			Pusher  struct {
				Name     string `json:"name"`
				Login    string `json:"login"`
				Username string `json:"username"`
			} `json:"pusher"`
			Sender struct {
				Login string `json:"login"`
			} `json:"sender"`
		}
		if err := json.Unmarshal(body, &v); err != nil {
			return pushEvent{}, errors.New("JSON inválido")
		}
		ev := pushEvent{SHA: v.After, Pusher: cmp.Or(v.Sender.Login, v.Pusher.Login, v.Pusher.Username, v.Pusher.Name)}
		return refEvent(ev, v.Ref, v.Deleted || strings.Trim(v.After, "0") == ""), nil

	case "GitLab":
		switch h.Get("X-Gitlab-Event") {
		case "Push Hook":
		default:
			return pushEvent{Ignore: fmt.Sprintf("evento %q (solo se despliega con push)", h.Get("X-Gitlab-Event"))}, nil
		}
		var v struct {
			Ref          string `json:"ref"`
			After        string `json:"after"`
			CheckoutSHA  string `json:"checkout_sha"`
			UserUsername string `json:"user_username"`
		}
		if err := json.Unmarshal(body, &v); err != nil {
			return pushEvent{}, errors.New("JSON inválido")
		}
		ev := pushEvent{SHA: cmp.Or(v.CheckoutSHA, v.After), Pusher: v.UserUsername}
		return refEvent(ev, v.Ref, strings.Trim(v.After, "0") == ""), nil

	case "Bitbucket":
		switch h.Get("X-Event-Key") {
		case "diagnostics:ping":
			return pushEvent{Ping: true}, nil
		case "repo:push":
		default:
			return pushEvent{Ignore: fmt.Sprintf("evento %q (solo se despliega con push)", h.Get("X-Event-Key"))}, nil
		}
		var v struct {
			Actor struct {
				Nickname    string `json:"nickname"`
				DisplayName string `json:"display_name"`
			} `json:"actor"`
			Push struct {
				Changes []struct {
					New *struct {
						Type   string `json:"type"`
						Name   string `json:"name"`
						Target struct {
							Hash string `json:"hash"`
						} `json:"target"`
					} `json:"new"`
				} `json:"changes"`
			} `json:"push"`
		}
		if err := json.Unmarshal(body, &v); err != nil {
			return pushEvent{}, errors.New("JSON inválido")
		}
		pusher := cmp.Or(v.Actor.Nickname, v.Actor.DisplayName)
		// Un push puede traer varias ramas: se devuelve la última rama actualizada.
		for i := len(v.Push.Changes) - 1; i >= 0; i-- {
			if n := v.Push.Changes[i].New; n != nil && n.Type == "branch" {
				return pushEvent{Branch: n.Name, SHA: n.Target.Hash, Pusher: pusher}, nil
			}
		}
		return pushEvent{Ignore: "el push no actualiza ninguna rama (tag o rama borrada)"}, nil
	}
	return pushEvent{}, errors.New("proveedor no reconocido")
}

func refEvent(ev pushEvent, ref string, deleted bool) pushEvent {
	branch, ok := strings.CutPrefix(ref, "refs/heads/")
	switch {
	case !ok:
		ev.Ignore = fmt.Sprintf("%s no es una rama", ref)
	case deleted:
		ev.Ignore = fmt.Sprintf("se borró la rama %s", branch)
	}
	if ok {
		ev.Branch = branch
	}
	return ev
}
