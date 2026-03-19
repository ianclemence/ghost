---
name: git
description: Execute Git operations on repositories. Invoke when user asks to "check git status", "commit changes", "push to remote", "create a branch", "merge branch X into Y", "view git log", "diff changes", "stash", "reset", or any git workflow. Requires git binary installed.
version: 1.1.0
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

## Quick Reference

| Task | Command |
|------|---------|
| Status | `git status` |
| Staged diff | `git diff --cached` |
| Unstaged diff | `git diff` |
| Diff main..HEAD | `git diff main...HEAD` |
| Log (5 entries) | `git log --oneline -n 5` |
| Stash | `git stash push -m "message"` |
| Stash pop | `git stash pop` |
| Commit all | `git add . && git commit -m "message"` |
| Amend last commit | `git commit --amend --no-edit` |
| Soft reset | `git reset --soft HEAD~1` |
| Push | `git push` |
| Push to new branch | `git push -u origin HEAD` |
| Pull (rebase) | `git pull --rebase origin $(git branch --show-current)` |
| Switch branch | `git switch main` |
| Create branch | `git switch -c new-branch` |
| Delete local branch | `git branch -d old-branch` |
| Delete remote branch | `git push origin --delete remote-branch` |
| Fetch all | `git fetch --all` |
| Rebase onto main | `git rebase origin/main` |
| Merge branch | `git merge feature-branch --no-ff` |
| Rebase-interactive (squash) | `git rebase -i HEAD~3` |

## Status and Diff

### Status

```bash
git status
```

### What changed (staged vs unstaged)

```bash
git diff         # unstaged changes
git diff --cached  # staged changes
```

### Diff against main (or any branch)

```bash
git diff main...HEAD   # what is on this branch that main doesn't have
git diff main..HEAD    # full diff including merge-base
```

### Diff a specific file

```bash
git diff -- path/to/file.txt
git diff --cached -- path/to/file.txt
```

## Commit Workflow

### Commit all changes

```bash
git add . && git commit -m "Describe what changed"
```

### Stage specific files only

```bash
git add path/to/file1 path/to/file2
git commit -m "Partial commit message"
```

### Amend the last commit (add forgotten files or fix message)

```bash
git add forgotten_file
git commit --amend --no-edit   # keep same message, add staged changes
git commit --amend -m "Fixed message"  # change message too
```

### Soft reset (undo commit, keep changes staged)

```bash
git reset --soft HEAD~1   # undo last commit, files stay staged
```

Use this when you committed too early and want to keep working on that commit.

## Branching

### Switch branch

```bash
git switch main
git switch -c new-branch   # create and switch in one step
```

### Delete a branch

```bash
git branch -d branch-to-delete        # safe (refuses if unmerged)
git branch -D branch-to-delete        # force delete
git push origin --delete remote-branch  # delete remote
```

### Rename current branch

```bash
git branch -m new-name
```

## Stash

### Stash all changes

```bash
git stash push -m "WIP: working on feature X"
```

### List stashes

```bash
git stash list
```

### Restore most recent stash (and remove it)

```bash
git stash pop
```

### Restore a specific stash (keep it in list)

```bash
git stash apply stash@{2}
```

### Drop a stash

```bash
git stash drop stash@{0}
```

## Push and Pull

### Push current branch

```bash
git push
```

### Push to a new remote branch (set upstream)

```bash
git push -u origin HEAD
```

### Pull with rebase (keeps history linear)

```bash
git pull --rebase origin $(git branch --show-current)
```

### Pull with merge (creates merge commit)

```bash
git pull origin $(git branch --show-current)
```

**Rule**: prefer `--rebase` on feature branches, merge on main.

## Merge and Rebase

### Merge a branch into current

```bash
git merge feature-branch --no-ff
```

`--no-ff` creates a merge commit even if fast-forward is possible — keeps history clear.

### Rebase current onto main

```bash
git fetch origin main
git rebase origin/main
```

**Rule**: never rebase commits that have been pushed to a shared remote.

## Rebase Interactive (Squash)

### Squash last 3 commits into one

```bash
git rebase -i HEAD~3
```

In the editor, change `pick` to `squash` (or `s`) for commits you want to combine. First commit's message is used by default — edit or keep as-is.

## Working with Remote

### Verify remote is configured

```bash
git remote -v
```

### Update remote URL

```bash
git remote set-url origin https://github.com/user/repo.git
```

### Fetch without merging

```bash
git fetch --all
```

### Check if branch is ahead/behind remote

```bash
git status -sb   # shows branch name and ahead/behind count
```

## Detached HEAD

When you check out a commit (not a branch), you are in detached HEAD state. Changes work normally but are not on any branch.

To save work done in detached HEAD:

```bash
git checkout -b saved-branch-name   # create branch from current commit
```

To exit detached HEAD back to a branch:

```bash
git switch main
```

## Conflict Resolution

When a merge or rebase produces conflicts:

1. `git status` shows conflicted files
2. Edit each conflicted file — markers look like `<<<<<<< HEAD`, `=======`, `>>>>>>> branch`
3. Resolve conflicts, remove markers
4. `git add resolved-file.txt`
5. `git commit` (for merge) or `git rebase --continue` (for rebase)

Abort if needed: `git merge --abort` or `git rebase --abort`

## Finding Things

### Search commits for keyword

```bash
git log --oneline --grep="fix bug"
```

### Show commits touching a specific file

```bash
git log --oneline -- path/to/file.txt
```

### Show who last modified each line

```bash
git blame path/to/file.txt
```
