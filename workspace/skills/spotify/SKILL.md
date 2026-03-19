---
name: spotify
description: Control Spotify playback on desktop client. Invoke ONLY when user explicitly says "Spotify" or asks to "play", "pause", "skip", "next track", or "what's playing". Requires spotify-cli wrapper (Windows: spotify-cli-windows, Linux: spotify-cli-linux).
version: 1.0.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [spotify, music, playback, audio]
prerequisites:
  commands: [spotify]
---

# Spotify Control

Controls the Spotify desktop client.

## Requirements

- **Windows**: [spotify-cli-windows](https://github.com/AleksandarDev/spotify-cli-windows) or similar wrapper.
- **Linux/Mac**: `spotify-cli-linux` or `shpotify`.

## Commands (Windows Example)

Using `spotify-cli` (example wrapper):

- **Play/Pause**: `spotify play` / `spotify pause`
- **Next Track**: `spotify next`
- **Previous Track**: `spotify prev`
- **Volume**: `spotify vol up` / `spotify vol down`
- **Status**: `spotify status`

## Alternative (PowerShell)

If no CLI tool is installed, you can use media keys via generic system control:

- **Play/Pause**: `[System.Windows.Forms.SendKeys]::SendWait("^{p}")` (depends on app focus)
- **Better**: Use `nircmd` media keys:
  - `nircmd.exe sendkeypress 0xB3` (Play/Pause)
  - `nircmd.exe sendkeypress 0xB0` (Next)
  - `nircmd.exe sendkeypress 0xB1` (Previous)
