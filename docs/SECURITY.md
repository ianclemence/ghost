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
