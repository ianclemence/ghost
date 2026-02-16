---
name: "system"
description: "Controls system volume, power, and display. Invoke when user asks to change volume, mute, turn off screen, lock computer, or shutdown/restart."
---

# System Control

Controls the host machine's hardware functions.

## Commands

### Windows (requires `nircmd`)

Ensure [nircmd](https://www.nirsoft.net/utils/nircmd.html) is in your PATH.

- **Mute**: `nircmd.exe mutesysvolume 2` (toggles mute)
- **Volume Up**: `nircmd.exe changesysvolume 5000`
- **Volume Down**: `nircmd.exe changesysvolume -5000`
- **Max Volume**: `nircmd.exe setsysvolume 65535`
- **Turn Off Monitor**: `nircmd.exe monitor off`
- **Lock Workstation**: `rundll32.exe user32.dll,LockWorkStation`
- **Shutdown**: `shutdown /s /t 0`
- **Restart**: `shutdown /r /t 0`
- **Screenshot**: `nircmd.exe savescreenshot "C:\temp\screenshot.png"`
- **Speak (TTS)**: `PowerShell -Command "Add-Type -AssemblyName System.Speech; (New-Object System.Speech.Synthesis.SpeechSynthesizer).Speak('Hello Ghost')"`

### Linux / Raspberry Pi

- **Volume Up**: `amixer set Master 5%+`
- **Volume Down**: `amixer set Master 5%-`
- **Mute**: `amixer set Master toggle`
- **Turn Off HDMI**: `vcgencmd display_power 0`
- **Turn On HDMI**: `vcgencmd display_power 1`
- **Shutdown**: `sudo shutdown -h now`
- **Restart**: `sudo reboot`
