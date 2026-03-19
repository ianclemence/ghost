---
name: calendar
description: Manage Google Calendar events — view upcoming schedule, list meetings, create events, and set reminders. Invoke when user asks "what's on my calendar", "do I have meetings today", "schedule a meeting", "add an event", or "is tomorrow free". Requires gcalcli and OAuth2 authentication.
version: 1.0.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [calendar, google, schedule, meetings, events]
prerequisites:
  commands: [gcalcli]
---

# Calendar

Interacts with Google Calendar via CLI.

## Requirements

- **Tool**: [gcalcli](https://github.com/insanum/gcalcli)
- **Auth**: Must be authenticated via OAuth2.

## Commands

### View Schedule

- **Agenda (Next 5 days)**:

  ```bash
  gcalcli agenda
  ```

- **Week View**:

  ```bash
  gcalcli calw
  ```

- **Month View**:
  ```bash
  gcalcli calm
  ```

### Manage Events

- **Quick Add**:

  ```bash
  gcalcli quick "Dinner with Alice 7pm tomorrow"
  ```

- **Add Event (Interactive)**:

  ```bash
  gcalcli add
  ```

- **Search**:
  ```bash
  gcalcli search "Dentist"
  ```
