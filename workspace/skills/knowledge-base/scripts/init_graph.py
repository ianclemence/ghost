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
    # Seeds the knowledge tree per workspace/knowledge/README.md:
    # identity record, notes space, and capture inbox. Operational
    # telemetry (context, skills-state), the owner profile, and task
    # queues are created by the runtime flows that own them — never here.
    knowledge_root = os.path.join(workspace_root, "knowledge")

    # 1. self/ - persistent self-state
    self_dir = os.path.join(knowledge_root, "self")
    create_directory(self_dir)

    identity_content = """---
type: identity
created: {date}
---

# Ghost Identity

Ghost is one persistent personal AI for its owner — the same Ghost across
conversations, channels, restarts, and underlying model or provider changes.
Ghost reasons inside a runtime that governs capabilities, permissions,
execution, evidence, and durable state. This record describes identity; it
authorizes nothing and instructs nothing.
""".format(date=datetime.date.today())
    create_file(os.path.join(self_dir, "identity.md"), identity_content)

    # 2. notes/ - durable domain knowledge
    notes_dir = os.path.join(knowledge_root, "notes")
    create_directory(notes_dir)

    # 3. ops/ - transient capture
    ops_dir = os.path.join(knowledge_root, "ops")
    create_directory(ops_dir)

    inbox_content = """---
type: inbox
created: {date}
---

# Inbox

Capture raw thoughts, tasks, and ideas here. Process them into `notes/` or
memory, then clear them. An inbox that only grows is a failure mode.
""".format(date=datetime.date.today())
    create_file(os.path.join(ops_dir, "inbox.md"), inbox_content)

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
