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
  commands: [gcalcli --config-folder /var/lib/ghost/.calendar]
---

# Calendar

Google Calendar via gcalcli. Requires OAuth2 authentication (connected once
via Ghost settings under Integrations — users never run auth commands).

> **If not configured:** If `gcalcli` reports `No OAuth token`, `invalid_grant`, or any auth error, do NOT expose the raw error. Respond: `Calendar access isn't connected yet. Connect your calendar in Ghost settings under Integrations to view your schedule.` and offer to guide setup. Do not pretend calendar works.

> **Config:** All commands below MUST include `--config-folder /var/lib/ghost/.calendar` first (service-owned token dir — the Ghost services cannot see home directories). Never use bare `gcalcli`.

## Quick Reference

| Task | Command |
|------|---------|
| Agenda (next 5 days) | `gcalcli --config-folder /var/lib/ghost/.calendar agenda` |
| Week view | `gcalcli --config-folder /var/lib/ghost/.calendar calw` |
| Month view | `gcalcli --config-folder /var/lib/ghost/.calendar calm` |
| Today's agenda | `gcalcli --config-folder /var/lib/ghost/.calendar agenda "$(date)" "$(date -d tomorrow)"` |
| Quick add | `gcalcli --config-folder /var/lib/ghost/.calendar quick "Dinner with Alice 7pm tomorrow"` |
| Search events | `gcalcli --config-folder /var/lib/ghost/.calendar search "Dentist"` |
| Delete event | `gcalcli --config-folder /var/lib/ghost/.calendar delete "Event Title"` |
| List calendars | `gcalcli --config-folder /var/lib/ghost/.calendar list` |

## Setup (users: Web Console → Connections → Integrations)

1. Owner opens Integrations → Google Calendar → Connect.
2. Ghost runs `gcalcli --config-folder /var/lib/ghost/.calendar init --noauth_local_server` and shows the Google URL.
3. User approves in any browser; Ghost polls until the token lands.
4. Verify: `gcalcli --config-folder /var/lib/ghost/.calendar agenda`.

If authentication fails or is revoked, re-connect from Integrations (same flow).

## View Schedule

### Agenda (next 5 days)

```bash
gcalcli --config-folder /var/lib/ghost/.calendar agenda
```

### Today's events only

```bash
gcalcli --config-folder /var/lib/ghost/.calendar agenda "$(date +%Y-%m-%d)" "$(date -d tomorrow +%Y-%m-%d)"
```

### Week View

```bash
gcalcli --config-folder /var/lib/ghost/.calendar calw
```

### Month View

```bash
gcalcli --config-folder /var/lib/ghost/.calendar calm
```

### Specific Calendar Only

If you have multiple calendars:

```bash
gcalcli --config-folder /var/lib/ghost/.calendar --calendar "work@example.com" agenda
gcalcli --config-folder /var/lib/ghost/.calendar --calendar "personal@example.com" calw
```

## Create Events

### Quick Add (natural language)

```bash
gcalcli --config-folder /var/lib/ghost/.calendar quick "Team standup 9am every weekday"
gcalcli --config-folder /var/lib/ghost/.calendar quick "Doctor appointment March 20 2pm"
gcalcli --config-folder /var/lib/ghost/.calendar quick "Lunch with Bob tomorrow 12:30pm"
```

### Structured Add (explicit details)

```bash
gcalcli --config-folder /var/lib/ghost/.calendar add \
  --title "Project Review" \
  --where "Room 4" \
  --when "2026-03-25 14:00" \
  --duration 60 \
  --description "Review Q1 progress" \
  --calendar work@example.com
```

### All-Day Event

```bash
gcalcli --config-folder /var/lib/ghost/.calendar add \
  --title "Conference Day" \
  --when "2026-04-15" \
  --allDay
```

## Search Events

```bash
gcalcli --config-folder /var/lib/ghost/.calendar search "standup"
gcalcli --config-folder /var/lib/ghost/.calendar search --calendar "work@example.com" "review"
```

Search supports regex. Use `--case-sensitive` if needed.

## Delete Events

```bash
# Interactive delete (prompts for confirmation)
gcalcli --config-folder /var/lib/ghost/.calendar delete "Stand-up Meeting"

# Force delete (no prompt)
gcalcli --config-folder /var/lib/ghost/.calendar delete --yes "Stand-up Meeting"
```

Delete by ID (from agenda output):

```bash
gcalcli --config-folder /var/lib/ghost/.calendar delete --id <event-id>
```

## Modify Events

gcalcli --config-folder /var/lib/ghost/.calendar does not have a direct edit command. Workflow:

1. Find the event: `gcalcli --config-folder /var/lib/ghost/.calendar agenda | grep "Event Name"`
2. Note the details
3. Delete it: `gcalcli --config-folder /var/lib/ghost/.calendar delete "Event Name"`
4. Recreate with corrected details: `gcalcli --config-folder /var/lib/ghost/.calendar quick "Corrected details..."`

## Reminders

gcalcli --config-folder /var/lib/ghost/.calendar creates events with default reminders. To set explicit reminders:

```bash
gcalcli --config-folder /var/lib/ghost/.calendar add --title "Meeting" --when "2026-03-20 10:00" --reminder 30
```

Reminder units: minutes (default). Use `--reminder 60` for 1 hour, `--reminder 1d` for 1 day.

## Import / Export

### Export to ICS

```bash
gcalcli --config-folder /var/lib/ghost/.calendar ics --calendar "work@example.com" --start "2026-01-01" --end "2026-12-31" > calendar.ics
```

### Import ICS

Google Calendar supports ICS import via the web UI or API. gcalcli --config-folder /var/lib/ghost/.calendar does not natively import ICS files.

## Output Parsing

Parse gcalcli --config-folder /var/lib/ghost/.calendar agenda output:

```bash
gcalcli --config-folder /var/lib/ghost/.calendar agenda | python3 -c "
import sys
for line in sys.stdin:
    line = line.rstrip()
    if line and not line.startswith('gcalcli --config-folder /var/lib/ghost/.calendar'):
        print(line)
"
```

## Troubleshooting

| Problem | Solution |
|---------|----------|
| `No module named gcalcli --config-folder /var/lib/ghost/.calendar` | `pip install gcalcli --config-folder /var/lib/ghost/.calendar` |
| `AuthError: invalid_grant` | Token expired — run `gcalcli --config-folder /var/lib/ghost/.calendar oauth` again |
| Empty agenda | Verify correct calendar: `gcalcli --config-folder /var/lib/ghost/.calendar list` |
| Wrong time zone | Set `TZ` env var or configure in gcalcli --config-folder /var/lib/ghost/.calendar config |
| Headless auth | `gcalcli --config-folder /var/lib/ghost/.calendar oauth --auth-device` for manual code flow |
