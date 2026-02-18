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
python workspace/skills/knowledge-base/scripts/verify.py --note "index" --root workspace
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

## Rules of the Graph (The Kernel)

1.  **Markdown-YAML**: All notes must have valid YAML frontmatter with `type` and `created`.
2.  **Wikilinks**: Use `[[Link]]` to connect concepts. All links must resolve to existing files.
3.  **MOC Hierarchy**: Use Maps of Content (MOCs) like `index.md` to organize notes.
4.  **Atomic Notes**: One idea per file.
5.  **Prose Titles**: Use descriptive filenames.

## File Structure

```text
workspace/knowledge/
├── self/               # Agent Memory
│   ├── identity.md     # Who I am
│   └── methodology.md  # How I work
├── notes/              # Knowledge Graph
│   ├── index.md        # Entry point
│   ├── references/     # Ingested docs
│   └── ...             # Your notes
└── ops/                # System State
    ├── inbox.md        # Quick capture
    └── tasks.md        # Task queue
```
