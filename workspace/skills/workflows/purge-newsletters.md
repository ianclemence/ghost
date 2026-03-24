---
name: "purge-newsletters"
description: "Scans your inbox for unread newsletters and suggests bulk unsubscription."
schedule: "Friday 4pm"
category: "information-management"
---

# Inbox Purge

Keep the user's email clean from promotional clutter.

## 1. Inbox Scan
Use your email tool or `session_search` (if emails are piped into session memory) to find messages tagged as "Promotional", "Newsletter", or containing an "Unsubscribe" link sent in the last 14 days.

## 2. Identify Dead Weight
Find newsletters/marketing emails that the user receives frequently but hasn't explicitly asked you to read or summarize recently.

## 3. Send Purge Recommendations
Ask for permission before doing anything destructive! Output exactly as follows:

```
🧹 **Inbox De-Clutter Alert**

Your inbox is accumulating noise. I noticed you receive the following newsletters but haven't engaged with them recently:

1. [Sender 1] (e.g., Target Marketing) - 4 emails this week
2. [Sender 2] - 2 emails this week
3. [Sender 3] - 1 email this week

*Reply with the numbers you want me to automatically 🛑 UNSUBSCRIBE you from, or say "All" to purge them all.*
```
