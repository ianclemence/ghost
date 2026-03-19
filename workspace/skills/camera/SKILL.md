---
name: camera
description: Capture photos from the webcam or connected camera. Invoke when user asks to "take a photo", "see what's on camera", "look at this", "capture an image", or "what do you see". Works on Linux (ffmpeg/v4l2) and Windows (ffmpeg/dshow).
version: 1.0.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [camera, photo, webcam, capture, vision]
prerequisites:
  commands: [ffmpeg]
---

# Camera

Captures images from the connected camera.

## Commands

### Windows (requires `ffmpeg`)

List devices first:

```powershell
ffmpeg -list_devices true -f dshow -i dummy
```

Take a snapshot (replace "Integrated Camera" with your device name):

```powershell
ffmpeg -f dshow -i video="Integrated Camera" -vframes 1 -q:v 2 snapshot.jpg
```

### Raspberry Pi (Native)

Using `libcamera`:

```bash
libcamera-still -o snapshot.jpg --immediate
```

Using `fswebcam` (USB Webcams):

```bash
fswebcam -r 1280x720 --no-banner snapshot.jpg
```

## Usage

After taking a photo, you can ask Ghost to analyze it using the vision capabilities.
