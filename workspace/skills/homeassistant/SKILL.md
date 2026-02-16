---
name: "homeassistant"
description: "Controls smart home devices via Home Assistant. Invoke when user asks to turn on/off lights, check sensors, or run scenes."
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
