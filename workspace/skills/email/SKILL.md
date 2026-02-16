---
name: "email"
description: "Reads unread emails from IMAP. Invoke when user asks 'Do I have any emails?', 'Check inbox', or 'Read my emails'."
---

# Email Reader

Fetches unread emails via IMAP.

## Requirements

- **Tool**: `curl` (built-in on Windows/Linux)
- **Configuration**: Add these to `.env`:
  ```bash
  EMAIL_HOST=imap.gmail.com
  EMAIL_USER=your_email@gmail.com
  EMAIL_PASS=your_app_password
  ```

## Commands

### Check Unread Emails (Subject Only)

Uses `curl` to fetch unread message headers.

```bash
curl --url "imaps://$EMAIL_HOST/INBOX" --user "$EMAIL_USER:$EMAIL_PASS" -X "FETCH 1:* (BODY[HEADER.FIELDS (SUBJECT FROM)])"
```

### Search for Specific Sender

```bash
curl --url "imaps://$EMAIL_HOST/INBOX" --user "$EMAIL_USER:$EMAIL_PASS" -X "SEARCH UNSEEN FROM \"Amazon\""
```
