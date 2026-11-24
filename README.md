# Gestionador

[![CI](https://github.com/RenSunk/GestionadorServidores/actions/workflows/ci.yml/badge.svg)](https://github.com/RenSunk/GestionadorServidores/actions/workflows/ci.yml)
[![Licencia: AGPL-3.0](https://img.shields.io/badge/licencia-AGPL--3.0-blue)](LICENSE)

**Tu propio Heroku/Render en tus servidores.** Conecta máquinas Linux por SSH, apunta a un repositorio de Git y Gestionador construye, despliega y vigila tus apps con Docker: dominios con HTTPS automático, deploy al hacer push, equipos con permisos, métricas e historial de alertas.

Todo viene en **un solo binario de 20 MB** (API, interfaz web y base de datos) que no instala nada en los servidores.

![Dashboard de Gestionador](docs/screenshots/dashboard.png)

> **In English:** Gestionador is a self-hosted PaaS shipped as a single Go binary with an embedded React UI. It manages Linux servers over agentless SSH and deploys apps from Git with Docker or Podman: build-pack-style detection for Node, Python, Java and Go, automatic HTTPS via Caddy, zero-downtime swaps, push-to-deploy webhooks, role- and team-based access control, metrics history and alerts to Slack, Discord or any webhook. SQLite by default, PostgreSQL optional. Install with `curl -fsSL https://raw.githubusercontent.com/RenSunk/GestionadorServidores/main/scripts/install.sh | sudo bash`. The UI and docs are in Spanish; AI agents can follow the [step-by-step guide](#guía-para-agentes-de-ia).

## Contenido

- [Qué hace](#qué-hace) · [Capturas](#capturas)
- [Instalación en un servidor](#instalación-en-un-servidor) · [Acceso al panel](#acceso-al-panel) · [Primer uso](#primer-uso) · [Preparar un servidor de despliegue](#preparar-un-servidor-de-despliegue)
- [Ejecutar desde el código fuente](#ejecutar-desde-el-código-fuente)
- [Guía para agentes de IA](#guía-para-agentes-de-ia)
- [Configuración](#configuración) · [Cómo funciona](#cómo-funciona) · [Decisiones técnicas](#decisiones-técnicas) · [Seguridad](#seguridad) · [Stack](#stack)
- [Limitaciones conocidas](#limitaciones-conocidas) · [Roadmap](#roadmap) · [Licencia](#licencia)

## Qué hace

- **Servidores sin agente.** Agregas un servidor con una llave SSH y Gestionador lee CPU, RAM, disco y el estado de Docker cada 15 s con un script POSIX. Funciona con Docker y con Podman.
- **Deploy desde Git.** Con el Dockerfile del repo o sin él: detecta Node, Python, Java (Maven/Gradle) y Go, propone los comandos y genera el Dockerfile. Logs del build y de la app en vivo.
- **Si el deploy falla, la app sigue en pie.** Si el contenedor nuevo no arranca, se restaura el anterior. Rollback a cualquiera de las últimas 3 imágenes sin reconstruir.
- **Dominios con HTTPS automático** (Let's Encrypt vía Caddy) y **deploy sin corte**: el contenedor nuevo recibe tráfico solo cuando responde.
- **Deploy al hacer push** con webhooks firmados de GitHub, GitLab, Gitea/Forgejo y Bitbucket, o revisando la rama cada pocos minutos si el panel está detrás de NAT.
- **Variables de entorno cifradas**, límites de CPU y RAM, y cambios aplicados sin reconstruir cuando no hace falta.
- **Usuarios, roles y equipos.** Administrador, desarrollador o solo lectura; los equipos limitan qué proyectos y servidores ve cada persona. Invitaciones por enlace.
- **Métricas históricas y alertas.** Hasta 30 días de uso por servidor y por app. Alertas de servidor caído, recursos al límite, app detenida y deploy fallido, con aviso a Slack, Discord o cualquier webhook.

## Capturas

| | |
|---|---|
| ![Proyecto con su historial de deploys](docs/screenshots/proyecto.png) **Proyecto:** estado del contenedor, consumo y deploys con quién y qué los disparó. | ![Log de un deploy](docs/screenshots/deploy.png) **Deploy:** cada etapa con su log en vivo. |
| ![Nuevo proyecto con detección del lenguaje](docs/screenshots/nuevo-proyecto.png) **Nuevo proyecto:** detecta el lenguaje y genera el Dockerfile. | ![Servidor con historial de uso](docs/screenshots/servidor.png) **Servidor:** recursos en vivo e historial de hasta 30 días. |
| ![Historial de alertas](docs/screenshots/alertas.png) **Alertas:** activas e historial; también llegan a Slack o Discord. | ![Usuarios y equipos](docs/screenshots/equipo.png) **Equipo:** roles, invitaciones y equipos con acceso por proyecto. |

---

## Instalación en un servidor

Gestionador corre en una máquina Linux (el **panel**) y se conecta por SSH a los **servidores de despliegue**, donde corren las apps. Pueden ser la misma máquina.

```
 Navegador ──HTTPS──▶ Panel (Gestionador)  ──SSH──▶ Servidor 1 (Docker)
                      binario + SQLite      ──SSH──▶ Servidor 2 (Docker o Podman)
```

### Requisitos

| | Panel | Servidor de despliegue |
|---|---|---|
| Sistema | Linux amd64 o arm64 con systemd (Ubuntu 22.04+, Debian 12+, etc.) | Linux con SSH |
| Software | `curl` (nada más: no necesita Docker, Node ni base de datos) | Docker o Podman con `podman-docker` |
| Recursos | Menos de 50 MB de RAM | Lo que pidan tus apps |
| Red | Acceso SSH saliente a los servidores | Puertos 80 y 443 abiertos si usarás dominios |

### Instalar

```bash
curl -fsSL https://raw.githubusercontent.com/RenSunk/GestionadorServidores/main/scripts/install.sh | sudo bash
```

El script ([scripts/install.sh](scripts/install.sh)):

1. comprueba el sistema (Linux, amd64/arm64, systemd, root);
2. descarga el binario de la [última release](https://github.com/RenSunk/GestionadorServidores/releases) y **verifica su SHA-256**;
3. crea el usuario de sistema `gestionador`, `/etc/gestionador/env` y la unidad systemd endurecida;
4. arranca el servicio en `127.0.0.1:8080`, espera a que responda y muestra el **código de instalación**.

Es **idempotente**: volver a ejecutarlo actualiza a la última versión sin tocar los datos.

| Opción | Uso |
|---|---|
| `--version v0.1.0` | Instala una versión concreta. `curl … \| sudo bash -s -- --version v0.1.0` |
| `--binary RUTA` | Usa un binario local (desde un clon del repo): `sudo scripts/install.sh --binary bin/gestionador-linux-amd64` |
| `--addr HOST:PUERTO` | Dirección de escucha en la primera instalación (después, edita `/etc/gestionador/env`) |

Comprueba la instalación:

```bash
systemctl is-active gestionador               # → active
curl -fsS http://127.0.0.1:8080/api/health    # → {"database":"sqlite","status":"ok","version":"v…"}
/usr/local/bin/gestionador -version           # → gestionador v…
```

Instalación manual (sin script), PostgreSQL, respaldos y desinstalación: **[docs/instalacion.md](docs/instalacion.md)**.

### Acceso al panel

El panel escucha solo en `127.0.0.1:8080`. **No lo expongas directamente a internet**: la sesión debe viajar cifrada. Elige una opción:

**Con dominio (recomendado):** apunta un registro DNS `A` (por ejemplo, `panel.tudominio.com`) a la IP de la máquina, abre los puertos 80/443 y pon Caddy delante, que obtiene el certificado solo:

```bash
sudo apt-get install -y caddy   # Debian 12+ y Ubuntu 24.04+; en otros sistemas: https://caddyserver.com/docs/install
echo 'panel.tudominio.com {
	reverse_proxy 127.0.0.1:8080
}' | sudo tee /etc/caddy/Caddyfile
sudo systemctl reload caddy
curl -fsS https://panel.tudominio.com/api/health
```

> Si la máquina del panel también será servidor de despliegue con dominios, el proxy de las apps necesita los mismos puertos 80/443: cambia los puertos del proxy de apps en la página del servidor, o usa otra máquina para el panel.

**Sin dominio:** abre un túnel SSH desde tu computadora y entra a <http://localhost:8080>:

```bash
ssh -L 8080:127.0.0.1:8080 usuario@ip-del-panel
```

### Primer uso

1. **Crea la cuenta de administrador** en el panel. Si no entras desde la propia máquina, te pide el código de instalación que mostró el script (o `sudo journalctl -u gestionador | grep código`). Con el túnel SSH no hace falta.
2. **Agrega un servidor** en *Servidores → Agregar*: elige "Llave de Gestionador", copia la llave pública que muestra y agrégala al servidor (ver la sección siguiente).
3. **Crea un proyecto** en *Proyectos → Nuevo*: pega la URL de un repo (prueba con `https://github.com/render-examples/flask-hello-world`), pulsa *Analizar repositorio*, revisa los comandos detectados y despliega.
4. **Opcional:** agrega un dominio al proyecto, activa el deploy automático, invita al equipo y configura las alertas en *Configuración*.

### Preparar un servidor de despliegue

En cada servidor donde quieras desplegar apps (puede ser el mismo panel), con un usuario con `sudo`:

```bash
# 1. Docker (si no está instalado)
curl -fsSL https://get.docker.com | sudo sh

# 2. Usuario para Gestionador con permiso sobre Docker
sudo useradd -m -s /bin/bash deploy
sudo usermod -aG docker deploy

# 3. Llave pública de Gestionador (cópiala de Servidores → Agregar)
sudo -u deploy mkdir -p -m 700 /home/deploy/.ssh
echo 'ssh-ed25519 AAAA… gestionador' | sudo -u deploy tee -a /home/deploy/.ssh/authorized_keys
sudo chmod 600 /home/deploy/.ssh/authorized_keys
```

En el panel, registra el servidor con host = IP del servidor, usuario `deploy` y puerto SSH 22. Si algo falta, la página del servidor indica el comando exacto para arreglarlo. Para usar la propia máquina del panel, usa host `127.0.0.1`.

---

## Ejecutar desde el código fuente

Para desarrollar o probar sin instalar.

### Requisitos

| Herramienta | Versión | Comprobar |
|---|---|---|
| Go | 1.27+ (con Go ≥ 1.21 instalado, `go` descarga solo la versión exacta de `go.mod`) | `go version` |
| Node.js | 24+ (incluye npm) | `node --version` |
| make, git | cualquiera | `make --version`, `git --version` |

Para desplegar apps hace falta además un servidor con SSH y Docker; puede ser tu propia máquina (`127.0.0.1`) si tiene `sshd` y Docker.

### Compilar y ejecutar

```bash
git clone https://github.com/RenSunk/GestionadorServidores.git
cd GestionadorServidores
make build                          # frontend + binario → bin/gestionador
./bin/gestionador -data-dir ./data  # → http://localhost:8080
```

Abre <http://localhost:8080> y crea la cuenta de administrador (desde la misma máquina no pide código). Los datos quedan en `./data` (ignorado por git); borra esa carpeta para empezar de cero.

Sin `make`: `cd web && npm ci && npm run build && cd .. && CGO_ENABLED=0 go build -o bin/gestionador ./cmd/gestionador`.

### Desarrollo

```bash
make dev-api   # backend Go en http://localhost:8080
make dev-web   # frontend con hot-reload en http://localhost:5173 (usa este)
```

Vite redirige `/api/*` al backend, así que en desarrollo se trabaja en `:5173`.

### Pruebas y verificación

```bash
gofmt -l .                       # sin salida = formato correcto
go vet ./...
go test ./...                    # usa SQLite temporal; no necesita servicios externos
cd web && npx tsc -b && npm run lint
```

Las pruebas de métricas y alertas también corren sobre PostgreSQL: `TEST_DATABASE_URL=postgres://usuario:clave@localhost:5432/db go test ./internal/metrics ./internal/alerts`. El CI ([.github/workflows/ci.yml](.github/workflows/ci.yml)) ejecuta todo esto en cada push.

Para publicar una versión: `git tag v0.2.0 && git push origin v0.2.0`. El workflow de release compila los binarios, calcula las sumas y crea la release que usa el script de instalación.

---

## Guía para agentes de IA

Instrucciones para que un agente (Claude, Codex, etc.) instale o ejecute Gestionador sin intervención, salvo las decisiones que corresponden al usuario. Cada paso tiene un comando de verificación: **si una verificación falla, detente, consulta [Solución de problemas](#solución-de-problemas) y no continúes con el paso siguiente.**

### Reglas

1. **Pregunta antes de empezar:** (a) ¿instalar en un servidor (*modo A*) o ejecutar desde el código (*modo B*)?; (b) en modo A, ¿hay un dominio para el panel? Si no, usa el túnel SSH.
2. **Credenciales:** no inventes la contraseña del administrador. Lo preferible es que la persona cree la cuenta en el navegador. Si te pide crearla tú, pídele nombre, email y contraseña (de 10 a 72 caracteres) y no los guardes en archivos, scripts ni en el historial del shell.
3. **No expongas el puerto 8080** a internet ni cambies `--addr` a `0.0.0.0` sin HTTPS delante.
4. **Nunca borres, muestres ni copies fuera** `/var/lib/gestionador` (base de datos y `secret.key`) o `/etc/gestionador/env`. Sin `secret.key` los secretos guardados no se pueden recuperar.
5. El script de instalación es idempotente: reejecutarlo es seguro y es la forma de actualizar.
6. No modifiques `~/.bashrc` ni configuraciones globales del sistema más allá de lo indicado aquí.

### Modo A: instalar en un servidor Linux

| # | Acción | Verificación (resultado esperado) |
|---|---|---|
| A1 | Comprobar requisitos: `uname -sm; systemctl --version \| head -1; command -v curl; sudo -n true && echo sudo-ok` | `Linux x86_64` o `Linux aarch64`; una versión de systemd; una ruta de curl; `sudo-ok` (si pide contraseña, pídesela a la persona o que ejecute el paso A2 ella misma) |
| A2 | `curl -fsSL https://raw.githubusercontent.com/RenSunk/GestionadorServidores/main/scripts/install.sh \| sudo bash` | Termina con código 0 y muestra `✓ Responde en http://127.0.0.1:8080` |
| A3 | `systemctl is-active gestionador && curl -fsS http://127.0.0.1:8080/api/health` | `active` y JSON con `"status":"ok"` |
| A4 | Acceso. **Con dominio:** confirma que `getent hosts DOMINIO` devuelve la IP pública de la máquina; luego instala Caddy como en [Acceso al panel](#acceso-al-panel). **Sin dominio:** indica a la persona el comando del túnel SSH. | `curl -fsS https://DOMINIO/api/health` devuelve `"status":"ok"` (solo con dominio) |
| A5 | Cuenta de administrador. Preferido: la persona entra al panel y la crea (código: `sudo journalctl -u gestionador -o cat \| grep -o 'código=[A-Z0-9]*' \| tail -1`). Alternativa por API, **ejecutada en la máquina del panel** (no pide código): ver abajo. | `curl -fsS http://127.0.0.1:8080/api/auth/status` → `"setupRequired":false` |
| A6 | (Opcional) Registrar un servidor de despliegue: ver abajo. | El servidor queda `"status":"online"` |

Crear el administrador por API (A5). Requiere `jq` (`sudo apt-get install -y jq`); los datos los escribe la persona y solo viven en variables del shell:

```bash
read -r -p "Nombre: " GN; read -r -p "Email: " GE; read -r -s -p "Contraseña: " GP; echo
jq -n --arg n "$GN" --arg e "$GE" --arg p "$GP" '{name:$n,email:$e,password:$p}' \
  | curl -fsS -o /dev/null -w '%{http_code}\n' -H 'Content-Type: application/json' -d @- http://127.0.0.1:8080/api/auth/setup
# → 201 (409 = ya existe un administrador)
```

Registrar un servidor de despliegue por API (A6). Requiere iniciar sesión y haber [preparado el servidor](#preparar-un-servidor-de-despliegue) con la llave pública que devuelve `/api/ssh-key`:

```bash
J=$(mktemp)   # cookie de sesión temporal (usa GE y GP del paso anterior)
jq -n --arg e "$GE" --arg p "$GP" '{email:$e,password:$p}' \
  | curl -fsS -c "$J" -H 'Content-Type: application/json' -d @- http://127.0.0.1:8080/api/auth/login >/dev/null
curl -fsS -b "$J" http://127.0.0.1:8080/api/ssh-key | jq -r .publicKey     # → agrégala a authorized_keys del servidor
curl -fsS -b "$J" -H 'Content-Type: application/json' \
  -d '{"name":"produccion-1","host":"IP_DEL_SERVIDOR","port":22,"username":"deploy","authMethod":"instance_key"}' \
  http://127.0.0.1:8080/api/servers | jq '{name, status, lastError}'          # → "status": "online"
rm -f "$J"; unset GP
```

### Modo B: ejecutar desde el código fuente

| # | Acción | Verificación |
|---|---|---|
| B1 | `go version; node --version; make --version \| head -1` | Go ≥ 1.21 (descarga 1.27 solo), Node ≥ 24, make presente. Si falta algo, pide permiso antes de instalarlo |
| B2 | `git clone https://github.com/RenSunk/GestionadorServidores.git && cd GestionadorServidores && make build` | Existe `bin/gestionador` y `./bin/gestionador -version` imprime `gestionador …` |
| B3 | `./bin/gestionador -data-dir ./data` (en segundo plano o en otra terminal) | El log muestra `servidor iniciado addr=:8080`; `curl -fsS http://127.0.0.1:8080/api/health` → `"status":"ok"` |
| B4 | La persona abre <http://localhost:8080> y crea la cuenta (o usa la API de A5 contra `127.0.0.1:8080`) | `/api/auth/status` → `"setupRequired":false` |
| B5 | (Opcional) Verificar el código: `go test ./... && cd web && npx tsc -b && npm run lint` | Todo termina sin errores |

Para detener la instancia de modo B: `kill -INT <pid>` (apaga ordenadamente). Si el puerto 8080 está ocupado, usa `-addr :18080`.

### Solución de problemas

| Síntoma | Causa probable | Qué hacer |
|---|---|---|
| El script dice `no se pudo descargar …/releases/…` | El repo aún no tiene releases, o no hay salida a GitHub | Usa modo B para compilar y luego `sudo scripts/install.sh --binary bin/gestionador-linux-amd64` desde el clon |
| `la suma SHA-256 no coincide` | Descarga corrupta o interceptada | Reintenta; si persiste, no instales y avisa a la persona |
| `no se detectó systemd` | Contenedor, WSL sin systemd u otro init | Usa modo B, o ejecuta el binario con el supervisor del sistema: `gestionador -addr 127.0.0.1:8080 -data-dir /var/lib/gestionador` |
| El servicio no responde tras instalar | Puerto ocupado o error al arrancar | `sudo journalctl -u gestionador -n 50`; cambia `GESTIONADOR_ADDR` en `/etc/gestionador/env` y `sudo systemctl restart gestionador` |
| `bind: address already in use` en modo B | Otro proceso usa el 8080 | Ejecuta con `-addr :18080` |
| `go: downloading go1.27…` falla | Sin acceso a internet para la toolchain | Instala Go 1.27+ desde <https://go.dev/dl/> |
| El servidor de despliegue queda *Sin conexión*: `autenticación rechazada` | La llave pública no está en `authorized_keys` o el usuario es otro | Revisa el paso 3 de [Preparar un servidor](#preparar-un-servidor-de-despliegue) y los permisos (700 para `.ssh`, 600 para el archivo) |
| *La llave del host cambió* | El servidor se reinstaló (o hay un intermediario) | Confírmalo con la persona antes de hacer nada; luego elimina y vuelve a agregar el servidor |
| Docker: *sin permisos* | El usuario no está en el grupo `docker` | `sudo usermod -aG docker deploy` y *Verificar ahora* en el panel |
| El panel pide un *código de instalación* | La cuenta se crea desde otra máquina o a través del proxy | `sudo journalctl -u gestionador -o cat \| grep -o 'código=[A-Z0-9]*' \| tail -1` |
| El login falla tras varios intentos | Límite de intentos (8 fallos en 15 min) | Espera 15 minutos |

### Ubicaciones

| Qué | Dónde (instalación con el script) |
|---|---|
| Binario | `/usr/local/bin/gestionador` |
| Datos (SQLite, `secret.key`, builds) | `/var/lib/gestionador` |
| Configuración | `/etc/gestionador/env` (variables de [Configuración](#configuración)) |
| Servicio | `/etc/systemd/system/gestionador.service` · `systemctl status gestionador` |
| Logs | `journalctl -u gestionador -f` |
| Salud | `GET /api/health` → `{"status":"ok"}` (sin sesión) |

Para modificar el código, lee también [AGENTS.md](AGENTS.md).

---

## Configuración

Se define con variables de entorno (en `/etc/gestionador/env` si se instaló con el script) o con flags.

| Variable de entorno | Flag | Por defecto | Descripción |
|---|---|---|---|
| `GESTIONADOR_ADDR` | `-addr` | `:8080` (el servicio usa `127.0.0.1:8080`) | Dirección HTTP |
| `GESTIONADOR_DATA_DIR` | `-data-dir` | `./data` (el servicio usa `/var/lib/gestionador`) | SQLite, clave maestra y archivos de build |
| `GESTIONADOR_DATABASE_URL` | `-database-url` | *(vacío)* | Vacío = SQLite. O `postgres://usuario:clave@host:5432/db` |
| `GESTIONADOR_SECRET_KEY` | — | *(generada)* | Clave maestra en base64 (32 bytes). Si no se define, se crea en `DATA_DIR/secret.key` |
| `GESTIONADOR_LOG_LEVEL` | `-log-level` | `info` | `debug`, `info`, `warn`, `error` |
| — | `-version` | — | Imprime la versión y sale |

> ⚠️ **Respalda `secret.key`.** Cifra las llaves SSH, tokens y variables de entorno guardadas. Sin ella no se pueden recuperar.

## Cómo funciona

```mermaid
flowchart LR
    U[Navegador] -- HTTPS --> G
    GH[GitHub / GitLab / Gitea] -- webhook firmado --> G
    subgraph G[Gestionador · un binario]
        API[API Go] --- DB[(SQLite o PostgreSQL)]
        API --- UI[React embebido]
        API --- MON[Monitor y alertas]
    end
    G -- SSH --> S1
    G -- SSH --> S2
    subgraph S1[Servidor 1]
        C1[Caddy · HTTPS] --> A1[app-a]
        C1 --> A2[app-b]
    end
    subgraph S2[Servidor 2]
        A3[app-c]
    end
    MON -. Slack / Discord .-> W[Webhook]
```

Un deploy:

1. **Clonar.** El panel clona la rama (solo el último commit) con go-git. Los tokens de repos privados nunca salen del panel.
2. **Construir.** El código viaja como tar por SSH a `docker build -`. El servidor no necesita Git ni credenciales.
3. **Publicar.** Sin dominio: reemplaza el contenedor y comprueba que siga vivo; si no, restaura el anterior. Con dominio: arranca el nuevo junto al actual y Caddy cambia el tráfico cuando responde.
4. **Limpiar.** Conserva las últimas 3 imágenes para el rollback.

Detalle de cada parte en **[docs/funcionalidades.md](docs/funcionalidades.md)**.

## Decisiones técnicas

- **Un binario, cero dependencias.** El frontend se embebe con `go:embed` y SQLite es Go puro (modernc), así que compila con `CGO_ENABLED=0` para amd64 y arm64. PostgreSQL es opcional, con las mismas migraciones (goose) en ambos dialectos.
- **SSH en vez de agentes.** Nada que instalar ni actualizar en los servidores. Las conexiones se reutilizan en un pool, y la llave del host se fija en la primera conexión (TOFU): si cambia, el servidor queda bloqueado hasta revisarlo.
- **Secretos cifrados en reposo.** Llaves SSH, tokens, variables de entorno y webhooks se guardan con AES-256-GCM. Las variables llegan al contenedor por stdin de la sesión SSH: nunca aparecen en la línea de comandos, en archivos temporales ni en los logs.
- **Caddy generado desde la base.** Un Caddyfile completo por servidor, recargado sin cortar conexiones. El cambio de contenedor sin corte se hace dentro de la red Docker, sin publicar puertos.
- **Permisos en el servidor, por ruta.** Cada ruta de la API tiene un rol mínimo y el alcance de equipos se resuelve en un middleware; un recurso fuera del alcance responde 404 para no revelar que existe.
- **Métricas con agregación propia.** Promedio y pico por minuto (48 h) y por hora (90 días), reducidos a 300 puntos por consulta. Las alertas usan histéresis y ventanas sin huecos para no avisar de más.

## Seguridad

- Contraseñas con bcrypt; sesiones en cookie `HttpOnly`/`SameSite=Lax` (`Secure` con HTTPS), guardadas como hash; límite de intentos de login.
- Protección CSRF por `Sec-Fetch-Site`/`Origin`. La primera cuenta exige un código de instalación del log si no se crea desde la misma máquina.
- Webhooks de Git verificados con la firma de cada proveedor (HMAC o token), comparada en tiempo constante.
- Binarios de release con sumas SHA-256 verificadas por el instalador; unidad systemd endurecida (el proceso solo puede escribir en su directorio de datos).

## Stack

| | |
|---|---|
| Backend | Go 1.27 (`net/http` de la stdlib), `x/crypto/ssh`, go-git, goose, modernc SQLite, pgx |
| Frontend | React 19, TanStack Query, React Router, Tailwind CSS 4, Vite; gráficos en SVG propio |
| En los servidores | Docker o Podman, Caddy 2 |
| Calidad | Pruebas en Go sobre SQLite y PostgreSQL (con race detector), oxlint, TypeScript estricto, GitHub Actions |

Unas 9.700 líneas de Go (más 1.650 de pruebas) y 5.500 de TypeScript.

### Estructura

```
cmd/gestionador/        Punto de entrada: configuración y conexión de módulos
internal/
  auth/                 Usuarios, sesiones, invitaciones, roles y alcance por equipos
  teams/                Equipos: miembros, proyectos y servidores
  servers/              Servidores: alta por SSH, monitor y contenedores
  projects/             Proyectos: deploys, rollback, logs en vivo (SSE), webhooks de Git
  buildspec/            Detección del lenguaje y Dockerfile generado
  gitx/                 Clonado con go-git y empaquetado del contexto (.dockerignore)
  proxy/                Caddy por servidor: Caddyfile, recarga y verificación de dominios
  metrics/              Historial de uso por minuto y por hora, con retención
  alerts/               Reglas de alerta, historial y aviso por webhook
  sshx/                 Cliente SSH: TOFU, pool de conexiones, ejecución con stdin
  secrets/              Cifrado AES-256-GCM
  database/             SQLite/PostgreSQL y migraciones (goose) para ambos
  settings/ httpx/ server/ config/
web/                    Frontend React (embebido en el binario por web/embed.go)
scripts/install.sh      Instalador y actualizador (systemd)
deploy/                 Unidad systemd y ejemplo de Caddy para el panel
docs/                   Instalación manual, funcionalidades y capturas
```

## Limitaciones conocidas

- Solo servidores Linux, y un contenedor por proyecto (sin réplicas ni balanceo entre servidores).
- No hay recuperación de contraseña por email.
- La cola de deploys automáticos vive en memoria: si el panel se reinicia con un push en cola, se despliega en la siguiente revisión periódica, o con el próximo push si no hay revisión configurada.
- Detrás de un proxy inverso, el límite de intentos de login se aplica por email y no por IP.
- No hay dominios comodín ni un ajuste para el email de contacto de Let's Encrypt.

## Roadmap

- [x] Binario único con SQLite/PostgreSQL, migraciones y cifrado de secretos
- [x] Servidores por SSH con monitor de recursos (Docker y Podman)
- [x] Deploys desde Git, con Dockerfile propio o generado, logs en vivo y rollback
- [x] Variables de entorno, límites de CPU/RAM y edición sin reconstruir
- [x] Dominios con HTTPS automático y deploy sin corte (verificado con Let's Encrypt en un servidor real)
- [x] Deploy automático por webhook o revisión periódica
- [x] Usuarios, roles, invitaciones y equipos
- [x] Métricas históricas y alertas (Slack, Discord o webhook)
- [x] Instalador de un comando con releases verificadas
- [ ] Recuperación de contraseña por email
- [ ] Bases de datos administradas (PostgreSQL, Redis) junto a cada proyecto
- [ ] Respaldos programados de volúmenes

## Licencia

[AGPL-3.0](LICENSE). Puedes usar, modificar y autoalojar Gestionador libremente. Si ofreces una versión modificada a otras personas a través de la red, debes publicar su código fuente bajo la misma licencia.
