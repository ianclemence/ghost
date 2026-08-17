# Ghost OS Roadmap

## Overview

This document outlines the progression from Ghost as a Go binary to a full operating system. The approach is pragmatic: build a purpose-built Linux distribution on top of stock Linux, similar to Raspberry Pi OS or Home Assistant OS.

---

## Architecture Progression

| Stage | What It Is | Effort | Time |
|-------|-----------|--------|------|
| **Ghost Binary** (current) | Go binary + config files on any Linux | Done | — |
| **Ghost Ready** | Ghost + dependencies pre-installed, auto-starts on boot | Low | 2-4 weeks |
| **Ghost Distribution** | Custom Debian image with Ghost pre-baked, read-only root, OTA updates | Medium | 2-3 months |
| **Ghost OS** | Full system: custom boot flow, dedicated UI layer, hardware abstraction, app ecosystem | High | 6-12 months |

---

## Stage 1: Ghost Ready

**Goal:** Make Ghost work out of the box — plug it in, it works.

### Current vs Ready State

| Current State | Ready State |
|--------------|-----------------|
| User clones repo, runs `setup.sh` | Image is pre-flashed. First boot runs wizard. |
| `.env` and `config.json` edited by hand | Web-based onboarding at `ghost.local` |
| Ollama installed separately | Ollama bundled, models pre-cached or downloaded on first boot |
| Ghost started manually or via systemd | Ghost starts automatically, restarts on crash |
| Updates via `git pull` | OTA updates via secure channel |
| Storage on SD card | Managed storage with automatic backups |

### First Boot Wizard

When someone plugs in their Pi:

1. Pi boots, creates WiFi hotspot `Ghost-Setup-XXXX`
2. User connects phone/laptop, gets captive portal
3. Wizard steps:
   - Set WiFi credentials
   - Create admin password
   - Pair Telegram/WhatsApp (QR code scan)
   - Download default models (Qwen 3.5:0.8b, etc.)
   - Done — redirect to `ghost.local` dashboard

### Technical Implementation

```
/ghost/
  /bin/ghost              # Existing binary
  /bin/ghost-web          # Web console: setup wizard + admin dashboard (Go + embedded web server)
  /bin/ghost-updater      # OTA update daemon
  /etc/ghost/             # Configs (read-only base + writable overlay)
  /var/ghost/             # Runtime data, SQLite, models, skills
  /lib/systemd/system/ghost.service  # Auto-start
```

### Key Components

- **systemd** for service management
- **Overlay filesystem** — base OS is read-only, user data is writable
- **mDNS/Bonjour** — `ghost.local` works on the network
- **WiFi hotspot + captive portal** — `hostapd` + `dnsmasq` + Go HTTP server

### Implementation Tasks

1. **Harden systemd service**
   - Auto-start on boot
   - Health check: if Ghost crashes 3 times in 5 minutes, enter recovery mode
   - Recovery mode: web UI at `ghost.local:8766` showing logs, option to reset config

2. **First-boot wizard skeleton**
   - Go HTTP server on port 80
   - WiFi scan + connect
   - Set admin password
   - Telegram bot token input
   - Store config in `/var/ghost/config.json`

---

## Stage 2: Ghost Distribution

**Goal:** Bake the entire stack into a flashable `.img` file.

### Image Contents

```
Base: Debian 12 (Bookworm) ARM64, minimal
  ├── Linux kernel 6.6 (Raspberry Pi Foundation)
  ├── Boot firmware (Pi 5 specific)
  ├── Ghost Appliance Layer
  │     ├── Ghost binary
  │     ├── First-boot wizard
  │     ├── Ollama (pre-installed)
  │     ├── Web dashboard (embedded in Ghost or separate)
  │     └── Update daemon
  ├── Container runtime (optional, for multi-agent)
  └── Read-only root with overlay
```

### Build Pipeline

```
ghost-os-build/
  ├── Dockerfile           # Build environment
  ├── build.sh             # Main script
  ├── stage0/              # Base Debian bootstrap
  ├── stage1/              # Pi firmware + kernel
  ├── stage2/              # Ghost appliance install
  └── stage3/              # Finalization, compression
```

Output: `ghost-os-v1.0.0-pi5.img.xz` — ~2GB, flashable with Raspberry Pi Imager or BalenaEtcher.

### OTA Updates

Image-based updates using A/B partition scheme:

- **A/B partitions**: Two root partitions. Update downloads to inactive partition, reboot switches.
- **Image deltas**: Only download changed blocks (like Docker layers). Use `rauc` or `mender`.
- **Rollback**: If Ghost doesn't start in 2 minutes, automatically rollback.

### Implementation Tasks

1. **Study existing tools:**
   - **pi-gen** (Raspberry Pi's official image builder)
   - **Home Assistant OS** build system
   - **Umbrel** — Bitcoin node OS, similar architecture

2. **Create build script:**
   - Bootstrap minimal Debian ARM64 in a container
   - Install dependencies (Ollama, Go runtime, systemd services)
   - Install Ghost binary + first-boot wizard
   - Configure overlay filesystem
   - Generate `.img` file

3. **Test cycle:**
   - Flash to SD, boot Pi 5
   - Run through first-boot wizard
   - Verify Ghost starts, Telegram responds
   - Verify OTA update mechanism

---

## Stage 3: Ghost OS

**Goal:** Full operating system with multi-agent runtime and hardware abstraction.

### Architecture

```
┌─────────────────────────────────────────┐
│           Ghost OS Kernel               │
│  (not Linux kernel — the Ghost runtime) │
├─────────────────────────────────────────┤
│  Agent 1  │  Agent 2  │  Agent N      │
│  (Home)   │  (Work)   │  (Research)   │
│  SQLite   │  SQLite   │  SQLite        │
│  HNSW     │  HNSW     │  HNSW          │
├─────────────────────────────────────────┤
│  Shared Services: Router, Memory Bus,   │
│  Tool Registry, Channel Manager         │
├─────────────────────────────────────────┤
│  Hardware Abstraction: GPIO, I2C, SPI,  │
│  Audio, Camera, Network, GPU            │
├─────────────────────────────────────────┤
│  Linux Kernel 6.6 + Device Drivers    │
└─────────────────────────────────────────┘
```

Each "agent" is an isolated Ghost instance with its own memory, skills, and channels — sharing hardware efficiently.

### UI Modes

| Mode | Use Case |
|------|----------|
| **Headless** (current) | No monitor. All interaction via phone/telegram. |
| **Kiosk** | Touchscreen on the device. Simple chat UI, settings, status. |
| **Desktop** | Full monitor + keyboard. Terminal dashboard + web browser. |

### Kiosk UI Stack

- Wayland compositor (minimal, no full desktop)
- WebKit or Flutter embedder
- Ghost serves the UI via local HTTP
- Touch-optimized, dark mode, always-on

### Implementation Tasks

1. **Hardware abstraction layer**
   - Detect Pi 5 vs RK1 vs x86
   - Adjust default models based on RAM (8GB → 3B models, 16GB → 7B, etc.)
   - GPIO access for skills (camera, sensors, etc.)

2. **Multi-agent isolation**
   - Each agent gets isolated memory, skills, channels
   - Shared hardware abstraction layer
   - Resource management and scheduling

3. **Kiosk mode**
   - Wayland compositor (cage or similar)
   - Web-based UI served by Ghost
   - Touch-optimized interface

---

## Naming Convention

| What You Call It | What It Actually Is | When to Use |
|-----------------|---------------------|-------------|
| **Ghost** | The Go binary + config | Now, always |
| **Ghost Appliance** | Pre-installed, auto-starting system | Stage 1 |
| **Ghost OS** | Custom Linux distribution | Stage 2, when you have an image |
| **Ghost Platform** | Multi-agent runtime + hardware abstraction | Stage 3, when agents are isolated |

---

## Learning Resources

| Topic | Resource |
|-------|----------|
| **Linux systemd** | `man systemd.service`, Arch Wiki systemd page |
| **Pi image building** | github.com/RPi-Distro/pi-gen |
| **Home Assistant OS** | github.com/home-assistant/operating-system |
| **A/B updates** | rauc.io (Robust Auto-Update Controller) |
| **Overlay filesystems** | `overlayfs` kernel docs |
| **Embedded web servers in Go** | `net/http` + `html/template` |
| **Wayland kiosk** | `cage` compositor (github.com/cage-kiosk/cage) |

---

## Immediate Next Steps (This Week)

1. **Make Ghost start on boot reliably**
   - Harden existing systemd service
   - Add health check with recovery mode

2. **First-boot wizard skeleton**
   - Go HTTP server on port 80
   - WiFi scan + connect
   - Set admin password
   - Telegram bot token input

---

## 2-4 Week Goals

1. **Image Builder**
   - Study pi-gen, Home Assistant OS, Umbrel build systems
   - Create build script for minimal Debian ARM64
   - Install Ghost appliance layer
   - Generate test image

2. **Testing**
   - Flash to SD, boot Pi 5
   - Run first-boot wizard
   - Verify Ghost starts and responds

---

## Month 2-3 Goals

1. **Web Dashboard Improvements**
   - System status: CPU temp, RAM, disk, model load
   - Memory browser: search conversations, curated memory
   - Skill manager: install/remove from web UI
   - Model manager: download/delete Ollama models

2. **Hardware Abstraction**
   - Detect hardware platform
   - Adjust defaults based on RAM
   - GPIO access for skills

3. **Release v1.0**
   - Flashable image on website
   - Documentation: "Flash, plug in, configure in 5 minutes"
   - Community support channel
