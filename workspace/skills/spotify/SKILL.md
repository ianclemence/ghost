---
name: spotify
description: Control Spotify playback, search tracks, manage playlists, and view what's playing. Invoke when user mentions "Spotify", "play music", "pause", "next track", "what's playing on Spotify", "search for a song", or "Spotify queue". Requires spotify-cli wrapper or dbus.
version: 1.1.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [spotify, music, playback, audio, playlist]
prerequisites:
  commands: []
---

# Spotify Control

Controls the Spotify desktop client via CLI wrapper or D-Bus.

## Quick Reference

| Task           | Command                          |
| -------------- | -------------------------------- |
| Play/Pause     | `spotify play` / `spotify pause` |
| Next           | `spotify next`                   |
| Previous       | `spotify prev`                   |
| Volume up      | `spotify vol up`                 |
| Volume down    | `spotify vol down`               |
| What's playing | `spotify status`                 |
| Search tracks  | `spotify search "query"`         |

## CLI Wrappers

Install a wrapper that exposes `spotify` command:

- **Windows**: [spotify-cli-windows](https://github.com/AleksandarDev/spotify-cli-windows)
- **Linux**: [spotify-cli-linux](https://github.com/pwittchen/spotify-cli-linux) or `pip install spotify-cli`
- **macOS**: [shpotify](https://github.com/hnarayanan/shpotify)

### Verify Installation

```bash
spotify --version
# or
spotify status
```

If no wrapper is available, fall back to D-Bus (Linux) or PowerShell (Windows).

## Playback Control

### Play / Pause

```bash
spotify play
spotify pause
```

### Next / Previous

```bash
spotify next
spotify prev
```

### Seek

```bash
# Forward 30s (if supported)
spotify fwd
# Back 30s
spotify back
```

## Current Track

```bash
spotify status
```

Returns current track name, artist, album, and play state.

### Parse what's playing (Linux D-Bus)

```bash
dbus-send --print-reply --dest=org.mpris.MediaPlayer2.spotify /org/mpris/MediaPlayer2 org.freedesktop.DBus.Properties.Get string:"org.mpris.MediaPlayer2.Player" string:"Metadata" | grep -E "(title|artist|album)" -A 1
```

## Search

```bash
spotify search "song name"
```

Returns track name, artist, and URI.

### Open a track/playlist

```bash
spotify play uri:spotify:track:TRACK_ID
spotify play uri:spotify:playlist:PLAYLIST_ID
spotify play uri:spotify:album:ALBUM_ID
```

To get a URI: right-click a track in the Spotify app → Share → Copy Spotify URI.

## Queue Management

```bash
# Add to queue (if wrapper supports)
spotify queue spotify:track:TRACK_ID
```

## Volume

```bash
spotify vol up
spotify vol down
spotify vol 50
```

## Linux D-Bus Fallback

When no CLI wrapper is installed, use D-Bus directly:

```bash
# Play/Pause
dbus-send --print-reply --dest=org.mpris.MediaPlayer2.spotify /org/mpris/MediaPlayer2 org.mpris.MediaPlayer2.Player.PlayPause

# Next
dbus-send --print-reply --dest=org.mpris.MediaPlayer2.spotify /org/mpris/MediaPlayer2 org.mpris.MediaPlayer2.Player.Next

# Previous
dbus-send --print-reply --dest=org.mpris.MediaPlayer2.spotify /org/mpris/MediaPlayer2 org.mpris.MediaPlayer2.Player.Previous
```

## Windows PowerShell Fallback

When no CLI wrapper is available, use Windows media keys via `nircmd` (must be installed):

```bash
# Play/Pause
nircmd.exe sendkeypress 0xB3

# Next
nircmd.exe sendkeypress 0xB0

# Previous
nircmd.exe sendkeypress 0xB1
```

Note: Media keys work only if Spotify window is in focus.

## Device Selection

To play on a specific device, use Spotify Connect. Open Spotify on the target device, then use the app to switch playback source. CLI wrappers generally cannot control which device receives playback.

## Limitations

- CLI tools control the local desktop client only
- Remote control requires Spotify Premium or Spotify Connect
- D-Bus interface is Linux-only
- PowerShell media key approach requires Spotify window in focus
