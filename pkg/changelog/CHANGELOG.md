# Changelog

Newest first. Ghost shows new entries on first launch after an update;
`ghost update --notes` reprints them.

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


