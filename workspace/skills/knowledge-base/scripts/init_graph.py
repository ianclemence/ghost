import os
import argparse
import datetime

def create_directory(path):
    if not os.path.exists(path):
        os.makedirs(path)
        print(f"Created directory: {path}")
    else:
        print(f"Directory already exists: {path}")

def create_file(path, content):
    if not os.path.exists(path):
        with open(path, 'w', encoding='utf-8') as f:
            f.write(content)
        print(f"Created file: {path}")
    else:
        print(f"File already exists: {path}")

def init_graph(workspace_root):
    # Define the Three-Space Architecture
    knowledge_root = os.path.join(workspace_root, "knowledge")
    
    # 1. self/ - Agent Memory
    self_dir = os.path.join(knowledge_root, "self")
    create_directory(self_dir)
    
    identity_content = """---
type: identity
created: {date}
---

# Agent Identity

I am an autonomous agent operating within the Ghost environment.
My purpose is to assist the user by executing tasks, managing knowledge, and maintaining a coherent system state.

## Core Directives
1. **Be Helpful**: Prioritize user intent.
2. **Be Safe**: Do not delete data without confirmation.
3. **Be Organized**: Maintain the integrity of the knowledge graph.
""".format(date=datetime.date.today())
    create_file(os.path.join(self_dir, "identity.md"), identity_content)

    methodology_content = """---
type: methodology
created: {date}
---

# Methodology

This file documents the operational principles of this knowledge graph.

## Three-Space Architecture
- **self/**: My persistent memory (identity, goals, methodology).
- **notes/**: The user's knowledge graph (interconnected markdown files).
- **ops/**: Transient operational state (inbox, tasks, logs).

## Wikilinks
We use `[[WikiLinks]]` to connect concepts. 
- Use prose-friendly titles.
- Link generously to build context.
""".format(date=datetime.date.today())
    create_file(os.path.join(self_dir, "methodology.md"), methodology_content)

    # 2. notes/ - Knowledge Graph
    notes_dir = os.path.join(knowledge_root, "notes")
    create_directory(notes_dir)
    
    index_content = """---
type: moc
created: {date}
tags: [entry-point, root]
---

# Knowledge Graph Root

Welcome to the knowledge graph. This is the entry point for all traversal.

## Main Maps of Content (MOCs)
- [[Projects]] - Active and archived projects.
- [[Areas]] - Areas of responsibility.
- [[Resources]] - Reference materials and guides.
- [[Archives]] - Completed or inactive items.

## Inbox
- [[../ops/inbox|Inbox]] - Unprocessed items.
""".format(date=datetime.date.today())
    create_file(os.path.join(notes_dir, "index.md"), index_content)

    # 3. ops/ - Operational State
    ops_dir = os.path.join(knowledge_root, "ops")
    create_directory(ops_dir)
    
    inbox_content = """---
type: inbox
created: {date}
---

# Inbox

Capture raw thoughts, tasks, and ideas here. Process them into `notes/` or `self/` later.

- [ ] Review the new knowledge graph structure.
""".format(date=datetime.date.today())
    create_file(os.path.join(ops_dir, "inbox.md"), inbox_content)

    tasks_content = """---
type: tasks
created: {date}
---

# Task Queue

- [ ] Initialize knowledge graph structure.
""".format(date=datetime.date.today())
    create_file(os.path.join(ops_dir, "tasks.md"), tasks_content)

    print("\nKnowledge Graph initialized successfully in 'workspace/knowledge/'.")

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Initialize the Knowledge Graph structure.")
    # Assuming the script is run from workspace/skills/knowledge-base/scripts
    # We want the root to be workspace/
    # script_dir = os.path.dirname(os.path.abspath(__file__))
    # workspace_root = os.path.abspath(os.path.join(script_dir, "../../../"))
    
    # Simpler: pass the workspace root as an argument or default to current directory
    parser.add_argument("--root", default=".", help="Root directory for the workspace (default: current directory)")
    
    args = parser.parse_args()
    init_graph(args.root)
