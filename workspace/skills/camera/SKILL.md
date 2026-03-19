---
name: camera
description: Capture photos, video, and time-lapses from webcam or connected cameras. Invoke when user asks to "take a photo", "see what's on camera", "record video", "capture a time-lapse", or "what do you see". Works on Linux (ffmpeg/v4l2/libcamera) and Windows (ffmpeg/dshow).
version: 1.1.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [camera, photo, webcam, capture, video, timelapse, vision]
prerequisites:
  commands: [ffmpeg]
---

# Camera

Capture photos, video, and time-lapses from connected cameras.

## Quick Reference

| Task | Command |
|------|---------|
| List devices (Windows) | `ffmpeg -list_devices true -f dshow -i dummy` |
| Take photo (Windows) | `ffmpeg -f dshow -i video="Camera" -vframes 1 -q:v 2 out.jpg` |
| Take photo (Linux) | `fswebcam -r 1280x720 --no-banner out.jpg` |
| Record video (Linux) | `ffmpeg -f v4l2 -i /dev/video0 -t 30 out.mp4` |
| Time-lapse (Linux) | `ffmpeg -f v4l2 -i /dev/video0 -vf fps=1 -t 3600 out.mp4` |

## Find Camera Device

### Windows (dshow)

```powershell
ffmpeg -list_devices true -f dshow -i dummy
```

Look for "Integrated Camera" or "USB Camera" in the output.

### Linux (v4l2)

```bash
v4l2-ctl --list-devices
# or
ls /dev/video*
```

### Raspberry Pi (libcamera)

```bash
libcamera-still --list-cameras
```

## Capture Photo

### Windows (ffmpeg/dshow)

```powershell
ffmpeg -f dshow -i video="Integrated Camera" -vframes 1 -q:v 2 snapshot.jpg
```

`-q:v 2` sets JPEG quality (2=high, 23=low).

### Linux (fswebcam)

```bash
fswebcam -r 1280x720 --no-banner snapshot.jpg
```

Flags:
- `-r 1920x1080` — resolution
- `--no-banner` — remove timestamp overlay
- `-S 3` — skip 3 frames (avoids first-frame noise)

### Raspberry Pi (libcamera)

```bash
libcamera-still -o snapshot.jpg --immediate
libcamera-still -o snapshot.jpg -t 5000  # with 5s preview
```

## Record Video

### Linux (ffmpeg/v4l2)

```bash
ffmpeg -f v4l2 -i /dev/video0 -t 30 -c:v libx264 out.mp4
```

- `-t 30` — duration in seconds
- `-c:v libx264` — H.264 encoding
- `-s 1280x720` — resolution

### Windows (ffmpeg/dshow)

```powershell
ffmpeg -f dshow -i video="Integrated Camera" -t 30 -c:v libx264 out.mp4
```

## Time-Lapse

Capture one frame per second for N seconds:

```bash
ffmpeg -f v4l2 -i /dev/video0 -vf fps=1 -t 3600 -c:v libx264 timelapse.mp4
```

- `fps=1` — 1 frame per second
- `-t 3600` — record for 1 hour

For Raspberry Pi:

```bash
libcamera-vid -o timelapse.mp4 -t 3600000 --framerate 1
```

## Post-Capture

After capturing, feed the image to the vision model:

```
User: take a photo and describe what you see
→ Capture image → read_file tool on image → describe
```

## Multi-Camera Selection

If multiple cameras exist, select explicitly:

```powershell
# Windows: list shows "video=Camera 1" and "video=Camera 2"
ffmpeg -f dshow -i video="Camera 2" -vframes 1 out.jpg
```

```bash
# Linux: specify device
ffmpeg -f v4l2 -i /dev/video2 -vframes 1 out.jpg
```

## Troubleshooting

| Problem | Solution |
|---------|----------|
| "Cannot find device" | Try `ffmpeg -list_devices true` to find exact name |
| Black image | Camera may need time to initialize; add `-ss 0.5` input offset |
| Poor quality | Increase resolution (`-r 1920x1080`) or lower `-q:v` value |
| Audio not recording | Add `-f dshow -i audio="Microphone"` separate input |
| fswebcam not found | `sudo apt-get install fswebcam` |
| libcamera not found | `sudo apt-get install libcamera-tools` |
