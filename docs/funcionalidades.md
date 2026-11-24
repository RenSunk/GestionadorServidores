# Funcionalidades en detalle

Referencia de cómo funciona cada parte de Gestionador. Para una visión general, ver el [README](../README.md); para instalarlo, [instalacion.md](instalacion.md).

- [Servidores](#servidores)
- [Proyectos y deploys](#proyectos-y-deploys)
- [Configuración del proyecto](#configuración-del-proyecto)
- [Dominios y HTTPS](#dominios-y-https)
- [Deploy automático](#deploy-automático)
- [Usuarios y roles](#usuarios-y-roles)
- [Equipos](#equipos)
- [Métricas históricas y alertas](#métricas-históricas-y-alertas)

## Servidores

- **Autenticación:** llave SSH de la instancia (recomendado: se genera sola y solo copias la pública), llave privada propia o contraseña. Las credenciales se guardan cifradas.
- **Llave del host (TOFU):** se fija en la primera conexión. Si cambia, el servidor queda *Sin conexión* hasta revisarlo.
- **Monitor:** cada 15 s se ejecuta un script POSIX por SSH que lee `/proc`, `df` y `docker version`. No se instala nada en el servidor. Las muestras se guardan como historial (ver *Métricas históricas y alertas*).
- Solo servidores Linux.

## Proyectos y deploys

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

Los logs del build y del contenedor se ven en vivo (Server-Sent Events). Un solo deploy a la vez por proyecto. Sin dominio, hay un corte breve entre detener el contenedor viejo y arrancar el nuevo; con dominio el deploy es sin corte (ver *Dominios y HTTPS*).

## Configuración del proyecto

Todo se edita desde la pestaña **Configuración** y se aplica en el próximo deploy. Si hay cambios guardados que el contenedor aún no usa, el proyecto muestra un aviso con la acción adecuada:

- **Variables de entorno**: se guardan cifradas y llegan al contenedor por stdin de la sesión SSH, sin archivos temporales (funciona también con Docker instalado como snap); los valores no aparecen en la línea de comandos ni en el log del deploy. Admiten varias líneas. Se puede pegar un `.env` completo. Las marcadas **"En build"** se pasan además como `--build-arg` (útil para `NEXT_PUBLIC_*`, `VITE_*`); quedan en los metadatos de la imagen, así que no deben ser secretos. Con un Dockerfile propio hay que declararlas con `ARG`.
- **Límites de CPU y memoria**: `--cpus` y `--memory` (sin swap extra). Si la app supera la memoria, Docker la reinicia.
- **Edición**: nombre, repositorio, rama, token, carpeta raíz, tipo de build, lenguaje y comandos, y puertos. El servidor no se puede cambiar.
- **Aplicar sin reconstruir**: los cambios de variables, puertos o límites se aplican recreando el contenedor con la imagen actual (segundos, sin build). Los cambios de build (repo, comandos, variables de build) piden un deploy completo.
- **Rollback**: desde la lista de deploys se puede volver a cualquiera de las últimas 3 imágenes que siguen en el servidor, sin reconstruir y con la configuración actual.

## Dominios y HTTPS

Cada servidor con dominios tiene un contenedor **Caddy** (`gestionador-caddy`) en la red Docker `gestionador`. Gestionador genera el Caddyfile completo desde la base de datos y lo recarga sin cortar conexiones cada vez que cambia algo.

- **Dominios por proyecto**, cada uno con su certificado: **automático** (Let's Encrypt; requiere que el DNS apunte al servidor y los puertos 80/443 abiertos) o **interno** (CA local de Caddy, para pruebas o redes internas).
- **Verificar**: comprueba el registro DNS y pide la página por HTTPS directamente al servidor, indicando qué falta (DNS, certificado, app caída).
- **Deploy sin corte**: si el proyecto tiene dominio y no publica un puerto, el contenedor nuevo arranca junto al actual; cuando responde, Caddy cambia el tráfico y recién entonces se apaga el anterior. Si el nuevo no responde, la versión actual sigue atendiendo.
- Los puertos de Caddy se pueden cambiar en la página del servidor (por defecto 80/443), útil si ya están ocupados.

## Deploy automático

En la configuración del proyecto, **Deploy automático** despliega la rama cada vez que haces push. Hay dos formas, y se pueden usar juntas:

- **Webhook** (instantáneo): GitHub, GitLab, Gitea/Forgejo y Bitbucket. Cada proyecto tiene su URL (`/hooks/git/<id>`) y un secreto con el que se verifica la firma de cada entrega. También acepta `X-Gestionador-Token: <secreto>` para dispararlo desde un CI con `curl`. Requiere que el proveedor pueda llegar a Gestionador.
- **Revisión periódica** (cada 1, 5, 15 o 60 minutos): compara el último commit de la rama con el último desplegado. Funciona aunque Gestionador esté detrás de NAT o en tu PC. No reintenta un commit cuyo deploy falló.

Si llega un push durante un deploy, queda uno en cola y se despliega al terminar. El historial indica qué disparó cada deploy y la interfaz muestra el resultado del último webhook (aceptado, ignorado por la rama o rechazado por la firma).

> Para recibir webhooks, Gestionador debe ser accesible desde internet. Exponlo siempre con HTTPS (por ejemplo, detrás de Caddy) para que la cookie de sesión no viaje en claro.

## Usuarios y roles

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

## Equipos

El rol define **qué** puede hacer cada persona; los equipos, **sobre qué**. Un administrador los gestiona en **Configuración → Equipos**: cada equipo tiene miembros, proyectos y servidores.

- Un proyecto o servidor **sin equipos es visible para todos**, así que nada cambia hasta crear el primero.
- Al asignarlo a uno o más equipos, **solo lo ven sus miembros y los administradores**. Para el resto, el recurso y todo lo que cuelga de él (deploys, logs, dominios) responde 404, sin revelar que existe.
- Para crear un proyecto en un servidor restringido, hay que ser miembro de uno de sus equipos.
- Las páginas de proyecto y servidor muestran a los administradores quién tiene acceso.

## Métricas históricas y alertas

**Historial.** El monitor ya sondea cada servidor cada 15 s; ahora cada muestra se agrega por minuto (promedio y pico) y se guarda en la base. Una vez por minuto también se mide el consumo de cada contenedor de app (los que llevan la etiqueta `gestionador.project`). Los minutos se conservan 48 h y se resumen por hora, que se conserva 90 días. La página del servidor muestra CPU, RAM y disco (en vivo, 1 h, 6 h, 24 h, 7 días o 30 días), y la pestaña **Métricas** de cada proyecto muestra su contenedor. Donde faltan datos (servidor caído, instancia apagada), la línea se corta en lugar de inventar valores.

**Alertas.** Se abren y se cierran solas; hay una sola abierta por tipo y objetivo:

| Alerta | Se abre cuando | Se cierra cuando |
|---|---|---|
| Servidor caído | 2 sondeos fallidos seguidos (~30 s) | vuelve a responder |
| CPU / RAM alta | el promedio de los últimos N minutos (5 por defecto) supera el umbral (90 %) | baja 5 puntos por debajo del umbral |
| Disco lleno | el disco supera el umbral (90 %) | baja 2 puntos por debajo |
| App detenida | el contenedor de un proyecto ya desplegado no existe, se detuvo o se reinicia en bucle (se ignora durante un deploy) | vuelve a estar en ejecución |
| Deploy fallido | un deploy falla (manual, webhook o revisión periódica) | es un evento: queda en el historial |

Las alertas activas aparecen en el menú (con contador), en el dashboard y en la página del servidor o proyecto afectado. Cada persona solo ve las de los recursos de sus equipos. Un administrador ajusta umbrales y tipos en **Configuración → Alertas** y puede indicar un **webhook**: Slack y Discord reciben un mensaje en su formato, y cualquier otra URL recibe JSON (`event`, `text`, `alert`). La URL se guarda cifrada y hay un botón para enviar una prueba.
