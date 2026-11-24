package projects

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"gestionador/internal/proxy"
)

type Domain struct {
	ID        string        `json:"id"`
	ProjectID string        `json:"projectId"`
	Hostname  string        `json:"hostname"`
	TLSMode   proxy.TLSMode `json:"tlsMode"`
	CreatedAt time.Time     `json:"createdAt"`
}

var (
	ErrDomainNotFound  = errors.New("dominio no encontrado")
	ErrDuplicateDomain = errors.New("ese dominio ya está asignado a un proyecto")
)

var hostnameRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// normalizeHostname acepta "https://App.Example.com/" y devuelve "app.example.com".
func normalizeHostname(h string) (string, error) {
	h = strings.ToLower(strings.TrimSpace(h))
	h = strings.TrimPrefix(strings.TrimPrefix(h, "https://"), "http://")
	h = strings.TrimSuffix(strings.TrimSuffix(h, "/"), ".")
	switch {
	case h == "":
		return "", invalid("escribe un dominio")
	case strings.HasPrefix(h, "*."):
		return "", invalid("los dominios comodín (*.ejemplo.com) todavía no están soportados")
	case strings.ContainsAny(h, "/:@ "):
		return "", invalid("escribe solo el dominio, sin ruta ni puerto (ej. app.ejemplo.com)")
	case net.ParseIP(h) != nil:
		return "", invalid("usa un nombre de dominio, no una IP")
	case len(h) > 253 || !hostnameRe.MatchString(h):
		return "", invalid("dominio inválido: %q", h)
	}
	return h, nil
}

// ---------- Store ----------

func (st *Store) ListDomains(ctx context.Context, projectID string) ([]Domain, error) {
	rows, err := st.db.QueryContext(ctx, `SELECT id, project_id, hostname, tls_mode, created_at FROM domains
		WHERE project_id = $1 ORDER BY created_at`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Domain{}
	for rows.Next() {
		var d Domain
		if err := rows.Scan(&d.ID, &d.ProjectID, &d.Hostname, &d.TLSMode, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (st *Store) GetDomain(ctx context.Context, id string) (Domain, error) {
	var d Domain
	err := st.db.QueryRowContext(ctx, `SELECT id, project_id, hostname, tls_mode, created_at FROM domains WHERE id = $1`, id).
		Scan(&d.ID, &d.ProjectID, &d.Hostname, &d.TLSMode, &d.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrDomainNotFound
	}
	return d, err
}

func (st *Store) CreateDomain(ctx context.Context, d Domain) error {
	_, err := st.db.ExecContext(ctx, `INSERT INTO domains (id, project_id, hostname, tls_mode, created_at) VALUES ($1, $2, $3, $4, $5)`,
		d.ID, d.ProjectID, d.Hostname, d.TLSMode, d.CreatedAt)
	if err != nil && isUniqueViolation(err) {
		return ErrDuplicateDomain
	}
	return err
}

func (st *Store) DeleteDomain(ctx context.Context, id string) error {
	_, err := st.db.ExecContext(ctx, `DELETE FROM domains WHERE id = $1`, id)
	return err
}

func (st *Store) CountDomains(ctx context.Context, projectID string) (int, error) {
	var n int
	err := st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM domains WHERE project_id = $1`, projectID).Scan(&n)
	return n, err
}

func (st *Store) SetLiveDeployment(ctx context.Context, projectID, deploymentID string) error {
	_, err := st.db.ExecContext(ctx, `UPDATE projects SET live_deployment_id = $2 WHERE id = $1`, projectID, nullString(deploymentID))
	return err
}

// serverDomain es un dominio con lo necesario para enrutarlo.
type serverDomain struct {
	Domain
	ContainerPort    int
	LiveDeploymentID string
}

func (st *Store) domainsForServer(ctx context.Context, serverID string) ([]serverDomain, error) {
	rows, err := st.db.QueryContext(ctx, `SELECT d.id, d.project_id, d.hostname, d.tls_mode, d.created_at,
			p.container_port, COALESCE(p.live_deployment_id, '')
		FROM domains d JOIN projects p ON p.id = d.project_id
		WHERE p.server_id = $1 ORDER BY d.hostname`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []serverDomain
	for rows.Next() {
		var d serverDomain
		if err := rows.Scan(&d.ID, &d.ProjectID, &d.Hostname, &d.TLSMode, &d.CreatedAt, &d.ContainerPort, &d.LiveDeploymentID); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (st *Store) ProxyPorts(ctx context.Context, serverID string) (proxy.Ports, error) {
	p := proxy.DefaultPorts
	err := st.db.QueryRowContext(ctx, `SELECT http_port, https_port FROM server_proxy WHERE server_id = $1`, serverID).Scan(&p.HTTP, &p.HTTPS)
	if errors.Is(err, sql.ErrNoRows) {
		return proxy.DefaultPorts, nil
	}
	return p, err
}

func (st *Store) SetProxyPorts(ctx context.Context, serverID string, p proxy.Ports) error {
	_, err := st.db.ExecContext(ctx, `INSERT INTO server_proxy (server_id, http_port, https_port) VALUES ($1, $2, $3)
		ON CONFLICT (server_id) DO UPDATE SET http_port = excluded.http_port, https_port = excluded.https_port`,
		serverID, p.HTTP, p.HTTPS)
	return err
}

// ---------- Service ----------

func upstreamAlias(deploymentID string) string { return "d-" + deploymentID }

func (s *Service) Domains(ctx context.Context, projectID string) ([]Domain, error) {
	if _, err := s.store.GetProject(ctx, projectID); err != nil {
		return nil, err
	}
	return s.store.ListDomains(ctx, projectID)
}

// AddDomain guarda el dominio y lo activa en Caddy. Si Caddy no lo acepta, no se guarda.
func (s *Service) AddDomain(ctx context.Context, projectID, hostname string, mode proxy.TLSMode) (Domain, error) {
	p, err := s.store.GetProject(ctx, projectID)
	if err != nil {
		return Domain{}, err
	}
	host, err := normalizeHostname(hostname)
	if err != nil {
		return Domain{}, err
	}
	if mode == "" {
		mode = proxy.TLSAuto
	}
	if mode != proxy.TLSAuto && mode != proxy.TLSInternal {
		return Domain{}, invalid("modo de certificado inválido")
	}

	d := Domain{ID: uuid.Must(uuid.NewV7()).String(), ProjectID: p.ID, Hostname: host, TLSMode: mode, CreatedAt: time.Now().UTC()}
	if err := s.store.CreateDomain(ctx, d); err != nil {
		return Domain{}, err
	}
	err = s.attachLive(ctx, p)
	if err == nil {
		err = s.syncProxy(ctx, p.ServerID)
	}
	if err != nil {
		_ = s.store.DeleteDomain(context.WithoutCancel(ctx), d.ID)
		return Domain{}, &ProxyError{Err: err}
	}
	slog.Info("dominio agregado", "project", p.Name, "domain", host)
	return d, nil
}

// RemoveDomain lo borra y actualiza Caddy. Si Caddy no responde, devuelve un aviso.
func (s *Service) RemoveDomain(ctx context.Context, id string) (warning string, err error) {
	d, err := s.store.GetDomain(ctx, id)
	if err != nil {
		return "", err
	}
	p, err := s.store.GetProject(ctx, d.ProjectID)
	if err != nil {
		return "", err
	}
	if err := s.store.DeleteDomain(ctx, id); err != nil {
		return "", err
	}
	if err := s.syncProxy(ctx, p.ServerID); err != nil {
		return "Se quitó el dominio, pero no se pudo actualizar Caddy: " + err.Error(), nil
	}
	return "", nil
}

// ProxyError: el servidor no pudo aplicar la configuración de Caddy (422).
type ProxyError struct{ Err error }

func (e *ProxyError) Error() string { return e.Err.Error() }

// attachLive conecta el contenedor en ejecución a la red de Caddy con su alias.
// Los contenedores creados desde la fase 4 ya lo están; los anteriores se conectan en caliente.
func (s *Service) attachLive(ctx context.Context, p Project) error {
	live, err := s.liveDeployment(ctx, p)
	if err != nil || live == "" {
		return err // sin deploys: se enrutará cuando haya uno
	}
	res, err := s.servers.Run(ctx, p.ServerID, fmt.Sprintf(
		`docker network inspect %[1]s >/dev/null 2>&1 || docker network create %[1]s >/dev/null
docker container inspect %[2]s >/dev/null 2>&1 || exit 0
docker inspect -f '{{json .NetworkSettings.Networks}}' %[2]s | grep -q '"%[1]s"' || docker network connect --alias %[3]s %[1]s %[2]s`,
		proxy.Network, p.containerName(), upstreamAlias(live)))
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("no se pudo conectar el contenedor a la red del proxy: %s", strings.TrimSpace(res.Stdout+res.Stderr))
	}
	return nil
}

// liveDeployment es el deploy cuyo contenedor recibe tráfico. Para proyectos
// desplegados antes de la fase 4 se usa el último deploy exitoso.
func (s *Service) liveDeployment(ctx context.Context, p Project) (string, error) {
	if p.LiveDeploymentID != "" {
		return p.LiveDeploymentID, nil
	}
	d, err := s.store.LatestSucceeded(ctx, p.ID)
	if errors.Is(err, ErrNothingToRestore) {
		return "", nil
	}
	return d.ID, err
}

// syncProxy regenera el Caddyfile del servidor con todos sus dominios.
func (s *Service) syncProxy(ctx context.Context, serverID string) error {
	unlock := s.proxy.Lock(serverID)
	defer unlock()

	domains, err := s.store.domainsForServer(ctx, serverID)
	if err != nil {
		return err
	}
	ports, err := s.store.ProxyPorts(ctx, serverID)
	if err != nil {
		return err
	}

	// Una ruta por proyecto y modo de certificado.
	type key struct {
		project string
		tls     proxy.TLSMode
	}
	byKey := map[key]*proxy.Route{}
	var order []key
	for _, d := range domains {
		live := d.LiveDeploymentID
		if live == "" {
			p, err := s.store.GetProject(ctx, d.ProjectID)
			if err != nil {
				return err
			}
			if live, err = s.liveDeployment(ctx, p); err != nil {
				return err
			}
		}
		if live == "" {
			continue // nunca se desplegó: no hay a dónde enviar el tráfico
		}
		k := key{d.ProjectID, d.TLSMode}
		if r, ok := byKey[k]; ok {
			r.Hosts = append(r.Hosts, d.Hostname)
			continue
		}
		byKey[k] = &proxy.Route{Hosts: []string{d.Hostname}, TLS: d.TLSMode, Upstream: upstreamAlias(live), Port: d.ContainerPort}
		order = append(order, k)
	}
	routes := make([]proxy.Route, 0, len(order))
	for _, k := range order {
		routes = append(routes, *byKey[k])
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	return s.proxy.Apply(ctx, serverID, ports, routes)
}

// ProxyInfo es el estado de Caddy en un servidor.
type ProxyInfo struct {
	proxy.Ports
	State   string `json:"state"`
	Domains int    `json:"domains"`
	Error   string `json:"error,omitempty"`
}

func (s *Service) ProxyInfo(ctx context.Context, serverID string) (ProxyInfo, error) {
	ports, err := s.store.ProxyPorts(ctx, serverID)
	if err != nil {
		return ProxyInfo{}, err
	}
	domains, err := s.store.domainsForServer(ctx, serverID)
	if err != nil {
		return ProxyInfo{}, err
	}
	info := ProxyInfo{Ports: ports, Domains: len(domains), State: "unknown"}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if st, err := s.proxy.Status(ctx, serverID); err != nil {
		info.Error = err.Error()
	} else {
		info.State = st.State
	}
	return info, nil
}

// SetProxyPorts cambia los puertos de Caddy; si ya hay dominios, lo recrea.
func (s *Service) SetProxyPorts(ctx context.Context, serverID string, p proxy.Ports) (ProxyInfo, error) {
	if p.HTTP < 1 || p.HTTP > 65535 || p.HTTPS < 1 || p.HTTPS > 65535 || p.HTTP == p.HTTPS {
		return ProxyInfo{}, invalid("puertos inválidos")
	}
	old, err := s.store.ProxyPorts(ctx, serverID)
	if err != nil {
		return ProxyInfo{}, err
	}
	if err := s.store.SetProxyPorts(ctx, serverID, p); err != nil {
		return ProxyInfo{}, err
	}
	if err := s.syncProxy(ctx, serverID); err != nil {
		_ = s.store.SetProxyPorts(context.WithoutCancel(ctx), serverID, old)
		return ProxyInfo{}, &ProxyError{Err: err}
	}
	return s.ProxyInfo(ctx, serverID)
}

// ---------- Verificación ----------

type DomainCheck struct {
	DNS struct {
		OK        bool     `json:"ok"`
		Records   []string `json:"records"`
		ServerIPs []string `json:"serverIps"`
		Message   string   `json:"message"`
	} `json:"dns"`
	HTTPS struct {
		OK      bool   `json:"ok"`
		Status  int    `json:"status"`
		Issuer  string `json:"issuer"`
		Message string `json:"message"`
	} `json:"https"`
}

// CheckDomain revisa el DNS y pide la página por HTTPS directamente al servidor
// (con el dominio como SNI), así funciona aunque el DNS todavía no apunte.
func (s *Service) CheckDomain(ctx context.Context, id string) (DomainCheck, error) {
	d, err := s.store.GetDomain(ctx, id)
	if err != nil {
		return DomainCheck{}, err
	}
	p, err := s.store.GetProject(ctx, d.ProjectID)
	if err != nil {
		return DomainCheck{}, err
	}
	ports, err := s.store.ProxyPorts(ctx, p.ServerID)
	if err != nil {
		return DomainCheck{}, err
	}
	var c DomainCheck
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	// DNS
	serverIPs := lookup(ctx, p.ServerHost)
	c.DNS.ServerIPs = serverIPs
	c.DNS.Records = lookup(ctx, d.Hostname)
	switch {
	case len(c.DNS.Records) == 0:
		c.DNS.Message = "El dominio no resuelve. Crea un registro A (o AAAA) que apunte a " + strings.Join(serverIPs, ", ") + "."
	case slices.ContainsFunc(c.DNS.Records, func(ip string) bool { return slices.Contains(serverIPs, ip) }):
		c.DNS.OK = true
		c.DNS.Message = "El dominio apunta a este servidor."
	default:
		c.DNS.Message = "El dominio apunta a " + strings.Join(c.DNS.Records, ", ") + " y el servidor es " + strings.Join(serverIPs, ", ") +
			". Si usas un proxy como Cloudflare es normal; si no, corrige el registro A."
	}
	if d.TLSMode == proxy.TLSAuto && c.DNS.OK && allPrivate(serverIPs) {
		c.DNS.Message += " Ojo: la IP es privada; Let's Encrypt necesita llegar al servidor desde internet."
	}

	// HTTPS contra el servidor
	addr := net.JoinHostPort(p.ServerHost, strconv.Itoa(ports.HTTPS))
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, addr)
		},
		TLSClientConfig: &tls.Config{ServerName: d.Hostname, InsecureSkipVerify: d.TLSMode == proxy.TLSInternal},
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 12 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+d.Hostname+"/", nil)
	resp, err := client.Do(req)
	if err != nil {
		c.HTTPS.Message = httpsError(err, d.TLSMode)
		return c, nil
	}
	resp.Body.Close()
	c.HTTPS.Status = resp.StatusCode
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		c.HTTPS.Issuer = resp.TLS.PeerCertificates[0].Issuer.CommonName
	}
	c.HTTPS.OK = resp.StatusCode < 500
	switch {
	case resp.StatusCode == http.StatusBadGateway:
		c.HTTPS.Message = "Caddy responde pero la app no (502). ¿Está desplegada y en ejecución?"
	case !c.HTTPS.OK:
		c.HTTPS.Message = fmt.Sprintf("La app respondió con error %d.", resp.StatusCode)
	case d.TLSMode == proxy.TLSInternal:
		c.HTTPS.Message = "Responde con un certificado interno de Caddy (los navegadores lo marcarán como no confiable)."
	default:
		c.HTTPS.Message = "Responde con un certificado válido."
	}
	return c, nil
}

func lookup(ctx context.Context, host string) []string {
	if ip := net.ParseIP(host); ip != nil {
		return []string{ip.String()}
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return []string{}
	}
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, a.IP.String())
	}
	return out
}

func allPrivate(ips []string) bool {
	for _, s := range ips {
		if ip := net.ParseIP(s); ip != nil && !ip.IsPrivate() && !ip.IsLoopback() {
			return false
		}
	}
	return len(ips) > 0
}

func httpsError(err error, mode proxy.TLSMode) string {
	var certErr *tls.CertificateVerificationError
	var unknown x509.UnknownAuthorityError
	var hostErr x509.HostnameError
	switch {
	case errors.As(err, &certErr), errors.As(err, &unknown), errors.As(err, &hostErr):
		if mode == proxy.TLSAuto {
			return "El certificado todavía no es válido. Si acabas de agregar el dominio, Caddy puede tardar un minuto en obtenerlo; revisa que el DNS apunte al servidor y que los puertos 80/443 estén abiertos."
		}
		return "Error de certificado: " + err.Error()
	case strings.Contains(err.Error(), "connection refused"):
		return "No hay nada escuchando en el puerto HTTPS del servidor. ¿Está Caddy en ejecución?"
	case strings.Contains(err.Error(), "timeout"), strings.Contains(err.Error(), "deadline"):
		return "El servidor no respondió por HTTPS (¿firewall bloqueando el puerto?)."
	case strings.Contains(err.Error(), "tls: internal error"):
		return "Caddy todavía no tiene certificado para este dominio. Revisa el DNS y vuelve a verificar en un minuto."
	}
	return "No se pudo conectar por HTTPS: " + err.Error()
}
