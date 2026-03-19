---
name: process-manager
description: Monitor CPU and memory usage, list running processes, and terminate unresponsive or unwanted programs. Invoke when user asks "what processes are running", "kill process X", "CPU usage", "memory hogs", or "task manager". Linux/macOS users: use ps/kill/htop. Windows users: tasklist/taskkill.
version: 1.0.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [processes, system-monitor, task-manager, CPU, memory, killing]
prerequisites:
  commands: [tasklist, taskkill]
---

# Process Manager

## Tools

- **System**: Windows Task Manager CLI tools (`tasklist`, `taskkill`).
- **Safety**: Only kill processes explicitly requested by the user or clearly identified as problematic (e.g., hanging).

## Linux / Mac Commands

- **List Processes**: `ps aux --sort=-%cpu | head -n 10` (Top CPU)
- **Interactive**: `htop` (if installed) or `top`
- **Kill Process**: `kill -9 <PID>` or `pkill -f <name>`

## Windows Usage

1.  **List Processes**:
    - `tasklist` (basic list)
    - `tasklist /v` (verbose)
    - `Get-Process | Sort-Object CPU -Descending | Select-Object -First 10` (PowerShell: Top CPU consumers)

2.  **Kill Process**:
    - By Name: `taskkill /IM process_name.exe /F`
    - By PID: `taskkill /PID 1234 /F`
    - **Note**: `/F` forces termination. Use with caution.

## Workflow

1.  **Identify**: Run `tasklist` or PowerShell `Get-Process` to find the target process ID (PID) or name.
2.  **Confirm**: Ensure the process is not critical system infrastructure (e.g., `svchost.exe`, `explorer.exe` unless specifically requested).
3.  **Terminate**: Execute the kill command.
4.  **Verify**: Run `tasklist` again to confirm the process is gone.

## Examples

```powershell
# List top 5 memory hogs
Get-Process | Sort-Object WorkingSet -Descending | Select-Object -First 5

# Kill notepad
Stop-Process -Name "notepad" -Force
# OR
taskkill /IM notepad.exe /F
```
