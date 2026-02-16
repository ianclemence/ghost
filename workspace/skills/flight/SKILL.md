---
name: "flight"
description: "Tracks flight status. Invoke when user asks 'Where is flight UA123?', 'Flight status for...', or 'Is the flight on time?'."
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
