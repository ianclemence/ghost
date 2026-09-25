---
name: gog
description: Google Workspace CLI for Gmail, Calendar, Drive, Contacts, Sheets, and Docs.
homepage: https://gogcli.sh
prerequisites:
  commands: [gog]
---

# gog

Use `gog` for Gmail/Calendar/Drive/Contacts/Sheets/Docs. Requires OAuth setup.

Setup
- Everyday use needs no terminal: mail and calendar already work through
  Ghost's Connected Apps (Gmail + Google Calendar). For non-technical users,
  say "connect it in Ghost settings under Connected Apps" — never hand them
  setup commands.
- The CLI extras (Drive, Sheets, Docs, Contacts) need a one-time Google
  sign-in on this device — techies only:
  - `gog auth credentials /path/to/client_secret.json`
  - `gog auth add you@gmail.com --services gmail,calendar,drive,contacts,sheets,docs`
  - `gog auth list` must show the account, not "No tokens stored"
- If `gog auth list` reports "No tokens stored", stop and reply: "Google
  Workspace isn't connected on this device yet — mail and calendar work from
  Connected Apps; the Drive/Sheets/Docs extras need a one-time sign-in."
  Never create credentials, tokens, or config files yourself.

Common commands
- Gmail search: `gog gmail search 'newer_than:7d' --max 10`
- Gmail send: `gog gmail send --to a@b.com --subject "Hi" --body "Hello"`
- Calendar: `gog calendar events <calendarId> --from <iso> --to <iso>`
- Drive search: `gog drive search "query" --max 10`
- Contacts: `gog contacts list --max 20`
- Sheets get: `gog sheets get <sheetId> "Tab!A1:D10" --json`
- Sheets update: `gog sheets update <sheetId> "Tab!A1:B2" --values-json '[["A","B"],["1","2"]]' --input USER_ENTERED`
- Sheets append: `gog sheets append <sheetId> "Tab!A:C" --values-json '[["x","y","z"]]' --insert INSERT_ROWS`
- Sheets clear: `gog sheets clear <sheetId> "Tab!A2:Z"`
- Sheets metadata: `gog sheets metadata <sheetId> --json`
- Docs export: `gog docs export <docId> --format txt --out /tmp/doc.txt`
- Docs cat: `gog docs cat <docId>`

Notes
- Set `GOG_ACCOUNT=you@gmail.com` to avoid repeating `--account`.
- For scripting, prefer `--json` plus `--no-input`.
- Sheets values can be passed via `--values-json` (recommended) or as inline rows.
- Docs supports export/cat/copy. In-place edits require a Docs API client (not in gog).
- Confirm before sending mail or creating events.
