---
name: weather
description: Get current weather conditions and forecasts for any location. Invoke when user asks "what's the weather", "is it going to rain", "weather forecast", "temperature in X", "will I need an umbrella", or "how hot will it be tomorrow". Uses wttr.in or open-meteo API — no API key required.
version: 1.1.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [weather, forecast, temperature, rain, conditions]
prerequisites:
  commands: [curl]
homepage: https://wttr.in/:help
---

# Weather

Uses wttr.in (default) or open-meteo as fallback. No API key required.

## Quick Reference

| Task | Command |
|------|---------|
| Current (default) | `curl -s "wttr.in/New+York"` |
| Current (open-meteo) | `curl -s "wttr.in/New+York?format=j1"` |
| 3-day forecast | `curl -s "wttr.in/New+York?format=j1"` |
| JSON (open-meteo) | `curl -s "https://api.open-meteo.com/v1/forecast?latitude=40.71&longitude=-74.01&current_weather=true"` |
| AQI (separate) | See `aqi` skill |

## wttr.in

```bash
curl -s "wttr.in/New+York"
curl -s "wttr.in/New+York?format=j1"   # JSON
curl -s "wttr.in/New+York?lang=de"      # German
```

## open-meteo (fallback)

```bash
# Find coordinates first
curl -s "https://geocoding-api.open-meteo.com/v1/search?name=London&count=1&format=json"
# Then query
curl -s "https://api.open-meteo.com/v1/forecast?latitude=51.51&longitude=-0.13&current_weather=true&daily=weathercode,temperature_2m_max,temperature_2m_min&timezone=auto"
```

Two free services, no API keys needed.

## wttr.in (primary)

Quick one-liner:

```bash
curl -s "wttr.in/London?format=3"
# Output: London: ⛅️ +8°C
```

Compact format:

```bash
curl -s "wttr.in/London?format=%l:+%c+%t+%h+%w"
# Output: London: ⛅️ +8°C 71% ↙5km/h
```

Full forecast:

```bash
curl -s "wttr.in/London?T"
```

Format codes: `%c` condition · `%t` temp · `%h` humidity · `%w` wind · `%l` location · `%m` moon

Tips:

- URL-encode spaces: `wttr.in/New+York`
- Airport codes: `wttr.in/JFK`
- Units: `?m` (metric) `?u` (USCS)
- Today only: `?1` · Current only: `?0`
- PNG: `curl -s "wttr.in/Berlin.png" -o /tmp/weather.png`

## Open-Meteo (fallback, JSON)

Free, no key, good for programmatic use:

```bash
curl -s "https://api.open-meteo.com/v1/forecast?latitude=51.5&longitude=-0.12&current_weather=true"
```

Find coordinates for a city, then query. Returns JSON with temp, windspeed, weathercode.

Docs: https://open-meteo.com/en/docs
