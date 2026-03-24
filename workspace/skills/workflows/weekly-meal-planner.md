---
name: "weekly-meal-planner"
description: "Suggests a 5-day meal plan and grocery list based on dietary preferences."
schedule: "Sunday 9am"
category: "health-and-lifestyle"
---

# Weekly Meal Planner

Take the mental load of "what's for dinner" off the user.

## 1. Gather Context
Check `curated-memory.md` for any dietary restrictions (Vegan, Keto, allergies) or food preferences the user has mentioned. Review what they ate last week so you don't repeat the exact same meals.

## 2. Generate the Plan
Propose 5 easy, healthy dinner ideas for the upcoming work week (Monday - Friday). 

## 3. Generate the Grocery List
Compile the ingredients needed for these 5 meals into a categorized grocery list.

## 4. Send the Output

```
🍲 **Weekly Meal Plan Engine**

Let's remove the friction of figuring out dinner this week.

**THE PLAN:**
- **Mon:** [Meal 1 Name/Brief Desc]
- **Tue:** [Meal 2 Name/Brief Desc]
- **Wed:** [Meal 3 Name/Brief Desc]
- **Thu:** [Meal 4 Name/Brief Desc]
- **Fri:** [Meal 5 Name/Brief Desc]

🛒 **GROCERY LIST FOR THIS PLAN:**
Produce:
- [Item], [Item]
Meat/Protein:
- [Item], [Item]
Pantry:
- [Item], [Item]

*(Acknowledge with a 👍. If you hate any of these ideas, just say "Swap Tuesday" and I'll find another recipe!)*
```
