import os
import re
import yaml
import argparse
import sys

def find_file(root_dir, filename):
    """
    Search for a file by name (with or without extension) in the given root directory.
    Returns the absolute path if found, None otherwise.
    """
    clean_name = filename.strip("[]")
    base_name, ext = os.path.splitext(clean_name)
    if not ext:
        target_names = [clean_name, clean_name + ".md"]
    else:
        target_names = [clean_name]

    for root, dirs, files in os.walk(root_dir):
        for file in files:
            if file in target_names:
                return os.path.join(root, file)
    return None

def check_frontmatter(content, filepath):
    """
    Check if the file has valid YAML frontmatter.
    Returns (True, None) or (False, error_message).
    """
    if not content.startswith("---"):
        return False, "Missing frontmatter (must start with ---)"
    
    try:
        # Find the second ---
        parts = content.split("---", 2)
        if len(parts) < 3:
            return False, "Incomplete frontmatter (missing closing ---)"
        
        frontmatter = parts[1]
        data = yaml.safe_load(frontmatter)
        
        if not isinstance(data, dict):
             return False, "Frontmatter is not a valid YAML dictionary"
             
        # Check for required fields (Kernel Primitive: markdown-yaml)
        # We enforce 'type', 'created', 'description', and 'tags'/'topics'
        required_fields = ['type', 'created']
        for field in required_fields:
            if field not in data:
                return False, f"Missing required field: '{field}'"
        
        # Enforce description for discovery
        if 'description' not in data:
             return False, "Missing required field: 'description' (Discovery-First)"

        # Enforce taxonomy (tags or topics)
        if 'tags' not in data and 'topics' not in data:
             return False, "Missing required field: 'tags' or 'topics'"
            
        return True, None
    except yaml.YAMLError as e:
        return False, f"Invalid YAML: {e}"
    except Exception as e:
        return False, f"Error parsing frontmatter: {e}"

def check_wikilinks(content, workspace_root):
    """
    Check if all wikilinks resolve to existing files.
    Returns a list of broken links.
    """
    wikilinks = re.findall(r'\[\[(.*?)\]\]', content)
    broken_links = []
    
    for link in wikilinks:
        # Handle piped links [[Target|Text]]
        target = link.split('|', 1)[0]
        
        # Skip example links (used in documentation)
        if target in ["Link Target", "Target", "wikilinks", "like this"]:
             continue

        # Check if target exists
        if not find_file(workspace_root, target):
            broken_links.append(link)
            
    return broken_links

def verify_note(filepath, workspace_root):
    """
    Verify a single note against kernel primitives.
    """
    print(f"Verifying {os.path.basename(filepath)}...")
    issues = []
    
    try:
        with open(filepath, 'r', encoding='utf-8') as f:
            content = f.read()
            
        # 1. Check Frontmatter
        valid_fm, fm_error = check_frontmatter(content, filepath)
        if not valid_fm:
            issues.append(f"Frontmatter Error: {fm_error}")
            
        # 2. Check Wikilinks
        broken_links = check_wikilinks(content, workspace_root)
        if broken_links:
            for link in broken_links:
                issues.append(f"Broken Wikilink: [[{link}]]")
                
    except Exception as e:
        issues.append(f"Read Error: {e}")
        
    return issues

def verify_graph(workspace_root):
    """
    Verify the entire knowledge graph.
    """
    knowledge_root = os.path.join(workspace_root, "knowledge")
    notes_dir = os.path.join(knowledge_root, "notes")
    self_dir = os.path.join(knowledge_root, "self")
    
    all_issues = {}
    
    # Walk through notes/ and self/
    for search_dir in [notes_dir, self_dir]:
        if not os.path.exists(search_dir):
            continue
            
        for root, dirs, files in os.walk(search_dir):
            for file in files:
                if file.endswith(".md"):
                    filepath = os.path.join(root, file)
                    issues = verify_note(filepath, workspace_root)
                    if issues:
                        rel_path = os.path.relpath(filepath, workspace_root)
                        all_issues[rel_path] = issues
                        
    return all_issues

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Verify Knowledge Graph Integrity.")
    parser.add_argument("--root", default=".", help="Root directory for the workspace.")
    parser.add_argument("--note", help="Verify a specific note (filename).")
    
    args = parser.parse_args()
    
    print("\n--- Knowledge Graph Verification ---\n")
    
    if args.note:
        # Verify single note
        knowledge_root = os.path.join(args.root, "knowledge")
        filepath = find_file(knowledge_root, args.note)
        if filepath:
            issues = verify_note(filepath, args.root)
            if issues:
                print(f"\nIssues found in {args.note}:")
                for issue in issues:
                    print(f"- {issue}")
            else:
                print(f"\n{args.note} is valid.")
        else:
            print(f"Error: Note '{args.note}' not found.")
    else:
        # Verify all
        all_issues = verify_graph(args.root)
        
        if all_issues:
            print(f"\nFound issues in {len(all_issues)} files:\n")
            for filepath, issues in all_issues.items():
                print(f"File: {filepath}")
                for issue in issues:
                    print(f"  - {issue}")
                print("")
            print("Verification Failed.")
            sys.exit(1)
        else:
            print("\nAll checks passed. Knowledge Graph is healthy.")
            sys.exit(0)
