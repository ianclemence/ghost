---
name: homeassistant
description: Control smart home devices via Home Assistant. Invoke when user asks to "turn on the lights", "set thermostat to 72", "is the front door locked", "trigger scene movie mode", "check sensor readings", or any smart home command. Requires HASS_URL and HASS_TOKEN env vars.
version: 1.0.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [homeassistant, smart-home, IoT, automation, sensors]
prerequisites:
  commands: [curl]
---

# Smart Home Bridge (Home Assistant)

Controls devices via the Home Assistant REST API.

## Requirements

- **Tool**: `curl`
- **Configuration**: Add these to `.env`:
  ```bash
  HASS_URL=http://homeassistant.local:8123
  HASS_TOKEN=your_long_lived_access_token
  ```

## Commands

### Turn Light On

```bash
curl -X POST \
  -H "Authorization: Bearer $HASS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"entity_id": "light.bedroom"}' \
  "$HASS_URL/api/services/light/turn_on"
```

### Turn Light Off

```bash
curl -X POST \
  -H "Authorization: Bearer $HASS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"entity_id": "light.bedroom"}' \
  "$HASS_URL/api/services/light/turn_off"
```

### Get State (Check if light is on)

```bash
curl -X GET \
  -H "Authorization: Bearer $HASS_TOKEN" \
  -H "Content-Type: application/json" \
  "$HASS_URL/api/states/light.bedroom"
```
