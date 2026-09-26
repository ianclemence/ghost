# Ghost Pod — Home Specification

Status: direction. This document is the product and engineering target for
bringing Ghost into the home. It is deliberately a specification, not an
implementation report: nothing here claims shipped capability.

## The one-sentence product

Muse's Home Link is a cloud agent reaching into your house. **Ghost Pod is
your house's agent, which is also your phone's, and the cloud is optional.**

The Pod is a small always-on computer that lives on your shelf. It runs Ghost
locally, holds your memory and keys locally, and speaks the home protocols
people actually buy.

## What we adopt from the reference design

The Meta Home Link guide (as reported publicly) gets four things right. We
adopt all four:

- **BLE for first-time setup.** No account, no cloud handshake. You bring your
  phone to the Pod, the Pod shows up, you confirm. This is the normie path.
- **A tiny, stable device command surface.** Health, discover, and firmware
  update are the right primitives. Device-specific verbs come later and stay
  boring.
- **Signed firmware with an explicit OTA command.** Updating is a first-class,
  auditable action — never an invisible background download.
- **Approval separated from access.** The agent does not reach devices
  directly; a gate decides, per request. Ghost's broker already works this way.

## Where Ghost Pod goes further

### Matter and Thread first, not a proprietary tunnel

A Pod that is a **Matter controller** and a **Thread border router** covers the
bulbs, locks, sensors, and plugs people already own. If we invent a tunnel
instead, we inherit a compat matrix and a cloud dependency we don't need.

- Matter controller: commission, control, and read devices locally.
- Thread border router: give low-power devices a mesh that works without Wi-Fi.
- Wi-Fi/BLE fallback for devices that speak neither.

### Local-only by default, remote by exception

Everything works with the WAN unplugged: lights, locks, sensors, automations,
voice, and the agent's home reasoning. Remote access is a feature you turn on,
not the substrate. **Offline is a supported state, not a failure mode.**

### Per-device scopes with receipts

Every actuation is an auditable fact:

```
front-door lock: unlock
  requested by: owner (phone, 21:04)
  policy: device.control / granted 09-20 → 12-20
  outcome: unlocked (device ack 34ms)
```

The Pod keeps the receipt. The owner can ask "what happened at the front door
last night?" and get the list — from their own machine.

### Physical-presence confirmation for sensitive devices

Locks, garage doors, and cameras require a press on the Pod (or an explicit
phone confirmation) in addition to the agent's request. Hardware can do what a
chat prompt cannot: prove a human is present.

### No vendor lock on the radio

The radio module is chosen for protocol support and supply, never as a lock-in.
BLE pairing, Thread, Matter, and Wi-Fi are open standards; a future revision
can change silicon without changing the product.

## Device surface (v1)

| Command | Purpose | Notes |
|---|---|---|
| `device.health` | Pod + radio status | Read-only, always allowed |
| `device.discover` | Scan the local network/Thread mesh | Read-only |
| `device.pair` | Commission a new device (BLE/Matter) | Requires physical presence |
| `device.ota` | Install a **signed** firmware update | Explicit; never silent |
| `device.control` | Actuate a device (via broker) | Per-device scope + receipt |

Device-specific verbs are expressed as capabilities (e.g. `light.set`,
`lock.unlock`) and resolved by the runtime — the model never picks a protocol.

## Security model

- **Keys never leave the Pod.** Device credentials and Ghost's vault are
  encrypted at rest, never in the workspace, never in an image.
- **No secrets in the OS image.** We do not bake credentials into firmware or
  filesystem images. Provisioning is an explicit, owner-driven step.
- **Outbound is a capability.** Nothing the Pod sees is exportable without a
  per-action approval and a scoped payload.
- **Approval is not access.** A granted capability authorizes an action; it
  never grants ambient reach.
- **Receipts are local.** The audit trail lives on the Pod. If you reset it,
  it is gone — by design.

## Phased plan

1. **Spec + threat model** (this document), reviewed
   against the export failure class before any hardware.
2. **BLE pairing + `device.health`/`discover`** on reference hardware, local
   only, no cloud.
3. **Matter controller** for lights/plugs/sensors; per-device scopes; receipts.
4. **Thread border router**; battery-device coverage.
5. **Lock/camera class** with physical-presence confirmation.
6. **Signed OTA** pipeline, and only then a remote-access option.

Each phase ships with its receipts and its failure states, not just its happy
path.
