---
name: homeassistant
description: Control smart home devices via Home Assistant REST API — lights, switches, climate, media players, sensors, and scenes. Invoke when user asks to "turn on the lights", "set thermostat to 72", "is the front door locked", "trigger scene movie mode", "check temperature", "play music on Sonos", or any smart home command. Requires HASS_URL and HASS_TOKEN env vars.
version: 1.1.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [homeassistant, smart-home, IoT, automation, sensors]
prerequisites:
  commands: [curl]
---

# Smart Home Bridge (Home Assistant)

Controls devices via the Home Assistant REST API. Requires `HASS_URL` and `HASS_TOKEN` in `.env` (or Ghost settings → Integrations).

> **Preferred path:** Call the `hass` tool (`list`, `state`, `turn_on`, `turn_off` with `entity_id`). Device control is consequential: Ghost asks for approval first unless you've allowed it. Only use the `curl` commands below if the tool reports it is unavailable.

## Quick Reference

| Task              | Command                                                                                                                            |
| ----------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| List all entities | `curl -s "$HASS_URL/api/states" -H "Authorization: Bearer $HASS_TOKEN"`                                                            |
| Light on/off      | `curl -X POST -H "Authorization: Bearer $HASS_TOKEN" -d '{"entity_id":"light.room"}' "$HASS_URL/api/services/light/turn_on"`       |
| Get entity state  | `curl -s "$HASS_URL/api/states/light.bedroom" -H "Authorization: Bearer $HASS_TOKEN"`                                              |
| Trigger scene     | `curl -X POST -H "Authorization: Bearer $HASS_TOKEN" -d '{"entity_id":"scene.movie_mode"}' "$HASS_URL/api/services/scene/turn_on"` |
| List services     | `curl -s "$HASS_URL/api/services" -H "Authorization: Bearer $HASS_TOKEN"`                                                          |

## Setup

```bash
HASS_URL=http://homeassistant.local:8123
HASS_TOKEN=your_long_lived_access_token
```

To create a long-lived access token: Home Assistant UI → Profile → Long-Lived Access Tokens → Create Token.

Verify connection:

```bash
curl -s "$HASS_URL/api/states" -H "Authorization: Bearer $HASS_TOKEN" | python3 -c "import sys,json; data=json.load(sys.stdin); print(f'{len(data)} entities')"
```

## Lights

### Turn On

```bash
curl -X POST \
  -H "Authorization: Bearer $HASS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"entity_id": "light.bedroom"}' \
  "$HASS_URL/api/services/light/turn_on"
```

With brightness and color:

```bash
curl -X POST \
  -H "Authorization: Bearer $HASS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"entity_id": "light.bedroom", "brightness_pct": 80, "kelvin": 2700}' \
  "$HASS_URL/api/services/light/turn_on"
```

### Turn Off

```bash
curl -X POST \
  -H "Authorization: Bearer $HASS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"entity_id": "light.bedroom"}' \
  "$HASS_URL/api/services/light/turn_off"
```

### Toggle (flip state)

```bash
curl -X POST \
  -H "Authorization: Bearer $HASS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"entity_id": "light.bedroom"}' \
  "$HASS_URL/api/services/light/toggle"
```

## Climate (Thermostat / HVAC)

```bash
# Set temperature
curl -X POST \
  -H "Authorization: Bearer $HASS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"entity_id": "climate.living_room", "temperature": 22}' \
  "$HASS_URL/api/services/climate/set_temperature"

# Set HVAC mode (off, heat, cool, auto)
curl -X POST \
  -H "Authorization: Bearer $HASS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"entity_id": "climate.living_room", "hvac_mode": "heat"}' \
  "$HASS_URL/api/services/climate/set_hvac_mode"
```

## Switches

```bash
# Toggle switch on
curl -X POST \
  -H "Authorization: Bearer $HASS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"entity_id": "switch.garage_door"}' \
  "$HASS_URL/api/services/switch/turn_on"

# Toggle off
curl -X POST \
  -H "Authorization: Bearer $HASS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"entity_id": "switch.garage_door"}' \
  "$HASS_URL/api/services/switch/turn_off"

# Toggle flip
curl -X POST \
  -H "Authorization: Bearer $HASS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"entity_id": "switch.garage_door"}' \
  "$HASS_URL/api/services/switch/toggle"
```

## Scenes

```bash
# Activate scene by name
curl -X POST \
  -H "Authorization: Bearer $HASS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"entity_id": "scene.movie_mode"}' \
  "$HASS_URL/api/services/scene/turn_on"

# Activate scene by entity_id (from scene entity)
curl -X POST \
  -H "Authorization: Bearer $HASS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"entity_id": "scene.1478"}' \
  "$HASS_URL/api/services/scene/turn_on"
```

## Media Players

```bash
# Play/Pause
curl -X POST \
  -H "Authorization: Bearer $HASS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"entity_id": "media_player.sonos"}' \
  "$HASS_URL/api/services/media_player/media_play_pause"

# Volume
curl -X POST \
  -H "Authorization: Bearer $HASS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"entity_id": "media_player.sonos", "volume_level": 0.5}' \
  "$HASS_URL/api/services/media_player/volume_set"

# Play specific media
curl -X POST \
  -H "Authorization: Bearer $HASS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"entity_id": "media_player.sonos", "media_content_id": "https://example.com/track.mp3", "media_content_type": "music"}' \
  "$HASS_URL/api/services/media_player/play_media"
```

## Sensors (Read State)

```bash
# Get sensor reading
curl -s "$HASS_URL/api/states/sensor.temperature_living_room" \
  -H "Authorization: Bearer $HASS_TOKEN" | python3 -c "
import sys,json
d = json.load(sys.stdin)
print(f\"{d['attributes'].get('friendly_name','Sensor')}: {d['state']} {d['attributes'].get('unit_of_measurement','')}\")"

# List all sensors
curl -s "$HASS_URL/api/states" \
  -H "Authorization: Bearer $HASS_TOKEN" | python3 -c "
import sys,json
for e in json.load(sys.stdin):
    if e['entity_id'].startswith('sensor.'):
        print(f\"{e['entity_id']}: {e['state']} {e['attributes'].get('unit_of_measurement','')}\")"
```

## Discover Entity IDs

```bash
# List all entity IDs
curl -s "$HASS_URL/api/states" -H "Authorization: Bearer $HASS_TOKEN" | python3 -c "
import sys,json
for e in json.load(sys.stdin):
    domain = e['entity_id'].split('.')[0]
    name = e['attributes'].get('friendly_name', e['entity_id'])
    state = e['state']
    print(f'{e[\"entity_id\"]:40} {state:20} {name}')"

# Filter by domain
curl -s "$HASS_URL/api/states" -H "Authorization: Bearer $HASS_TOKEN" | python3 -c "
import sys,json
for e in json.load(sys.stdin):
    if e['entity_id'].startswith('light.') or e['entity_id'].startswith('switch.'):
        print(e['entity_id'])"
```

## Input Helpers (input_boolean, input_number)

```bash
# Toggle input boolean
curl -X POST \
  -H "Authorization: Bearer $HASS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"entity_id": "input_boolean.do_not_disturb"}' \
  "$HASS_URL/api/services/input_boolean/toggle"

# Set input number
curl -X POST \
  -H "Authorization: Bearer $HASS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"entity_id": "input_number.target_temperature", "value": 23.5}' \
  "$HASS_URL/api/services/input_number/set_value"
```

## Zones and Devices

```bash
# List devices
curl -s "$HASS_URL/api/devices" -H "Authorization: Bearer $HASS_TOKEN"

# List zones
curl -s "$HASS_URL/api/zones" -H "Authorization: Bearer $HASS_TOKEN"
```

## Error Handling

| Response                         | Meaning                                           |
| -------------------------------- | ------------------------------------------------- |
| `{"error": "entity_not_found"}`  | Wrong entity_id — check with `/api/states`        |
| `{"error": "service_not_found"}` | Wrong service domain (e.g. `light/` vs `switch/`) |
| 401 Unauthorized                 | Invalid or expired token                          |
| 403 Forbidden                    | Token lacks permission for this entity            |
| 404 Not Found                    | Wrong HASS_URL or entity doesn't exist            |
