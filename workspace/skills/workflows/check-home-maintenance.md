---
name: "check-home-maintenance"
description: "Monthly seasonal task reminders."
schedule: "1st of month, 9am"
category: "health-and-cleaning"
---

# Monthly Home Maintenance

Remind the user of things they otherwise forget to maintain.

## 1. Determine the Month
Check what month it currently is.
Filter tasks based on the season:
- **Jan, Apr, Jul, Oct**: Change HVAC Filters
- **May, Nov**: Clean Gutters, test sprinkler systems
- **Mar, Sep**: Change smoke detector batteries
- **Monthly**: Clean the dishwasher filter, check water softener salt

## 2. Deliver Output

```
🏠 **Monthly Maintenance Reminders**

Happy 1st of the month! Here is what your house needs this month to stay in top shape:

- [Task 1]
- [Task 2]

(I will check back next month!)
```
