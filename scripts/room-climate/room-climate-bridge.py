#!/usr/bin/env python3
"""Room climate bridge: Arduino Uno + DHT11 -> Ghost.

The Uno prints one JSON line per reading on its serial port:

    {"t":31.5,"h":62.0}
    {"err":"no-read"}

This bridge turns those readings into what Ghost already understands:

  * <workspace>/state/room-climate.json - the latest reading, overwritten
    each time. Skills read this file to answer "what's the temperature in
    here?".
  * <workspace>/state/sensors/*.json - Ghost's sensor inbox
    (pkg/proactive/sensors.go). Written only when the temperature leaves
    the comfort band (>=30 C or <=5 C, Ghost's own thresholds), so Ghost
    pages the owner once per hot or cold spell instead of every reading.
    Humidity stays in the latest-reading file only: Ghost reads humidity
    events but never treats them as urgent, so inbox files for humidity
    would only ever pile up.

Stdlib only: serial access goes through termios, no pyserial needed.
Runs under ghost-room-climate.service.
"""

from __future__ import annotations

import glob
import json
import os
import select
import sys
import termios
import time
from datetime import datetime, timezone

# A USB serial adapter re-enumerates to the next number when unplugged
# (ttyUSB0 -> ttyUSB1), so the port is found each cycle instead of fixed.
# Set ROOM_CLIMATE_PORT to pin one explicitly.
SERIAL_PORT = os.environ.get("ROOM_CLIMATE_PORT", "")
BAUD = 9600
WORKSPACE = os.environ.get("GHOST_WORKSPACE_DIR") or os.path.expanduser("~/.ghost/workspace")
DEVICE = os.environ.get("ROOM_CLIMATE_DEVICE", "room-uno")

# Ghost's own comfort band (pkg/proactive/sensors.go: TempHighC, TempLowC).
TEMP_HIGH_C = 30.0
TEMP_LOW_C = 5.0

LATEST_EVERY_S = 15   # refresh the latest-reading file at most this often
RECONNECT_S = 3       # wait between retries when the board is absent
IDLE_REOPEN_S = 30    # reopen the port if the board goes silent this long


def log(msg: str) -> None:
    print(f"room-climate: {msg}", flush=True)


# USB vendors worth accepting: WCH CH340 (Uno clones), Arduino, FTDI.
KNOWN_VENDORS = {"1a86", "2341", "0403"}


def usb_vendor(path: str) -> str | None:
    """idVendor of the USB device behind a tty, or None when unreadable."""
    name = os.path.basename(path)
    base = f"/sys/class/tty/{name}/device"
    for rel in ("../idVendor", "../../idVendor"):
        try:
            with open(os.path.normpath(os.path.join(base, rel)), encoding="ascii") as fh:
                return fh.read().strip()
        except OSError:
            continue
    return None


def find_port() -> str | None:
    """The board's serial device: pinned port if given, else any known adapter."""
    if SERIAL_PORT:
        return SERIAL_PORT if os.path.exists(SERIAL_PORT) else None
    ports = sorted(glob.glob("/dev/ttyUSB*") + glob.glob("/dev/ttyACM*"))
    if not ports:
        return None
    for port in ports:
        vendor = usb_vendor(port)
        if vendor in KNOWN_VENDORS:
            return port
    return ports[0]


def utc_now() -> str:
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def write_json(path: str, payload: dict) -> None:
    """Write via rename so a reader never sees a half-written file."""
    tmp = f"{path}.tmp"
    with open(tmp, "w", encoding="utf-8") as fh:
        json.dump(payload, fh, indent=2)
        fh.write("\n")
    os.replace(tmp, path)


def temperature_level(t: float) -> str:
    if t >= TEMP_HIGH_C:
        return "high"
    if t <= TEMP_LOW_C and t != 0:
        return "low"
    return "ok"


class Bridge:
    def __init__(self) -> None:
        self.state_dir = os.path.join(WORKSPACE, "state")
        self.sensor_dir = os.path.join(self.state_dir, "sensors")
        os.makedirs(self.sensor_dir, exist_ok=True)
        self.latest = os.path.join(self.state_dir, "room-climate.json")
        self.buf = b""
        self.level = "ok"
        self.level_known = False
        self.last_publish = 0.0

    # -- Ghost's sensor inbox -------------------------------------------------
    def pending_event(self, kind: str) -> bool:
        """True while Ghost still has an unacted event of this kind pending."""
        try:
            return any(f.endswith(".json") and kind in f for f in os.listdir(self.sensor_dir))
        except OSError:
            return False

    def publish_event(self, kind: str, value: float, unit: str) -> None:
        path = os.path.join(self.sensor_dir, f"{DEVICE}-{kind}-{int(time.time())}.json")
        write_json(path, {
            "device": DEVICE,
            "kind": kind,
            "value": round(value, 1),
            "unit": unit,
            "at": utc_now(),
        })
        log(f"event: {kind}={value:.1f}{unit}")

    # -- readings -------------------------------------------------------------
    def on_reading(self, temp: float, humidity: float, pin: object = None) -> None:
        now = time.time()
        if now - self.last_publish >= LATEST_EVERY_S:
            payload = {
                "device": DEVICE,
                "source": "arduino-uno-dht11",
                "temperature_c": round(temp, 1),
                "humidity_pct": round(humidity, 1),
                "at": utc_now(),
            }
            if isinstance(pin, int):
                payload["dht_pin"] = pin   # which board pin the scanner locked on
            write_json(self.latest, payload)
            self.last_publish = now
            log(f"latest: {temp:.1f}C {humidity:.1f}%")

        level = temperature_level(temp)
        crossed = level != "ok" and (not self.level_known or level != self.level)
        if crossed and not self.pending_event("temperature"):
            self.publish_event("temperature", temp, "C")
        self.level = level
        self.level_known = True

    def on_line(self, raw: str) -> None:
        line = raw.strip()
        if not line or not line.startswith("{"):
            return
        try:
            data = json.loads(line)
        except ValueError:
            return
        if "err" in data:
            # The sensor did not answer (wiring, or a DHT11 still warming up).
            log(f"board reports: {data['err']}")
            return
        if "warning" in data:
            # Found, but on a pin that belongs to the computer link.
            log(f"board says: {data.get('fix', 'check the wiring')}")
            return
        if "found" in data:
            log(f"sensor found on pin {data['found']}")
        try:
            temp = float(data["t"])
            humidity = float(data["h"])
        except (KeyError, TypeError, ValueError):
            return
        pin = data.get("pin")
        self.on_reading(temp, humidity, pin if isinstance(pin, int) else None)

    # -- serial ---------------------------------------------------------------
    @staticmethod
    def open_port(path: str) -> int:
        fd = os.open(path, os.O_RDWR | os.O_NOCTTY | os.O_NONBLOCK)
        attrs = termios.tcgetattr(fd)
        attrs[0] = 0                                        # iflag: no processing
        attrs[1] = 0                                        # oflag: no processing
        attrs[2] = termios.CS8 | termios.CREAD | termios.CLOCAL  # cflag: 8N1
        attrs[3] = 0                                        # lflag: raw
        attrs[4] = termios.B9600                            # ispeed
        attrs[5] = termios.B9600                            # ospeed
        attrs[6][termios.VMIN] = 0
        attrs[6][termios.VTIME] = 0
        termios.tcsetattr(fd, termios.TCSANOW, attrs)
        termios.tcflush(fd, termios.TCIOFLUSH)
        return fd

    def run(self) -> None:
        log(f"started: readings go to {self.latest}")
        while True:
            port = find_port()
            if port is None:
                log("waiting for the board on USB (no serial device yet)")
                time.sleep(RECONNECT_S)
                continue
            try:
                fd = self.open_port(port)
            except OSError as exc:
                log(f"waiting for {port}: {exc.strerror or exc}")
                time.sleep(RECONNECT_S)
                continue

            log(f"serial open: {port} at {BAUD} baud")
            self.buf = b""
            last_data = time.time()
            try:
                while True:
                    ready, _, _ = select.select([fd], [], [], 1.0)
                    if not ready:
                        if time.time() - last_data > IDLE_REOPEN_S:
                            raise OSError("board silent")
                        continue
                    chunk = os.read(fd, 512)
                    if not chunk:
                        continue
                    last_data = time.time()
                    self.buf += chunk
                    while b"\n" in self.buf:
                        line, self.buf = self.buf.split(b"\n", 1)
                        self.on_line(line.decode("utf-8", "replace"))
            except OSError as exc:
                log(f"serial lost ({exc.strerror or exc}); reconnecting")
                try:
                    os.close(fd)
                except OSError:
                    pass
                time.sleep(RECONNECT_S)


if __name__ == "__main__":
    sys.exit(Bridge().run() or 0)
