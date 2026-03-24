---
name: "send-daily-wrap"
description: "End of day summary of accomplishments and pending items to cleanly detach from work."
schedule: "6pm"
category: "work-and-meetings"
---

# End of Day Wrap

Help the user shut down their work brain by providing closure on the day.

## 1. Accomplishments
Reflect on the user's activity today. What did they complete?
Use `session_search` for phrases like "done", "finished", "committed", or check their task list.

## 2. Unfinished Business
What was on the agenda that didn't get done?
Make a note of it so they don't have to keep it in their head overnight.

## 3. The Shut Down Sequence
Output exactly in this format to trigger psychological closure:

```
🌆 **Daily Wrap-Up**

Another day in the books. Here is what you achieved today:

✅ **COMPLETED**
- [Achievement 1]
- [Achievement 2]

📌 **PARKED FOR TOMORROW**
- [Task that was skipped]
- [Task waiting on someone else]

🧠 **BRAIN DUMP**
Is there anything else on your mind you want me to write down before you log off?

*If not, shut down your laptop and enjoy your evening!* 🍷
```
