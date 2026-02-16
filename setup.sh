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

# 1. Update & Install System Dependencies
echo -e "${YELLOW}[1/4] Updating system and installing dependencies...${NC}"
# Check if apt-get is available (Debian/Ubuntu/PiOS)
if check_command "apt-get"; then
    sudo apt-get update
    
    # Core deps
    DEPENDENCIES="golang git python3 python3-pip ffmpeg alsa-utils espeak fswebcam"
    
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
    echo -e "${RED}[WARNING] Not a Debian-based system. Please manually install: golang, git, python3, ffmpeg, alsa-utils, espeak, fswebcam${NC}"
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

# 3. Build Ghost
echo ""
echo -e "${YELLOW}[3/4] Building Ghost binary...${NC}"
if ! check_command "go"; then
    echo -e "${RED}[ERROR] Go is not installed. Cannot build.${NC}"
    exit 1
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
    ./ghost agent
fi
