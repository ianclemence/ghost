---
name: "spotify"
description: "Controls Spotify playback. Invoke when user asks to play music, pause, skip, or change volume on Spotify."
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
