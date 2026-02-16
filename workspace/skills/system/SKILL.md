---
name: "system"
description: "Controls computer hardware. Invoke when user says 'volume up', 'mute', 'screen off', 'lock', 'shutdown', or 'speak'. Supports targeting specific devices (e.g., 'on my PC', 'on the laptop')."
---

# System Control

Controls hardware functions locally or on remote devices via SSH.

## Dynamic Targeting

Ghost can control any SSH-enabled device.

1.  **Add a Device**: "Remember that my gaming PC is at 192.168.1.50"
2.  **Control it**: "Turn up volume on my gaming PC"

**Prerequisites**:

- The target device must have **OpenSSH Server** running.
- The Pi (Ghost) must have its public key in the target's `authorized_keys`.
- The default SSH user is assumed to be the same as `REMOTE_PC_USER` in `.env`, or you can specify `user@host` in the memory.

## Commands

### Remote Control (Targeting a specific device)

When user specifies a target (e.g., "on PC", "on 192.168.1.50"):

- **Mute**: `ssh $TARGET_HOST "nircmd.exe mutesysvolume 2"`
- **Volume Up**: `ssh $TARGET_HOST "nircmd.exe changesysvolume 5000"`
- **Volume Down**: `ssh $TARGET_HOST "nircmd.exe changesysvolume -5000"`
- **Lock**: `ssh $TARGET_HOST "rundll32.exe user32.dll,LockWorkStation"`
- **Shutdown**: `ssh $TARGET_HOST "shutdown /s /t 0"`
- **Speak**: `ssh $TARGET_HOST "PowerShell -Command \"Add-Type -AssemblyName System.Speech; (New-Object System.Speech.Synthesis.SpeechSynthesizer).Speak('Hello from Pi')\""`

### Local Control (Default / No target specified)

When user says "Turn up volume" (implies local device):

#### Windows (Primary: nircmd)

Ensure `nircmd.exe` is in your PATH or in `C:\Tools\nircmd\nircmd.exe`.

- **Mute**: `cmd /c "where nircmd && nircmd mutesysvolume 2 || C:\Tools\nircmd\nircmd.exe mutesysvolume 2"`
- **Volume Up**: `cmd /c "where nircmd && nircmd changesysvolume 5000 || C:\Tools\nircmd\nircmd.exe changesysvolume 5000"`
- **Volume Down**: `cmd /c "where nircmd && nircmd changesysvolume -5000 || C:\Tools\nircmd\nircmd.exe changesysvolume -5000"`
- **Max Volume**: `cmd /c "where nircmd && nircmd setsysvolume 65535 || C:\Tools\nircmd\nircmd.exe setsysvolume 65535"`
- **Turn Off Monitor**: `cmd /c "where nircmd && nircmd monitor off || C:\Tools\nircmd\nircmd.exe monitor off"`
- **Lock Workstation**: `rundll32.exe user32.dll,LockWorkStation`
- **Shutdown**: `shutdown /s /t 0`
- **Restart**: `shutdown /r /t 0`
- **Screenshot**: `cmd /c "where nircmd && nircmd savescreenshot \"C:\temp\screenshot.png\" || C:\Tools\nircmd\nircmd.exe savescreenshot \"C:\temp\screenshot.png\""`
- **Speak (TTS)**: `PowerShell -Command "Add-Type -AssemblyName System.Speech; (New-Object System.Speech.Synthesis.SpeechSynthesizer).Speak('Hello Ghost')"`

#### Windows (Fallback: PowerShell)

If `nircmd` is missing, use this for volume:

- **Volume Up**: `PowerShell -Command "$obj = new-object -com wscript.shell; $obj.SendKeys([char]175)"`
- **Volume Down**: `PowerShell -Command "$obj = new-object -com wscript.shell; $obj.SendKeys([char]174)"`
- **Mute**: `PowerShell -Command "$obj = new-object -com wscript.shell; $obj.SendKeys([char]173)"`

#### Linux / Raspberry Pi

- **Volume Up**: `amixer set Master 5%+`
- **Volume Down**: `amixer set Master 5%-`
- **Mute**: `amixer set Master toggle`
- **Turn Off HDMI**: `vcgencmd display_power 0`
- **Turn On HDMI**: `vcgencmd display_power 1`
- **Shutdown**: `sudo shutdown -h now`
- **Restart**: `sudo reboot`
