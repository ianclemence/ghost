# Ghost — Product Strategy

## Purpose

This document states who Ghost is for, what it sells, and how it makes money. It is the reference for every product and engineering decision. Read it before reading the [Roadmap](ROADMAP.md) and the implementation plans under [`plans/`](plans/).

---

## The product thesis

Ghost is a **local-first personal AI appliance**. The software runs entirely on hardware you own. The assistant works on your network, uses your compute, and keeps your data on-device. It is private by design and available even when the internet is not.

The product is the **service** — updates, remote access, and support. The installable image is just how the software reaches the device; it is not the product on its own.

The buyer is a **privacy-conscious mainstream user**: someone who wants a capable personal AI that does not require a cloud subscription, does not train on their conversations, and does not depend on a provider they do not control.

---

## Market position

Ghost is a **software product you run on hardware you own**. It is not a cloud service: the assistant works locally, keeps your data on-device, and does not depend on an external provider. This is the Apple-HomeKit positioning — you own the device, and Ghost owns the experience and the upkeep.

| Option | Verdict |
|--------|---------|
| BYO-hardware appliance | **Primary market.** Software + managed service on hardware the user already owns. |
| Full hardware device | Later expansion. Shares the same pipeline; deferred until demand is proven. |
| Open-source core | The engine and web console are open — this wins the hobbyists who test, report, and spread the word. |

### The moat

Ghost's defensible asset is the **complete lifecycle**: first-boot wizard, admin console, credential recovery, managed updates, remote access, and resilience. That lifecycle is what turns Ghost into a product rather than a library.

---

## Business model

**Open core, paid managed service.**

- **Open source the core.** The Ghost engine, web console, and agents are open. This is how we win the hobbyists who test, report, and spread the word. They are the discovery flywheel.
- **Sell the managed layer as a subscription.** Users pay for the parts that must be centralised and maintained:
  - **Cloud relay** — lets the mobile app reach Ghost from anywhere without port forwarding.
  - **Managed OTA updates** — Ghost ships and installs updates to devices we never touch.
  - **Support** — a supported, guaranteed-working experience.
- **Hardware is a later, optional bundle.** The pipeline (image, OTA, relay) is shared, so a hardware bundle costs nothing to defer. Build it only if the subscription proves demand.

This is the Home Assistant + GitLab model: free local software, paid "make it just work everywhere" service.

### Revenue hook

The cloud relay is the subscription anchor. The mobile app is unusable off the home network without it, and it is the one component that must be paid. Managed updates and support justify renewal.

---

## Scope boundaries

- **Hardware** is deferred: the product is software + managed service on hardware the user already owns. A physical bundle is a later expansion that shares the same pipeline.
- **The image** is a distribution mechanism, not the product. The value is the service on top of it.
- **Cloud is an enhancement, never a requirement.** The product stays local-first; the cloud adds remote access, updates, and support.

---

## Product principles

1. **Local-first.** The assistant must work offline. The cloud is an enhancement, never a requirement.
2. **Private by default.** Data stays on-device. No training on user data. Telemetry is opt-in.
3. **Ownership.** Users own their hardware and their data.
4. **It just works.** Setup, updates, and remote access must be trivial for a non-technical user.
5. **The service is the product.** Updates, relay, and support are the paid value.

---

## What this means for engineering

The engineering priorities follow the business model. The four build tracks, in order:

1. **Cloud relay / pairing** — the subscription anchor.
2. **Simple install** — how a mainstream user gets Ghost onto hardware.
3. **OTA updates** — the recurring value and the reason the subscription renews.
4. **Opt-in telemetry** — how we run updates and support on devices we cannot see.

Each has a dedicated implementation plan under [`plans/`](plans/). The [Roadmap](ROADMAP.md) sequences them into phases.
