---
name: "send-morning-briefing"
description: "Daily morning briefing — gathers weather, calendar, priorities, and urgent items, then delivers a comprehensive summary to start your day."
schedule: "7am"
category: "daily-command-center"
---

# Morning Briefing

You are providing the user with their morning command center. Follow these steps sequentially and present the output as a beautiful, scannable briefing.

## 1. Gather Weather
Use a tool (like `browser_navigate` to a weather site, or a known weather API) to get today's forecast for the user's location.
- Note the current temperature, conditions, and high/low for the day.
- Add a tiny piece of advice (e.g., "Grab an umbrella" or "Sunglasses day").

## 2. Gather Calendar
Use your calendar access or `session_search` (if the user mentioned it previously) to pull today's events.
- Identify the **First event**.
- Note **Key meetings** and **Free blocks**.

## 3. Check Priorities & Overdue Tasks
Query the user's tools or use `memory_curate` facts to find their top priorities.

## 4. Output the Briefing
Compile the information into a message that looks exactly like this:

```
☀️ Good morning! Here is your command center for today:

🌤️ **WEATHER**
[City]: [Conditions], [Temp]
High [X] / Low [Y] — [Advice]

📅 **TODAY'S SCHEDULE**
[First event time] — [Event name]
[Time] — [Event]
[X] meetings total today.

🎯 **TOP PRIORITIES**
1. [Most important task]
2. [Second priority]
3. [Third priority]

💡 **TODAY'S TIP**
[Contextual advice based on their calendar]
```

Deliver this using standard messaging. If this is a cron trigger, your response to this prompt will be sent right to the user!
