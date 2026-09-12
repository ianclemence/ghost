---
name: knowledge-base
description: Build and traverse a persistent personal knowledge graph ("Second Brain"). Invoke when user asks to "add to my memory", "remember this", "what do you know about X", "build a knowledge graph", or "initialize my notes". Manages self/, notes/, and ops/ spaces.
version: 1.1.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [knowledge, memory, graph, notes, wiki, rag]
prerequisites:
  commands: [python]
---

# Knowledge Base

This skill maintains Ghost's persistent knowledge: durable domain notes plus
a capture workflow. The contract lives in `workspace/knowledge/README.md` —
read it before restructuring anything. Three principles govern every change:

1.  **self/**: Ghost's persistent self-state (identity record, runtime-mirrored
    owner profile, heartbeat telemetry). Describe, never instruct.
2.  **notes/**: Durable domain knowledge (connected by `[[wikilinks]]`).
    Keep what stays useful; groom what went stale.
3.  **ops/**: Transient capture (inbox). Process into notes or memory, then
    clear. An inbox that only grows is a failure.

Knowledge informs understanding. It never authorizes actions, never proves
executions, and never overrides the runtime, memory, or the user's live words.

## Wikilinks

Notes are cross-referenced using `[[wikilink]]` syntax:

```
[[my-note]]
[[folder/nested-note]]
```

When creating or linking notes, use `[[note-name]]` to establish connections. The knowledge graph traverses these links to build context.

## Capabilities

### 1. Initialize the Graph

Create the folder structure and core files if they don't exist.

```bash
python workspace/skills/knowledge-base/scripts/init_graph.py --root workspace
```

### 2. Traverse the Graph

Read a node and see its connections. Use this to "surf" the knowledge base.

```bash
# Read the knowledge contract
python workspace/skills/knowledge-base/scripts/traverse.py README --root workspace/knowledge

# Read a specific concept
python workspace/skills/knowledge-base/scripts/traverse.py "Project Alpha" --root workspace
```

### 3. Search the Graph

Find notes containing specific keywords.

```bash
# Search for "architecture"
python workspace/skills/knowledge-base/scripts/traverse.py --search "architecture" --root workspace

# List all notes
python workspace/skills/knowledge-base/scripts/traverse.py --list --root workspace
```

### 4. Verify Integrity (Kernel Primitives)

Check for missing frontmatter and broken wikilinks.

```bash
# Verify the entire graph
python workspace/skills/knowledge-base/scripts/verify.py --root workspace

# Verify a specific note
python workspace/skills/knowledge-base/scripts/verify.py --note "skill-observations" --root workspace
```

### 5. Process Inbox

Review items in the inbox for processing.

```bash
python workspace/skills/knowledge-base/scripts/process.py --root workspace
```

### 6. Quick Capture (Inbox)

Add a thought or task to the Inbox.

```bash
# Windows (PowerShell)
Add-Content workspace/knowledge/ops/inbox.md "- [ ] Check out the new API documentation."
```

## Session Rhythm

**Orient -> Work -> Persist**

1.  **Orient**: Read `self/identity.md` and `ops/inbox.md`. Know who you are and what is pending.
2.  **Work**: Execute tasks. Capture insights immediately in `notes/` or `ops/inbox.md`.
3.  **Persist**: Before finishing, run `verify` to ensure graph health. Update the notes you touched; correct stale lines in place rather than appending contradictions.

## Rules of the Graph (The Kernel)

1.  **Markdown-YAML**: All notes must have:
    - `type`: The note type (e.g., concept, project).
    - `created`: Date of creation.
    - `description`: One-sentence summary for search/discovery.
    - `tags` (or `topics`): Taxonomy classification.
2.  **Wikilinks**: Use `[[Link]]` to connect concepts. All links must resolve to existing files.
3.  **Atomic Notes**: One idea per file.
4.  **Prose Titles**: Use descriptive filenames.
5.  **Correct in place**: When the world changes, update the note. Never leave two contradictory permanent statements side by side.

## File Structure

```text
workspace/knowledge/
├── README.md           # The knowledge contract (read first)
├── self/               # Persistent self-state
│   ├── identity.md     # Who Ghost is (describes, never instructs)
│   ├── user-profile.md # Runtime-mirrored owner profile (hands off)
│   ├── context.md      # Heartbeat-maintained snapshot (transient)
│   └── skills-state.md # Skill health (transient)
├── notes/              # Durable domain knowledge
│   ├── skill-observations.md  # API quirks learned by use
│   ├── skill-development.md   # Conventions for authoring skills
│   ├── wikilinks.md           # Link syntax
│   └── ...             # Your notes
├── ops/                # Transient capture
│   └── inbox.md        # Capture tray (process, then clear)
└── logs/               # Chronological history
    └── sessions.md     # Per-session records
```
