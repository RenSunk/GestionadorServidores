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
  buildspec/            Lenguajes soportados, detección por archivos y Dockerfile generado
  gitx/                 Clonado con go-git y empaquetado del contexto (.dockerignore)
  projects/             Módulo de proyectos: deploys, rollback, logs en vivo (SSE)
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

## Proyectos y deploys (fase 2)

Flujo de un deploy:

1. **Clonar**: la instancia clona el repo (rama elegida, solo el último commit) con go-git. Repos privados por HTTPS + token; el token nunca sale de la instancia.
2. **Build**: el contexto (sin `.git` y respetando `.dockerignore`) viaja como tar por SSH a `docker build -` en el servidor. El servidor no necesita git.
3. **Publicar**: detiene el contenedor `gs-<proyecto>`, arranca el nuevo y verifica que siga vivo a los 5 s. Si falla, **restaura el anterior**. Esta fase no se puede cancelar para no dejar la app caída.
4. **Limpiar**: conserva las últimas 3 imágenes del proyecto.

### Proyectos sin Dockerfile

Al crear un proyecto se analiza el repo. Si trae Dockerfile, se ofrece usarlo; si no, se preselecciona el lenguaje detectado con sus comandos sugeridos (al estilo de Render) y Gestionador genera el Dockerfile en cada deploy:

| Lenguaje | Se detecta por | Ejemplos de framework |
|---|---|---|
| Node.js (20/22/24) | `package.json` (+ npm/pnpm/yarn por lockfile) | Next.js, NestJS, Nuxt, Express, Fastify, Vite estático |
| Python (3.11–3.13) | `requirements.txt`, `pyproject.toml`, `Pipfile`, `manage.py` | Django, FastAPI, Flask |
| Java Maven / Gradle (17/21/25) | `pom.xml`, `build.gradle(.kts)` | Spring Boot |
| Go | `go.mod` | — |
| Otro (imagen base) | — | PHP, Ruby, .NET… con cualquier imagen pública |

El usuario puede cambiar lenguaje, versión, comando de build y comando de inicio, y ver el Dockerfile antes de crear el proyecto. La app recibe la variable `PORT` y debe escuchar en `0.0.0.0`.

Los logs del build y del contenedor se ven en vivo (Server-Sent Events). Un solo deploy a la vez por proyecto. Hay un corte breve entre detener el contenedor viejo y arrancar el nuevo; el despliegue sin corte llega con los dominios (fase 4).

## Configuración del proyecto (fase 3)

Todo se edita desde la pestaña **Configuración** y se aplica en el próximo deploy. Si hay cambios guardados que el contenedor aún no usa, el proyecto muestra un aviso con la acción adecuada:

- **Variables de entorno**: se guardan cifradas y llegan al contenedor por stdin de la sesión SSH, sin archivos temporales (funciona también con Docker instalado como snap); los valores no aparecen en la línea de comandos ni en el log del deploy. Admiten varias líneas. Se puede pegar un `.env` completo. Las marcadas **"En build"** se pasan además como `--build-arg` (útil para `NEXT_PUBLIC_*`, `VITE_*`); quedan en los metadatos de la imagen, así que no deben ser secretos. Con un Dockerfile propio hay que declararlas con `ARG`.
- **Límites de CPU y memoria**: `--cpus` y `--memory` (sin swap extra). Si la app supera la memoria, Docker la reinicia.
- **Edición**: nombre, repositorio, rama, token, carpeta raíz, tipo de build, lenguaje y comandos, y puertos. El servidor no se puede cambiar.
- **Aplicar sin reconstruir**: los cambios de variables, puertos o límites se aplican recreando el contenedor con la imagen actual (segundos, sin build). Los cambios de build (repo, comandos, variables de build) piden un deploy completo.
- **Rollback**: desde la lista de deploys se puede volver a cualquiera de las últimas 3 imágenes que siguen en el servidor, sin reconstruir y con la configuración actual.

## Dominios y HTTPS (fase 4)

Cada servidor con dominios tiene un contenedor **Caddy** (`gestionador-caddy`) en la red Docker `gestionador`. Gestionador genera el Caddyfile completo desde la base de datos y lo recarga sin cortar conexiones cada vez que cambia algo.

- **Dominios por proyecto**, cada uno con su certificado: **automático** (Let's Encrypt; requiere que el DNS apunte al servidor y los puertos 80/443 abiertos) o **interno** (CA local de Caddy, para pruebas o redes internas).
- **Verificar**: comprueba el registro DNS y pide la página por HTTPS directamente al servidor, indicando qué falta (DNS, certificado, app caída).
- **Deploy sin corte**: si el proyecto tiene dominio y no publica un puerto, el contenedor nuevo arranca junto al actual; cuando responde, Caddy cambia el tráfico y recién entonces se apaga el anterior. Si el nuevo no responde, la versión actual sigue atendiendo.
- Los puertos de Caddy se pueden cambiar en la página del servidor (por defecto 80/443), útil si ya están ocupados.

## Deploy automático (fase 5)

En la configuración del proyecto, **Deploy automático** despliega la rama cada vez que haces push. Hay dos formas, y se pueden usar juntas:

- **Webhook** (instantáneo): GitHub, GitLab, Gitea/Forgejo y Bitbucket. Cada proyecto tiene su URL (`/hooks/git/<id>`) y un secreto con el que se verifica la firma de cada entrega. También acepta `X-Gestionador-Token: <secreto>` para dispararlo desde un CI con `curl`. Requiere que el proveedor pueda llegar a Gestionador.
- **Revisión periódica** (cada 1, 5, 15 o 60 minutos): compara el último commit de la rama con el último desplegado. Funciona aunque Gestionador esté detrás de NAT o en tu PC. No reintenta un commit cuyo deploy falló.

Si llega un push durante un deploy, queda uno en cola y se despliega al terminar. El historial indica qué disparó cada deploy y la interfaz muestra el resultado del último webhook (aceptado, ignorado por la rama o rechazado por la firma).

> Para recibir webhooks, Gestionador debe ser accesible desde internet. Exponlo siempre con HTTPS (por ejemplo, detrás de Caddy) para que la cookie de sesión no viaje en claro.

## Usuarios y roles (fase 6)

Gestionador exige iniciar sesión. Al arrancar sin cuentas, la primera visita crea la del **administrador**. Desde otra máquina, pide además el **código de instalación** que aparece en el log, así nadie puede reclamar una instancia recién expuesta.

| Rol | Puede |
|---|---|
| Administrador | Todo: servidores, proxy, eliminar proyectos y gestionar usuarios |
| Desarrollador | Crear, configurar y desplegar proyectos; variables, dominios y deploy automático |
| Solo lectura | Ver servidores, proyectos, deploys y logs. No ve variables ni el secreto del webhook |

- **Invitaciones:** un administrador crea un enlace para un email y un rol. Vale 7 días, se usa una vez y no requiere configurar correo.
- **Sesiones:** cookie `HttpOnly` y `SameSite=Lax` (`Secure` con HTTPS), con el token guardado como SHA-256. Se renuevan con el uso, vencen a los 30 días, se cierran al deshabilitar al usuario y, al cambiar la contraseña, se cierran todas las demás.
- **Protección:**
  - contraseñas con bcrypt;
  - límite de intentos fallidos por IP y email;
  - rechazo de escrituras desde otros sitios (CSRF);
  - siempre queda al menos un administrador activo.
- **Autoría:** los permisos se aplican en el servidor por ruta. El historial muestra quién hizo cada deploy manual.

## Roadmap

- [x] Fase 0 — Estructura: binario único, SQLite/Postgres, migraciones, cifrado de secretos
- [x] Fase 1 — Registrar servidores por SSH y ver sus recursos
- [x] Fase 2 — Proyectos desde Git con Dockerfile: build, deploy y logs
- [x] Detección del lenguaje y Dockerfile generado para proyectos sin Dockerfile
- [x] Fase 3 — Variables de entorno, límites de CPU/RAM, edición y rollback
- [x] Fase 4 — Dominios + SSL automático con Caddy (Let's Encrypt verificado en un servidor real) y deploy sin corte
- [x] Fase 5 — Webhooks de Git (deploy automático al hacer push)
- [x] Fase 6 — Usuarios, sesiones, invitaciones y roles
- [ ] Fase 7 — Equipos (acceso por proyecto o servidor)
- [ ] Fase 8 — Métricas históricas y alertas
