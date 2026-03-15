#!/usr/bin/env bash
# install.sh — Build and install ghost-bridge as a systemd service.
# Works for any username on any Linux system. Run from the bridge/ directory.
# Usage: ./install.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BINARY_NAME="ghost-bridge"
BINARY_PATH="$SCRIPT_DIR/$BINARY_NAME"
SERVICE_NAME="ghost-bridge"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
TEMPLATE="$SCRIPT_DIR/ghost-bridge.service.template"

echo "👻 Ghost Bridge Installer"
echo "─────────────────────────────────────"
echo "  User:       $USER"
echo "  Home:       $HOME"
echo "  Bridge dir: $SCRIPT_DIR"
echo "─────────────────────────────────────"

# ── 1. Build ──────────────────────────────────────────────────────────────
echo ""
echo "📦 Building ghost-bridge..."
cd "$SCRIPT_DIR"
go build -o "$BINARY_NAME" .
echo "✅ Built: $BINARY_PATH"

# ── 2. Generate service file from template ────────────────────────────────
echo ""
echo "📝 Generating systemd service file..."

if [ ! -f "$TEMPLATE" ]; then
  echo "❌ Template not found: $TEMPLATE"
  exit 1
fi

# Replace placeholders with real values
GENERATED_SERVICE="$SCRIPT_DIR/${SERVICE_NAME}.service"
sed \
  -e "s|__USER__|$USER|g" \
  -e "s|__BRIDGE_DIR__|$SCRIPT_DIR|g" \
  "$TEMPLATE" > "$GENERATED_SERVICE"

echo "✅ Generated: $GENERATED_SERVICE"
cat "$GENERATED_SERVICE"

# ── 3. Install service ────────────────────────────────────────────────────
echo ""
echo "🔧 Installing systemd service (requires sudo)..."
sudo cp "$GENERATED_SERVICE" "$SERVICE_FILE"
sudo systemctl daemon-reload
sudo systemctl enable "$SERVICE_NAME"
sudo systemctl restart "$SERVICE_NAME"

# ── 4. Verify ─────────────────────────────────────────────────────────────
echo ""
echo "⏳ Waiting for service to start..."
sleep 2

STATUS=$(systemctl is-active "$SERVICE_NAME" 2>/dev/null || echo "unknown")
if [ "$STATUS" = "active" ]; then
  echo "✅ ghost-bridge is running!"
else
  echo "⚠️  Service status: $STATUS"
  echo "   Check logs: sudo journalctl -u ghost-bridge -n 20"
fi

echo ""
echo "📋 Recent logs:"
sudo journalctl -u "$SERVICE_NAME" -n 15 --no-pager

echo ""
echo "─────────────────────────────────────"
echo "🔧 Ghost Remote Bridge (remote control only) installed for user: $USER"
echo ""
echo "Useful commands:"
echo "  sudo journalctl -u ghost-bridge -f     # follow logs"
echo "  sudo systemctl restart ghost-bridge    # restart"
echo "  sudo systemctl status ghost-bridge     # check status"
echo "  ./install.sh                           # rebuild + reinstall"
