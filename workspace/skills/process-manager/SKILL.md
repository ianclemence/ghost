---
name: process-manager
description: Monitor CPU and memory usage, list running processes, and terminate unresponsive programs. Invoke when user asks "what processes are running", "kill process X", "CPU usage", "memory hogs", or "which process is using port 8080". Linux: ps/kill/pkill. Windows: tasklist/taskkill.
version: 1.1.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [processes, system-monitor, task-manager, CPU, memory, killing]
prerequisites:
  commands: []
---

# Process Manager

Monitor and manage running processes.

## Quick Reference

| Task | Linux | Windows |
|------|-------|---------|
| List processes | `ps aux` | `tasklist` |
| Top memory | `ps aux --sort=-%mem | head` | `tasklist /FI "MEMUSAGE gt 100000"` |
| Kill by name | `pkill name` | `taskkill /IM name.exe /F` |
| Kill by PID | `kill PID` | `taskkill /PID 1234 /F` |
| Port to process | `lsof -i :8080` | `netstat -ano \| findstr :8080` |

## List Processes

### Linux

```bash
ps aux | head -20    # first 20 processes
ps aux --sort=-%cpu  # sort by CPU
ps aux --sort=-%mem  # sort by memory
```

### Windows

```powershell
tasklist
tasklist /FO TABLE    # table format (default)
tasklist /FO CSV      # CSV for parsing
tasklist /FI "STATUS eq running"   # running only
tasklist /FI "MEMUSAGE gt 100000"  # > 100MB
```

## Resource Usage

### Linux

```bash
top -bn1 | head -20          # snapshot of top processes
ps aux | awk '{print $4"\t"$11}' | sort -rn | head  # memory %
free -h                       # total system memory
df -h                         # disk space
```

### Windows

```powershell
tasklist /FI "MEMUSAGE gt 100000" /FO CSV | Sort-Object { [double]$_ -split ','[1] } | Select-Object -First 10
wmic OS get FreePhysicalMemory,TotalVisibleMemorySize /Value   # memory
wmic diskdrive get model,size   # disk space
```

## Kill Processes

**Always try SIGTERM (15) first, then SIGKILL (9) as a last resort.**

### Linux — Graceful (SIGTERM)

```bash
kill 12345         # SIGTERM (15) — graceful
kill -15 12345     # explicit SIGTERM
```

### Linux — Force (SIGKILL)

```bash
kill -9 12345     # SIGKILL — immediate, no cleanup
```

### Linux — By Name

```bash
pkill firefox       # SIGTERM by name
pkill -9 chrome     # SIGKILL by name
killall process-name
```

### Windows

```powershell
taskkill /IM notepad.exe /F      # by name
taskkill /PID 1234 /F            # by PID
taskkill /IM chrome.exe /T /F     # /T kills child processes too
```

## Port-to-Process

### Linux

```bash
# What's using port 8080?
lsof -i :8080
# or
ss -tlnp | grep :8080
```

### Windows

```powershell
netstat -ano | findstr :8080
# Then: tasklist /FI "PID eq 12345"
```

## Zombie Processes

### Linux

```bash
ps aux | grep -w Z    # Z = zombie
```

Zombies have no resources but indicate a parent that didn't reap. Kill the parent:
```bash
kill -9 PARENT_PID
```

## Orphaned Processes (Linux)

Child processes whose parent died. Re-parent to init (PID 1):
```bash
kill -17 -1    # sends SIGHUP to reparent
```

## Process Priority (Linux)

Lower nice = higher priority:
```bash
nice -n 10 command    # start low priority
renice 5 -p PID       # change running process priority
```

## Service Management (Linux)

```bash
systemctl status nginx
systemctl restart nginx
systemctl stop nginx
systemctl list-units --type=service --state=running
```

## Windows Service Management

```powershell
Get-Service nginx      # status
Start-Service nginx
Stop-Service nginx
Restart-Service nginx
```

## Cron Jobs (Linux) — See Also: `tmux` Skill

To schedule process monitoring:
```bash
crontab -e
# */5 * * * * ps aux --sort=-%cpu | head -5 >> /tmp/cpu_report.txt
```
