package proxy

import (
	"strings"
	"testing"
)

func TestCaddyfile(t *testing.T) {
	got := Caddyfile([]Route{
		{Hosts: []string{"www.b.com", "b.com"}, TLS: TLSAuto, Upstream: "d-2", Port: 8000},
		{Hosts: []string{"a.test"}, TLS: TLSInternal, Upstream: "d-1", Port: 3000},
	})
	want := `# Generado por Gestionador. No editar: se reescribe en cada cambio.
{
	admin localhost:2019
}

a.test {
	tls internal
	encode zstd gzip
	reverse_proxy d-1:3000 {
		lb_try_duration 10s
	}
}

b.com, www.b.com {
	encode zstd gzip
	reverse_proxy d-2:8000 {
		lb_try_duration 10s
	}
}
`
	if got != want {
		t.Errorf("Caddyfile:\n%s\nwant:\n%s", got, want)
	}
	if strings.Contains(Caddyfile(nil), "reverse_proxy") {
		t.Error("sin rutas no debe haber sitios")
	}
}

func TestEnsureCmdPorts(t *testing.T) {
	cmd := ensureCmd(Ports{HTTP: 8080, HTTPS: 8443})
	for _, want := range []string{"-p 8080:80", "-p 8443:443 -p 8443:443/udp", "gestionador.ports=8080,8443", "docker network create gestionador"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("falta %q", want)
		}
	}
}
