---
name: notion
description: Search the Notion pages shared with the Ghost integration. Invoke when user asks to "find the doc about X", "search my notes for Y", or "look in Notion for Z". Requires a Notion integration token with pages shared to it.
version: 1.0.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [notion, docs, notes, search]
    connector: true
prerequisites:
  connected_apps: [notion]
---

# Notion Docs Search

> **Preferred path:** call the `docs_search` tool first. It is governed — evidence,
> approvals, and honest provider failures — and needs no shell. The tool uses the connected Notion integration token and only sees pages shared with it.
>
> The commands in this skill are the fallback, not the default.

Call the `docs_search` tool with `query` — it uses the connected Notion
integration token internally. Only pages shared with the integration are
visible; sharing is the access control.

If Notion isn't connected, direct the user to Connected Apps → paste an
integration token and share the pages Ghost should search.
