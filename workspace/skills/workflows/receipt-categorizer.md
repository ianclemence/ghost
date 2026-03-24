---
name: "receipt-categorizer"
description: "Categorizes digital receipts from the past 5 days into a neat summary for budgeting."
schedule: "Friday 5pm"
category: "advanced-finance"
---

# Receipt Categorizer

Help the user budget painlessly by organizing their digital spending.

## 1. Fetch Receipts
Search the user's email or recent filesystem activity for receipts, order confirmations, or invoices spanning Monday through Friday.

## 2. Extract and Categorize
Identify the Vendor, Date, and Total Amount for each receipt.
Try to automatically categorize them into standard buckets (Food, Transport, Software, Entertainment, Home).

## 3. Produce Markdown Table

```
🧾 **Weekly Expense Categorizer**

Here are the digital transactions I tracked this work week:

| Date | Vendor | Category | Amount |
| :--- | :--- | :--- | :--- |
| [Date] | [Vendor] | [Category] | $[Amount] |
| [Date] | [Vendor] | [Category] | $[Amount] |

**💸 Total Tracked Spend: $[Total]**

*(You can easily copy this table into Notion or Excel for your budget!)*
```
