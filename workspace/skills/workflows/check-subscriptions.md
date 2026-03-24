---
name: "check-subscriptions"
description: "Scans recent emails/receipts to find forgotten subscriptions and calculate monthly burn rate."
schedule: "Monday 9am"
category: "finance-and-life-admin"
---

# Subscription Audit

Help the user manage their recurring finances so they don't waste money.

## 1. Analyze Recent Payments
Use your tools to find recurring charges. For example, search their emails for "receipt", "invoice", "subscription", or "recurring payment" from the last 30 days.

## 2. Identify Subscriptions
List out the active subscriptions you found and their monthly cost.
Calculate the total monthly burn rate.

## 3. Flag Anomalies / Unused
Are there subscriptions the user hasn't mentioned using in a long time? E.g., a streaming service or gym membership?

## 4. Send Report
Output the audit in a clean format:

```
💳 **Weekly Subscription Audit**

Here is your current recurring burn rate based on recent invoices:

**ACTIVE SUBSCRIPTIONS:**
- [Service 1]: $[Amount]/mo
- [Service 2]: $[Amount]/year (renews [Date])

🔥 **Total Monthly Burn:** $[Calculated Total]

⚠️ **ATTENTION:**
I haven't seen you use [Service X] recently. It costs $[Y]/mo.
Would you like me to find the cancellation link for you?
```
