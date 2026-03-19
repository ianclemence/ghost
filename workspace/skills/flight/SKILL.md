---
name: flight
description: Track real-time flight status by airline and flight number. Invoke when user asks "where is flight UA123", "is AA456 on time", "flight status", "arrival time for DL100", or "is my flight delayed". Requires AviationStack API key in AVIATION_API_KEY env var.
version: 1.0.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [flight, aviation, travel, tracking, airlines]
prerequisites:
  commands: [curl]
---

# Flight Tracker

Tracks flight status using the AviationStack API (Free Tier).

## Requirements

- **API Key**: You need a free API key from [aviationstack.com](https://aviationstack.com).
- **Env Var**: Add `AVIATION_API_KEY=your_key` to `.env`.

## Commands

### Track Flight

Get status by Flight IATA code (e.g., "UA123").

```bash
curl -s "http://api.aviationstack.com/v1/flights?access_key=$AVIATION_API_KEY&flight_iata=UA123"
```

### Arrivals by Airport

Check arrivals at a specific airport (e.g., "JFK").

```bash
curl -s "http://api.aviationstack.com/v1/flights?access_key=$AVIATION_API_KEY&arr_iata=JFK&limit=5"
```
