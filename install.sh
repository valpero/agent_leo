#!/usr/bin/env bash
# Valpero Agent Installer
# Usage: curl -sSL https://valpero.com/agent/install.sh | sudo bash -s -- --token=val_agnt_xxx
# Uninstall: curl -sSL https://valpero.com/agent/install.sh | sudo bash -s -- --uninstall
set -euo pipefail

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; BOLD='\033[1m'; NC='\033[0m'
info()    { echo -e "${CYAN}[valpero]${NC} $*"; }
success() { echo -e "${GREEN}[valpero]${NC} $*"; }
warn()    { echo -e "${YELLOW}[valpero]${NC} $*"; }
error()   { echo -e "${RED}[valpero] ERROR:${NC} $*" >&2; exit 1; }

AGENT_VERSION="1.0.0"
BIN_PATH="/usr/local/bin/valpero-agent"
CONF_DIR="/etc/valpero"
CONF_FILE="$CONF_DIR/agent.conf"
SERVICE_NAME="valpero-agent"

# ── Parse args ──────────────────────────────────────────────────────────────
TOKEN=""
UNINSTALL=false
UPDATE=false

for arg in "$@"; do
  case "$arg" in
    --token=*)     TOKEN="${arg#*=}" ;;
    --uninstall)   UNINSTALL=true ;;
    --update)      UPDATE=true ;;
  esac
done
TOKEN="${TOKEN:-${VALPERO_TOKEN:-}}"

# ── Require root ─────────────────────────────────────────────────────────────
[ "$(id -u)" -eq 0 ] || error "This script must be run as root (use sudo)"

# ── Uninstall ─────────────────────────────────────────────────────────────────
if [ "$UNINSTALL" = true ]; then
  info "Uninstalling Valpero Agent..."
  systemctl disable --now "$SERVICE_NAME" 2>/dev/null || true
  rm -f /etc/systemd/system/${SERVICE_NAME}.service
  systemctl daemon-reload
  rm -f "$BIN_PATH"
  rm -rf "$CONF_DIR"
  id -u valpero &>/dev/null && userdel valpero 2>/dev/null || true
  echo ""
  echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo -e "${GREEN} Valpero Agent uninstalled.${NC}"
  echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  exit 0
fi

# ── Update (keep existing token) ──────────────────────────────────────────────
if [ "$UPDATE" = true ]; then
  [ -f "$CONF_FILE" ] || error "Agent not installed. Run without --update to install."
  TOKEN="$(grep VALPERO_TOKEN "$CONF_FILE" | cut -d= -f2)"
  info "Updating Valpero Agent (token preserved)..."
fi

# ── Validate token ────────────────────────────────────────────────────────────
[ -z "$TOKEN" ]              && error "--token is required. Get it from https://valpero.com/dashboard/servers"
[[ "$TOKEN" == val_agnt_* ]] || error "Invalid token format (expected val_agnt_...)"

# ── Detect OS / arch ─────────────────────────────────────────────────────────
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64)  BIN_ARCH="amd64" ;;
  aarch64) BIN_ARCH="arm64" ;;
  armv7*)  BIN_ARCH="armv7" ;;
  *) error "Unsupported architecture: $ARCH" ;;
esac

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
[ "$OS" = "linux" ] || error "Only Linux is supported"

# ── Detect distro for nicer output ───────────────────────────────────────────
DISTRO=""
if [ -f /etc/os-release ]; then
  DISTRO="$(. /etc/os-release && echo "$NAME $VERSION_ID")"
fi

echo ""
echo -e "${BOLD}  Valpero Agent v${AGENT_VERSION}${NC}"
echo -e "  Platform : ${DISTRO:-$OS} / $ARCH"
echo -e "  Binary   : $BIN_PATH"
echo -e "  Token    : ${TOKEN:0:16}…"
echo ""

# ── Download binary ───────────────────────────────────────────────────────────
BIN_URL="https://valpero.com/agent/valpero-agent-linux-${BIN_ARCH}"
TMP_BIN="$(mktemp)"

info "Downloading agent binary (linux/${BIN_ARCH})..."
if ! curl -sSfL --progress-bar "$BIN_URL" -o "$TMP_BIN"; then
  rm -f "$TMP_BIN"
  error "Failed to download from $BIN_URL"
fi
chmod 755 "$TMP_BIN"
mv "$TMP_BIN" "$BIN_PATH"
success "Binary installed → $BIN_PATH"

# ── Create system user ────────────────────────────────────────────────────────
if ! id -u valpero &>/dev/null; then
  useradd --system --no-create-home --shell /usr/sbin/nologin valpero
  success "Created system user 'valpero'"
fi

# Docker group (optional, for container monitoring)
if getent group docker &>/dev/null; then
  usermod -aG docker valpero
  info "Added 'valpero' to docker group (enables container monitoring)"
fi

# ── Store config ──────────────────────────────────────────────────────────────
mkdir -p "$CONF_DIR"
cat > "$CONF_FILE" <<EOF
VALPERO_TOKEN=${TOKEN}
EOF
chmod 600 "$CONF_FILE"
chown root:valpero "$CONF_FILE"
success "Config saved → $CONF_FILE (mode 600)"

# ── Systemd service ───────────────────────────────────────────────────────────
cat > /etc/systemd/system/${SERVICE_NAME}.service <<'UNIT'
[Unit]
Description=Valpero Server Monitoring Agent
Documentation=https://valpero.com/docs/agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=valpero
Group=valpero
EnvironmentFile=/etc/valpero/agent.conf
ExecStart=/usr/local/bin/valpero-agent --token=${VALPERO_TOKEN}
Restart=always
RestartSec=15
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=read-only
ReadOnlyPaths=/
ReadWritePaths=/tmp
PrivateTmp=yes
ProcSubset=all

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable "$SERVICE_NAME"
systemctl restart "$SERVICE_NAME"
success "Service '${SERVICE_NAME}' enabled and started"

# ── Verify service started ────────────────────────────────────────────────────
sleep 2
if systemctl is-active --quiet "$SERVICE_NAME"; then
  success "Agent is running"
else
  warn "Service may not have started. Check: journalctl -u $SERVICE_NAME -n 20"
fi

# ── Done ──────────────────────────────────────────────────────────────────────
echo ""
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${GREEN} Valpero Agent installed successfully!${NC}"
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""
echo -e "  ${BOLD}Status${NC}     systemctl status $SERVICE_NAME"
echo -e "  ${BOLD}Logs${NC}       journalctl -u $SERVICE_NAME -f"
echo -e "  ${BOLD}Update${NC}     curl -sSL https://valpero.com/agent/install.sh | sudo bash -s -- --update"
echo -e "  ${BOLD}Uninstall${NC}  curl -sSL https://valpero.com/agent/install.sh | sudo bash -s -- --uninstall"
echo ""
echo "  Your server will appear in the dashboard within 30 seconds."
echo ""
