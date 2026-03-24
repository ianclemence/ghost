---
name: "track-sleep"
description: "Nightly prompt to log sleep and mood to detect patterns."
schedule: "9pm"
category: "health-and-cleaning"
---

# Sleep Tracker & Mental Check-in

Track the user's overall wellbeing by prompting them every evening.

## 1. Context Check
Has the user already discussed how tired they are or how their day went today? (Use `session_search`). If yes, don't ask about it again. Ensure you don't overwhelm them.

## 2. Generate Prompt
Ask a quick question about their energy levels today. Keep it extremely brief and non-intrusive.

```
🧠 **Evening Check-in**

How are you feeling as the day winds down on a scale of 1-10? 
Any thoughts on what you want to achieve tomorrow?

(Just checking your vibe so we can look for patterns!)
```

## 3. Storage
When the user replies to this, you should autonomously use `memory_curate` to append their answer to a dedicated "Mood & Energy" section in the data store for future trend analysis.
