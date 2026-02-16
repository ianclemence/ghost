---
name: "network"
description: "Scans the local network for connected devices. Invoke when user asks 'Who is on the WiFi?', 'Scan network', or 'List devices'."
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
