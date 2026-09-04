---
name: calendar
description: Manage Google Calendar events — view upcoming schedule, list meetings, create events, search, and delete. Invoke when user asks "what's on my calendar", "do I have meetings today", "schedule a meeting", "add an event", "delete my 3pm meeting", or "is tomorrow free". Requires gcalcli and OAuth2 authentication.
version: 1.1.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [calendar, google, schedule, meetings, events]
prerequisites:
  commands: [gcalcli]
---

# Calendar

Google Calendar via gcalcli. Requires OAuth2 authentication.

> **If not configured:** If `gcalcli` reports `No OAuth token` or `oauth` required, do NOT expose the raw error. Respond: `Calendar access isn't connected yet. Connect your calendar in Ghost settings under Integrations to view your schedule.` and offer to guide setup. Do not pretend calendar works.

## Quick Reference

| Task | Command |
|------|---------|
| Agenda (next 5 days) | `gcalcli agenda` |
| Week view | `gcalcli calw` |
| Month view | `gcalcli calm` |
| Today's agenda | `gcalcli agenda "$(date)" "$(date -d tomorrow)"` |
| Quick add | `gcalcli quick "Dinner with Alice 7pm tomorrow"` |
| Search events | `gcalcli search "Dentist"` |
| Delete event | `gcalcli delete "Event Title"` |
| List calendars | `gcalcli list` |

## Setup

### 1. Install gcalcli

```bash
pip install gcalcli
```

### 2. Authenticate (OAuth2)

```bash
gcalcli oauth
```

Opens browser for Google sign-in. Token stored in `~/.gcalcli_oauth`.
For headless setups: `gcalcli oauth --auth-device` prints a URL for manual auth.

### 3. Verify

```bash
gcalcli agenda
```

If authentication fails, re-auth: `gcalcli oauth`

## View Schedule

### Agenda (next 5 days)

```bash
gcalcli agenda
```

### Today's events only

```bash
gcalcli agenda "$(date +%Y-%m-%d)" "$(date -d tomorrow +%Y-%m-%d)"
```

### Week View

```bash
gcalcli calw
```

### Month View

```bash
gcalcli calm
```

### Specific Calendar Only

If you have multiple calendars:

```bash
gcalcli --calendar "work@example.com" agenda
gcalcli --calendar "personal@example.com" calw
```

## Create Events

### Quick Add (natural language)

```bash
gcalcli quick "Team standup 9am every weekday"
gcalcli quick "Doctor appointment March 20 2pm"
gcalcli quick "Lunch with Bob tomorrow 12:30pm"
```

### Structured Add (explicit details)

```bash
gcalcli add \
  --title "Project Review" \
  --where "Room 4" \
  --when "2026-03-25 14:00" \
  --duration 60 \
  --description "Review Q1 progress" \
  --calendar work@example.com
```

### All-Day Event

```bash
gcalcli add \
  --title "Conference Day" \
  --when "2026-04-15" \
  --allDay
```

## Search Events

```bash
gcalcli search "standup"
gcalcli search --calendar "work@example.com" "review"
```

Search supports regex. Use `--case-sensitive` if needed.

## Delete Events

```bash
# Interactive delete (prompts for confirmation)
gcalcli delete "Stand-up Meeting"

# Force delete (no prompt)
gcalcli delete --yes "Stand-up Meeting"
```

Delete by ID (from agenda output):

```bash
gcalcli delete --id <event-id>
```

## Modify Events

gcalcli does not have a direct edit command. Workflow:

1. Find the event: `gcalcli agenda | grep "Event Name"`
2. Note the details
3. Delete it: `gcalcli delete "Event Name"`
4. Recreate with corrected details: `gcalcli quick "Corrected details..."`

## Reminders

gcalcli creates events with default reminders. To set explicit reminders:

```bash
gcalcli add --title "Meeting" --when "2026-03-20 10:00" --reminder 30
```

Reminder units: minutes (default). Use `--reminder 60` for 1 hour, `--reminder 1d` for 1 day.

## Import / Export

### Export to ICS

```bash
gcalcli ics --calendar "work@example.com" --start "2026-01-01" --end "2026-12-31" > calendar.ics
```

### Import ICS

Google Calendar supports ICS import via the web UI or API. gcalcli does not natively import ICS files.

## Output Parsing

Parse gcalcli agenda output:

```bash
gcalcli agenda | python3 -c "
import sys
for line in sys.stdin:
    line = line.rstrip()
    if line and not line.startswith('gcalcli'):
        print(line)
"
```

## Troubleshooting

| Problem | Solution |
|---------|----------|
| `No module named gcalcli` | `pip install gcalcli` |
| `AuthError: invalid_grant` | Token expired — run `gcalcli oauth` again |
| Empty agenda | Verify correct calendar: `gcalcli list` |
| Wrong time zone | Set `TZ` env var or configure in gcalcli config |
| Headless auth | `gcalcli oauth --auth-device` for manual code flow |
