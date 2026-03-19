---
name: shopping
description: Maintain a persistent shopping list. Invoke when user says "add X to my shopping list", "what do I need to buy", "remove X from the list", or "clear my shopping list". Items persist in workspace/data/shopping_list.txt.
version: 1.0.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [shopping, todo, list, productivity]
prerequisites:
  commands: []
---

# Shopping List

A simple file-based shopping list manager.

## Storage

Items are stored in `workspace/data/shopping_list.txt`.

## Commands

### Add Item

Appends an item to the list.

```bash
# Windows
echo "Milk" >> workspace/data/shopping_list.txt

# Linux/Pi
mkdir -p workspace/data && echo "Milk" >> workspace/data/shopping_list.txt
```

### View List

Reads the current list.

```bash
# Windows/Linux
type workspace/data/shopping_list.txt 2>nul || cat workspace/data/shopping_list.txt 2>/dev/null || echo "List is empty."
```

### Clear List

Clears all items.

```bash
# Windows
echo. > workspace/data/shopping_list.txt

# Linux/Pi
echo "" > workspace/data/shopping_list.txt
```
