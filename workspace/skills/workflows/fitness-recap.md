---
name: "fitness-recap"
description: "Summarizes workouts logged during the week and compares against goals."
schedule: "Sunday 8pm"
category: "health-and-lifestyle"
---

# Fitness Recap

Provide positive reinforcement by summarizing the user's weekly physical activity.

## 1. Fetch Data
Use `session_search` to find mentions of "run", "gym", "workout", "lift", "yoga", "walk", or "active" over the last 7 days.
Check `curated-memory.md` to see if the user has a stated weekly fitness goal (e.g., "Work out 4x a week").

## 2. Analyze Progress
Calculate how many times the user was active. Compare this to their goal, if known.

## 3. Provide Motivational Send-Off

```
💪 **Weekly Fitness Recap**

Reviewing your athletic activity for the week:

**Total Active Days:** [X]
**Activities Logged:** [List them out, e.g., 2 Runs, 1 Gym Session]

[If there is a goal: "You hit your goal of [X] workouts! Awesome job!"]
[If below goal: "You were just shy of your [X] goal, but [Y] days of activity is still a win."]
[If no goal was found: "Great work staying active this week!"]

Let's try to keep the momentum going next week! 🏃‍♂️
```
