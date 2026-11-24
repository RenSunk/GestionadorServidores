# AGENTS.md

Guía para agentes de IA que trabajan en este repositorio. Para **instalar o ejecutar** Gestionador, sigue [README.md → Guía para agentes de IA](README.md#guía-para-agentes-de-ia).

## Antes de terminar cualquier cambio

```bash
gofmt -l .                  # debe quedar vacío
go vet ./...
go test ./...
cd web && npx tsc -b && npm run lint
```

Si tocaste el frontend, compílalo (`npm --prefix web run build`) antes de compilar el binario: Go embebe `web/dist`.

## Convenciones

- **Idioma:** la interfaz, los mensajes de error para el usuario, los logs y los comentarios están en español. Los identificadores, en inglés.
- **Formato:** `gofmt` para Go. El frontend **no tiene configuración de Prettier**: no ejecutes `prettier --write` (reformatea todo el proyecto); sigue el estilo de los archivos vecinos.
- **Dependencias:** stdlib primero. Pregunta antes de agregar un módulo de Go o un paquete de npm.
- **Base de datos:** cada cambio de esquema es una migración nueva y numerada en **ambos** dialectos: `internal/database/migrations/sqlite/000NN_*.sql` y `.../postgres/000NN_*.sql`. Booleanos: `INTEGER` en SQLite, `BOOLEAN` en Postgres; fechas: `TIMESTAMP` / `TIMESTAMPTZ`. No edites migraciones ya publicadas. Las consultas usan placeholders `$1, $2…` (válidos en ambos).
- **Rutas nuevas de la API:** el rol mínimo se decide en `internal/auth/http.go` (mapas de rutas públicas, de desarrollador y GET de administrador; el resto de los GET es para lectura y los demás métodos, para administradores). Si la ruta lleva `{id}` de un proyecto, servidor, deploy o dominio, el alcance por equipos se aplica solo (`resourceOf` en `internal/auth/scope.go`); las listas deben filtrarse con `auth.ScopeFrom(ctx)`.
- **Secretos:** todo dato sensible se guarda cifrado con `secrets.Box` y nunca se registra en logs ni se pasa en la línea de comandos de un comando SSH (usar stdin).
- **Comandos remotos:** deben ser POSIX `sh` y funcionar con Docker y con Podman (`podman-docker`). Usa nombres de imagen completos (`docker.io/library/...`).
- **Colores de estado** en la interfaz: bien `#0ca30c`, crítico `#d03b3b`, advertencia `#fab219`, siempre acompañados de icono o texto.

## Pruebas

- `go test ./...` usa SQLite temporal y no necesita servicios externos.
- Las pruebas de `internal/metrics` e `internal/alerts` corren también sobre PostgreSQL con `TEST_DATABASE_URL=postgres://…`.
- Las pruebas de extremo a extremo (deploys reales) necesitan un servidor con SSH y Docker; no las simules con mocks en el código de producción.

## Mapa rápido

| Quiero cambiar… | Mira en… |
|---|---|
| Arranque y conexión de módulos | `cmd/gestionador/main.go` |
| Flujo de un deploy | `internal/projects/service.go`, `docker.go` |
| Detección de lenguaje / Dockerfile generado | `internal/buildspec/` |
| Dominios y Caddy | `internal/proxy/`, `internal/projects/domains.go` |
| Webhooks y revisión periódica | `internal/projects/autodeploy.go` |
| Login, roles, invitaciones | `internal/auth/` |
| Monitor de servidores | `internal/servers/service.go`, `watch.go` |
| Métricas y alertas | `internal/metrics/`, `internal/alerts/` |
| Páginas del frontend | `web/src/pages/`, datos en `web/src/lib/` |
| Instalador | `scripts/install.sh`, `deploy/gestionador.service` |
