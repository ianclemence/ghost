---
type: reference
tags: [external-repo, arscontexta, architecture]
created: 2026-02-18
path: tmp/arscontexta
---

# Ars Contexta Repository

> **Note**: This is a reference to an external repository cloned for analysis.

## Location
`d:\laragon\www\ghost\tmp\arscontexta`

## Description
Ars Contexta is an "Agentic Knowledge System" generator. It uses a conversation-driven "Derivation Engine" to build a custom knowledge base architecture for a specific domain.

## Key Concepts
- **Three-Space Architecture**: `self/`, `notes/`, `ops/`.
- **Kernel Primitives**: 15 invariants like `markdown-yaml` and `wiki-links`.
- **Derivation Engine**: A meta-skill that interviews the user to generate the system.

## Relevance to Ghost
We are adopting the **Three-Space Architecture** for our `knowledge-base` skill to ensure separation of concerns between agent memory, user knowledge, and system operations.
