# Ghost State

This directory stores the agent's internal state and persistence data.

- **`state.json`**: Tracks the last active channel, timestamps, and other dynamic state variables.

## Note
These files are automatically updated by the agent during runtime. They are excluded from git tracking to prevent merge conflicts and accidental commits of local state.
