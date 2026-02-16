#!/bin/bash

# Color codes
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}Checking Ghost Skills Dependencies (Raspberry Pi/Linux)...${NC}"

# Function to check command availability
check_command() {
    if command -v "$1" &> /dev/null; then
        echo -e "${GREEN}✅ $1 found${NC}"
        return 0
    else
        echo -e "${RED}❌ $1 not found${NC}"
        return 1
    fi
}

# Update package lists first
echo -e "${YELLOW}Updating package lists...${NC}"
sudo apt-get update

# 1. Python & gcalcli
if check_command "python3"; then
    if ! check_command "gcalcli"; then
        echo -e "${YELLOW}Installing gcalcli...${NC}"
        # Ensure pip is installed
        if ! check_command "pip3"; then
             sudo apt-get install -y python3-pip
        fi
        # Install gcalcli via pip (break system packages if needed on newer debian, or use venv - simplicity here)
        pip3 install gcalcli --break-system-packages 2>/dev/null || pip3 install gcalcli
    fi
else
    echo -e "${RED}Python3 is missing. Installing...${NC}"
    sudo apt-get install -y python3 python3-pip
    pip3 install gcalcli --break-system-packages 2>/dev/null || pip3 install gcalcli
fi

# 2. System Control Tools (ALSA for audio, vcgencmd for Pi display)
# alsa-utils usually comes with Pi OS, but good to check
if ! check_command "amixer"; then
    echo -e "${YELLOW}Installing alsa-utils (Audio)...${NC}"
    sudo apt-get install -y alsa-utils
fi

# vcgencmd is standard on Pi OS. If missing (e.g. Ubuntu on Pi), might need specific packages.
if ! check_command "vcgencmd"; then
    echo -e "${YELLOW}⚠️ vcgencmd not found. If not on Raspberry Pi OS, display control might not work.${NC}"
fi

# 3. Camera Tools (libcamera / fswebcam)
if ! check_command "libcamera-still"; then
    echo -e "${YELLOW}libcamera-still not found. Installing fswebcam as fallback...${NC}"
    sudo apt-get install -y fswebcam
fi

# 4. Text-to-Speech (Piper is great for Pi, but let's stick to simple espeak for base)
if ! check_command "espeak"; then
    echo -e "${YELLOW}Installing espeak (TTS)...${NC}"
    sudo apt-get install -y espeak
fi

# 5. Spotify (optional, requires more complex setup usually, just hinting)
if ! check_command "spotify-cli"; then
    echo -e "${YELLOW}Note: Spotify CLI for Linux often requires manual setup (e.g., spotify-tui or spotify-cli-linux). Skipping auto-install.${NC}"
fi

echo -e "${GREEN}Done!${NC}"
echo -e "You may need to run 'gcalcli agenda --noauth_local_webserver' to authorize Google Calendar on a headless Pi."
