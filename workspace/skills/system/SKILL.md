---
name: "system"
description: "Controls computer hardware. Invoke when user says 'volume up', 'mute', 'screen off', 'lock', 'shutdown', or 'speak'. Supports local (Pi) and remote (PC) control."
---

# System Control

Controls hardware functions locally or remotely via SSH.

## Configuration (Remote PC Control)

To control a Windows PC from your Raspberry Pi, you must set these environment variables in your `.env` file:

```bash
REMOTE_PC_USER=your_windows_username
REMOTE_PC_HOST=192.168.1.100  # Your PC's local IP
```

**Prerequisites**:

1.  Enable **OpenSSH Server** on Windows (Settings > Apps > Optional Features).
2.  Set up SSH keys so the Pi can connect without a password (run `ssh-copy-id user@host`).
3.  Ensure `nircmd.exe` is in the Windows PATH.

## Commands

### Remote Control (Targeting Windows PC)

When user says "Turn up **PC** volume" or "Lock **PC**":

- **Mute PC**: `ssh $REMOTE_PC_USER@$REMOTE_PC_HOST "nircmd.exe mutesysvolume 2"`
- **Volume Up PC**: `ssh $REMOTE_PC_USER@$REMOTE_PC_HOST "nircmd.exe changesysvolume 5000"`
- **Volume Down PC**: `ssh $REMOTE_PC_USER@$REMOTE_PC_HOST "nircmd.exe changesysvolume -5000"`
- **Lock PC**: `ssh $REMOTE_PC_USER@$REMOTE_PC_HOST "rundll32.exe user32.dll,LockWorkStation"`
- **Shutdown PC**: `ssh $REMOTE_PC_USER@$REMOTE_PC_HOST "shutdown /s /t 0"`
- **Speak on PC**: `ssh $REMOTE_PC_USER@$REMOTE_PC_HOST "PowerShell -Command \"Add-Type -AssemblyName System.Speech; (New-Object System.Speech.Synthesis.SpeechSynthesizer).Speak('Hello from Pi')\""`

### Local Control (Targeting the device running Ghost)

When user says "Turn up **local** volume" or just "volume up" (default):

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
