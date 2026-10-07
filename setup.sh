#!/bin/bash

# Color codes
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Canonical local model tags. Single-sourced with pkg/config
# DefaultLocalTag / DefaultEmbeddingTag — if you change these, change
# those constants too (covered by TestDefaultModelTagSingleSourced).
DEFAULT_MODEL_TAG="qwen3:0.6b"
DEFAULT_EMBED_TAG="embeddinggemma"

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
        echo -e "${YELLOW}[INFO] Pre-pulling ${DEFAULT_MODEL_TAG} + ${DEFAULT_EMBED_TAG} models...${NC}"
        ollama pull "$DEFAULT_MODEL_TAG"
        ollama pull "$DEFAULT_EMBED_TAG"
    else
        echo -e "${RED}[ERROR] curl is required for Ollama installation.${NC}"
    fi
else
    echo -e "${GREEN}[OK] Ollama already installed.${NC}"
    ollama pull "$DEFAULT_MODEL_TAG"
    ollama pull "$DEFAULT_EMBED_TAG"
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
if ! make install-ghost; then
    echo -e "${RED}[ERROR] Install failed (see the make output above). Ghost is NOT installed.${NC}"
    exit 1
fi
echo -e "${GREEN}[OK] Binary installed to /usr/local/bin/ghost${NC}"

# ── 4. Services ───────────────────────────────────────────────────────────
# `make install-ghost` already wrote, enabled and started the system units
# (ghost, ghost-web, ghost-speech, backups) under /var/ghost. There is exactly
# one install shape; offering a second per-user unit here would overwrite the
# system one and leave two daemons fighting for the same port.
echo ""
echo -e "${YELLOW}[4/4] Services${NC}"
if [ "$NO_SERVICE" = "1" ]; then
    echo -e "${BLUE}[INFO] Not starting services (--no-service).${NC}"
else
    sudo systemctl enable --now ghost ghost-web 2>/dev/null || true
    echo -e "${GREEN}[OK] Ghost and the web console are running and start on boot.${NC}"
fi

# ── Done ──────────────────────────────────────────────────────────────────
echo ""
echo -e "${GREEN}===================================================${NC}"
echo -e "${GREEN}  Setup Complete!${NC}"
echo -e "${GREEN}===================================================${NC}"
echo ""
echo -e "${BLUE}Next, in order:${NC}"
echo "  1. ghost onboard --guided       # choose the AI Ghost thinks with (resumable)"
echo "  2. ghost pair                   # shows a QR: scan it with the Ghost app"
echo "  3. ghost                        # or just talk to it here in the terminal"
echo ""
echo -e "${BLUE}Useful commands:${NC}"
echo "  sudo systemctl status ghost          # check service status"
echo "  sudo journalctl -u ghost -f          # follow logs"
echo "  sudo systemctl restart ghost         # restart after config changes"
echo "  make install-ghost                   # rebuild + reinstall everything"
echo ""
