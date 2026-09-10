# Security Contract

## Console authentication

- Owner password verified with bcrypt (cost 12); minimum 8 characters plus
  a common-password blocklist. No usernames exist, so there is nothing to
  enumerate; wrong passwords always return the same generic error.
- Sessions are 24-byte random bearer tokens, HttpOnly cookies, SameSite
  Lax, `Secure` whenever served over TLS. 30-minute TTL (7 days with
  "remember me"), absolute expiry, no sliding renewal.
- **Brute force:** max 5 failures per IP, then escalating cooldowns of
  10 min → 30 min → 1 h → 2 h (cap). Counters decay after a quiet day and
  persist on disk, so restarts don't clear an attacker's cooldown. HTTP
  429 while locked; success resets.
- **Lock is real:** the Lock button signs out server-side. There is a
  `POST /api/logout` endpoint; hiding the UI without it is not locking.
- **Password change kills everything:** changing the owner password
  revokes all sessions including the one that made the call.
- The session list shows IDs with masked tokens; revocation is by ID
  (full tokens are accepted for backward compatibility). A session can
  never revoke itself — use sign out.
- Recovery (`POST /api/reset-password`, `ghost reset-password --force`)
  is localhost/local-shell only by design.

## Permissions

- Every consequential action goes through the broker: allow / ask / deny.
  Unknown capabilities fail closed to ask.
- There is exactly one scope derivation (`ScopeFor`: explicit target >
  contact > session > owner). Console "Always allow" stores the canonical
  scope of the request it answers — never a widened one — so a grant
  matches precisely the runtime invocation it was meant for.
- In the console, scopes read as "this chat only", "messages to X", or
  "everywhere on this Ghost". Denials are stored policy and win over
  allows, including in permissive modes.
- Approval requests expire after 15 minutes; expired approvals cannot be
  resolved or consumed. Allow-once approvals execute exactly once.

## Browser and computer use

The full execution path and its invariants are documented in
[BROWSER_GOVERNANCE.md](BROWSER_GOVERNANCE.md) and
[COMPUTER_PLACEMENT.md](COMPUTER_PLACEMENT.md). The boundary is the
capability operation (never model intent), resolved by the runtime:

- Model browser calls go through the **browser gate**: owner, context,
  task/generation and the permission broker are resolved server-side from
  the turn — never from model-supplied arguments — before any executor
  runs. Read operations pass; state-changing operations require broker
  authorization (allow / durable ask / deny). Forged owner/context/
  session/operation arguments cannot widen authority.
- Browser sessions are isolated per owner + context + task: cookie jars
  never cross contexts, two tasks never share a session, and an approval
  resumes the SAME pinned session or refuses.
- All page output is redacted (secret-shaped strings) and labeled
  untrusted before it reaches the model. The user-visible text is
  unchanged. A page can never redefine Ghost policy.
- Computer holds are leases with TTL and heartbeat renewal; boot expires
  every survivor, so a restarted Ghost never drives a computer on behalf
  of a dead task. Financial, credential, and destructive operations
  require approval, and evidence records what ran.
- Durable work carries a generation token: completions from a rotated-out
  worker are dropped instead of mutating live state, and a terminal job
  (cancelled/expired/failed/succeeded) is never overwritten by a stale
  completion.
- No runtime evidence = no successful execution claim. A state-changing
  operation that returns no evidence is reported as unverified, never as
  done, and never emits a success event.

## Backup safety

See [BACKUP.md](BACKUP.md). Backups are passphrase-encrypted; secrets
never enter them unless explicitly opted in; restore validates before
touching state and always restarts the gateway afterward.

## Skills

Skills are instruction sets ("what Ghost knows how to do"), not agents, chats,
models, or plugins. They never execute on their own: Ghost reads the skill text
and then proposes actions through the normal governed tools.

Security rules:
- `exec`/`sandbox` are always classified high-impact by the Permission Broker,
  even inside a read-only skill. A skill can never silently run arbitrary
  shell; it needs an explicit grant/approval and produces evidence.
- External (GitHub/ClawHub) skills are bounded text downloads: blocked binary
  extensions, <=50 files, <=200KB/file, <=1MB total, a root `SKILL.md` with
  name + description, no path traversal. Install provenance is recorded in
  `.ghost-source.json` inside the skill so the owner always sees source +
  revision.
- Enable/disable is a deterministic `SKILL.md` <-> `SKILL.md.disabled` rename
  and is visible to the loader/model immediately; removal deletes the skill
  directory. No skill lifecycle persists after disable/removal.
- `skill_manage` (create/patch/delete/enable/disable) is broker-governed:
  create/patch/delete are high-impact, enable/disable are consequential. A
  skill can never modify Ghost's own skill system or grant itself
  capabilities without an explicit owner decision.
- Skill lifecycle operations emit canonical events (`skill.installed/enabled/
  disabled/updated/removed`) that project to Activity; they are published only
  by trusted runtime operations, never by the model/skills.
- ClawHub archives are extracted with the safe Go extractor (traversal,
  symlink, absolute-path, size and count validated), never the OS `unzip`.
- GitHub installs record resolved commit SHA when GitHub provides it; the CLI
  and gateway installers share the same bounded validation boundary and write
  provenance to `.ghost-source.json`.

## Authorization is monotonic

The Permission Broker is the single authorization authority. Authorization
decisions are ordered `allow < ask < deny`, and any composition keeps the most
restrictive result (`permissions.Combine`). A downstream layer — routine scope,
context scope, or a registered deny-only guard (`Governance.AddGuard`) — may
reduce authority but may never turn a denial into an allow. There is no allow
override below the broker. The model, providers, skills, integrations, and UI
can only be subject to this policy; none of them can grant authority.

Guards are deny-only by construction: returning a non-empty reason denies,
returning empty abstains, and there is no allow result, so guard order cannot
change the outcome. Approval can never grant more than was requested, and
resume revalidates authorization instead of inheriting a stale approval.
