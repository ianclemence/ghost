---
name: travel
description: Look up places, get directions and travel/commute info. Invoke when the user asks "how do I get to X", "directions to Y", "how long to get to work", "what's the fastest route", "where is the nearest pharmacy", or "travel time". Uses OpenStreetMap and OSRM — free, no API key required.
version: 1.0.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [travel, directions, commute, maps, routing, transit]
prerequisites:
  commands: [curl]
---

# Travel

Free, no-key routing and place lookup via OpenStreetMap data. Works with a
place name, an address, or coordinates.

## Geocoding (place name → coordinates)

Query [Nominatim](https://nominatim.org) (OpenStreetMap's geocoder):

```bash
curl -s "https://nominatim.openstreetmap.org/search?q=New+York&format=json&limit=1"
```

Response: `[{"lat":"40.71...","lon":"-74.00...","display_name":"..."}]`

Set a sensible `User-Agent` header so the API is happy and never hammer it —
one request per place is enough.

## Routing (A → B, fastest / shortest)

Query [OSRM](https://router.project-osrm.org) (free routing server) with
`from=lon,lat;to=lon,lat` (note: **longitude first**):

```bash
curl -s "https://router.project-osrm.org/route/v1/driving/-74.006,40.7128;-73.9857,40.7484?overview=false"
```

Read `routes[0].distance` (meters) and `routes[0].duration` (seconds). Convert
for the user (distance → km/mi, duration → min).

## What to do

1. Resolve the two places to coordinates with Nominatim.
2. Get the route with OSRM.
3. Report simply: distance and travel time, and a one-line plain direction if
   useful. Don't dump raw JSON — turn it into a sentence.
4. For "nearest X", geocode the user's location and either use the place search
   or hand off to `find-nearby`. Do not guess.

## Rules

- Never fabricate a route or time. If the API is unreachable, say so honestly.
- Distinguish "driving" from "walking": OSRM `/walking/` profile for on-foot
  estimates when the user implies it.
- Respect the user's actual home/work if it's in memory or a recent capture,
  rather than asking every time.
