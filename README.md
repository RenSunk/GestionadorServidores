# Gestionador

Plataforma autoalojada para desplegar proyectos (Java, Python, Node, Next, Nest...) en tus propios servidores usando Docker. Se distribuye como **un solo binario** que incluye la API, el frontend y la base de datos.

## Requisitos de desarrollo

- Go 1.27+
- Node 24+

## Desarrollo

Dos terminales:

```bash
make dev-api   # backend Go en http://localhost:8080
make dev-web   # frontend con hot-reload en http://localhost:5173 (usa este)
```

Vite redirige `/api/*` al backend, así que en desarrollo trabajas siempre en `:5173`.

## Compilar

```bash
make build     # → bin/gestionador (frontend embebido)
./bin/gestionador
```

Otros comandos: `make help`.

## Configuración

| Variable de entorno         | Flag            | Por defecto | Descripción |
|-----------------------------|-----------------|-------------|-------------|
| `GESTIONADOR_ADDR`          | `-addr`         | `:8080`     | Dirección HTTP |
| `GESTIONADOR_DATA_DIR`      | `-data-dir`     | `./data`    | SQLite, clave maestra y archivos locales |
| `GESTIONADOR_DATABASE_URL`  | `-database-url` | *(vacío)*   | Vacío = SQLite. O `postgres://usuario:clave@host:5432/db` |
| `GESTIONADOR_SECRET_KEY`    | —               | *(generada)* | Clave maestra en base64 (32 bytes). Si no se define, se crea en `DATA_DIR/secret.key` |
| `GESTIONADOR_LOG_LEVEL`     | `-log-level`    | `info`      | `debug`, `info`, `warn`, `error` |

> ⚠️ **Respalda `secret.key`.** Cifra las llaves SSH, tokens y variables de entorno guardadas. Sin ella no se pueden recuperar.

## Estructura

```
cmd/gestionador/        Punto de entrada (main)
internal/
  config/               Carga de configuración (env + flags)
  database/             Conexión SQLite/Postgres + migraciones (goose)
    migrations/sqlite/
    migrations/postgres/
  httpx/                Helpers HTTP compartidos (JSON, errores)
  secrets/              Cifrado AES-256-GCM de datos sensibles
  server/               Router HTTP, health y middlewares
  servers/              Módulo de servidores: CRUD, sondeo SSH y monitor
  settings/             Ajustes clave/valor de la instancia
  sshx/                 Cliente SSH: conexión, TOFU, pool de conexiones
web/                    Frontend React + Vite + Tailwind
  embed.go              Embebe web/dist en el binario
  src/
    components/         Componentes compartidos (Layout...)
    pages/              Una página por ruta
    lib/api.ts          Cliente HTTP
```

## Servidores (fase 1)

- **Autenticación:** llave SSH de la instancia (recomendado: se genera sola y solo copias la pública), llave privada propia o contraseña. Las credenciales se guardan cifradas.
- **Llave del host (TOFU):** se fija en la primera conexión. Si cambia, el servidor queda *Sin conexión* hasta revisarlo.
- **Monitor:** cada 15 s se ejecuta un script POSIX por SSH que lee `/proc`, `df` y `docker version`. No se instala nada en el servidor. El historial de 30 min vive en memoria.
- Solo servidores Linux.

## Roadmap

- [x] Fase 0 — Estructura: binario único, SQLite/Postgres, migraciones, cifrado de secretos
- [x] Fase 1 — Registrar servidores por SSH y ver sus recursos
- [ ] Fase 2 — Proyectos desde Git con Dockerfile: build, deploy y logs
- [ ] Fase 3 — Variables de entorno, límites de CPU/RAM, historial y rollback
- [ ] Fase 4 — Dominios + SSL automático con Caddy
- [ ] Fase 5 — Detección automática sin Dockerfile, webhooks de Git
- [ ] Fase 6 — Usuarios, equipos y permisos; métricas históricas y alertas
