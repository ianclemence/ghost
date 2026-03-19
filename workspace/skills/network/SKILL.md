---
name: network
description: Scan the local network to discover connected devices, find IP addresses, and check device availability. Invoke when user asks "who is on the WiFi", "scan the network", "find devices on my LAN", "is X connected", or "network scan". Requires nmap.
version: 1.0.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [network, scan, nmap, LAN, WiFi, discovery]
prerequisites:
  commands: [nmap]
---

# Network Scanner

Scans the local network for active hosts using `nmap`.

## Requirements

- **Tool**: `nmap`
- **Installation**:
  - Windows: [Download Installer](https://nmap.org/download.html)
  - Linux/Pi: `sudo apt-get install nmap`

## Commands

### Quick Scan (Ping Scan)

Lists all devices currently online (UP) on the local subnet.

```bash
# Detects the local subnet automatically (assuming 192.168.1.0/24 for simplicity, adjust if needed)
nmap -sn 192.168.1.0/24
```

### Detailed Scan (Slow)

Tries to identify OS and services (requires root/admin).

```bash
nmap -O 192.168.1.0/24
```
