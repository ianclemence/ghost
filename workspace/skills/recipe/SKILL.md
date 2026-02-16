---
name: "recipe"
description: "Finds recipes for food. Invoke when user asks 'Find a recipe for...', 'How to cook...', or 'Ingredients for...'."
---

# Recipe Finder

Searches for recipes using a public API (TheMealDB).

## Commands

### Search by Name
Finds recipes matching a keyword (e.g., "chicken", "pasta").

```bash
curl -s "https://www.themealdb.com/api/json/v1/1/search.php?s=Arrabiata"
```

### Get Random Recipe
Fetches a random meal idea.

```bash
curl -s "https://www.themealdb.com/api/json/v1/1/random.php"
```

### Filter by Category
Lists meals in a category (e.g., "Seafood", "Vegan").

```bash
curl -s "https://www.themealdb.com/api/json/v1/1/filter.php?c=Seafood"
```
