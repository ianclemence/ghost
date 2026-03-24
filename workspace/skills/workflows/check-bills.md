---
name: "check-bills"
description: "Warns about upcoming due dates and unusual spikes in utility bills."
schedule: "Monday 8am"
category: "finance-and-life-admin"
---

# Bill Monitoring

Ensure the user never misses a payment and is aware of unusual spikes.

## 1. Find Upcoming Bills
Scan emails or connected accounts for statements, bills, or "due" notices within the next 14 days. Look for keywords like "Statement available", "Payment Due", "Utility".

## 2. Compare Against Average
If it's a variable bill (like Electricity, Water, or Credit Card), compare the current amount to the previous month's amount if available.
Flag any bill that is >20% higher than usual.

## 3. Send Bill Radar
Output a concise overview. If there are no upcoming bills in the next 14 days, output `HEARTBEAT_OK` and stop exactly there.

```
🧾 **Upcoming Bills Radar**

**DUE THIS WEEK:**
- **[Bill Name]**: $[Amount] (Due [Date])
- **[Bill Name]**: $[Amount] (Due [Date])

🚨 **SPIKE ALERTS:**
- Your [Utility/Card] bill is $[Amount], which is [X]% higher than last month.

**ON THE HORIZON (Next Week):**
- [Bill Name]: $[Amount]
```
