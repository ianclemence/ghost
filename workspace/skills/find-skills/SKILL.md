---
name: find-skills
description: Discover, verify, and install third-party Ghost skills. Invoke when the user asks for a new capability ("can you handle PDFs?", "is there a skill for X"), wants to install a skill from a repo or URL, or asks what skills exist. Verifies provenance and freshness before recommending anything.
version: 1.0.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [skills, discovery, install, ecosystem]
---

# Find Skills

Discover and install skills through the package manager — never by pasting files.

## Install

```bash
ghost skills add owner/repo[@skill][/path][#ref]   # GitHub shorthand
ghost skills add git@host:owner/repo.git           # SSH / private repos
ghost skills add https://host/owner/repo           # any git host
ghost skills add ./local/path                      # local directory
ghost skills add example.com@skill-name            # well-known index
ghost skills add <source> --copy                   # copy instead of symlink
```

Identity is `owner/repo@skill`. Every install writes two locks: a
machine-global tree-SHA lock and a checked-in `skills-lock.json`
content-hash lock for reproducible workspaces.

## Verify before recommending

1. Prefer well-known publishers and skills with a history of installs.
2. Check freshness: `ghost skills update <name>` reports whether upstream moved.
3. Read the installed `SKILL.md` before invoking a new skill the first time.
4. Never execute install scripts bundled with a skill sight unseen; the
   skill's own `prerequisites.commands` list is the install surface.

## Updates

```bash
ghost skills update            # check everything locked
ghost skills update <name>     # check one skill
ghost skills update --apply    # reinstall whatever moved
```

## Rules

- The canonical store is the single source of truth; workspace installs
  are symlinks by default (`--copy` only when the consumer must own files).
- Fetch is bounded (10 MiB, 1000 files, no symlinks/traversal) and git
  runs anonymous-first with no prompts — a stalled credential helper
  fails the install instead of hanging the agent.
- If a source fails all tiers, say which tiers failed and why; never fall
  back to raw file pasting.
