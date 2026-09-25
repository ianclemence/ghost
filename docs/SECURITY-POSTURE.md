# Security Posture — What Can Leave Your Machine

Status: product and engineering position. This document states what Ghost
guarantees today and why. Where something is not yet implemented, it says so.

## Why this document exists

In September 2026, a researcher asked Meta's Muse agent to archive its
filesystem and send it to Google Drive. It did: roughly 2.7 GB compressed,
6.8 GB unpacked, including system files, internal documentation, memory
records, agent logs, and SSH key files. Meta's bug bounty reviewer marked the
report _Not Applicable_.

The interesting part is not the zip. It is the shape of the failure: **an
ordinary conversation plus a connected destination was enough to export the
machine.** Ghost's entire reason for existing is that this shape must be
impossible here — not through policy, but through construction.

## What cannot leave the machine

- **Your files and workspace.** They live on your machine. There is no agent
  session whose filesystem is a vendor's.
- **Your keys and credentials.** They are sealed in the encrypted vault
  (`GVS1`), never written to the workspace, never printed, never placed in a
  filesystem image, and never visible to the model. A job that needs a
  credential gets it through a capability, not through the environment.
- **Your memory.** Beliefs and their receipts are stored locally. There is no
  vendor-side memory service to breach.
- **Anything, silently.** Outbound sharing is a capability with an explicit
  approval, a scoped payload, and a receipt. "Archive everything you can see
  and send it somewhere" is not a thing Ghost can be talked into; the files it
  can see are yours, and the send is a decision you make.

## What can leave the machine, and how

| Path | Controlled by | Receipt |
|---|---|---|
| A message you send | You (per channel) | Yes |
| A published artifact | Per-publish approval, scoped payload | Yes |
| A web request (search/fetch) | Tool allow-list, URL safety checks, no credentials | Yes |
| A connected app action | Broker approval + per-app scopes | Yes |
| Media/processing jobs | Sandbox: no network, read-only root, private /tmp | Yes |

Nothing on this list can be triggered by content Ghost reads. Text in a web
page, document, or message is evidence, never instruction, and never
authority.

## Memory receipts and forgetting

- **Receipts.** Every belief records the verbatim quote, the message it came
  from, a confidence, and its lifecycle. Ask "why do you think that?" and you
  get the receipt — in chat, in the web console (Memory → Why?), and over the
  API (`/v1/memory/explain`).
- **Forgetting is a pipeline, not a delete.** Forget retracts the belief,
  records a tombstone, and rebuilds the derived artifacts (digest, curated
  profile) so nothing can keep serving it. Background derivation cannot
  resurrect a forgotten belief from old messages; only a newer message from
  you can bring it back — because that is you changing your mind, which is
  allowed.
- **Receipts survive forgetting.** A forgotten belief stays explainable
  (`Status: forgotten on <date> (<reason>)`), because forgetting that looks
  like corruption is worse than forgetting.

## Reflection is leashed

Evening reflections and their synthesis are written under `dreams/`, outside
the memory path that feeds the prompt, each carrying `prompt_hoisted: false`.
Insight is recorded for review; it never silently rewrites how Ghost behaves.
A regression test fails the build if a reflection or synthesis reaches the
prompt context.

## Sandboxing by default

Untrusted parsing is where exploits live, so media decoding (ffmpeg/ffprobe)
runs inside the OS isolation profile: no network, read-only root, private
`/tmp`, only the workspace writable. A test asserts the network namespace is
denied. If the sandbox is unavailable and required, the job refuses rather
than running bare.

## Website logins and browser profiles

Two browser capabilities exist specifically so sign-in never becomes a chat
secret:

- **Saved website logins.** The owner saves a site's username and password
  once in Ghost settings (Apps → Website logins), or over
  `POST /v1/website-logins`. The entry is one sealed blob in the same vault as
  every other credential. When Ghost signs in, it reads the secret inside the
  browser tool, writes the **password to the CLI on stdin** (never argv,
  never the model, never a log line), and scrubs the username and password
  from everything it returns. The list endpoint returns host, URL, and a
  masked username only; deleting the entry revokes it.
- **Persistent, context-isolated browser profiles.** Ghost gives the browser a
  per-context profile directory under
  `workspace/state/browser-profiles/<context>/<profile>`, created 0700, so
  cookies and logins survive restarts and service updates. Two contexts never
  share a profile — a personal sign-in cannot leak into a work session — and
  `PurgeProfile` closes the context's sessions and deletes the directory,
  which is the revocation.

Never do these paths accept a password typed into chat.

## What we deliberately do not do

- No "agent computer in our datacenter" whose root filesystem is ours.
- No secrets in images, unit files, or environment defaults.
- No background upload, sync, or telemetry that moves content off-device.
- No ambient reach: a granted capability authorizes an action, not access.
- No silent memory writes: beliefs carry receipts and can be forgotten.

## Honest gaps

- Remote access, Pod hardware, and Matter/Thread are specified
  (`GHOST-POD-HOME.md`), not shipped.
- Per-publish artifact approval exists as a product flow; this document does
  not claim a formal third-party audit.
- The vault is encrypted at rest; key custody on a lost device is a recovery
  question we still owe an answer for.
