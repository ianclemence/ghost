---
name: "track-packages"
description: "Consolidates delivery statuses from order confirmation emails."
schedule: "8am, 5pm"
category: "finance-and-life-admin"
---

# Package Tracking

Keep the user updated on expected deliveries without them needing to check multiple apps.

## 1. Find Tracking Numbers
Use your email tool to scan for "Out for delivery", "Shipped", "Order confirmed", or tracking numbers from USPS, UPS, FedEx, Amazon, or DHL in the last 7 days.

## 2. Determine Status
For each package, identify its current state:
- Out for Delivery Today
- In Transit (Expected Date)
- Delivered

## 3. Provide Status Board
Format the output as a clean shipping board.
If there are no packages currently in transit or delivered recently, output `HEARTBEAT_OK` and stop. Do not send an empty board.

```
📦 **Package Delivery Status**

🟢 **DELIVERED TODAY**
- [Item description / Store] - Left at [Location]

🚚 **OUT FOR DELIVERY**
- [Item description] via [Carrier]

⏳ **IN TRANSIT**
- [Item] - Arriving [Date]
```
