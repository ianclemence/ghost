# Changelog

Newest first. Ghost shows new entries on first launch after an update;
`ghost update --notes` reprints them.

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


