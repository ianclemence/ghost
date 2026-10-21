#!/usr/bin/env bash
#
# Build the room-climate sketch for an Arduino Uno with the system AVR
# toolchain (Debian/Ubuntu: sudo apt install gcc-avr avr-libc). No Arduino
# IDE and no x86-only Arduino tools are needed, so this also builds on
# arm64 Pods.
#
# Usage:  scripts/room-climate/build.sh [output.hex]

set -euo pipefail

SKETCH_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/uno_dht11"
OUT="${1:-$SKETCH_DIR/build/uno_dht11.hex}"
CORE_DIR="${ARDUINO_AVR_CORE:-$HOME/.arduino15/packages/arduino/hardware/avr/1.8.8}"
VARIANT_DIR="$CORE_DIR/variants/standard"
DHT_LIB="${DHT_LIBRARY:-$HOME/Arduino/libraries/DHT_sensor_library}"

# Board identity: ATmega328P @ 16 MHz, as on an Arduino Uno (and clones).
MCU=atmega328p
F_CPU=16000000L
ARDUINO_DEF=10819

BUILD_DIR="$(dirname "$OUT")/obj"
mkdir -p "$BUILD_DIR" "$(dirname "$OUT")"

command -v avr-g++ >/dev/null || {
  echo "avr-g++ not found: sudo apt install gcc-avr avr-libc" >&2
  exit 1
}
[ -f "$CORE_DIR/cores/arduino/Arduino.h" ] || {
  echo "Arduino core not found at $CORE_DIR (set ARDUINO_AVR_CORE)" >&2
  exit 1
}
[ -f "$DHT_LIB/DHT.cpp" ] || {
  echo "DHT library not found at $DHT_LIB (set DHT_LIBRARY)" >&2
  exit 1
}

INCLUDES=(-I"$CORE_DIR/cores/arduino" -I"$VARIANT_DIR" -I"$DHT_LIB")
DEFINES=(
  -DF_CPU="$F_CPU" -DARDUINO="$ARDUINO_DEF"
  -DARDUINO_AVR_UNO -DARDUINO_ARCH_AVR
)
COMMON=(-mmcu="$MCU" -Os -g -ffunction-sections -fdata-sections "${DEFINES[@]}" "${INCLUDES[@]}")

compile_c() { avr-gcc  -c -std=gnu11        -Wall "${COMMON[@]}" "$1" -o "$2"; }
compile_s() { avr-gcc  -c -x assembler-with-cpp -g "${COMMON[@]}" "$1" -o "$2"; }
compile_cpp() {
  avr-g++ -c -std=gnu++11 -fpermissive -fno-exceptions -fno-threadsafe-statics \
    -Wall "${COMMON[@]}" "$1" -o "$2"
}

# --- Sketch: .ino becomes a plain .cpp with <Arduino.h> pulled in first ------
SKETCH_CPP="$BUILD_DIR/sketch.cpp"
{
  grep -q '#include <Arduino.h>' "$SKETCH_DIR/uno_dht11.ino" || echo '#include <Arduino.h>'
  cat "$SKETCH_DIR/uno_dht11.ino"
} > "$SKETCH_CPP"
compile_cpp "$SKETCH_CPP" "$BUILD_DIR/sketch.o"

# --- Library ------------------------------------------------------------------
compile_cpp "$DHT_LIB/DHT.cpp" "$BUILD_DIR/DHT.o"

# --- Core (Arduino runtime) ---------------------------------------------------
OBJS=("$BUILD_DIR/sketch.o" "$BUILD_DIR/DHT.o")
# Object names keep the source extension: wiring_pulse.c and wiring_pulse.S
# both exist in the core and would otherwise collide on one .o.
for f in "$CORE_DIR"/cores/arduino/*.c; do
  o="$BUILD_DIR/core_$(basename "$f").o"
  compile_c "$f" "$o"
  OBJS+=("$o")
done
for f in "$CORE_DIR"/cores/arduino/*.S; do
  o="$BUILD_DIR/core_$(basename "$f").o"
  compile_s "$f" "$o"
  OBJS+=("$o")
done
for f in "$CORE_DIR"/cores/arduino/*.cpp; do
  o="$BUILD_DIR/core_$(basename "$f").o"
  compile_cpp "$f" "$o"
  OBJS+=("$o")
done

# --- Link, then produce the hex the bootloader understands --------------------
ELF="$BUILD_DIR/uno_dht11.elf"
avr-g++ -mmcu="$MCU" -Wall -Os -Wl,--gc-sections -o "$ELF" "${OBJS[@]}"
avr-objcopy -O ihex -R .eeprom "$ELF" "$OUT"

avr-size --mcu="$MCU" -C "$ELF" 2>/dev/null || avr-size "$ELF"
echo "built: $OUT"
