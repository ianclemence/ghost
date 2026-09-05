# Ghost Roadmap

## Current status (update this file: the phase table below is historical)

- **Backend/appliance architecture: COMPLETE and validated.** Ghost now has the
  capability substrate, permission broker, canonical events/activity, durable
  memory + RAG with context isolation, routines, contexts, voice and device
  interfaces, provider reliability, appliance setup/health, `ghost verify`,
  `ghost benchmark`, and the Golden Conversation Suite. See
  [CAPABILITY_ARCHITECTURE.md](CAPABILITY_ARCHITECTURE.md) and
  [EVALUATION.md](EVALUATION.md).
- **Backend status: READY FOR MOBILE.**
- **Next direction: the mobile product experience** (Capacitor-based app:
  pairing, the primary Ghost conversation, voice push-to-talk, approvals,
  Activity, Memory, settings, offline/reconnection UX, real-device validation).
  Mobile functionality is not yet built; the backend contract it consumes is
  documented in [MOBILE_API.md](MOBILE_API.md).

## Overview (historical planning)

This roadmap originally sequenced the work needed to turn Ghost from a developer project into a personal AI that belongs to its owner. It is aligned to the [Product Strategy](PRODUCT.md): open core, Ghost Connect as the paid managed experience, BYO-hardware first, hardware deferred.

Each phase maps to an implementation plan under [`plans/`](plans/) where one exists. The phases build on each other: a phase's plan is only workable once its prerequisites are met.

---

## Phases

| Phase | Goal | Build track | Plan |
|-------|------|-------------|------|
| **0 — Foundation** (done) | Web console, auth lifecycle, recovery, security. | — | — |
| **1 — Remote access** (done) | Relay + secure device pairing; mobile works from anywhere. | Cloud relay / pairing | [`01-cloud-relay.md`](plans/01-cloud-relay.md) |
| **2 — Install** (done) | Appliance setup/provisioning, health, self-healing skills. | Simple install | [`02-install-experience.md`](plans/02-install-experience.md) |
| **3 — Updates** | Ghost ships and installs updates to devices it never touches. | OTA updates | [`03-ota-updates.md`](plans/03-ota-updates.md) |
| **4 — Observability** | Runs updates/support on devices we cannot see. | Opt-in telemetry | [`04-telemetry.md`](plans/04-telemetry.md) |
| **5 — Move** | Ghost identity portability: replace the hardware, keep the Ghost. | Encrypted Ghost State export/import | (`ghost state` — implemented) |
| **6 — Ghost Connect** | Managed-service platform + billing once services prove value. | Billing / account | (follows telemetry) |
| **7 — Hardware (optional)** | A physical Ghost bundle. Shares the pipeline; only if demand is proven. | Device bundle | (deferred) |
| **8 — Backend substrate & appliance** (done) | Capability/permission/event substrate, memory+RAG with context isolation, routines, contexts, voice/devices, verify/benchmark/golden. | — | CAPABILITY_ARCHITECTURE / EVALUATION |
| **9 — Mobile (next)** | Capacitor mobile app over the documented backend contract. | Mobile productization | MOBILE_API.md |

---

## Phase 0 — Foundation (current)

The foundation for Ghost's persistent identity and appliance lifecycle is in place:

- **Web console** (`ghost-web`): setup wizard + admin dashboard on port 80.
- **Admin credential lifecycle**: password creation, change, and reset; recovery mode; session invalidation on change.
- **Security hardening**: `.secrets.json` secrets boundary, atomic config writes, directory permissions (`0700`), recovery bound to localhost, gateway enforces device credentials for LAN peers (loopback trusted).
- **Self-improvement**: learning pipeline, skill drafts, mid-turn steering, recall summarization.
- **Dashboard redesign**: responsive layout, polished cards, skill editor.

**Foundational requirement from Phase 0:** identity portability is a data-model concern first. Phase 0 establishes a **canonical Ghost State schema** — the explicit definition of what is portable, what is rebound, and what is replaceable infrastructure (see [Product Strategy, "The Ghost State"](PRODUCT.md#the-ghost-state)). Every subsequent feature must answer the question *"does this become part of Ghost State?"*, so that five years of accumulated state never turns out to be unmigratable. Build the data model for migration now; ship the polished "Ghost Moves With You" experience later. This keeps "identity first, hardware second" true as the other phases are built, rather than retrofitting portability onto a finished system.

**Definition of done for Phase 0:** a clean first-boot-to-dashboard flow on a Pi, with recovery and password reset working. This is largely complete.

**Exit criterion:** the mobile app can pair over LAN (in progress separately).

**LAN pairing implemented:** secure device pairing is now available on the internal API (`/v1/pairing/*`). Tokens are short-lived (5-minute expiry), single-use, and SHA-256 hashed. Per-device credentials authenticate WebSocket and REST connections via headers only (query parameters not supported). Recovery mode is bound to localhost. See [TESTING.md](TESTING.md#5-pairing--device-auth) for test procedures.

---

## Phase 1 — Remote access (cloud relay)

**Goal:** the mobile app reaches Ghost from anywhere without port forwarding. This is the first Ghost Connect service.

**Why now:** the mobile app is one of the primary interfaces to a personal AI that lives on your hardware. Without remote access, that AI is only available when you're on the same network. The relay is the first service users pay for, so it must exist before any monetization.

**Plan:** [`01-cloud-relay.md`](plans/01-cloud-relay.md)

**Definition of done:** a user on cellular data can open the app and talk to a Ghost on their home network, end-to-end, via a Ghost-hosted relay. LAN pairing still works as the fallback.

---

## Phase 2 — Simple install

**Goal:** a mainstream user can install Ghost on a Pi (or supported board) without flashing a raw image or using a terminal.

**Why now:** the image is plumbing, not the product. The product requires a trivial install path so the privacy-conscious mainstream — not just hobbyists — can adopt it.

**Plan:** [`02-install-experience.md`](plans/02-install-experience.md)

**Definition of done:** a user can either (a) buy a pre-flashed microSD card, or (b) follow a two-minute flow using an official installer, and reach the setup wizard without touching a terminal.

---

## Phase 3 — OTA updates

**Goal:** Ghost ships and installs updates to devices it never touches, with automatic rollback on failure.

**Why now:** updates are the recurring value that justifies the subscription. A user who cannot update has no reason to keep paying.

**Plan:** [`03-ota-updates.md`](plans/03-ota-updates.md)

**Definition of done:** a device receives, verifies, and installs an update over the relay; if the device fails to boot after the update, it rolls back automatically.

---

## Phase 4 — Opt-in telemetry

**Goal:** we can see the health and version of the devices we support, with explicit user consent.

**Why now:** we cannot run updates and support on devices we cannot see. Telemetry must be in place before the subscription support promise is made.

**Plan:** [`04-telemetry.md`](plans/04-telemetry.md)

**Definition of done:** opted-in devices report version, health, and error counts to the relay; the data is privacy-preserving and removable.

---

## Phase 5 — Move (Ghost moves with you)

**Goal:** a user can move Ghost to new hardware and continue where they left off — same identity, memory, skills, configuration, and personality.

The underlying capability is **Ghost identity portability**: Ghost is a persistent identity that happens to have compute attached. Migration to new hardware is the first use case; the mechanism is designed so it can later support:

- restore after hardware failure
- cloning / recovery
- replacing the control-plane device
- upgrading compute (moving from a Pi to an x86 mini-PC)
- running Ghost across multiple trusted devices

**Why now:** identity-first is the architectural principle behind the whole product. If replacing hardware means installing a new Ghost from scratch, the identity is tied to the machine — and the moat is gone.

**Plan:** none yet — new phase.

**Non-goals for the first cut:** cloud-only migration. An **encrypted local backup/export** is a first-class path; Ghost must not imply that data must live in the cloud to move.

**Secrets and permissions:** some credentials must be re-authenticated or explicitly re-paired on the new machine rather than exported. Never promise that every secret simply transfers.

**Definition of done:** a user can export Ghost to an encrypted archive, move it to new hardware, re-pair, and continue as the same Ghost.

---

## Phase 6 — Ghost Connect

**Goal:** launch the managed-service platform and billing once the underlying services have proven their value.

**Preconditions:** Phase 1 (relay) and Phase 3 (updates) shipped; Phase 4 (telemetry) gives us visibility.

**Out of scope for now:** billing implementation. The subscription mechanics are deferred until the preceding phases prove demand. See the Product Strategy for the business model and the Free/Connect split.

---

## Phase 7 — Hardware (optional)

**Goal:** a physical Ghost bundle (device + pre-flashed OS + subscription).

**Decision gate:** only if the BYO subscription proves demand. It shares the image, OTA, and relay pipeline, so deferring it costs nothing.

**Out of scope for now.** Until this phase opens, Ghost runs on hardware the user already owns.

### Supported hardware

Ghost runs on any Linux device, and its identity is **hardware-independent**. Think of it as two roles, which may live on the same box or split across devices:

- **Control plane** — the always-on device that hosts Ghost's memory, identity, skills, and automations. Low-power single-board computers are the reference target.
- **Compute** — hardware that runs heavier local models, attached when the control plane is not enough. Can be an x86 mini-PC, an NPU, or a GPU box — and, for deeper reasoning, a cloud model.

**Hardware-aware defaults** are implemented (`pkg/hardware`): Ghost detects the
machine class (Raspberry Pi 5, RK1-class 12–16 GB ARM, x86 mini-PC, GPU
workstation/server) and derives model tier, concurrency, and context sizes
automatically — the same Ghost with appropriate defaults on each.

**Current reference development appliance:** Raspberry Pi 5 · 8 GB RAM ·
32 GB microSD. NVMe storage is a later storage migration, not a redesign.

**Supported platforms:** any Linux device. Ghost runs on whatever hardware you own; the reference targets are where the appliance experience is guaranteed, and nothing in the software restricts it to those boards.

**Minimum requirements** (reference target):
- Raspberry Pi 5 (8 GB) or another Linux device
- 32 GB microSD storage
- A phone with the Ghost app

### Capability tiers

| Tier | Typical hardware | Local model scale | Role |
|------|------------------|-------------------|------|
| Starter | RK1 (16 GB) / Pi 5 (8 GB) | 1B–3B fast, 7B usable | Control plane: always-on personal AI |
| Pro | Control plane + x86 mini-PC (64–128 GB) | 7B–13B | Personal AI with heavier compute attached |
| Ultra | Control plane + workstation (128 GB+) | 20B–34B | Advanced personal AI workstation |

The device runs local models via Ollama and the [Intelligence Router](PRODUCT.md#architecture) falls back to a cloud model only when the task needs deeper reasoning. Most interactions stay on-device.

### Hardware as a product

When Phase 7 opens, the bundle is: a supported board, Ghost pre-flashed, and the Ghost Connect subscription. The engineering pipeline (image, OTA, relay) is already built by then, so the bundle adds a supply chain, not new software.

---

## Principles that govern the roadmap

1. **The experience is the product.** Ghost Core makes it possible, Ghost OS gives it a persistent home, and Ghost Connect provides optional managed services.
2. **Identity first, hardware second.** A Ghost installation should be replaceable without replacing the user's Ghost.
3. **Local-first is non-negotiable.** Nothing in Phases 1–5 makes the device dependent on the cloud. The relay and updates enhance a local-first product; they never replace it.
4. **Build shared plumbing first.** Relay, image, and OTA are needed by both the BYO path and any future hardware bundle. No work is wasted.
5. **Monetize last.** Ship the value (relay, updates, telemetry, move) before adding billing.
