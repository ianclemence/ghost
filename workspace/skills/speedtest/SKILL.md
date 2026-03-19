---
name: speedtest
description: Measure internet connection speed (ping, download, upload). Invoke when user asks "check my internet speed", "run a speed test", "how fast is my connection", or "bandwidth test".
version: 1.0.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [network, speedtest, bandwidth, internet, connectivity]
prerequisites:
  commands: [speedtest-cli]
---

# Network Speed Monitor

## Cross-Platform Usage

Works on Windows, Linux, and Mac.

1.  **Check Speed**:
    - Command: `speedtest-cli --simple`
    - Goal: Get a quick summary of Ping, Download, and Upload speeds.
    - If the user wants a detailed image, use `speedtest-cli --share`.

2.  **Troubleshooting**:
    - If `speedtest-cli` is not found, install it automatically: `pip install speedtest-cli`.
    - If it fails, ensure network connectivity is active.

## Commands

```powershell
# Install
pip install speedtest-cli

# Run Simple Test
speedtest-cli --simple

# Run with Shareable Image link
speedtest-cli --share
```
