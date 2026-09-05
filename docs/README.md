# Ghost Documentation

This is the home for Ghost's product and engineering documentation.

## Start here

1. **[Capability & Substrate Architecture](CAPABILITY_ARCHITECTURE.md)** — how Ghost actually works today: entity, agent, capabilities, permission broker, canonical events, activity, credentials, routines, contexts, channels, voice, devices.
2. **[Reference Appliance](REFERENCE_APPLIANCE.md)** — what a correctly configured Ghost is, the memory-scope model, hardware classes, and the engineering philosophy.
3. **[README](../README.md)** — product overview, quick start, commands, configuration, services.

## Product & status

- **[Roadmap & Status](ROADMAP.md)** — what is complete, current status (backend READY FOR MOBILE), and the next direction.
- **[Product Strategy](PRODUCT.md)** — product framing (kept for history).

## Engineering references

- **[Evaluation](EVALUATION.md)** — `ghost verify`, `ghost benchmark`, and the Golden Conversation Suite.
- **[Mobile API Contract](MOBILE_API.md)** — the stable backend surface consumed by the mobile app.
- **[Appliance Architecture](APPLIANCE_ARCHITECTURE.md)** — provisioning/setup, canonical health, provider resilience, durable requests, secrets boundaries.
- **[Testing](TESTING.md)** — how to test Ghost.
- **[Connection Flow](CONNECTION_FLOW.md)** — pairing and device connectivity.
- **[Personal Context](PERSONAL_CONTEXT.md)** — the append-only personal model store.
- **[Scheduled Intelligence](SCHEDULED_INTELLIGENCE.md)** — scheduler and routines design.

## Implementation plans (historical)

Each plan is self-contained and describes work that has since shipped or been superseded by the reference architecture.

| Plan | Goal |
|------|------|
| [01 — Cloud relay / pairing](plans/01-cloud-relay.md) | The mobile app reaches Ghost from anywhere (relay is live). |
| [02 — Install experience](plans/02-install-experience.md) | A mainstream user installs Ghost (appliance setup shipped). |
| [03 — OTA updates](plans/03-ota-updates.md) | Ghost ships and installs updates with rollback. |
| [04 — Opt-in telemetry](plans/04-telemetry.md) | Runs updates/support on devices we cannot see. |
