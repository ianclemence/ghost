---
name: github
description: Search code across the repositories your connected GitHub account can see. Invoke when user asks to "find where X is defined", "search the codebase for Y", or "look at repo Z". Prefers the connected GitHub account (read-only token) when available; falls back to gh CLI.
version: 1.0.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [github, code, search, repository]
    connector: true
prerequisites:
  connected_apps: [github]
  commands: [gh]
---

# GitHub Code Search

> **Preferred path:** call the `code_search` tool first. It is governed — evidence,
> approvals, and honest provider failures — and needs no shell. Use it for searching code in the connected GitHub account; the `gh` CLI and the workflow skills below are the fallback for actions code search does not cover.
>
> The commands in this skill are the fallback, not the default.

Call the `code_search` tool with `query` (and optional `repo: owner/name`)
— it uses the connected GitHub token internally. Trust-user model: use a
read-only token; the token's own scopes govern what Ghost can see.

If GitHub isn't connected, direct the user to Connected Apps → paste a
personal access token, or fall back to `gh` CLI (`gh search code`, `gh repo
view`) when authenticated locally.
