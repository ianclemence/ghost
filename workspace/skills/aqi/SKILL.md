---
name: aqi
description: Check current Air Quality Index (AQI), PM2.5, PM10, and pollen levels for any city. Invoke when user asks "air quality", "AQI", "pollution level", "is the air safe to breathe", or "pollen count" for a specific location.
version: 1.1.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [air-quality, AQI, pollution, weather, environment, health]
prerequisites:
  commands: [python]
---

# Air Quality Index (AQI) & Pollen

Uses Open-Meteo's geocoding and air quality APIs. No API key required.

## Quick Reference

| Task          | Command                                                                                                                      |
| ------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| Check AQI     | `python workspace/skills/aqi/scripts/check_aqi.py "New York"`                                                                |
| Geocode city  | `curl -s "https://geocoding-api.open-meteo.com/v1/search?name=City&count=1&language=en&format=json"`                         |
| AQI by coords | `curl -s "https://air-quality-api.open-meteo.com/v1/air-quality?latitude=LAT&longitude=LON&current=us_aqi,pm2_5,pm10,ozone"` |

## Primary Method (Python)

```bash
python workspace/skills/aqi/scripts/check_aqi.py "CityName"
```

## Manual Method

### 1. Geocode

```bash
curl -s "https://geocoding-api.open-meteo.com/v1/search?name=London&count=1&language=en&format=json" | python3 -c "
import sys,json
d = json.load(sys.stdin)
r = d['results'][0]
print(f\"{r['name']}: lat={r['latitude']}, lon={r['longitude']}\")"
```

### 2. Fetch AQI

```bash
curl -s "https://air-quality-api.open-meteo.com/v1/air-quality?latitude=51.51&longitude=-0.13&current=us_aqi,pm2_5,pm10,ozone,dust,nitrogen_dioxide" | python3 -c "
import sys,json
d = json.load(sys.stdin)['current']
aqi = d['us_aqi']
pm25 = d['pm2_5']
pm10 = d['pm10']
print(f'AQI: {aqi} ({aqi_label(aqi))}')
print(f'PM2.5: {pm25} µg/m³')
print(f'PM10: {pm10} µg/m³')
def aqi_label(v):
    if v<=50: return 'Good'
    elif v<=100: return 'Moderate'
    elif v<=150: return 'Unhealthy for Sensitive'
    elif v<=200: return 'Unhealthy'
    elif v<=300: return 'Very Unhealthy'
    return 'Hazardous'
print(f'Category: {aqi_label(aqi)}')
"
```

## AQI Scale

| AQI     | Category                       |
| ------- | ------------------------------ |
| 0–50    | Good                           |
| 51–100  | Moderate                       |
| 101–150 | Unhealthy for Sensitive Groups |
| 151–200 | Unhealthy                      |
| 201–300 | Very Unhealthy                 |
| 301+    | Hazardous                      |

## Coverage

Open-Meteo AQI covers most cities globally. Not all pollutants are available in all locations.
