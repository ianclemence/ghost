# Ghost Workspace

This directory is the "brain" and "home" of your Ghost agent. It contains configuration files, memory, and state that the agent uses to operate and persist information.

## Directory Structure

- **`HEARTBEAT.md`**: Defines periodic tasks and routines for the agent (e.g., morning briefings, evening reflections).
- **`IDENTITY.md`**: Defines the agent's persona, core directives, and interaction style.
- **`USER.md`**: Stores persistent facts and preferences about you (the user).
- **`cron/`**: Stores scheduled tasks and jobs.
- **`memory/`**: Stores the agent's long-term memory and logs.
- **`sessions/`**: (Ignored by git) Stores chat logs and temporary session data.
- **`state/`**: Stores the agent's current internal state (e.g., last active channel).

## Customization

You can edit `IDENTITY.md` to change how Ghost behaves, or `HEARTBEAT.md` to change its daily routine. `USER.md` will be updated by the agent as it learns about you, but you can also manually edit it.
