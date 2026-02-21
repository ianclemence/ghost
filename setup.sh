#!/bin/bash

# Color codes
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}===================================================${NC}"
echo -e "${GREEN}  Ghost: Your Sovereign AI Presence (Linux/Pi Setup)${NC}"
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

# Detect architecture mismatch (64-bit kernel, 32-bit userland)
# This is common on Raspberry Pi OS (64-bit kernel with 32-bit userland)
if [ "$(uname -m)" = "aarch64" ] && [ "$(dpkg --print-architecture 2>/dev/null)" = "armhf" ]; then
    echo -e "${YELLOW}[WARNING] Detected 64-bit kernel with 32-bit userland. Forcing GOARCH=arm.${NC}"
    export GOARCH=arm
    export GOARM=7
    export CGO_ENABLED=1
fi

# 1. Update & Install System Dependencies
echo -e "${YELLOW}[1/4] Updating system and installing dependencies...${NC}"
# Check if apt-get is available (Debian/Ubuntu/PiOS)
if check_command "apt-get"; then
    sudo apt-get update
    
    # Core deps
    DEPENDENCIES="golang git python3 python3-pip ffmpeg alsa-utils espeak fswebcam adb nmap"
    
    # Check if we need to install anything
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

# 2. Install Python Tools (gcalcli)
echo ""
echo -e "${YELLOW}[2/4] Installing Python tools (Calendar skill)...${NC}"
if ! check_command "gcalcli"; then
    pip3 install gcalcli --break-system-packages 2>/dev/null || pip3 install gcalcli
    echo -e "${GREEN}[OK] gcalcli installed.${NC}"
else
    echo -e "${GREEN}[OK] gcalcli already installed.${NC}"
fi

# 2.5. Install PicoLM (Local LLM)
echo ""
echo -e "${YELLOW}[2.5/4] Installing PicoLM (Local LLM)...${NC}"
PICOLM_DIR="$HOME/.picolm"
mkdir -p "$PICOLM_DIR"

if [ ! -f "$PICOLM_DIR/bin/picolm" ]; then
    echo "Cloning PicoLM..."
    # Clone to a temporary directory
    if [ -d "/tmp/picolm_src" ]; then
        rm -rf /tmp/picolm_src
    fi
    git clone https://github.com/picolm/picolm.git /tmp/picolm_src
    
    echo "Building PicoLM..."
    cd /tmp/picolm_src/picolm
    
    # Detect architecture for make target
    if [ "$(uname -m)" = "aarch64" ] || [ "$(uname -m)" = "armv7l" ]; then
         echo "Building for Pi/ARM..."
         make pi
    else
         echo "Building for x86/Native..."
         make native
    fi
    
    mkdir -p "$PICOLM_DIR/bin"
    cp picolm "$PICOLM_DIR/bin/"
    
    # Clean up
    cd "$OLDPWD"
    rm -rf /tmp/picolm_src
    echo -e "${GREEN}[OK] PicoLM binary installed to $PICOLM_DIR/bin/picolm${NC}"
else
    echo -e "${GREEN}[OK] PicoLM binary already exists.${NC}"
fi

# Model
MODEL_DIR="$PICOLM_DIR/models"
MODEL_PATH="$MODEL_DIR/tinyllama-1.1b-chat-v1.0.Q4_K_M.gguf"

if [ ! -f "$MODEL_PATH" ]; then
    echo "Downloading TinyLlama model (638MB)..."
    mkdir -p "$MODEL_DIR"
    curl -L -o "$MODEL_PATH" "https://huggingface.co/TinyLlama/TinyLlama-1.1B-Chat-v1.0-GGUF/resolve/main/tinyllama-1.1b-chat-v1.0.Q4_K_M.gguf"
    echo -e "${GREEN}[OK] Model downloaded to $MODEL_PATH${NC}"
else
    echo -e "${GREEN}[OK] TinyLlama model already exists.${NC}"
fi

# 3. Build Ghost
echo ""
echo -e "${YELLOW}[3/4] Building Ghost binary...${NC}"

# Check for .env file
if [ ! -f ".env" ]; then
    if [ -f ".env.example" ]; then
        echo -e "${YELLOW}[INFO] No .env file found. Creating from .env.example...${NC}"
        cp .env.example .env
        echo -e "${YELLOW}[IMPORTANT] Please edit .env and add your API keys before running Ghost!${NC}"
    else
        echo -e "${RED}[WARNING] No .env or .env.example found. You may need to configure API keys manually.${NC}"
    fi
fi

if ! check_command "go"; then
    echo -e "${RED}[ERROR] Go is not installed. Cannot build.${NC}"
    exit 1
fi

echo -e "${YELLOW}Running code generation (bundling workspace)...${NC}"
# Run go generate to trigger the 'cp -r ../../workspace .' command in main.go
go generate ./cmd/ghost
if [ $? -ne 0 ]; then
    echo -e "${RED}[ERROR] Code generation failed. Trying manual copy...${NC}"
    # Fallback if go generate fails (e.g., missing cp command context)
    rm -rf cmd/ghost/workspace
    cp -r workspace cmd/ghost/workspace
fi

go build -o ghost ./cmd/ghost
if [ $? -eq 0 ]; then
    echo -e "${GREEN}[OK] Build successful: ./ghost${NC}"
else
    echo -e "${RED}[ERROR] Build failed.${NC}"
    exit 1
fi

# 4. Service Setup (Optional)
echo ""
echo -e "${YELLOW}[4/4] Service Configuration${NC}"
read -p "Do you want to install Ghost as a system service (auto-start on boot)? (y/N) " INSTALL_SERVICE
if [[ "$INSTALL_SERVICE" =~ ^[Yy]$ ]]; then
    if [ -f "Makefile" ]; then
        make install
        echo -e "${GREEN}[OK] Service installed. Check status with: sudo systemctl status ghost${NC}"
    else
        echo -e "${RED}[ERROR] Makefile not found. Cannot install service automatically.${NC}"
    fi
fi

echo ""
echo -e "${GREEN}===================================================${NC}"
echo -e "${GREEN}  Setup Complete!${NC}"
echo -e "${GREEN}===================================================${NC}"
echo ""

read -p "Do you want to start Ghost now? (Y/N) " RUN_NOW
if [[ "$RUN_NOW" =~ ^[Yy]$ ]]; then
    ./ghost gateway --debug
fi
