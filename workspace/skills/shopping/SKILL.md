---
name: "shopping"
description: "Manages a simple shopping list. Invoke when user says 'Add to list', 'What do I need to buy', or 'Remove from list'."
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
