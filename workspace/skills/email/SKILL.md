---
name: email
description: Read, search, send, and manage email via connected Gmail/Outlook apps or local IMAP/SMTP. Invoke when user asks to "check my email", "send an email", "list unread emails", "search emails", "reply to email", or "compose a new email". Prefers connected apps (Gmail/Outlook OAuth) when available; falls back to himalaya CLI with IMAP/SMTP credentials.
version: 1.0.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [email, gmail, outlook, imap, smtp, communication]
    connector: true
prerequisites:
  connected_apps: [gmail, outlook]
  commands: [himalaya]
---

# Email

Email lives behind connected apps. Never ask for passwords in chat.

## Preferred: connected apps (Gmail / Outlook)

1. Check Connected Apps (`gmail`, `outlook`) status first.
2. If not connected, direct the user to Connected Apps → browser sign-in, then pull to refresh.
3. Sending always requires broker approval and returns acknowledgement evidence (`message_id`, `timestamp`) before claiming Done.
4. Filter one-time codes, password-reset links, and magic links before showing body to the model.

## Fallback: himalaya CLI (IMAP/SMTP)

Requires `himalaya` binary + `~/.config/himalaya/config.toml` with IMAP/SMTP
credentials. See `email/himalaya/references/configuration.md` for setup and
`email/himalaya/references/message-composition.md` for MML composition.

```bash
himalaya envelope list -m INBOX -n 20
himalaya message read <id>
himalaya message write --to user@example.com --subject "Hi" --body "Hello"
```

If neither a connected app nor himalaya is configured, tell the user exactly
what to connect rather than guessing.
