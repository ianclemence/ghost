# Ghost Hybrid Workflows

Ghost incorporates a sophisticated "Hybrid Workflow System." Unlike a standard AI prompt, a workflow is a **Scheduled Skill** that executes automatically at a specific time, pulling together multiple tools (calendar, web, files) to deliver a proactive result.

## How it Works

Workflows live in your `workspace/skills/workflows/` directory. They look just like standard Ghost Skills (markdown files), but they have a special `schedule` parameter in their YAML frontmatter:

```yaml
---
name: "triage-tasks"
description: "Morning review of what's due, overdue, and top 3 priorities."
schedule: "8am"
---
```

When Ghost starts, it automatically parses any natural language schedule (`7am`, `Monday 9am`, `1st of month, 9am`) into a standard Cron expression (`0 7 * * *`) and loads it into the internal engine. 

You do **not** need to manually trigger them. At 8:00 AM, Ghost will execute the `triage-tasks` workflow and message you the result.

## The 12 Bundled Workflows

Ghost comes with 12 pre-configured, highly valuable workflows designed to establish it as your personal command center:

### Daily Command Center
*   **`send-morning-briefing` (7am)**: Compiles weather, calendar events, and top priorities into a scannable morning report.
*   **`prep-tomorrow` (9pm)**: Reviews tomorrow's calendar and suggests what to prepare to reduce morning anxiety.

### Work & Meetings
*   **`plan-week` (Sunday 6pm)**: Sunday evening weekly planning — reviews your calendar, sets priorities, and preps you for Monday.
*   **`prep-next-meeting` (every 30 mins)**: Scans for meetings starting soon and pulls context and past notes on attendees.
*   **`triage-tasks` (8am)**: Categorizes tasks into overdue, due today, and top 3 priorities.
*   **`send-daily-wrap` (6pm)**: Summarizes completed tasks and parking lot items to help you detach from work.

### Finance & Life Admin
*   **`check-subscriptions` (Monday 9am)**: Scans receipts to find unused subscriptions and total monthly burn rate.
*   **`track-packages` (8am, 5pm)**: Consolidates delivery statuses from order emails.
*   **`check-bills` (Monday 8am)**: Warns about upcoming due dates and unusual spikes in recurring bills.

### Health & Cleaning
*   **`track-sleep` (9pm)**: Prompts for a quick mood/energy rating to detect weekly trends.
*   **`schedule-chores` (Saturday 9am)**: Suggests targeted weekend chores based on what wasn't done recently.
*   **`check-home-maintenance` (1st of month, 9am)**: Seasonal task reminders (filters, batteries, gutters).

### Digital Hygiene
*   **`clean-downloads` (Friday 5pm)**: Scans your Downloads folder and suggests automated cleanup of old/large files.

### Information Management
*   **`review-reading-list` (Sunday 10am)**: Retrieves all saved links from the week, reads them, and provides a concise backlog summary.
*   **`purge-newsletters` (Friday 4pm)**: Scans your inbox for unread newsletters and suggests bulk unsubscription.

### Networking & Relationships
*   **`birthday-and-gift-radar` (Monday 8am)**: Scans calendar/contacts for upcoming birthdays and suggests tailored gifts.
*   **`network-nudge` (1st of month, 10am)**: Looks for VIP contacts you haven't interacted with recently and suggests a reason to reach out.

### Health & Lifestyle
*   **`weekly-meal-planner` (Sunday 9am)**: Suggests a 5-day meal plan and grocery list based on dietary preferences.
*   **`fitness-recap` (Sunday 8pm)**: Summarizes workouts logged during the week and compares against goals.

### Advanced Finance
*   **`receipt-categorizer` (Friday 5pm)**: Categorizes digital receipts from the past 5 days into a neat summary for budgeting.

## Enabling / Disabling

To disable a workflow, simply remove the `schedule` property from its YAML frontmatter, or delete the `.md` file from your `workspace/skills/workflows/` folder.

To trigger a workflow immediately without waiting for its schedule, just tell Ghost: *"Run the check-bills workflow."*
