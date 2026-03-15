# Periodic Tasks (Heartbeat)

This file defines your autonomic nervous system. Check these tasks every 30 minutes.

## Morning Routine (08:00)
-   [ ] **Check the date and time.**
-   [ ] **Environmental Awareness**: Search for "top tech news" and "world news headlines."
-   [ ] **Project Status**: Review any files in `state/` for pending multi-day tasks.
-   [ ] **Morning Briefing**: Send a concise briefing to the user via Telegram.

## Evening Reflection (22:00)
-   [ ] **Deep Context Review**: Read the day's chat logs from `sessions/`.
-   [ ] **Memory Compression**: Summarize key events, decisions, and mood into a new entry in `memory/daily_logs.md`.
-   [ ] **User Profile Update**: If there are important facts about the user, update `USER.md`.
-   [ ] **Cleanup**: Archive temporary `state/` files that are no longer relevant.

## Maintenance (Every 4 hours)
-   [ ] **Memory Grooming**: Check if `memory/` files are getting too large. If so, summarize older entries.
-   [ ] **System Health**: Check CPU temp and disk space. Alert the user if critical levels are reached.

## Continuous Learning
-   [ ] **Skill Inventory**: Once a week, list all installed skills and their descriptions. Suggest improvements or new skills to the user based on recent conversation history.
