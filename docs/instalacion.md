# Instalación manual

> **La forma rápida es el instalador:** `curl -fsSL https://raw.githubusercontent.com/RenSunk/GestionadorServidores/main/scripts/install.sh | sudo bash` (ver el [README](../README.md#instalación-en-un-servidor)). Esta guía explica cada paso por separado, para instalarlo a mano o entender qué hace el script.

Gestionador es un solo binario sin dependencias: no necesita Docker, Node ni una base de datos externa en la máquina donde corre. Se conecta por SSH a los servidores donde se despliegan las apps.

```
 Tu navegador ──HTTPS──▶ Gestionador (panel)  ──SSH──▶ Servidor 1 (Docker)
                         binario + SQLite      ──SSH──▶ Servidor 2 (Docker o Podman)
```

El panel y un servidor de despliegue pueden ser la misma máquina (ver [Panel y apps en la misma máquina](#panel-y-apps-en-la-misma-máquina)).

## Requisitos

**Máquina del panel:** Linux amd64 o arm64, menos de 50 MB de RAM (unos 30 MB en uso normal) y acceso SSH saliente hacia los servidores.

**Cada servidor de despliegue:**
- Linux con SSH y un usuario que pueda usar Docker (`sudo usermod -aG docker <usuario>`). Si Docker falta o está detenido, la página del servidor muestra el comando para arreglarlo.
- Docker o Podman con `podman-docker` (probado con Docker 29 y Podman 4.9).
- Puertos 80 y 443 abiertos si vas a usar dominios con HTTPS.

## 1. Obtener el binario

Descárgalo de las [releases](https://github.com/RenSunk/GestionadorServidores/releases) y verifica la suma:

```bash
ARCH=amd64   # o arm64
curl -fLO https://github.com/RenSunk/GestionadorServidores/releases/latest/download/gestionador-linux-$ARCH
curl -fLO https://github.com/RenSunk/GestionadorServidores/releases/latest/download/SHA256SUMS
sha256sum -c --ignore-missing SHA256SUMS
```

O compílalo (Go 1.27+ y Node 24+):

```bash
make build-all            # → bin/gestionador-linux-amd64 y bin/gestionador-linux-arm64
scp bin/gestionador-linux-amd64 usuario@panel:/tmp/gestionador
```

En la máquina del panel:

```bash
sudo install -m 0755 gestionador-linux-$ARCH /usr/local/bin/gestionador   # o /tmp/gestionador si lo compilaste
sudo useradd --system --home /var/lib/gestionador --shell /usr/sbin/nologin gestionador
```

## 2. Servicio systemd

```bash
sudo cp deploy/gestionador.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now gestionador
journalctl -u gestionador -f
```

La unidad escucha solo en `127.0.0.1:8080`, guarda todo en `/var/lib/gestionador` y no puede escribir en ningún otro lugar del sistema. Para cambiar la configuración, crea `/etc/gestionador/env`:

```bash
# /etc/gestionador/env (permisos 600)
GESTIONADOR_LOG_LEVEL=info
# GESTIONADOR_DATABASE_URL=postgres://gestionador:clave@localhost:5432/gestionador
# GESTIONADOR_SECRET_KEY=<base64 de 32 bytes>   # si prefieres no guardarla en disco
```

## 3. HTTPS

No expongas el puerto 8080 directamente: la cookie de sesión debe viajar cifrada. Con [Caddy](https://caddyserver.com/docs/install) el certificado es automático:

```bash
sudo cp deploy/Caddyfile.example /etc/caddy/Caddyfile   # edita el dominio
sudo systemctl reload caddy
```

Gestionador detecta `X-Forwarded-Proto: https` y marca la cookie como `Secure`. Cualquier otro proxy inverso (nginx, Traefik) sirve igual si envía esa cabecera y no almacena en buffer las respuestas `text/event-stream` (los logs en vivo).

**Sin dominio público:** usa un túnel SSH y entra por `http://localhost:8080`:

```bash
ssh -L 8080:127.0.0.1:8080 usuario@panel
```

## 4. Primer acceso

Abre el panel y crea la cuenta de administrador. Si no entras desde la misma máquina (por ejemplo, a través del proxy), te pedirá el **código de instalación**, que aparece en el log:

```bash
journalctl -u gestionador | grep -i "código"
```

Después:
1. **Servidores → Agregar**: copia la llave pública de Gestionador en `~/.ssh/authorized_keys` del usuario del servidor (o usa tu propia llave o una contraseña).
2. **Proyectos → Nuevo**: pega la URL del repositorio. Gestionador detecta el lenguaje y propone los comandos.
3. **Configuración**: invita al equipo y ajusta las alertas (umbrales y webhook de Slack o Discord).

## Panel y apps en la misma máquina

Agrega la propia máquina como servidor con host `127.0.0.1`. Si el panel ya usa los puertos 80/443 (por ejemplo, con el Caddy del paso 3), cambia los puertos del proxy de apps en la página del servidor, o sirve el panel en otra máquina.

## Respaldos

Todo el estado está en el directorio de datos:

| Archivo | Contiene |
|---|---|
| `gestionador.db` (+ `-wal`, `-shm`) | Base SQLite: servidores, proyectos, usuarios, historial |
| `secret.key` | Clave maestra que cifra llaves SSH, tokens, variables de entorno y el webhook de alertas |

> ⚠️ Sin `secret.key` los secretos guardados no se pueden recuperar. Respáldala aparte de la base.

Para una copia consistente sin detener el servicio:

```bash
sudo -u gestionador sqlite3 /var/lib/gestionador/gestionador.db ".backup /var/lib/gestionador/respaldo.db"
```

Con PostgreSQL, respalda la base con `pg_dump` y guarda la clave maestra por separado.

## Actualizar

```bash
sudo install -m 0755 /tmp/gestionador-nuevo /usr/local/bin/gestionador
sudo systemctl restart gestionador
```

Las migraciones de la base se aplican solas al arrancar. El apagado espera a que los deploys en curso guarden su estado; las apps desplegadas siguen corriendo durante la actualización porque viven en los servidores, no en el panel.

## Desinstalar

```bash
sudo systemctl disable --now gestionador
sudo rm /etc/systemd/system/gestionador.service /usr/local/bin/gestionador
sudo rm -r /var/lib/gestionador /etc/gestionador   # borra todos los datos
```

Los contenedores de las apps (`gs-*`) y el proxy (`gestionador-caddy`) quedan en cada servidor; bórralos con `docker rm -f` si ya no los necesitas.
