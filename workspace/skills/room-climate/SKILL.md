---
name: room-climate
description: Room temperature and humidity measured by the Arduino Uno + DHT11 sensor attached to this Pod. Use when the owner asks about the temperature, humidity or climate in the room, how hot or cold or humid it is in here, or wants a comfort reading.
version: 1.0.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [sensor, temperature, humidity, room, hardware]
prerequisites:
  commands: [cat]
---

# Room climate (Arduino Uno + DHT11)

The Pod feels the room through an Arduino Uno with a DHT11 sensor on
`/dev/ttyUSB0`. A small bridge (`room-climate-bridge.py`) rewrites the latest
reading into `state/room-climate.json` under the workspace, refreshed about
every 15 seconds.

## Answering the owner

1. Read `state/room-climate.json` with `read_file`.
2. Report `temperature_c` (°C) and `humidity_pct` (%), with the `at`
   timestamp so the owner knows how fresh it is.
3. Example: "อุณหภูมิห้อง 31.0 °C ความชื้น 62% (อ่านเมื่อ 12:00)" — answer in
   whatever language the owner used.

If the file is missing, malformed, or older than 15 minutes the sensor is not
reporting. Say so plainly instead of guessing — then, only if useful, offer
the Pod's own temperature (`system_status`) or outdoor conditions
(`weather_now`).

## Comfort bands (for extra context)

| Temperature | Comfort |
|---|---|
| < 15 °C | cold |
| 15–24 °C | comfortable |
| 24–28 °C | warm |
| ≥ 28 °C | hot |

| Humidity | Comfort |
|---|---|
| < 30 % | dry |
| 30–60 % | comfortable |
| 60–70 % | humid |
| > 70 % | very humid |

## Proactive alerts — already built in

When a reading leaves the 5–30 °C band, the bridge drops an event into
`state/sensors/` and Ghost pages the owner on its own ("Heads up from
room-uno: high temperature at room-uno"). Nothing to configure: only heat and
cold pages, comfort bands stay silent, and humidity never pages by design.

## If the owner asks to change something

- Thresholds live in the bridge (`TEMP_HIGH_C`, `TEMP_LOW_C`).
- Wiring: DHT11 VCC→5V, DATA→D2, GND→GND on the Uno.
- Rebuild and reflash with `scripts/room-climate/upload.sh` — the service
  pauses during upload and resumes after.
