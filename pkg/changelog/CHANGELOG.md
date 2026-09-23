# Changelog

Newest first. Ghost shows new entries on first launch after an update;
`ghost update --notes` reprints them.

## [0.24.13] - 2026-09-23

- Calendar runs on the direct Google Calendar API: agenda, natural
  language quick-add, and delete-by-query with real event evidence.
  gcalcli stays for device-flow-connected users, whose client-bound
  tokens only it can redeem; full removal waits on a Ghost-managed
  OAuth client.

## [0.24.12] - 2026-09-23

Follow-through fixes, same trust story.

- Transient provider blips (timeouts, rate limits, network) retry with
  bounded backoff in model and tool calls; auth and config failures
  still fail fast.
- The browser declares a 90-second bound and closes its session after a
  hung call, so leaked Chromium can no longer starve later turns.
- The Golden grader no longer mistakes distant completion words for
  Ghost's act (p-11 class), and its plural nouns cover third parties.
- Doctor and state suites are hermetic again (PATH pinning, v8-aware
  fixture).
- The web console has an Ideas section; the mobile app stays focused
  (ideas live in conversation).

## [0.24.11] - 2026-09-23

Trust you can read, delight you can feel.

- **Verification pages.** `ghost eval release-notes` assembles
  `docs/VERIFICATION-<version>.md` from Ghost's own checks — verify,
  benchmark, golden results, and limits — committed per release.
- **Tasks surface.** `ghost tasks`, `/tasks`, and `/task` show and manage
  durable routines (pause, resume, cancel) through the same routines
  service the gateway and mobile app use.
- **Ideas with evidence.** `ghost ideas`, `/ideas`, and `/idea` surface
  suggestions that cite their sources, with accept/dismiss receipts.
  `refresh` derives them deterministically; `draft` asks the model and
  verifies every citation, marking failures unverified.

## [0.24.10] - 2026-09-22

- Inline styling no longer leaks when the model breaks a line mid-span.
  Bold, italic, and code split across line breaks now rejoin before
  rendering, on both the live stream and the committed reply — without
  touching fences, tables, headings, quotes, or lists.

## [0.24.9] - 2026-09-22

- Your messages show as `You ┃ text` on one row, with continuation pipes
  aligned beneath.
- The input box now grows while you type a long message (up to its cap,
  then it scrolls) instead of staying one row.

## [0.24.8] - 2026-09-22

- Streaming replies render markdown live, Scout-style: headings, lists,
  quotes, code, and tables format as they arrive instead of raw `##` and
  `**` markers, long lines wrap instead of breaking mid-word, and
  paragraph gaps are preserved. Completion still prints only the
  remaining tail.

## [0.24.7] - 2026-09-22

- Replies now stream line-by-line into the conversation as they arrive,
  opencode style — no more watching a one-row preview and getting the
  whole answer at the end. Completion prints only the remaining tail,
  never the full text again.
- The idle footer is trimmed to `/ commands` (left) and `esc quit`
  (right), matching Scout.

## [0.24.6] - 2026-09-22

- The terminal transcript can no longer silently drop replies. Committed
  lines now print and drain like Scout's transcript (no print cursor left
  to desync), so `/clear`, `/thread`, and history reloads cannot swallow
  later responses.

## [0.24.5] - 2026-09-22

- The terminal no longer loses Ghost replies after `/clear`, `/thread`,
  or returning with `/main`. Replacing the transcript used to leave the
  scrollback cursor behind, silently swallowing later replies; the cursor
  now restarts with the transcript.

## [0.24.4] - 2026-09-22

- `ghost update` no longer prints scope and sudo notes on the Deploying
  line.

## [0.24.3] - 2026-09-22

Evaluation-driven fixes (Golden Suite 58/59 → 59/59, confirmed by external
JEV evaluation before and after).

- **Memory recall no longer falsely reports absence.** Asking what you
  like or prefer could answer "I don't have that stored yet" even when the
  preference was stored, because the fast recall path only looked up two
  predicates while stored beliefs use a wider vocabulary. The fast path now
  covers the liking family and falls through to full retrieval instead of
  asserting an absence it cannot prove.
- **Computer actions report dispatch honestly.** Control actions that were
  dispatched but not independently screen-verified used to report "executor
  reported false", which read as failure and contradicted the recorded
  success evidence. They now report "dispatched but not independently
  screen-verified".

## [0.24.2] - 2026-09-21

Reasoning-hygiene release.

- **Reasoning never surfaces.** Provider chain-of-thought is identified and
  discarded at the provider boundary: the OpenAI-compatible streaming and
  non-streaming paths read the full reasoning allowlist (`reasoning_content`,
  `reasoning`, `reasoning_text`) into the separate `ReasoningContent` field and
  never into answer content.
- **Inline `<think>` tags are stripped** from content with a streaming-safe
  filter that survives token-boundary splits, so a model that emits reasoning
  inline cannot leak it to the agent loop, the TUI, or the SSE bridge to the
  app. Anthropic, Moonshot, and Claude CLI content is sanitized at the same
  boundary.
- Thinking remains off by default; this is the hard guarantee that holds even
  when a caller opts a reasoning model in.

## [0.24.1] - 2026-09-21

Updater parity release.

- `ghost update` / `ghost update --check` print **what's new** on top of "Already current" and after an install, matching Scout.
- Embedded changelog + last-seen marker; `ghost update --notes` prints the full changelog.
- Version comparison is base-version aware, so a `-dirty` or dev-suffixed installed binary is correctly recognised as current instead of looking different.

## [0.24.0] - 2026-09-21

Updater release.

### One updater, no sudo
Ghost's updater ran entirely under sudo and rewrote an appliance-wide install.
It now prefers a per-user layout that needs no root and escalates only the
steps that genuinely require it.

- **`ghost update`** runs as you: `--check`, `--notes`, `--force`, `--dry-run`, `--channel release|dev`.
- **User scope needs no sudo**: binary in `~/.local/bin`, service via `systemctl --user` (`make install-user`).
- **System scope** escalates only the binary swap and service restart.
- **Verified artifacts**: the release channel resolves from GitHub Releases, verifies sha256 (+ Ed25519 when `GHOST_RELEASE_PUBKEY` is set), smoke-tests, and atomically installs.
- **`ghost update --check`** reports installed vs available and the detected scope.

### Retired
The standalone `ghost-update-daemon` is gone — it was a second, divergent
updater. `ghost auto-update` now ticks the same release-channel updater.


## [0.23.45] - 2026-09-21

# Ghost v0.23.45 — "Thinking" reads with a capital T

The composer's top rule now reads **Thinking** (capital T) when Ghost is
reasoning without a tool, matching the tool labels it alternates with
("Searching…", "Reading…"):

```
── ▖ Thinking · 4s ────────────────────────
── ▖ Searching the web… · 4s ─────────────
```

Docs updated. Build clean; `cmd/ghost` suite green.


## [0.23.44] - 2026-09-21

# Ghost v0.23.44 — no tool count in the prompt status

The composer's top rule used to read `⠋ thinking · 4s · 2 tools`. The tool
count is gone — the prompt now names the live activity and elapsed time only:

```
── ▖ thinking · 4s ────────────────────────
── ▖ Searching the web… · 4s ─────────────
── ▖ Reading notes.md · 3s ───────────────
```

Tool detail is still available in the `/details` trail (Ctrl+O).

Also removed a dead status helper and its unused styles that still rendered a
tool count. Build clean; `cmd/ghost` suite green.


