---
name: healthcheck
description: Host security hardening and system status for Ghost deployments. Use when a user asks for security audits, firewall/SSH/update hardening, risk posture, or version status checks.
---

# Ghost Host Hardening

## Overview

Assess and harden the host running Ghost using standard system tools.

## Core rules

- Require explicit approval before any state-changing action.
- Do not modify remote access settings without confirming how the user connects.
- Prefer reversible, staged changes.
- If role/identity is unknown, provide recommendations only.

## Workflow

### 1) Establish context (read-only)

Determine OS, privilege level, and network exposure.

Run system checks (numbered):

1. **OS**: `uname -a` (Linux) or `systeminfo` (Windows).
2. **Ports**:
   - Linux: `ss -ltnup`
   - Windows: `netstat -ano | findstr LISTEN`
3. **Firewall**:
   - Linux: `ufw status` or `iptables -L`
   - Windows: `netsh advfirewall show allprofiles`

### 2) Security Audit (Manual)

Since Ghost does not have a built-in audit command yet, use standard tools:

1. **Check for root login (Linux)**: `grep PermitRootLogin /etc/ssh/sshd_config`
2. **Check for password auth (Linux)**: `grep PasswordAuthentication /etc/ssh/sshd_config`
3. **Check Windows Defender**: `Get-MpComputerStatus` (PowerShell)

### 3) Check Ghost Status

Run the available Ghost commands:

1. `ghost status` - Check agent health and connections.
2. `ghost version` - Check current build version.

### 4) Determine Risk Tolerance (Profiles)

Ask the user to pick a target profile. This guides the remediation plan.

1. **Home/Workstation Balanced** (Recommended for most):
   - **Firewall**: On (Default Deny Inbound).
   - **SSH**: Allowed from LAN only.
   - **Updates**: Auto-install security patches.
   - **Services**: File sharing / Media streaming allowed.

2. **VPS Hardened** (For public servers):
   - **Firewall**: Strict (Deny everything except SSH/80/443).
   - **SSH**: Key-only auth (no passwords), Root login DISABLED.
   - **Services**: Minimal. All unused ports closed.

3. **Developer Convenience** (Lowest security):
   - **Firewall**: Permissive (Allow high ports 3000-9000).
   - **SSH**: Password auth allowed.
   - **Warning**: Not recommended for devices with public IPs.

### 5) Remediation Plan

If issues are found (e.g., SSH root login enabled, Firewall off), propose a fix.

**Example Fixes**:

- **Linux Firewall**: `sudo ufw enable`, `sudo ufw allow ssh`
- **Windows Firewall**: `netsh advfirewall set allprofiles state on`

### 5) Execution

Ask for confirmation before running any "fix" commands.

## Periodic Checks

You can schedule these checks using `ghost cron`.

Example:
`ghost cron add --name "daily-health" --schedule "0 9 * * *" --command "ghost status"`
