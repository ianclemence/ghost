# Network Speed Monitor Skill

## Description

This skill allows the agent to check the current internet connection speed (upload/download/ping) using the `speedtest-cli` tool.

## Dependencies

- **Python**: Must be installed.
- **speedtest-cli**: Install via `pip install speedtest-cli`.

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
