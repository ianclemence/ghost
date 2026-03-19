---
name: network
description: Scan the local network to discover connected devices, find IP addresses, and check availability. Invoke when user asks "who is on the WiFi", "scan the network", "find devices on my LAN", "is X connected", "show me active IPs", or "network scan". Requires nmap.
version: 1.1.0
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

## Quick Reference

| Task | Command |
|------|---------|
| Ping scan (find devices) | `nmap -sn 192.168.1.0/24` |
| Auto-detect subnet | `CIDR=$(ip -4 addr show | grep -oP '(?<=inet\s)\d+(\.\d+){3}/\d+' | head -1) && nmap -sn $CIDR` |
| Top 100 ports | `nmap -sT --top-ports 100 192.168.1.0/24` |
| OS detection | `nmap -O 192.168.1.0/24` |
| Zero-dependency ARP | `arp -a` |

## Subnet Detection

Always detect the actual subnet first — never assume 192.168.1.0/24.

Linux/Pi:

```bash
ip -4 addr show | grep -oP '(?<=inet\s)\d+(\.\d+){3}/\d+'
# Or
hostname -I | awk '{print $1}'
```

macOS:

```bash
networksetup -getinfo Wi-Fi | grep "Router IP" | awk '{print $3}'
# Or
ifconfig en0 | grep "inet " | awk '{print $2}'
```

Windows:

```powershell
ipconfig | Select-String "IPv4.*192" | ForEach-Object { ($_ -split ":")[-1].Trim() }
# Get default gateway
ipconfig | Select-String "Default Gateway"
```

Extract subnet from gateway IP: `192.168.1.1/24` from `192.168.1.1`.

## Ping Scan (Device Discovery)

Lists all devices currently online. Fast — sends ICMP echo (ping).

```bash
nmap -sn 192.168.1.0/24 | python3 -c "
import sys
lines = sys.stdin.read()
for line in lines.split('\n'):
    if 'Nmap scan report' in line:
        ip = line.split('for ')[-1].strip()
        print(ip)
    elif 'MAC Address' in line:
        mac = line.split('MAC Address: ')[-1].split()[0]
        vendor = ' '.join(line.split('MAC Address: ')[-1].split()[1:])
        print(f'  {mac}  {vendor}')
"
```

Output: IP per line, with MAC and vendor on the same device line.

## Port Scan

### Top 100 most common ports

```bash
nmap -sT --top-ports 100 192.168.1.0/24 -oG - | grep "^Host:" | python3 -c "
import sys
for line in sys.stbin:
    fields = line.strip().split()
    ip = fields[1]
    ports = [f.split('(')[0] for f in fields if '(' in f]
    if ports:
        print(f'{ip}: {', '.join(ports)}')
"
```

### Specific port scan (SSH/HTTP/SMB)

```bash
nmap -sT -p 22,80,443,445,3389 192.168.1.0/24
```

Common service ports:

| Service | Port |
|---------|------|
| SSH | 22 |
| HTTP | 80 |
| HTTPS | 443 |
| SMB | 445 |
| RDP | 3389 |
| rsync | 873 |
| NFS | 2049 |
| Docker | 2375/2376 |

## OS Detection

Tries to identify OS based on TCP stack fingerprints. Requires root.

```bash
sudo nmap -O 192.168.1.0/24
```

## No-nmap Fallback (ARP Table)

When nmap is unavailable, use the ARP table — no install needed.

Linux/macOS:

```bash
arp -a | python3 -c "
import sys, re
for line in sys.stdin:
    # Format: hostname (192.168.1.1) at aa:bb:cc:dd:ee:ff [ether] on eth0
    m = re.search(r'\((\d+\.\d+\.\d+\.\d+)\)\s+at\s+([0-9a-f:]+)', line)
    if m:
        ip, mac = m.group(1), m.group(2)
        print(f'{ip}  {mac}')
"
```

Windows:

```powershell
arp -a | Select-String "dynamic" | ForEach-Object {
    $parts = $_ -split '\s+'
    $ip = $parts[1].Trim()
    $mac = $parts[2].Trim()
    Write-Output \"$ip  $mac\"
}
```

## Resolve Hostnames

After scanning IPs, resolve their hostnames:

```bash
for ip in 192.168.1.1 192.168.1.10 192.168.1.50; do
    host=$(host $ip 2>/dev/null | awk '{print $NF}' | tr -d '.')
    echo \"$ip  ${host:-unknown}\"
done
```

## Port-to-Process (Linux)

Find what process is using a specific port:

```bash
sudo lsof -i :8080
# Or
sudo netstat -tlnp | grep :8080
```

## Installation

- Windows: Download from https://nmap.org/download.html
- Linux/Pi: `sudo apt-get install nmap`
- macOS: Pre-installed, or `brew install nmap`
