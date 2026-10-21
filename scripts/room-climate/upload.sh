#!/usr/bin/env bash
#
# Build the room-climate sketch and flash it to the Arduino Uno, restarting
# the bridge so it does not hold the serial port during the upload.
#
# Usage:  scripts/room-climate/upload.sh [port]   (default: /dev/ttyUSB0)

set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HEX="$DIR/uno_dht11/build/uno_dht11.hex"
SERVICE=ghost-room-climate.service

# The adapter re-enumerates (ttyUSB0 -> ttyUSB1) when unplugged, so the port
# is looked up instead of assumed. Pass one as $1 to pin it.
PORT="${1:-${ROOM_CLIMATE_PORT:-}}"
if [[ -z "$PORT" ]]; then
  fallback=""
  for p in /dev/ttyUSB* /dev/ttyACM*; do
    [[ -e "$p" ]] || continue
    name="$(basename "$p")"
    vendor="$(cat "/sys/class/tty/$name/device/../idVendor" 2>/dev/null || true)"
    case "$vendor" in
      1a86|2341|0403) PORT="$p"; break ;;   # CH340 / Arduino / FTDI
    esac
    [[ -n "$fallback" ]] || fallback="$p"
  done
  PORT="${PORT:-$fallback}"
fi
[[ -n "$PORT" && -e "$PORT" ]] || { echo "no serial device found - is the board plugged in?" >&2; exit 1; }
echo "port: $PORT"

bash "$DIR/build.sh" "$HEX"

# The bridge holds the port; avrdude needs it alone. Restart it afterwards
# whenever the unit is installed - even if a previous run left it stopped.
RESTART=0
if systemctl is-enabled --quiet "$SERVICE" 2>/dev/null; then
  RESTART=1
  if systemctl is-active --quiet "$SERVICE"; then
    echo "stopping $SERVICE for upload"
    sudo systemctl stop "$SERVICE"
  fi
fi
fuser -k "$PORT" 2>/dev/null || true
sleep 1

# The adapter can re-enumerate mid-flash (the board is being handled while
# it runs), which would leave avrdude blocked forever: cap it and always
# bring the bridge back up afterwards.
restart_service() {
  if [[ "${RESTART:-0}" == 1 ]]; then
    sudo systemctl start "$SERVICE" 2>/dev/null && echo "restarted $SERVICE"
  fi
}
trap restart_service EXIT

timeout 60 avrdude -p atmega328p -c arduino -P "$PORT" -b 115200 -D -U "flash:w:$HEX:i" \
  || { echo "flash failed (board unplugged mid-write?)" >&2; exit 1; }
