---
name: recipe
description: Search for food recipes by name, ingredient, or category. Invoke when user asks "find a recipe for X", "how do I make chicken pasta", "what can I cook with X", "recipe for lasagna", or "give me a random meal idea". Uses TheMealDB API — no API key required.
version: 1.0.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [recipe, food, cooking, mealdb, meals]
prerequisites:
  commands: [curl]
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
