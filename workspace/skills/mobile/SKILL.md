---
name: mobile
description: Control an Android device via ADB — open apps, tap, swipe, type, screenshot, install APKs. Invoke when user asks to "open app on my phone", "take a screenshot", "tap the button", "type on my phone", or "install this APK". Requires Android device with USB debugging enabled and adb installed.
version: 1.0.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [mobile, android, adb, automation, phone]
prerequisites:
  commands: [adb, python]
---

# Mobile Control (Android)

Controls an Android device via ADB (Android Debug Bridge).

## Requirements

- **Tool**: `adb` (Android Debug Bridge)
- **Python**: Python 3 installed
- **Device**: Android phone with "USB Debugging" enabled, connected via USB or Wireless ADB.

## Setup

1.  **Install ADB**:
    - **Windows**: `winget install Google.PlatformTools`
    - **Linux/Pi**: `sudo apt-get install adb`
2.  **Connect Phone**:
    - Enable Developer Options -> USB Debugging on your phone.
    - Plug in via USB.
    - Run `adb devices` and accept the popup on your phone.

## Commands

### 1. Start the Bridge (Background)

You must start the RPC bridge before sending commands. This bridge translates JSON-RPC to ADB commands.

```bash
# Windows
start /B python workspace\skills\mobile\scripts\android_bridge.py

# Linux/Pi
nohup python3 workspace/skills/mobile/scripts/android_bridge.py > /dev/null 2>&1 &
```

### 2. Send Commands

Use the `rpc.py` client to control the phone.

#### Open App

```bash
python workspace\skills\mobile\scripts\rpc.py open-app com.instagram.android
python workspace\skills\mobile\scripts\rpc.py open-app com.google.android.youtube
```

#### Tap Screen

Tap at specific coordinates (x, y).

```bash
python workspace\skills\mobile\scripts\rpc.py tap --x 500 --y 1000
```

#### Type Text

Types text into the currently focused field.

```bash
python workspace\skills\mobile\scripts\rpc.py enter-text --text "Hello Ghost"
```

#### Swipe

Swipe from (x1, y1) to (x2, y2).

```bash
python workspace\skills\mobile\scripts\rpc.py swipe --x1 500 --y1 1500 --x2 500 --y2 500 --duration 0.5
```

#### Screenshot

Takes a screenshot and saves it to a file.

```bash
python workspace\skills\mobile\scripts\rpc.py get-screen-image --print-metadata
```

#### Get UI Tree

Dumps the current screen hierarchy (XML) to understand what is on screen.

```bash
python workspace\skills\mobile\scripts\rpc.py get-tree
```

## Common Package Names

- **Settings**: `com.android.settings`
- **Chrome**: `com.android.chrome`
- **YouTube**: `com.google.android.youtube`
- **Maps**: `com.google.android.apps.maps`
- **Spotify**: `com.spotify.music`
