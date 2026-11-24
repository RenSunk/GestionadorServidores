#!/usr/bin/env bash
# Instala o actualiza Gestionador como servicio systemd.
#
#   curl -fsSL https://raw.githubusercontent.com/RenSunk/GestionadorServidores/main/scripts/install.sh | sudo bash
#
# Opciones (después de "bash -s --"):
#   --version vX.Y.Z   Versión a instalar (por defecto, la última release).
#   --binary RUTA      Usa un binario local en lugar de descargarlo (ej. bin/gestionador-linux-amd64).
#   --addr HOST:PUERTO Dirección donde escucha (por defecto 127.0.0.1:8080).
#
# Variable opcional: GESTIONADOR_BASE_URL descarga desde un espejo en lugar de las releases de GitHub.
#
# Es idempotente: volver a ejecutarlo actualiza el binario y reinicia el servicio
# sin tocar los datos (/var/lib/gestionador) ni la configuración (/etc/gestionador/env).
set -euo pipefail

REPO="RenSunk/GestionadorServidores"
VERSION="latest"
BINARY=""
ADDR="127.0.0.1:8080"
BIN_PATH="/usr/local/bin/gestionador"
UNIT_PATH="/etc/systemd/system/gestionador.service"
DATA_DIR="/var/lib/gestionador"
CONF_DIR="/etc/gestionador"

usage() {
  cat <<'EOF'
Uso: install.sh [--version vX.Y.Z] [--binary RUTA] [--addr HOST:PUERTO]
  --version  Versión a instalar (por defecto, la última release)
  --binary   Usa un binario local en lugar de descargarlo
  --addr     Dirección donde escucha (por defecto 127.0.0.1:8080)
EOF
}

say() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
ok() { printf '\033[1;32m ✓\033[0m %s\n' "$*"; }
die() { printf '\033[1;31m ✗ Error:\033[0m %s\n' "$*" >&2; exit 1; }

while [ $# -gt 0 ]; do
  case "$1" in
    --version) VERSION="${2:?falta el valor de --version}"; shift 2 ;;
    --binary) BINARY="${2:?falta el valor de --binary}"; shift 2 ;;
    --addr) ADDR="${2:?falta el valor de --addr}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) die "opción desconocida: $1 (usa --help)" ;;
  esac
done

# ---------- Comprobaciones ----------
[ "$(id -u)" -eq 0 ] || die "ejecútalo como root (con sudo)."
[ "$(uname -s)" = "Linux" ] || die "Gestionador solo corre en Linux."
[ -d /run/systemd/system ] || die "no se detectó systemd. Instala a mano: ver docs/instalacion.md."
case "$(uname -m)" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) die "arquitectura no soportada: $(uname -m) (solo amd64 y arm64)." ;;
esac
command -v curl >/dev/null || die "falta curl (ej. apt-get install -y curl)."
command -v sha256sum >/dev/null || die "falta sha256sum (paquete coreutils)."
ok "Linux $ARCH con systemd"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
ASSET="gestionador-linux-$ARCH"

# ---------- Binario y unidad systemd ----------
if [ -n "$BINARY" ]; then
  [ -f "$BINARY" ] || die "no existe el binario $BINARY"
  cp "$BINARY" "$TMP/$ASSET"
  SRC_DIR="$(cd "$(dirname "$0")/.." 2>/dev/null && pwd || true)"
  if [ -f "$SRC_DIR/deploy/gestionador.service" ]; then
    cp "$SRC_DIR/deploy/gestionador.service" "$TMP/gestionador.service"
  else
    die "con --binary hay que ejecutar el script desde el repositorio (necesita deploy/gestionador.service)."
  fi
  ok "Usando el binario local $BINARY"
else
  if [ -n "${GESTIONADOR_BASE_URL:-}" ]; then
    BASE="${GESTIONADOR_BASE_URL%/}"
  elif [ "$VERSION" = "latest" ]; then
    BASE="https://github.com/$REPO/releases/latest/download"
  else
    BASE="https://github.com/$REPO/releases/download/$VERSION"
  fi
  say "Descargando $ASSET ($VERSION)"
  for f in "$ASSET" gestionador.service SHA256SUMS; do
    curl -fsSL --retry 3 -o "$TMP/$f" "$BASE/$f" \
      || die "no se pudo descargar $BASE/$f. ¿Existe la release? Ver https://github.com/$REPO/releases"
  done
  (cd "$TMP" && grep -E " ($ASSET|gestionador\.service)\$" SHA256SUMS | sha256sum -c --quiet -) \
    || die "la suma SHA-256 no coincide: descarga corrupta o alterada."
  ok "Descarga verificada (SHA-256)"
fi
chmod 0755 "$TMP/$ASSET"
NEW_VERSION="$("$TMP/$ASSET" -version 2>/dev/null)" || die "el binario no se puede ejecutar en esta máquina."
ok "Binario válido: $NEW_VERSION"

# ---------- Usuario, directorios y archivos ----------
if ! id gestionador >/dev/null 2>&1; then
  useradd --system --home "$DATA_DIR" --shell /usr/sbin/nologin gestionador
  ok "Usuario de sistema 'gestionador' creado"
fi
install -d -m 0755 "$CONF_DIR"
if [ ! -f "$CONF_DIR/env" ]; then
  cat > "$CONF_DIR/env" <<EOF
# Configuración de Gestionador. Ver README → Configuración.
GESTIONADOR_ADDR=$ADDR
GESTIONADOR_LOG_LEVEL=info
# GESTIONADOR_DATABASE_URL=postgres://usuario:clave@localhost:5432/gestionador
EOF
  chmod 0600 "$CONF_DIR/env"
  ok "Configuración creada en $CONF_DIR/env"
fi

UPGRADE=false
[ -f "$BIN_PATH" ] && UPGRADE=true
install -m 0755 "$TMP/$ASSET" "$BIN_PATH.new" && mv -f "$BIN_PATH.new" "$BIN_PATH"
install -m 0644 "$TMP/gestionador.service" "$UNIT_PATH"
ok "Instalado $BIN_PATH"

# ---------- Servicio ----------
systemctl daemon-reload
if $UPGRADE; then
  systemctl enable gestionador >/dev/null 2>&1
  systemctl restart gestionador
  say "Servicio reiniciado con la nueva versión"
else
  systemctl enable --now gestionador >/dev/null 2>&1
  say "Servicio iniciado"
fi

# La dirección efectiva es la de /etc/gestionador/env (si existe) o la de la unidad.
LISTEN="$(sed -n 's/^GESTIONADOR_ADDR=//p' "$CONF_DIR/env" | tail -1)"
LISTEN="${LISTEN:-127.0.0.1:8080}"
case "$LISTEN" in :*) LISTEN="127.0.0.1$LISTEN" ;; 0.0.0.0:*) LISTEN="127.0.0.1:${LISTEN#0.0.0.0:}" ;; esac

HEALTH=""
for _ in $(seq 1 30); do
  if HEALTH="$(curl -fsS "http://$LISTEN/api/health" 2>/dev/null)"; then break; fi
  sleep 1
done
[ -n "$HEALTH" ] || die "el servicio no respondió en http://$LISTEN. Revisa: journalctl -u gestionador -n 50"
ok "Responde en http://$LISTEN: $HEALTH"

# ---------- Siguientes pasos ----------
echo
if curl -fsS "http://$LISTEN/api/auth/status" 2>/dev/null | grep -q '"setupRequired":true'; then
  CODE="$(journalctl -u gestionador --no-pager -o cat 2>/dev/null | sed -n 's/.*código=\([A-Za-z0-9-]*\).*/\1/p' | tail -1)"
  say "Falta crear la cuenta de administrador:"
  echo "    1. Abre el panel (ver README → Acceso al panel: túnel SSH o dominio con HTTPS)."
  echo "    2. Crea la cuenta. Si no entras desde esta máquina te pedirá el código de instalación:"
  echo "       ${CODE:-(ver: journalctl -u gestionador | grep código)}"
else
  say "Gestionador ya tiene cuentas creadas: entra con tu usuario."
fi
echo
echo "    Logs:       journalctl -u gestionador -f"
echo "    Datos:      $DATA_DIR   (respalda secret.key y gestionador.db)"
echo "    Config:     $CONF_DIR/env  (tras editarlo: systemctl restart gestionador)"
