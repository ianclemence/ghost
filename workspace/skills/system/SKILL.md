---
name: "system"
description: "Controls computer hardware. Invoke when user says 'volume up', 'mute', 'screen off', 'lock', 'shutdown', or 'speak'. PREFER THIS over Spotify for general volume."
---

# System Control

Controls the host machine's hardware functions.

## Commands

### Windows (Primary: nircmd)

Ensure `nircmd.exe` is in your PATH. If not found, these commands will fail.

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

### Windows (Fallback: PowerShell)

If `nircmd` is missing, use this for volume:

- **Volume Up**: `PowerShell -Command "$obj = new-object -com wscript.shell; $obj.SendKeys([char]175)"`
- **Volume Down**: `PowerShell -Command "$obj = new-object -com wscript.shell; $obj.SendKeys([char]174)"`
- **Mute**: `PowerShell -Command "$obj = new-object -com wscript.shell; $obj.SendKeys([char]173)"`

### Linux / Raspberry Pi

- **Volume Up**: `amixer set Master 5%+`
- **Volume Down**: `amixer set Master 5%-`
- **Mute**: `amixer set Master toggle`
- **Turn Off HDMI**: `vcgencmd display_power 0`
- **Turn On HDMI**: `vcgencmd display_power 1`
- **Shutdown**: `sudo shutdown -h now`
- **Restart**: `sudo reboot`
