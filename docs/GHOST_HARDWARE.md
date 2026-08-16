# Ghost Hardware Reference

Ghost is designed as a **personal AI system** that runs across layered computation — combining real-time local processing, on-device reasoning, and optional cloud cognition.

This document defines the hardware required to build a Ghost system that is **responsive, private, and capable**, from compact always-on devices to full AI workstations.

---

## 🧠 Capability Tiers

| Capability | Starter (Edge Node) | Pro (Local Intelligence Hub) | Ultra (AI Workstation) |
|----------|------------------------------|----------------------------------------|----------------------------------|
| CPU | RK1 (16GB) or Pi CM5 / x86 mini-PC | 12–16 core x86 (Ryzen 9 / i7/i9) | 24–32 core workstation |
| RAM | 16 GB | 64–128 GB | 128–256 GB |
| Storage | 1 TB NVMe | 2 TB NVMe + 2–4 TB SSD | 4–8 TB NVMe + 8–16 TB SSD/HDD |
| Accelerator | RK1 NPU + GPU (preferred) / Coral TPU fallback | NVIDIA GPU (20–24 GB VRAM) + optional NPU | 48 GB+ VRAM or multi-GPU |
| Local AI Role | Reflex + light reasoning (1B–7B) | Full local reasoning (7–13B) | Deep reasoning (20B–34B+) |
| Networking | Gigabit Ethernet | 2.5 GbE | 10 GbE |
| Power | 60–120W | 750–1000W | 1kW+ |
| Typical Role | Always-on assistant node | Central intelligence hub | Advanced AI workstation |

---

## ⚔️ Device Positioning

### 🥇 RK1 (Default Edge Platform)

RK1 provides the best balance of performance, cost, and on-device AI capability.

- Integrated **NPU** for AI acceleration
- Strong multi-core CPU for concurrent workloads
- GPU available for future inference acceleration
- Lower cost compared to comparable systems

Enables:
- Real-time intent classification
- Local embedding generation
- Continuous background processing
- Efficient hybrid AI routing

---

### 🥈 Raspberry Pi CM5

- Stable ecosystem
- Strong community support

Limitations:
- No meaningful AI acceleration
- Lower compute performance
- Limited scalability for advanced workloads

Best suited for:
- Basic deployments
- API-heavy usage
- Simpler automation workflows

---

## 🧠 System Architecture Requirements

Ghost operates as a **layered intelligence system**, not a single model runtime.

### Three Core Layers

```

⚡ Reflex Layer   → instant decisions
🧠 Local Layer   → fast reasoning
☁️ Cloud Layer   → deep cognition

```

---

## ⚡ Reflex Layer (Edge Processing)

Runs continuously with minimal latency.

### Responsibilities:
- Intent classification
- Command routing
- Wake-word detection
- Embedding generation
- Lightweight summarization

### Characteristics:
- Sub-50ms response time
- Always-on
- Low power usage

### Hardware Dependency:
- Strongly benefits from NPU acceleration (RK1)

---

## 🧠 Local LLM Layer

Handles most day-to-day intelligence.

### Responsibilities:
- RAG queries
- Memory summarization
- Tool execution planning
- Offline interaction

### Model Expectations:

| Tier | Capability |
|------|-----------|
| Starter | 1B–3B fast, 7B usable |
| Pro | 7B–13B |
| Ultra | 20B–34B |

---

## ☁️ Cloud Layer

Used selectively for:

- Deep reasoning
- Complex planning
- Coding
- Long-context tasks

Not required for:
- Simple queries
- Memory retrieval
- Command execution

---

## 🧠 Memory Architecture

Ghost memory is structured for efficiency and scalability.

### Storage Layers

```

Hot Memory   → RAM (active context)
Warm Memory  → SQLite
Cold Memory  → summarized long-term storage

```

### Key Principles

- Local embedding generation (CPU/NPU)
- Vector search via HNSW
- Separation of raw logs vs curated memory
- Minimized prompt size for performance

---

## ⚡ Performance Targets

| Function | Target |
|--------|-------|
| Intent detection | <50 ms |
| STT (time-to-first-token) | <300 ms |
| Local response | <500 ms |
| Cloud fallback | 1–2 s |

---

## 🎙 Voice & UX Pipeline

### Components

- Wake-word detection (always-on)
- Streaming STT (local)
- Acoustic Echo Cancellation (AEC)
- Neural TTS (low latency)

### Hardware Notes

- Far-field mic arrays are critical
- AEC must be hardware or DSP-backed
- NPU improves continuous listening efficiency

---

## 🤖 Internal Agent Roles

Ghost operates as a multi-agent system:

| Agent | Role |
|------|------|
| Reflex Agent | classify + route |
| Executor | local reasoning + tool use |
| Planner | complex reasoning |
| Memory Agent | retrieval + summarization |

---

## 🔌 System Flow

```

User Input
↓
Reflex Layer
↓
Routing Decision
├─ Instant Response
├─ Local LLM
└─ Cloud

```

---

## 🧊 Hardware Philosophy

Ghost is not designed to “run a model.”

It is designed to:

> Run an intelligent system with layered cognition.

---

## 🧰 Practical Hardware Guidance

### Recommended Starter Build

- RK1 (16GB)
- 1 TB NVMe (required)
- Far-field mic array (ReSpeaker or similar)
- Small UPS
- Wired Ethernet

---

### When to Move to x86 + GPU

Upgrade if you need:

- 13B+ models locally
- Vision processing
- Heavy multi-agent workflows
- Full offline capability

---

## ⚠️ Operational Considerations

### RK1 Tradeoffs

- Smaller ecosystem
- NPU tooling complexity

### Mitigation

- Start CPU-only
- Introduce NPU gradually
- Keep system modular

---

## 🔄 Deployment Progression

1. Basic deployment (CPU + API-assisted)
2. Local-first routing enabled
3. Embeddings generated locally
4. Reflex layer optimized
5. Full multi-agent execution
6. GPU acceleration (optional)

---

## 💰 Cost Overview

### Starter (RK1-based)

- Estimated build: **$250–$500**

Includes:
- Compute board
- Storage
- Audio setup
- Power + networking

---

### Pro

- Estimated: **$2,500–$5,000**

---

### Ultra

- Estimated: **$8,000–$20,000+**

---

## 🔐 Security & Sovereignty

- Local-first execution
- Encrypted storage (LUKS recommended)
- Firewall + LAN isolation
- Secure API access (BRIDGE_SECRET)
- Hardware kill switches (mic/camera)

---

## 📦 Form Factors

### Edge Node
- RK1 device + mic + speaker
- Always-on assistant

### Hub + Satellites
- Central compute + distributed input nodes

### Workstation
- GPU-powered system for full local intelligence

---

## ⚡ Performance Tips

- Stream tokens for better UX
- Keep prompts compact
- Avoid large system prompts
- Warm models after boot
- Ensure proper cooling (avoid throttling)

---

## 🧠 Summary

Ghost hardware is built around one idea:

> Intelligence should live with you — not depend on the cloud.

A well-configured system delivers:
- Instant responsiveness
- Strong privacy guarantees
- Scalable intelligence
- Efficient long-term operation
