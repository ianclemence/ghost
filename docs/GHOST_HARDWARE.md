# Ghost Sovereign Hardware Reference

Ghost is designed to run locally with no cloud dependency while providing advanced reasoning, coding, vision, and voice. This document captures hardware recommendations for building a sovereign Ghost device — from small “always-on” setups to powerful workstations — and explains why each specification matters.

## Capability Tiers

| Capability               | Sovereign Starter (Always‑On Voice + 1–7B Models)                                               | Sovereign Pro (7–13B, Faster Reasoning, Vision)                                     | Sovereign Ultra (20–34B+, Deep Reasoning, Multimodal)                 |
| ------------------------ | ----------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------- | --------------------------------------------------------------------- |
| CPU                      | Raspberry Pi 5 (8GB) or x86 mini‑PC (Intel N100 / Ryzen 5700U+)                                 | 12–16 core x86 (Ryzen 9 / Intel i7/i9)                                              | 24–32 core workstation/server (Threadripper / dual Xeon)              |
| RAM                      | 16–32 GB recommended (Pi has 8 GB; x86 preferred)                                               | 64–128 GB                                                                           | 128–256 GB                                                            |
| Storage                  | 1 TB NVMe SSD (OS + workspace)                                                                  | 2 TB NVMe (OS/workspace) + 2–4 TB SSD (media/RAG)                                   | 4–8 TB NVMe + 8–16 TB SSD/HDD (archives, embeddings)                  |
| Accelerator (AI)         | Edge TPU (Coral USB) or Intel VPU/NPU; optional Jetson Orin Nano/NX                             | Single NVIDIA GPU 20–24 GB VRAM (RTX 3090/4090/RTX 4000 Ada)                        | 48 GB+ VRAM (RTX 6000 Ada/A6000) or multi‑GPU/NVLink                  |
| Audio I/O                | Far‑field mic array (ReSpeaker 4/6/8‑mic or miniDSP UMA‑16), small amp + nearfield speaker, AEC | Beamforming array, USB audio interface (clean capture), active monitors, robust AEC | Room‑grade beamforming array, dedicated DSP, multi‑zone audio         |
| Vision                   | UVC 1080p camera                                                                                | 4K camera (IMX sensor), optional secondary camera                                   | Multi‑camera setup, depth sensor, high‑quality lenses                 |
| Networking               | Gigabit Ethernet, Wi‑Fi 6/6E                                                                    | 2.5 GbE, Wi‑Fi 6/6E, Bluetooth                                                      | 10 GbE option, Wi‑Fi 6E, BLE mesh for satellites                      |
| Power & UPS              | Pi PSU or 60–120 W PSU, small UPS for graceful shutdown                                         | 750–1000 W PSU, line‑interactive UPS                                                | 1–1.2 kW PSU, rack UPS (online/double conversion for critical uptime) |
| Thermal / Noise          | Quiet case, intake/exhaust, basic cooling                                                       | Large, quiet case, dust filtering, separate intake/exhaust                          | Server‑grade cooling, acoustic treatment or remote placement          |
| OS                       | Linux (Debian/Ubuntu), minimal services, firewall                                               | Linux (Debian/Ubuntu), hardened, encrypted storage                                  | Linux (Debian/Ubuntu), secure boot/TPM, full disk encryption          |
| Typical LLM size         | 1–3B fast; 7B usable (quantized)                                                                | 7–13B comfortable; 20B borderline                                                   | 20–34B usable; 70B only with multi‑GPU or massive CPU RAM (slow)      |
| Context window           | ~4k tokens with responsive UX                                                                   | 8k–16k tokens (faster prefill)                                                      | 16k–32k tokens (heavy prefill, better with GPU)                       |
| STT/TTS latency          | <300 ms TTF with local models and AEC                                                           | <200 ms TTF typical                                                                 | <150 ms TTF (dedicated DSP/accelerators)                              |
| Chat latency (streaming) | 0.6–2.0 s for short replies on CPU                                                              | <0.5 s typical with GPU                                                             | Near real‑time; heavy tool chains 2–4 s                               |

## Why These Specs Matter

- CPU & Threads: Prefill (processing the entire prompt) is the dominant cost for local LLMs. More cores/threads reduce prefill time significantly, especially for large system prompts and histories.
- RAM: Larger contexts, multiple concurrent sessions, and vector databases benefit from ample RAM to avoid swapping (which stalls token generation).
- Storage: Fast NVMe enables snappy RAG indexing, quick document search, and smooth media pipelines. Separate disks for OS/workspace vs media help I/O isolation.
- GPU/NPU: GPUs dramatically increase tokens/sec and reduce prefill time for 7–34B models. NPUs/VPUs/TPUs accelerate STT/TTS and basic vision on low‑power devices.
- Audio: Far‑field mic arrays with beamforming and acoustic echo cancellation (AEC) are essential for reliable wake‑word detection and hands‑free UX.
- Vision: Quality sensors (IMX) and optics matter for consistent low‑light performance; multi‑camera setups enable richer multimodal context.
- Networking: Wired first (Gigabit/2.5GbE/10GbE) for predictable latency; Wi‑Fi 6/6E for flexibility; BLE for peripherals. A small UPS prevents corruption and preserves state.
- Thermal & Noise: Sustained inference requires stable thermals. Quiet cases and filtered airflow maintain performance without distracting noise.

## Voice & UX Pipeline (Local‑Only)

- Wake Word: Lightweight engine (Porcupine/Vosk variants) running continuously with minimal CPU/NPU footprint; tuned to suppress false positives.
- STT: Local streaming STT accelerated by VPU/NPU when available; aim for <300 ms time‑to‑first‑token (TTF).
- AEC & Beamforming: Hardware or DSP‑backed AEC is critical to avoid feedback during TTS playback and to maintain high wake‑word accuracy.
- TTS: Local neural TTS (VITS/Coqui or similar) optimized for low latency; consider pre‑caching common prompts.
- UX Signals: LED indicators for “actively listening” or “speaking”; hardware mic mute and camera shutter for sovereignty and trust.

## Model Selection & Quantization

- Fit to VRAM/RAM: Approximate 4‑bit (int4) VRAM usage:
  - 7B: ~5–8 GB VRAM
  - 13B: ~10–16 GB VRAM
  - 20B: ~16–24 GB VRAM
  - 34B: ~28–40 GB VRAM (varies by architecture)
- CPU‑Only (Starter): Prefer 1–3B or 7B quantized models for conversational UX; stream tokens to reduce perceived latency.
- GPU (Pro/Ultra): 7–13B models feel natural with fast streaming; 20–34B provide stronger reasoning but require robust cooling and power.
- Thinking Trace: Useful for auditability and tool orchestration but adds tokens and latency. Keep off by default on small hardware; enable per‑task when needed.

## RAG & Memory

- Embeddings: Use smaller, fast embedding models for quick indexing; batch ingest offline.
- Vector DB: HNSW with tuned parameters (m/efConstruction/efSearch) to balance recall and performance; store on NVMe for high IOPS.
- Curation: Separate “curated memory” (long‑term truths) from raw retrieval to keep prompts compact and prefill costs low.

## Security & Sovereignty

- Local‑Only: No cloud calls for inference, memory, or RAG by default.
- Disk Encryption: LUKS for workspace/media disks; encrypt backups.
- Network Isolation: Firewall defaults, private LAN; optional VLANs for cameras/mics.
- Hardware Trust: Camera/mic kill switches; LEDs for recording state; minimize exposed services.
- Power Integrity: UPS for graceful shutdown, log preservation, and state consistency.

## Form Factor Options

- Desktop Unit: Mini‑PC + mic array + compact active speaker; quiet cooling; visible UX LEDs.
- Hub + Satellites: Central sovereign server (GPU workstation) with satellite mic/camera nodes over LAN — quieter living spaces and easier thermal management.
- Portable Sovereign: High‑end Linux laptop (32–64 GB RAM, RTX 4080/4090 mobile) + USB audio interface; good cooling is essential.

## Mobile App Control & Configuration

Ghost hardware is operated through the Ghost mobile app to keep management simple and secure while preserving local sovereignty:

- Control Surface: The app provides a unified UI to enable/disable communication channels (Telegram, Slack, LINE, email, etc.), manage provider selections (Kimi/Moonshot, Anthropic, OpenAI, Groq, Zhipu, Ollama), and set model routing/fallbacks.
- Secrets & Provisioning: API keys and tokens are stored locally via the Ghost API; hardware uses a `.env` file for secrets and `config/config.json` for architecture (models, routing, channels). The app writes updates through the local API with authentication.
- API & Security: A single local API port (default 8766) with a BRIDGE_SECRET controls access. Pairing requires the secret; keep it strong and never expose the port publicly without a firewall.
- Runtime Changes: Switching models, toggling channels, and updating keys is hot‑reload capable for most providers; the app surfaces status and diagnostics without leaking content.
- Network Assumptions: Prefer local LAN (wired or Wi‑Fi 6/6E). Remote control is possible via secure tunnels, but the recommended mode is local‑only for sovereignty.

Best Practices:

- Keep `.env` restricted (file permissions) and back up encrypted. Use the app to rotate keys and revoke providers cleanly.
- Manage routing/fallbacks to ensure local models (Ollama) are available when cloud providers fail or when you choose full offline operation.
- Use channel allowlists to control who can trigger the device across messaging platforms; review logs in the app’s diagnostics view.

## Upgrade Path

1. Start: Pi 5 + Coral TPU + quality mic array; NVMe over USB if possible; wired Ethernet.
2. Step‑Up: x86 mini‑PC (32–64 GB RAM, NVMe); retain TPU/VPU for STT/TTS/vision.
3. Pro: Add a single 24 GB VRAM NVIDIA GPU; upgrade PSU/cooling; configure streaming and compact prompts.
4. Ultra: Move to workstation with 48 GB+ VRAM or multi‑GPU/NVLink; expand storage; increase RAM for large contexts and multiple sessions.

## Example BOMs (Indicative)

- Starter (Always‑On Voice): Pi 5 (8 GB), 1 TB NVMe (USB enclosure), ReSpeaker 6‑mic array, Coral USB, compact amp + speaker, small UPS, wired Ethernet.
- Pro (Sovereign Hub): Ryzen 9, 64–128 GB RAM, 2 TB NVMe + 4 TB SSD, RTX 4090 (24 GB VRAM), UMA‑16 mic array, USB audio interface, 2.5 GbE, line‑interactive UPS, quiet case.
- Ultra (Lab‑Grade): Threadripper 7960X, 128–256 GB RAM, 4 TB NVMe + 8–16 TB SSD/HDD, RTX 6000 Ada (48 GB VRAM) or multi‑GPU, beamforming array + DSP, 10 GbE, rack UPS, server chassis in closet or rack.

## Practical Performance Tips

- Stream tokens to reduce perceived latency, especially on CPU‑only devices.
- Keep prompts compact; large system prompts and long histories multiply prefill cost.
- Disable “thinking” by default on small hardware; enable per‑task as needed for auditability.
- Warm the model (send a short prompt) after boot so weights are resident, reducing first‑response time.
- Ensure performance CPU governor and robust cooling to avoid throttling.

---

This reference aims to help you choose the right hardware for a Ghost device that remains private, responsive, and capable — fully sovereign and cloud‑independent. For a tailored bill of materials, share your constraints (budget, size, noise tolerance, expected workloads), and we’ll derive a precise configuration.

## Cost & Pricing

The following ranges reflect typical 2026 market prices for new parts, with notes on used components where common. Final pricing varies by region, availability, and warranty/support level.

### Starter (Always‑On Voice + 1–7B Models)

- Bill of Materials (BOM): $350–$700
  - Pi 5 (8 GB) or x86 mini‑PC: $100–$400
  - 1 TB NVMe + USB enclosure: $65–$80
  - Far‑field mic array: $60–$200
  - Coral TPU / Intel VPU (optional): $100–$150
  - Compact amp + speaker: $50–$100
  - Small UPS: $60–$120
  - Case/PSU/cooling/cables: $50–$100
- Suggested Retail: $699–$999
  - Includes assembly, OS provisioning, Ghost configuration, QA burn‑in, 1–2 year warranty, and support margin.

### Pro (7–13B Models, Faster Reasoning, Vision)

- BOM (new GPU): $3,000–$5,000
  - CPU/Motherboard/Case/Cooling: $700–$1,100
  - RAM 64–128 GB: $150–$450
  - Storage (2 TB NVMe + 4 TB SSD): $280–$430
  - GPU 24 GB VRAM (RTX 3090/4090/RTX 4000 Ada): $1,500–$2,500
  - PSU 850–1000 W: $120–$180
  - Audio I/O + mic array: $150–$300
  - UPS: $150–$300
- BOM (used GPU path): $2,300–$3,800 (e.g., used RTX 3090 $700–$1,000)
- Suggested Retail: $3,999–$6,999
  - Reflects quiet thermal engineering, hardware integration, warranty, device‑level support, and margin.

### Ultra (20–34B+, Deep Reasoning, Multimodal)

- BOM (new 48 GB GPU): $12,000–$20,000
  - Threadripper‑class CPU/Motherboard/Chassis: $1,400–$2,700
  - RAM 128–256 GB: $350–$1,200
  - Storage (4–8 TB NVMe + 8–16 TB SSD/HDD): $1,000–$2,000
  - GPU 48 GB VRAM (RTX 6000 Ada): $7,000–$8,000 (new)
  - PSU 1–1.2 kW: $200–$300
  - Audio/DSP, multi‑camera, UPS: $900–$2,000
- BOM (used A6000 single GPU): $6,000–$12,000
- Suggested Retail: $12,999–$24,999
  - Multi‑GPU or advanced sensor arrays can exceed $25k; add margin for rack mounting and pro support SLAs.

### Assumptions & Notes

- New‑parts baseline; used enterprise GPUs reduce BOM but affect warranty and supply predictability.
- Pricing includes professional assembly, QA, OS provisioning, Ghost configuration, and reasonable support/warranty margin (typically 20–40% over BOM + labor/overheads).
- Costs exclude taxes, shipping, and optional accessories (monitors, cameras beyond baseline, specialty microphones).
- Sovereignty features (UPS, encryption, kill switches, secure pairing) are part of the baseline value proposition and reflected in pricing.
