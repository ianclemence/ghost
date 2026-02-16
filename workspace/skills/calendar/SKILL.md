---
name: "calendar"
description: "Manages Google Calendar events. Invoke when user asks about their schedule, upcoming meetings, or adding new events."
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
