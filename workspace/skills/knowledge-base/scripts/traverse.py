import os
import re
import argparse
import sys

def find_file(root_dir, filename):
    """
    Search for a file by name (with or without extension) in the given root directory.
    Returns the absolute path if found, None otherwise.
    """
    # Normalize filename: remove brackets if present
    clean_name = filename.strip("[]")
    
    # Check for extension
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

def extract_links(content):
    """
    Extract wikilinks [[Link]] and markdown links [Link](path) from content.
    Returns a list of (link_text, link_target) tuples.
    """
    wikilinks = re.findall(r'\[\[(.*?)\]\]', content)
    mdlinks = re.findall(r'\[(.*?)\]\((.*?)\)', content)
    
    links = []
    for link in wikilinks:
        # Handle piped links [[Target|Text]]
        if '|' in link:
            target, text = link.split('|', 1)
            links.append((text, target))
        else:
            links.append((link, link))
            
    for text, target in mdlinks:
        links.append((text, target))
        
    return links

def read_node(filepath):
    """
    Read a markdown node and return its content and outgoing links.
    """
    try:
        with open(filepath, 'r', encoding='utf-8') as f:
            content = f.read()
            return content
    except Exception as e:
        print(f"Error reading file {filepath}: {e}")
        return None

def traverse(start_node, workspace_root):
    """
    Start traversing from a specific node.
    """
    current_path = find_file(workspace_root, start_node)
    
    if not current_path:
        # Try relative to knowledge/notes if not found in root
        notes_root = os.path.join(workspace_root, "knowledge", "notes")
        current_path = find_file(notes_root, start_node)
        
        if not current_path:
             # Try relative to knowledge/self
            self_root = os.path.join(workspace_root, "knowledge", "self")
            current_path = find_file(self_root, start_node)

    if not current_path:
        print(f"Error: Could not find node '{start_node}' in workspace.")
        return

    print(f"\n--- Reading Node: {os.path.basename(current_path)} ---\n")
    content = read_node(current_path)
    if content:
        # Print first 500 chars as preview
        print(content[:500] + "...\n" if len(content) > 500 else content + "\n")
        
        links = extract_links(content)
        if links:
            print("--- Outgoing Links ---")
            for i, (text, target) in enumerate(links):
                print(f"{i+1}. {text} -> {target}")
        else:
            print("(No outgoing links found)")
            
    return current_path

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Traverse the Knowledge Graph.")
    parser.add_argument("node", help="The name of the node (file) to read (e.g., 'index', 'Projects').")
    parser.add_argument("--root", default=".", help="Root directory for the workspace.")
    
    args = parser.parse_args()
    traverse(args.node, args.root)
