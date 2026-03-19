---
name: speedtest
description: Measure internet connection speed (ping, download, upload) using speedtest-cli. Invoke when user asks to "check my internet speed", "run a speed test", "how fast is my connection", or "bandwidth test". Requires speedtest-cli.
version: 1.1.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [network, speedtest, bandwidth, internet, connectivity]
prerequisites:
  commands: [speedtest-cli]
---

# Network Speed Monitor

Measures internet speed using `speedtest-cli`. Install: `pip install speedtest-cli`.

## Quick Reference

| Task            | Command                            |
| --------------- | ---------------------------------- |
| Human-readable  | `speedtest-cli`                    |
| JSON output     | `speedtest-cli --json`             |
| Simple (2-line) | `speedtest-cli --simple`           |
| List servers    | `speedtest-cli --list`             |
| Specific server | `speedtest-cli --server SERVER_ID` |

## Standard Test

```bash
speedtest-cli
```

Output:

```
Testing from ISPName (1.2.3.4)...
Retrieving speedtest.net server list...
Selecting best server based on ping...
Hosted by ServerName (City) [id]: 12.3 km latency: 23 ms
Testing download speed................................................................................
Download: 85.23 Mbit/s
Testing upload speed................................................................................
Upload: 12.45 Mbit/s
```

## JSON Output (for Parsing)

```bash
speedtest-cli --json | python3 -c "
import sys,json
d = json.load(sys.stdin)
print(f'Ping:      {d[\"ping\"]} ms')
print(f'Download:  {d[\"download\"]/1_000_000:.2f} Mbit/s')
print(f'Upload:    {d[\"upload\"]/1_000_000:.2f} Mbit/s')
print(f'Server:    {d[\"server\"][\"name\"]} ({d[\"server\"][\"country\"]})')
print(f'ISP:       {d[\"client\"][\"isp\"]}')
print(f'IP:        {d[\"client\"][\"ip\"]}')
"
```

## Simple Output

```bash
speedtest-cli --simple
# Output:
# Ping: 23.123 ms
# Download: 85.23 Mbit/s
# Upload: 12.45 Mbit/s
```

## Server Selection

### Find nearest servers

```bash
speedtest-cli --list | head -20
```

Output: each line is `SERVER_ID  ServerName  City  Country`

### Run against specific server

```bash
speedtest-cli --server 1234
```

## Latency vs Bandwidth

| Metric       | What it measures                                     |
| ------------ | ---------------------------------------------------- |
| Ping/latency | Round-trip delay to server (ms) — gaming/VoIP        |
| Download     | Data reception rate (Mbit/s) — streaming, browsing   |
| Upload       | Data send rate (Mbit/s) — video calls, cloud backups |

## Results History

speedtest-cli has no built-in history. To track over time:

```bash
speedtest-cli --json | python3 -c "
import sys,json,datetime
d = json.load(sys.stdin)
ts = datetime.datetime.now().isoformat()
print(f'{ts},{d[\"ping\"]},{d[\"download\"]/1_000_000:.2f},{d[\"upload\"]/1_000_000:.2f}')
" >> workspace/data/speedtest_history.csv
```

## Installation

```bash
pip install speedtest-cli
```

Note: the module is `speedtest-cli` but the command is `speedtest-cli`.
