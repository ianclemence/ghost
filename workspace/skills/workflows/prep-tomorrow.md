---
name: "prep-tomorrow"
description: "Evening routine that reviews tomorrow's calendar, suggests what to prep, and sets priorities."
schedule: "9pm"
category: "daily-command-center"
---

# Evening Prep

Help the user prepare for tomorrow to reduce morning anxiety.

## 1. Calendar Review
Look up the calendar for tomorrow.
- Identify what time they need to wake up based on their first meeting.
- Identify the most difficult or important meeting of the day.

## 2. Prepare Materials
Identify if any of tomorrow's meetings require preparation (e.g., reading a doc, printing a file, dressing a certain way).

## 3. Top Tasks
Ask yourself: "Based on what the user worked on today, what should their #1 priority be tomorrow?"

## 4. Output
Format your output as a short evening briefing:

```
🌙 Time to wind down! Here is a look at tomorrow to help you prep:

**Wake Up Target:** [Suggested wake up time] (First meeting is at [Time])

**What to Prep Tonight:**
- [Item 1, e.g., "Lay out clothes for your on-site meeting"]
- [Item 2, e.g., "Review the Q3 slides"]

**Tomorrow's #1 Priority:**
- [Main task]

Have a restful night! 😴
```
