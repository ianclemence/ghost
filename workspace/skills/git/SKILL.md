---
name: "git"
description: "Manages Git repositories. Invoke when user asks 'Check git status', 'Commit changes', 'Push code', or 'List branches'."
---

# Git Manager

Controls git repositories in the current workspace.

## Commands

### Status
Check what has changed.
```bash
git status
```

### Log
See recent commits.
```bash
git log --oneline -n 5
```

### Commit All
Adds all changes and commits with a message.
```bash
git add . && git commit -m "Update from Ghost"
```

### Push
Push changes to remote.
```bash
git push
```

### List Branches
```bash
git branch -a
```
