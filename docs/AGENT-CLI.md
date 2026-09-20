# Ghost Agent CLI (interactive)

The interactive `ghost agent` experience. It is Ghost's daily terminal surface:
talk to Ghost, watch it work, approve what it asks, and see where each answer
came from. The design is inspired by the pi coding agent's terminal UX, but the
behavior, commands, and status are Ghost's own — trust, memory, routines, and
provenance are first-class here in a way a coding agent does not need.

## Principles

- **Streaming, not waiting.** Tokens render as they arrive; tool activity shows
  inline ("Searching…", "Running…") so Ghost never looks frozen.
- **Where it ran is always visible.** Every turn reports whether it was served
  locally, by the Pod, or by cloud.
- **Approvals are first-class.** A permission request is a prompt in the flow,
  not something you discover in another app.
- **Never block the user.** While Ghost works, you can still type. Enter queues
  a steering message (injected into the running turn); Esc aborts.
- **Slash commands for everything.** No hidden flags; `/` opens the palette.
- **Calm rendering.** No flicker, readable in any terminal.

## Commands

| Command | Purpose |
|---|---|
| `/help` | List commands and keybindings |
| `/model [name]` | Show or switch the active model (presets first) |
| `/new` | Start a fresh conversation |
| `/session` | Show the current session key and turn count |
| `/memory [query]` | Ask Ghost what it remembers (read-only) |
| `/routines` | List the things Ghost does for you |
| `/clear` | Clear the screen (keeps the session) |
| `/quit` | Exit (also Ctrl+C twice) |

## Keys

| Key | Action |
|---|---|
| Enter | Send. While Ghost is working: queue a steering message |
| Esc | Abort the current turn; queued text returns to the editor |
| Ctrl+C | Clear the editor; twice quits |
| Ctrl+L | Open the model picker |
| Ctrl+O | Toggle tool-detail expansion |
| ↑ / ↓ | Editor history |
| PgUp / PgDn | Scroll the transcript |

## Status line

`model · where it runs · session · state` — for example:

```
deepseek-flash · cloud · cli:default · ready
```

While working: `… · working · 3 tools`. On a held approval: `waiting for you`.

## Turning it off

`ghost agent -m "..."` stays one-shot and machine-friendly; the TUI is used
only when no message is passed and stdin is a terminal.
