#!/bin/bash

# Color codes
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Canonical local model tag. Single-sourced with pkg/config
# DefaultLocalTag — if you change this, change that constant too
# (covered by TestDefaultModelTagSingleSourced).
DEFAULT_MODEL_TAG="qwen3:0.6b"

# Non-interactive flags: --yes answers every prompt affirmatively,
# --no-service skips the service/install prompts. CI=1 implies --yes.
ASSUME_YES=0
NO_SERVICE=0
for arg in "$@"; do
    case "$arg" in
        --yes|-y) ASSUME_YES=1 ;;
        --no-service) NO_SERVICE=1 ;;
    esac
done
if [ -n "$CI" ]; then
    ASSUME_YES=1
fi

# ask reads one prompt unless --yes/CI answered it already.
ask() {
    local prompt="$1" default_answer="$2" varname="$3" reply=""
    if [ "$ASSUME_YES" = "1" ]; then
        printf -v "$varname" '%s' "$default_answer"
        return 0
    fi
    if [ ! -t 0 ]; then
        echo -e "${RED}[ERROR] $prompt requires input but stdin is not a terminal. Re-run with --yes or pipe an answer.${NC}" >&2
        return 1
    fi
    read -p "$prompt" reply
    printf -v "$varname" '%s' "$reply"
}

echo -e "${GREEN}===================================================${NC}"
echo -e "${GREEN}  Ghost: Your Sovereign Intelligence (Linux/Pi Setup)${NC}"
echo -e "${GREEN}===================================================${NC}"
echo ""

# Helper function
check_command() {
    if command -v "$1" &> /dev/null; then
        return 0
    else
        return 1
    fi
}

generate_secret() {
    if check_command "openssl"; then
        openssl rand -base64 32 | tr -d '\n'
        return 0
    fi
    if check_command "python3"; then
        python3 - <<'PY'
import secrets
print(secrets.token_urlsafe(32), end="")
PY
        return 0
    fi
    cat /dev/urandom | tr -dc 'A-Za-z0-9' | head -c 48
}

# ── Service installer ─────────────────────────────────────────────────────
install_service() {
    local TEMPLATE="ghost.service.template"
    local GENERATED="ghost.service"
    local SERVICE_NAME="ghost"

    echo -e "${YELLOW}[INFO] Installing Ghost as a system service...${NC}"
    echo -e "${BLUE}  User       : ${USER}${NC}"
    echo -e "${BLUE}  Home       : ${HOME}${NC}"
    echo -e "${BLUE}  Binary     : /usr/local/bin/ghost${NC}"
    echo -e "${BLUE}  WorkingDir : ${HOME}/ghost${NC}"
    echo -e "${BLUE}  EnvFile    : ${HOME}/ghost/.env${NC}"
    echo ""

    # Use template if it exists, otherwise generate inline
    if [ -f "$TEMPLATE" ]; then
        sed \
            -e "s|__USER__|${USER}|g" \
            -e "s|__HOME__|${HOME}|g" \
            "$TEMPLATE" > "$GENERATED"
    else
        # Generate service file inline — no template needed
        cat > "$GENERATED" << EOF
[Unit]
Description=Ghost Pi - Sovereign AI Presence
After=network.target

[Service]
Type=simple
User=${USER}
WorkingDirectory=${HOME}/ghost
EnvironmentFile=${HOME}/ghost/.env
Environment=GHOST_WORKSPACE_DIR=${HOME}/ghost/workspace
ExecStart=/usr/local/bin/ghost serve
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF
    fi

    echo -e "${YELLOW}Generated service file:${NC}"
    cat "$GENERATED"
    echo ""

    # Install
    sudo cp "$GENERATED" /etc/systemd/system/${SERVICE_NAME}.service
    sudo systemctl daemon-reload
    sudo systemctl enable "$SERVICE_NAME"
    sudo systemctl restart "$SERVICE_NAME"

    # Verify
    sleep 3
    STATUS=$(systemctl is-active "$SERVICE_NAME" 2>/dev/null || echo "unknown")
    if [ "$STATUS" = "active" ]; then
        echo -e "${GREEN}[OK] Ghost service is running!${NC}"
        echo ""
        echo -e "${BLUE}Recent logs:${NC}"
        sudo journalctl -u "$SERVICE_NAME" -n 15 --no-pager
    else
        echo -e "${RED}[ERROR] Ghost service failed to start. Status: ${STATUS}${NC}"
        echo ""
        echo -e "${YELLOW}Logs:${NC}"
        sudo journalctl -u "$SERVICE_NAME" -n 20 --no-pager
        echo ""
        echo -e "${RED}Common causes:${NC}"
        echo "  1. .env file missing at ${HOME}/ghost/.env"
        echo "  2. Binary not found at /usr/local/bin/ghost — run: sudo make install-ghost"
        echo "  3. DEEPSEEK_API_KEY not set in .env"
        return 1
    fi
}

# ── Detect architecture mismatch ──────────────────────────────────────────
if [ "$(uname -m)" = "aarch64" ] && [ "$(dpkg --print-architecture 2>/dev/null)" = "armhf" ]; then
    echo -e "${YELLOW}[WARNING] Detected 64-bit kernel with 32-bit userland. Forcing GOARCH=arm.${NC}"
    export GOARCH=arm
    export GOARM=7
    export CGO_ENABLED=1
fi

# ── 1. System dependencies ────────────────────────────────────────────────
echo -e "${YELLOW}[1/4] Updating system and installing dependencies...${NC}"
if check_command "apt-get"; then
    sudo apt-get update

    DEPENDENCIES="golang git python3 python3-pip ffmpeg alsa-utils espeak fswebcam adb nmap tmux speedtest-cli cowsay poppler-utils pandoc chromium avahi-utils coreutils"

    NEEDS_INSTALL=false
    for dep in $DEPENDENCIES; do
        if ! check_command "$dep" && [ "$dep" != "python3-pip" ] && [ "$dep" != "alsa-utils" ]; then
            NEEDS_INSTALL=true
            break
        fi
    done

    if [ "$NEEDS_INSTALL" = true ]; then
        echo "Installing: $DEPENDENCIES"
        sudo apt-get install -y $DEPENDENCIES
    else
        echo -e "${GREEN}[OK] System dependencies already installed.${NC}"
    fi
else
    echo -e "${RED}[WARNING] Not a Debian-based system. Please manually install: golang, git, python3, ffmpeg, alsa-utils, espeak, fswebcam, adb, nmap${NC}"
fi

# ── 2. Python tools ───────────────────────────────────────────────────────
echo ""
echo -e "${YELLOW}[2/4] Installing Python tools (Calendar & Document skills)...${NC}"
# NOTE: ghost-web runs as root with ProtectHome=true, so a user-local
# ~/.local/bin/gcalcli is invisible to the service. Always ensure the
# system-wide /usr/local/bin/gcalcli exists regardless of check_command.
if [ ! -x /usr/local/bin/gcalcli ]; then
    sudo pip3 install gcalcli --break-system-packages 2>/dev/null || pip3 install gcalcli --break-system-packages 2>/dev/null || pip3 install gcalcli
fi
# Command-only skills ship ready: pyfiglet (ascii-art), yt-dlp + feedparser
# (internet-reading). System-wide so the root services see them too.
pip3 install pypdf python-docx pyfiglet yt-dlp feedparser firecrawl-anydoc --break-system-packages 2>/dev/null || pip3 install pypdf python-docx pyfiglet yt-dlp feedparser firecrawl-anydoc
echo -e "${GREEN}[OK] Python tools installed.${NC}"

# ── 2.5. Ollama ───────────────────────────────────────────────────────────
echo ""
echo -e "${YELLOW}[2.5/4] Installing Ollama (Local LLM)...${NC}"
if ! check_command "ollama"; then
    echo -e "${YELLOW}[INFO] Ollama not found. Installing...${NC}"
    if check_command "curl"; then
        curl -fsSL https://ollama.com/install.sh | sh
        echo -e "${GREEN}[OK] Ollama installed.${NC}"
        echo -e "${YELLOW}[INFO] Pre-pulling ${DEFAULT_MODEL_TAG} model...${NC}"
        ollama pull "$DEFAULT_MODEL_TAG"
    else
        echo -e "${RED}[ERROR] curl is required for Ollama installation.${NC}"
    fi
else
    echo -e "${GREEN}[OK] Ollama already installed.${NC}"
    ollama pull "$DEFAULT_MODEL_TAG"
fi

# ── 3. Build Ghost ────────────────────────────────────────────────────────
echo ""
echo -e "${YELLOW}[3/4] Building Ghost binary...${NC}"

# .env setup
if [ ! -f ".env" ]; then
    if [ -f ".env.example" ]; then
        echo -e "${YELLOW}[INFO] No .env file found. Creating from .env.example...${NC}"
        cp .env.example .env
        echo -e "${YELLOW}[IMPORTANT] Please edit .env and add your API keys before running Ghost!${NC}"
    else
        echo -e "${RED}[WARNING] No .env or .env.example found. Configure API keys manually.${NC}"
    fi
fi

# Auto-generate BRIDGE_SECRET if missing or placeholder
if [ -f ".env" ]; then
    if ! grep -q "^BRIDGE_SECRET=" ".env"; then
        secret="$(generate_secret)"
        echo "" >> .env
        echo "BRIDGE_SECRET=$secret" >> .env
        echo -e "${GREEN}[OK] Generated BRIDGE_SECRET and added to .env${NC}"
    else
        current_secret="$(grep "^BRIDGE_SECRET=" .env | tail -n 1 | cut -d '=' -f 2-)"
        if [ -z "$current_secret" ] || [ "$current_secret" = "pick_a_strong_secret_here" ]; then
            secret="$(generate_secret)"
            sed -i "s/^BRIDGE_SECRET=.*/BRIDGE_SECRET=$secret/" .env
            echo -e "${GREEN}[OK] Generated BRIDGE_SECRET and updated .env${NC}"
        fi
    fi
fi

# Fix tilde in GHOST_AGENTS_DEFAULTS_WORKSPACE if present
if [ -f ".env" ]; then
    if grep -q "GHOST_AGENTS_DEFAULTS_WORKSPACE=~" ".env"; then
        ABSOLUTE_WORKSPACE="${HOME}/ghost/workspace"
        sed -i "s|GHOST_AGENTS_DEFAULTS_WORKSPACE=~/ghost/workspace|GHOST_AGENTS_DEFAULTS_WORKSPACE=${ABSOLUTE_WORKSPACE}|g" .env
        echo -e "${GREEN}[OK] Fixed tilde in GHOST_AGENTS_DEFAULTS_WORKSPACE → ${ABSOLUTE_WORKSPACE}${NC}"
    fi
fi

if ! check_command "go"; then
    echo -e "${RED}[ERROR] Go is not installed. Cannot build.${NC}"
    exit 1
fi

echo -e "${YELLOW}Running code generation...${NC}"
go generate ./cmd/ghost
if [ $? -ne 0 ]; then
    echo -e "${RED}[ERROR] Code generation failed. Trying manual copy...${NC}"
    rm -rf cmd/ghost/workspace
    cp -r workspace cmd/ghost/workspace
fi

[ -f "ghost" ] && rm -f ghost

go build -o ghost ./cmd/ghost
if [ $? -eq 0 ]; then
    echo -e "${GREEN}[OK] Build successful: ./ghost${NC}"
else
    echo -e "${RED}[ERROR] Build failed.${NC}"
    exit 1
fi

# Install binary to the canonical location via make (single install path).
# Developer Mode converges on the same /usr/local/bin/ghost the services and
# `ghost update` use — a separate ~/.local/bin copy is what made updated
# binaries appear not to run.
make install-ghost
echo -e "${GREEN}[OK] Binary installed to /usr/local/bin/ghost${NC}"

# ── 4. Service setup ──────────────────────────────────────────────────────
echo ""
echo -e "${YELLOW}[4/4] Service Configuration${NC}"
if [ "$NO_SERVICE" = "1" ]; then
    echo -e "${BLUE}[INFO] Skipping service install (--no-service).${NC}"
else
    ask "Do you want to install Ghost as a system service (auto-start on boot)? (y/N) " "n" INSTALL_SERVICE || exit 1
    if [[ "$INSTALL_SERVICE" =~ ^[Yy]$ ]]; then
        install_service
    fi
fi

# ── Done ──────────────────────────────────────────────────────────────────
echo ""
echo -e "${GREEN}===================================================${NC}"
echo -e "${GREEN}  Setup Complete!${NC}"
echo -e "${GREEN}===================================================${NC}"
echo ""
echo -e "${BLUE}Finish onboarding:${NC}"
echo "  ./ghost onboard --guided        # guided provider/model setup (resumable)"
echo ""
echo -e "${BLUE}Useful commands:${NC}"
echo "  sudo systemctl status ghost          # check service status"
echo "  sudo journalctl -u ghost -f          # follow logs"
echo "  sudo systemctl restart ghost         # restart after config changes"
echo "  make install && sudo systemctl restart ghost   # rebuild + restart"
echo ""

ask "Do you want to start Ghost now? (Y/N) " "n" RUN_NOW || exit 1
if [[ "$RUN_NOW" =~ ^[Yy]$ ]]; then
    if systemctl is-active ghost &>/dev/null; then
        echo -e "${BLUE}Ghost service is already running. Tailing logs...${NC}"
        sudo journalctl -u ghost -f
    else
        ./ghost serve --debug
    fi
fi