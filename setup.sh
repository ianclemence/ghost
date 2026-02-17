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

# 2.1 Install Go Tools (wacli)
echo ""
echo -e "${YELLOW}[2.1/4] Installing Go tools (WhatsApp skill)...${NC}"
if ! check_command "wacli"; then
    echo "Installing wacli..."
    go install github.com/steipete/wacli/cmd/wacli@latest
    
    # Add Go bin to PATH if not already there
    export PATH=$PATH:$(go env GOPATH)/bin
    
    if check_command "wacli"; then
        echo -e "${GREEN}[OK] wacli installed.${NC}"
    else
        echo -e "${RED}[ERROR] wacli installation failed. Please check your Go environment.${NC}"
    fi
else
    echo -e "${GREEN}[OK] wacli already installed.${NC}"
fi

# 3. Build Ghost
echo ""
echo -e "${YELLOW}[3/4] Building Ghost binary...${NC}"
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
