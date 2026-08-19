# Ghost — Product Strategy

## Purpose

This document states who Ghost is for, what it sells, and how it makes money. It is the reference for every product and engineering decision. Read it before reading the [Roadmap](ROADMAP.md) and the implementation plans under [`plans/`](plans/).

---

## The product thesis

Ghost is the **persistent home of your personal AI**. It is not an appliance that happens to run an LLM; it is where your AI lives — your memory, identity, skills, tools, and automations, on hardware you own. It is private by design and works even when the internet does not.

Ghost is a **local-first** product: the software runs entirely on hardware you own, keeps your data on-device, and is never dependent on the cloud. The cloud is an optional enhancement, never a requirement.

The hierarchy:

- **Ghost** — your personal AI.
- **Ghost Core** — the open-source agent runtime (memory, tools, skills, model routing, local inference).
- **Ghost OS** — the appliance environment that gives Ghost a persistent home: install, pairing, identity, updates, backups, recovery, hardware and model management.
- **Ghost Connect** — optional managed services: remote relay, OTA management, encrypted backups, multi-device access and sync, support.

Users never buy "Ghost OS" and then get Ghost separately. Ghost is the product; Ghost OS is how it stays alive on a device; Ghost Connect is the optional convenience layer around it.

The buyer is a **privacy-conscious mainstream user**: someone who wants a capable personal AI that does not require a cloud subscription, does not train on their conversations, and does not depend on a provider they do not control.

---

## Market position

Ghost is a **software product you run on hardware you own**. It is not a cloud service: the assistant works locally, keeps your data on-device, and does not depend on an external provider.

Ghost is a positioning boundary with the adjacent players, not a competitor to them:

> **Home Assistant owns Home. Ghost owns Person.**

Ghost does not try to win smart-home control, device ecosystems, or home automation. Home Assistant is one integration Ghost can use, not an opponent. Ghost's territory is the person — their memory, identity, routines, skills, and personal workflows.

| Option | Verdict |
|--------|---------|
| BYO-hardware appliance | **Primary market.** Software + managed service on hardware the user already owns. |
| Full hardware device | Later expansion. Shares the same pipeline; deferred until demand is proven. |
| Open-source core | The engine and web console are open — this wins the hobbyists who test, report, and spread the word. |

### The moat

Ghost's defensible asset is the **persistent personal AI**: identity, memory, skills, learned workflows, data, and the appliance lifecycle that keeps it reliable. After five years Ghost holds thousands of conversations, preferences, documents, routines, and automations. Switching assistants means leaving that behind — that is the lock-in we build, and it is earned, not imposed.

The lifecycle (first-boot wizard, admin console, credential recovery, managed updates, remote access, resilience) matters because it makes that persistent identity *reliable*. The test:

> A Ghost installation should be replaceable without replacing the user's Ghost.

---

## Business model

**Open core, paid managed service.**

- **Ghost Core and Ghost OS are open.** The engine, web console, and agents are open — this is how we win the hobbyists who test, report, and spread the word. They are the discovery flywheel.
- **Ghost Connect is the paid subscription** for the parts that must be centralised and maintained.

The guiding principle:

> Never charge users to unlock the fundamental promise of owning their own AI. Charge for convenience around it.

### Ghost Free

Ghost works completely locally, with no account and no mandatory cloud:

- Local AI
- Local memory
- LAN access
- Local tools and skills
- No account required

### Ghost Connect

Optional managed services for convenience and reliability:

- **Remote relay** — the mobile app reaches Ghost from anywhere without port forwarding
- **OTA management** — Ghost ships and installs updates to devices we never touch
- **Encrypted backups** — including a first-class local backup/export path
- **Multi-device access and sync**
- **Support** — a supported, guaranteed-working experience
- **Optional cloud model routing** — deeper reasoning from a cloud model when the task calls for it

The free tier is genuinely useful on its own. Connect sells convenience and reliability, never functionality that was artificially removed.

---

## Revenue hook

The cloud relay is the first Ghost Connect service and the subscription anchor: the mobile app is unusable off the home network without it. Managed updates, encrypted backups, and support justify renewal.

---

## Architecture

Ghost is organized in three layers:

```
Ghost Connect   optional managed services
      ↓
Ghost OS        the appliance environment: install, identity, updates, recovery
      ↓
Ghost Core      open-source agent runtime: memory, tools, skills, local inference
```

The **Intelligence Router** sits inside Ghost Core and decides where each task should run. It selects the best available intelligence for the task, weighing:

- capability
- latency
- privacy
- cost
- availability
- hardware resources
- user preferences

The best model for a task is not always the cheapest. The user-facing principle is:

> Ghost decides where a task should run. You don't have to think about models.

---

## Scope boundaries

- **Hardware** is deferred: the product is software + managed service on hardware the user already owns. A physical bundle is a later expansion that shares the same pipeline.
- **Ghost's identity is hardware-independent.** Ghost is a persistent identity that happens to have compute attached. Replacing the device must mean moving Ghost, not reinstalling from scratch.
- **The image is a distribution mechanism, not the product.** The value is the service on top of it.
- **Cloud is an enhancement, never a requirement.** The product stays local-first; Ghost Connect adds remote access, updates, backups, and support.

---

## Product principles

1. **Local-first.** The personal AI must work offline. The cloud is an enhancement, never a requirement.
2. **Private by default.** Data stays on-device. No training on user data. Telemetry is opt-in.
3. **Ownership.** Users own their hardware and their data.
4. **Identity first, hardware second.** A Ghost installation should be replaceable without replacing the user's Ghost.
5. **It just works.** Setup, updates, and remote access must be trivial for a non-technical user.
6. **The service is the product.** Updates, relay, backups, and support are the paid value.

---

## What this means for engineering

The engineering priorities follow the business model. The build tracks, in order:

1. **Cloud relay / pairing** — the first Ghost Connect service and the subscription anchor.
2. **Simple install** — how a mainstream user gets Ghost onto hardware.
3. **OTA updates** — the recurring value and the reason the subscription renews.
4. **Opt-in telemetry** — how we run updates and support on devices we cannot see.
5. **Move** — identity portability: an encrypted local backup/export path and re-pairing so Ghost survives hardware changes.

Each has a dedicated implementation plan under [`plans/`](plans/) where one exists. The [Roadmap](ROADMAP.md) sequences them into phases.