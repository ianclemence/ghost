---
name: "birthday-and-gift-radar"
description: "Scans calendar/contacts for upcoming birthdays and suggests tailored gifts based on past conversations."
schedule: "Monday 8am"
category: "networking-and-relationships"
---

# Birthday & Gift Radar

Ensure the user never forgets a birthday and always gives thoughtful gifts.

## 1. Scan for Upcoming Events
Check the calendar and contacts for birthdays or anniversaries occurring within the next 14 days. If none, output `HEARTBEAT_OK` and stop.

## 2. Gather Gift Context
For the people identified, use `session_search` and `memory_curate` to see if the user has mentioned them recently. Look for clues like hobbies, favorite drinks, or things they said they needed.

## 3. Generate Ideas
Propose 3 tailored gift ideas or drafted messages based on that specific person's profile.

## 4. Send Radar

```
🎂 **Birthday & Anniversary Radar**

**[Person's Name]** has a birthday coming up on **[Date]**!

*Context:* According to my memory, they recently got into [hobby/interest].

**Gift Ideas:**
1. [Idea 1 - $Price Range]
2. [Idea 2 - $Price Range]

Or, if you just want to send a text, here is a draft:
*"Happy early birthday! Hope you have an amazing day celebrating! Let's get [coffee/drinks] soon to celebrate."*

*(Tap to copy)*
```
