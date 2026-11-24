// Package sshx encapsula las conexiones SSH a los servidores gestionados:
// autenticación, verificación de la llave del host (TOFU), ejecución de comandos
// y un pool que reutiliza conexiones abiertas.
package sshx

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

const dialTimeout = 10 * time.Second

var ErrHostKeyMismatch = errors.New("la llave del host cambió desde la última conexión; podría tratarse de un servidor reinstalado o de un ataque man-in-the-middle")

// Target describe cómo conectarse a un servidor.
type Target struct {
	Host string
	Port int
	User string
	// Exactamente uno de Signer o Password.
	Signer   ssh.Signer
	Password string
	// HostKey en formato authorized_keys. Vacío = primera conexión: se acepta
	// la llave que presente el servidor y se devuelve para fijarla (TOFU).
	HostKey string
}

func (t Target) addr() string {
	return net.JoinHostPort(t.Host, strconv.Itoa(t.Port))
}

// Dial abre una conexión SSH y devuelve también la llave que presentó el host.
func Dial(ctx context.Context, t Target) (*ssh.Client, ssh.PublicKey, error) {
	var pinned ssh.PublicKey
	if t.HostKey != "" {
		k, _, _, _, err := ssh.ParseAuthorizedKey([]byte(t.HostKey))
		if err != nil {
			return nil, nil, fmt.Errorf("llave del host guardada inválida: %w", err)
		}
		pinned = k
	}

	var auth []ssh.AuthMethod
	switch {
	case t.Signer != nil:
		auth = append(auth, ssh.PublicKeys(t.Signer))
	case t.Password != "":
		auth = append(auth, ssh.Password(t.Password),
			// Algunos servidores solo ofrecen keyboard-interactive para contraseñas.
			ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range answers {
					answers[i] = t.Password
				}
				return answers, nil
			}))
	default:
		return nil, nil, errors.New("no hay credenciales para conectarse")
	}

	var presented ssh.PublicKey
	cfg := &ssh.ClientConfig{
		User: t.User,
		Auth: auth,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			presented = key
			if pinned != nil && !bytes.Equal(key.Marshal(), pinned.Marshal()) {
				return ErrHostKeyMismatch
			}
			return nil
		},
		Timeout: dialTimeout,
	}

	dialer := net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", t.addr())
	if err != nil {
		return nil, nil, fmt.Errorf("no se pudo conectar a %s: %w", t.addr(), err)
	}

	// El handshake SSH no recibe contexto: usamos un deadline en el socket.
	deadline := time.Now().Add(dialTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)

	c, chans, reqs, err := ssh.NewClientConn(conn, t.addr(), cfg)
	if err != nil {
		conn.Close()
		if errors.Is(err, ErrHostKeyMismatch) {
			return nil, nil, ErrHostKeyMismatch
		}
		// "message type 51" = SSH_MSG_USERAUTH_FAILURE recibido durante keyboard-interactive:
		// x/crypto lo reporta como error de protocolo, pero es una contraseña rechazada.
		if msg := err.Error(); strings.Contains(msg, "unable to authenticate") || strings.Contains(msg, "unexpected message type 51") {
			return nil, nil, errors.New("autenticación rechazada: revisa el usuario y las credenciales")
		}
		return nil, nil, fmt.Errorf("handshake SSH: %w", err)
	}
	_ = conn.SetDeadline(time.Time{})

	return ssh.NewClient(c, chans, reqs), presented, nil
}

// Result es la salida de un comando remoto.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Run ejecuta cmd en una sesión nueva. Si ctx se cancela, la sesión se cierra.
func Run(ctx context.Context, client *ssh.Client, cmd string) (Result, error) {
	session, err := client.NewSession()
	if err != nil {
		return Result{}, fmt.Errorf("abrir sesión: %w", err)
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	done := make(chan error, 1)
	go func() { done <- session.Run(cmd) }()

	select {
	case <-ctx.Done():
		_ = session.Close()
		return Result{}, ctx.Err()
	case err := <-done:
		res := Result{Stdout: stdout.String(), Stderr: stderr.String()}
		var exitErr *ssh.ExitError
		if errors.As(err, &exitErr) {
			res.ExitCode = exitErr.ExitStatus()
			return res, nil
		}
		return res, err
	}
}

// Fingerprint devuelve la huella SHA256 de una llave en formato authorized_keys.
func Fingerprint(authorizedKey string) string {
	k, _, _, _, err := ssh.ParseAuthorizedKey([]byte(authorizedKey))
	if err != nil {
		return ""
	}
	return ssh.FingerprintSHA256(k)
}

// MarshalAuthorizedKey convierte una llave pública a una línea authorized_keys (sin salto final).
func MarshalAuthorizedKey(k ssh.PublicKey) string {
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(k)))
}

// ParsePrivateKey valida una llave privada en PEM/OpenSSH.
func ParsePrivateKey(pemBytes []byte) (ssh.Signer, error) {
	signer, err := ssh.ParsePrivateKey(pemBytes)
	var missing *ssh.PassphraseMissingError
	if errors.As(err, &missing) {
		return nil, errors.New("la llave privada está protegida con passphrase; usa una llave sin passphrase")
	}
	if err != nil {
		return nil, fmt.Errorf("llave privada inválida: %w", err)
	}
	return signer, nil
}

// GenerateKey crea una llave ed25519 y la devuelve en formato OpenSSH (PEM).
func GenerateKey(comment string) ([]byte, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	block, err := ssh.MarshalPrivateKey(priv, comment)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(block), nil
}

// Pool reutiliza una conexión SSH por servidor.
type Pool struct {
	mu      sync.Mutex
	clients map[string]*ssh.Client
}

func NewPool() *Pool {
	return &Pool{clients: make(map[string]*ssh.Client)}
}

// Run ejecuta cmd en el servidor id, abriendo la conexión con dial si no hay una.
// Si falla una conexión reutilizada (p. ej. el servidor se reinició), reintenta una vez.
func (p *Pool) Run(ctx context.Context, id string, dial func(context.Context) (*ssh.Client, error), cmd string) (Result, error) {
	for attempt := 0; ; attempt++ {
		client, reused, err := p.get(ctx, id, dial)
		if err != nil {
			return Result{}, err
		}
		res, err := Run(ctx, client, cmd)
		if err == nil || ctx.Err() != nil {
			return res, err
		}
		p.drop(id, client)
		if !reused || attempt > 0 {
			return res, err
		}
	}
}

func (p *Pool) get(ctx context.Context, id string, dial func(context.Context) (*ssh.Client, error)) (*ssh.Client, bool, error) {
	p.mu.Lock()
	if c, ok := p.clients[id]; ok {
		p.mu.Unlock()
		return c, true, nil
	}
	p.mu.Unlock()

	c, err := dial(ctx)
	if err != nil {
		return nil, false, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if existing, ok := p.clients[id]; ok { // otra goroutine ganó la carrera
		c.Close()
		return existing, true, nil
	}
	p.clients[id] = c
	return c, false, nil
}

func (p *Pool) drop(id string, c *ssh.Client) {
	p.mu.Lock()
	if p.clients[id] == c {
		delete(p.clients, id)
	}
	p.mu.Unlock()
	c.Close()
}

// Forget cierra la conexión del servidor id (al eliminarlo o cambiar credenciales).
func (p *Pool) Forget(id string) {
	p.mu.Lock()
	c, ok := p.clients[id]
	delete(p.clients, id)
	p.mu.Unlock()
	if ok {
		c.Close()
	}
}

func (p *Pool) CloseAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, c := range p.clients {
		c.Close()
		delete(p.clients, id)
	}
}
