---
name: "triage-tasks"
description: "Morning review of what's due, overdue, and top 3 priorities."
schedule: "8am"
category: "work-and-meetings"
---

# Task Triage

Kick off the workday by organizing the user's tasks.

## 1. Gather Tasks
Review memory (`memory_curate`) or recent conversations (`session_search` for phrases like "remind me to" or "I need to") to find open loops and tasks.

## 2. Categorize
Sort the tasks into:
- **Overdue!** (Things promised yesterday but not marked done)
- **Due Today** (Time-sensitive items)
- **Top 3 Priorities** (The highest impact tasks to tackle first)

## 3. Send Triage Report
Output exactly in this format:

```
✅ **Morning Task Triage**

Let's crush today. Here is where we stand:

🔴 **OVERDUE (Quick Wins)**
- [Task 1]
- [Task 2]

🟡 **DUE TODAY**
- [Task 1]

🟢 **TOP 3 PRIORITIES FOR FOCUS**
1. [Priority 1]
2. [Priority 2]
3. [Priority 3]

*Reply with a task name to mark it done, or say "Snooze [Task]" to push it to tomorrow.*
```
