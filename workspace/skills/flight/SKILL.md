---
name: flight
description: Track real-time flight status by flight number or airport. Invoke when user asks "where is flight UA123", "is AA456 on time", "flight status", "arrival time for DL100", " departures from JFK", or "is my flight delayed". Requires AviationStack API key in AVIATION_API_KEY env var.
version: 1.1.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [flight, aviation, travel, tracking, airlines]
prerequisites:
  commands: [curl]
---

# Flight

> **If flight number missing:** If user says `what's my flight status` without a flight number, ask `Which flight number should I check? (e.g., TG123)` and wait for the next message. When user replies with a short code like `TG123`, treat it as the answer and run `curl` with that flight. Do NOT use clarify tool for this — just ask naturally and resume. Tracker

AviationStack API. Requires `AVIATION_API_KEY` in `.env`. Get a free key at https://aviationstack.com.

## Quick Reference

| Task | Command |
|------|---------|
| Track by flight | `curl -s "...?access_key=$AVIATION_API_KEY&flight_iata=UA123"` |
| Departures by airport | `curl -s "...?access_key=$AVIATION_API_KEY&dep_iata=JFK"` |
| Arrivals by airport | `curl -s "...?access_key=$AVIATION_API_KEY&arr_iata=JFK"` |
| Auto-install key | AviatioStack API key setup |

## Setup

Sign up at https://aviationstack.com (free tier: 100 flights/month).

Add to `.env`:
```
AVIATION_API_KEY=your_key_here
```

Verify:
```bash
curl -s "http://api.aviationstack.com/v1/flights?access_key=$AVIATION_API_KEY&flight_iata=UA123&limit=1"
```

## Track a Specific Flight

```bash
curl -s "http://api.aviationstack.com/v1/flights?access_key=$AVIATION_API_KEY&flight_iata=UA123&limit=1" | python3 -c "
import sys,json
data = json.load(sys.stdin)
for f in data.get('data', []):
    print(f\"Flight:  {f['flight']['iata']}\")
    print(f\"Airline: {f['airline']['name']}\")
    print(f\"From:    {f['departure']['airport']} ({f['departure']['iata']}) -> {f['arrival']['airport']} ({f['arrival']['iata']})\")
    print(f\"Status:  {f['flight_status']}\")
    print(f\"Dep:     {f['departure']['scheduled'][:16]}  Gate: {f['departure'].get('gate','N/A')}  Terminal: {f['departure'].get('terminal','N/A')}\")
    print(f\"Arr:     {f['arrival']['scheduled'][:16]}  Gate: {f['arrival'].get('gate','N/A')}  Terminal: {f['arrival'].get('terminal','N/A')}\")
    if f.get('delay'):
        print(f\"Delay:   {f['delay']} min\")
    if f.get('departure',{}).get('delay'):
        print(f\"Dep Delay: {f['departure']['delay']} min\")
    if f.get('arrival',{}).get('delay'):
        print(f\"Arr Delay: {f['arrival']['delay']} min\")
"
```

## Departures from an Airport

```bash
curl -s "http://api.aviationstack.com/v1/flights?access_key=$AVIATION_API_KEY&dep_iata=JFK&limit=20" | python3 -c "
import sys,json
data = json.load(sys.stdin)
for f in data.get('data', []):
    delay = f.get('departure',{}).get('delay',0) or 0
    dmark = f'  DELAYED {delay}min' if delay > 0 else ''
    print(f\"{f['flight']['iata']:8} {f['airline']['iata']:6} -> {f['arrival']['iata']:6}  {f['departure']['scheduled'][:16]}  {dmark}\")
"
```

## Arrivals at an Airport

```bash
curl -s "http://api.aviationstack.com/v1/flights?access_key=$AVIATION_API_KEY&arr_iata=JFK&limit=20" | python3 -c "
import sys,json
data = json.load(sys.stdin)
for f in data.get('data', []):
    delay = f.get('arrival',{}).get('delay',0) or 0
    dmark = f'  DELAYED {delay}min' if delay > 0 else ''
    print(f\"{f['flight']['iata']:8} {f['airline']['iata']:6}  from {f['departure']['iata']:6}  {f['arrival']['scheduled'][:16]}  {dmark}\")
"
```

## Key Fields in Response

| Field | Description |
|-------|-------------|
| `flight_status` | scheduled, active, landed, cancelled, incident, diverted |
| `departure.scheduled` | Scheduled departure (ISO 8601) |
| `departure.actual` | Actual departure time |
| `departure.delay` | Delay in minutes (null if on time) |
| `departure.gate` | Gate number |
| `departure.terminal` | Terminal |
| `arrival.scheduled` | Scheduled arrival |
| `arrival.delay` | Arrival delay in minutes |
| `flight.iata` | Flight number (e.g. UA123) |
| `airline.name` | Airline name |

## Error Handling

| HTTP / API Error | Meaning |
|-----------------|---------|
| `{"error":{"code":104}}` | Monthly request limit reached (free tier) |
| `{"error":{"code":105}}` | No data for this flight |
| `{"error":{"code":106}}` | Invalid API key |
| Connection error | Network issue or API down — retry |

## Limitations

- Free tier: 100 requests/month
- Flight data coverage varies by region — US/Europe best, some airports limited
- Historical flights not available on free tier
