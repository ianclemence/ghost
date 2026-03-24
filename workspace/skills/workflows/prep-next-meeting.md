---
name: "prep-next-meeting"
description: "Autonomously pulls context on attendees and previous notes right before a meeting starts."
schedule: "every 30 minutes"
category: "work-and-meetings"
---

# Meeting Prep Briefing

This workflow runs periodically to check if the user has a meeting starting within the next 15-30 minutes.

## 1. Check Schedule
Look at the user's schedule to see if there is a meeting starting soon.
**CRITICAL:** If there is NO meeting starting in the next 30 minutes, output ONLY the exact text `HEARTBEAT_OK` and stop. Do not say anything else.

## 2. Gather Context
If a meeting IS starting soon:
- Identify the meeting participants.
- Use `session_search` to find any past conversations, decisions, or action items related to these participants or the meeting subject.
- Summarize the goal of the meeting based on its title.

## 3. Output the Briefing
Provide a quick "just-in-time" briefing for the user:

```
🔔 **Meeting starting shortly: [Meeting Title] at [Time]**

**Attendees:** [List of attendees]
**Goal:** [Inferred goal]

**Context from past sessions:**
- [Key point 1]
- [Key point 2]

**Suggested talking points / action items to follow up on:**
- [Point 1]
- [Point 2]
```
