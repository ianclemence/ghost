---
name: git
description: Execute Git operations on repositories. Invoke when user asks to "check git status", "commit changes", "push to remote", "create a branch", "merge branch X into Y", "view git log", "diff changes", "stash", or any git workflow. Requires git binary installed.
version: 1.0.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [git, version-control, repository, commit, branch]
prerequisites:
  commands: [git]
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
