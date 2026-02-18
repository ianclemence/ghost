---
name: knowledge-base
description: Manage and traverse the agent's persistent knowledge graph (Three-Space Architecture).
---

# Knowledge Base Skill

This skill enables the agent to maintain a persistent "Second Brain" using the **Three-Space Architecture**:
1.  **self/**: Identity, methodology, and long-term memory.
2.  **notes/**: The core knowledge graph (connected by `[[wikilinks]]`).
3.  **ops/**: Operational state (inbox, tasks, logs).

## Capabilities

### 1. Initialize the Graph
Create the folder structure and core files if they don't exist.

```bash
python workspace/skills/knowledge-base/scripts/init_graph.py --root workspace
```

### 2. Traverse the Graph
Read a node and see its connections. Use this to "surf" the knowledge base.

```bash
# Read the root index
python workspace/skills/knowledge-base/scripts/traverse.py index --root workspace

# Read a specific concept
python workspace/skills/knowledge-base/scripts/traverse.py "Project Alpha" --root workspace
```

### 3. Quick Capture (Inbox)
Add a thought or task to the Inbox.

```bash
# Windows (PowerShell)
Add-Content workspace/knowledge/ops/inbox.md "- [ ] Check out the new API documentation."
```

## Rules of the Graph
1.  **Link Generously**: Every note should link to at least one other note (`[[Parent]]` or `[[Child]]`).
2.  **Atomic Notes**: One idea per file.
3.  **Prose Titles**: Use descriptive filenames like `Agile Methodology.md` instead of `agile.md`.
4.  **Frontmatter**: All notes must have a YAML header with `type` and `created` date.

## File Structure
```text
workspace/knowledge/
├── self/               # Agent Memory
│   ├── identity.md     # Who I am
│   └── methodology.md  # How I work
├── notes/              # Knowledge Graph
│   ├── index.md        # Entry point
│   └── ...             # Your notes
└── ops/                # System State
    ├── inbox.md        # Quick capture
    └── tasks.md        # Task queue
```
