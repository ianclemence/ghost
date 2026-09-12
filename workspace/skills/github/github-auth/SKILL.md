---
name: github-auth
description: Set up GitHub authentication for the agent using git (universally available) or the gh CLI. Covers HTTPS tokens, SSH keys, credential helpers, and gh auth — with a detection flow to pick the right method automatically.
version: 1.1.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [GitHub, Authentication, Git, gh-cli, SSH, Setup]
    related_skills:
      [
        github-pr-workflow,
        github-code-review,
        github-issues,
        github-repo-management,
      ]
---

# GitHub Authentication Setup

This skill sets up authentication so the agent can work with GitHub repositories, PRs, issues, and CI. It covers two paths:

- **`git` (always available)** — uses HTTPS personal access tokens or SSH keys
- **`gh` CLI (if installed)** — richer GitHub API access with a simpler auth flow

## Detection Flow

When a user asks you to work with GitHub, run this check first:

```bash
# Check what's available
git --version
gh --version 2>/dev/null || echo "gh not installed"

# Check if already authenticated
gh auth status 2>/dev/null || echo "gh not authenticated"
git config --global credential.helper 2>/dev/null || echo "no git credential helper"
```

**Decision tree:**

1. If `gh auth status` shows authenticated → you're good, use `gh` for everything
2. If `gh` is installed but not authenticated → use "gh auth" method below
3. If `gh` is not installed → use "git-only" method below (no sudo needed)

---

## Method 1: Git-Only Authentication (No gh, No sudo)

This works on any machine with `git` installed. No root access needed.

### Option A: HTTPS with Personal Access Token (Recommended)

This is the most portable method — works everywhere, no SSH config needed.

**Step 1: Create a personal access token**

New GitHub connections should go through Ghost settings (Integrations),
which stores the credential in the runtime vault — never in chat, files,
or shell history. If guiding a manual setup, tell the user to go to:
**https://github.com/settings/tokens**

- Click "Generate new token (classic)"
- Give it a name like "ghost-agent"
- Select scopes:
  - `repo` (full repository access — read, write, push, PRs)
  - `workflow` (trigger and manage GitHub Actions)
  - `read:org` (if working with organization repos)
- Set expiration (90 days is a good default)
- Copy the token — it won't be shown again

**Step 2: Configure git to store the token**

Prefer the authenticated `gh` flow (Method 2 below) or Ghost settings
(Integrations), which keep the credential in the vault. As a local-only
fallback on machines without `gh`, use git's in-memory cache so the token
never touches disk:

```bash
# Cache in memory for 8 hours (28800 seconds) instead of saving to disk.
# Never use the plaintext "store" helper: it writes secrets to
# ~/.git-credentials where any process can read them.
git config --global credential.helper 'cache --timeout=28800'

# Now do a test operation that triggers auth — git will prompt for credentials
# Username: <their-github-username>
# Password: <the personal access token, NOT their GitHub password>
git ls-remote https://github.com/<their-username>/<any-repo>.git
```

After entering credentials once, they're cached in memory and reused until
the timeout. Never print, log, or echo a token, and never place one in a
remote URL (URLs leak into shell history and logs).

**Step 3: Configure git identity**

```bash
# Required for commits — set name and email
git config --global user.name "Their Name"
git config --global user.email "their-email@example.com"
```

**Step 4: Verify**

```bash
# Test push access (this should work without any prompts now)
git ls-remote https://github.com/<their-username>/<any-repo>.git

# Verify identity
git config --global user.name
git config --global user.email
```

### Option B: SSH Key Authentication

Good for users who prefer SSH or already have keys set up.

**Step 1: Check for existing SSH keys**

```bash
ls -la ~/.ssh/id_*.pub 2>/dev/null || echo "No SSH keys found"
```

**Step 2: Generate a key if needed**

```bash
# Generate an ed25519 key (modern, secure, fast)
ssh-keygen -t ed25519 -C "their-email@example.com" -f ~/.ssh/id_ed25519 -N ""

# Display the public key for them to add to GitHub
cat ~/.ssh/id_ed25519.pub
```

Tell the user to add the public key at: **https://github.com/settings/keys**

- Click "New SSH key"
- Paste the public key content
- Give it a title like "ghost-agent-<machine-name>"

**Step 3: Test the connection**

```bash
ssh -T git@github.com
# Expected: "Hi <username>! You've successfully authenticated..."
```

**Step 4: Configure git to use SSH for GitHub**

```bash
# Rewrite HTTPS GitHub URLs to SSH automatically
git config --global url."git@github.com:".insteadOf "https://github.com/"
```

**Step 5: Configure git identity**

```bash
git config --global user.name "Their Name"
git config --global user.email "their-email@example.com"
```

---

## Method 2: gh CLI Authentication

If `gh` is installed, it handles both API access and git credentials in one step.

### Interactive Browser Login (Desktop)

```bash
gh auth login
# Select: GitHub.com
# Select: HTTPS
# Authenticate via browser
```

### Token-Based Login (Headless / SSH Servers)

On a headless machine, run `gh auth login` and choose the device-code flow
— the user approves in a browser and no token is ever typed, pasted, or
stored by Ghost:

```bash
gh auth login
# choose GitHub.com → HTTPS → Login with a web browser (device code)

# Set up git credentials through gh
gh auth setup-git
```

### Verify

```bash
gh auth status
```

---

## Using the GitHub API Without gh

When `gh` is not available, you can still access the GitHub API using `curl`
with a personal access token. This is how the other GitHub skills implement
their fallbacks.

### Setting the Token for API Calls

```bash
# Export as env var for the single command (preferred — keeps it out of
# history and logs). Never print or echo the value.
GITHUB_TOKEN="<token>" curl -s -H "Authorization: token $GITHUB_TOKEN" \
  https://api.github.com/user
```

Never read tokens out of `~/.git-credentials` or any other credential
store: scraping stored secrets bypasses the user's credential boundary.
If no token is available in the environment, stop and point at setup
(Ghost settings → Integrations, or `gh auth login`) instead of hunting
for one on disk.

### Helper: Detect Auth Method

Use this pattern at the start of any GitHub workflow:

```bash
# Try gh first, fall back to git + curl
if command -v gh &>/dev/null && gh auth status &>/dev/null; then
  echo "AUTH_METHOD=gh"
elif [ -n "$GITHUB_TOKEN" ]; then
  echo "AUTH_METHOD=curl"
else
  echo "AUTH_METHOD=none"
  echo "Need authentication first (gh auth login or Ghost settings -> Integrations)"
  exit 1
fi
```

---

## Troubleshooting

| Problem                                                       | Solution                                                                                                        |
| ------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| `git push` asks for password                                  | GitHub disabled password auth. Use a personal access token as the password, or switch to SSH                    |
| `remote: Permission to X denied`                              | Token may lack `repo` scope — regenerate with correct scopes                                                    |
| `fatal: Authentication failed`                                | Cached credentials may be stale — run `git credential reject` then re-authenticate                              |
| `ssh: connect to host github.com port 22: Connection refused` | Try SSH over HTTPS port: add `Host github.com` with `Port 443` and `Hostname ssh.github.com` to `~/.ssh/config` |
| Credentials not persisting | Check `git config --global credential.helper` — prefer `cache` (memory-only); avoid `store` (plaintext on disk) |
| Multiple GitHub accounts                                      | Use SSH with different keys per host alias in `~/.ssh/config`, or per-repo credential URLs                      |
| `gh: command not found` + no sudo                             | Use git-only Method 1 above — no installation needed                                                            |
