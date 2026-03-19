---
name: recipe
description: Search for food recipes by name, ingredient, or category, and retrieve full details with ingredients and instructions. Invoke when user asks "find a recipe for X", "how do I make chicken pasta", "what can I cook with X", "recipe for lasagna", "show me the ingredients for Y", or "give me a random meal idea". Uses TheMealDB API — no API key required.
version: 1.1.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [recipe, food, cooking, mealdb, meals]
prerequisites:
  commands: [curl]
---

# Recipe Finder

TheMealDB API. No API key required.

## Quick Reference

| Task                      | Command                                                                         |
| ------------------------- | ------------------------------------------------------------------------------- |
| Search by name            | `curl -s "https://www.themealdb.com/api/json/v1/1/search.php?s=Arrabiata"`      |
| Random recipe             | `curl -s "https://www.themealdb.com/api/json/v1/1/random.php"`                  |
| Filter by category        | `curl -s "https://www.themealdb.com/api/json/v1/1/filter.php?c=Seafood"`        |
| Filter by area            | `curl -s "https://www.themealdb.com/api/json/v1/1/filter.php?a=Italian"`        |
| Filter by main ingredient | `curl -s "https://www.themealdb.com/api/json/v1/1/filter.php?i=chicken_breast"` |
| Lookup by ID              | `curl -s "https://www.themealdb.com/api/json/v1/1/lookup.php?i=52772"`          |
| List all categories       | `curl -s "https://www.themealdb.com/api/json/v1/1/list.php?c=list"`             |
| List all areas            | `curl -s "https://www.themealdb.com/api/json/v1/1/list.php?a=list"`             |
| List all ingredients      | `curl -s "https://www.themealdb.com/api/json/v1/1/list.php?i=list"`             |

## Search by Name

Returns array of meals matching the name.

```bash
curl -s "https://www.themealdb.com/api/json/v1/1/search.php?s=chicken" | python3 -c "
import sys,json
data = json.load(sys.stdin)
for m in data.get('meals', []) or []:
    print(f\"{m['idMeal']}  {m['strMeal']}  ({m.get('strArea','')}, {m.get('strCategory','')})\")
"
```

## Lookup by ID (Full Recipe)

After finding an ID from search, get full details including ingredients and instructions.

```bash
curl -s "https://www.themealdb.com/api/json/v1/1/lookup.php?i=52772" | python3 -c "
import sys,json
m = json.load(sys.stdin)['meals'][0]
print(f\"# {m['strMeal']}\")
print(f\"Category: {m['strCategory']}  |  Area: {m['strArea']}\")
print(f\"Source: {m['strSource']}\")
print()
print('## Ingredients')
for i in range(1,21):
    ing = m.get(f'strIngredient{i}','').strip()
    meas = m.get(f'strMeasure{i}','').strip()
    if ing:
        print(f'- {meas} {ing}')
print()
print('## Instructions')
print(m['strInstructions'])
"
```

## Filter by Category

Returns meals in that category. Useful categories: Beef, Chicken, Seafood, Vegetarian, Pasta, Dessert, Side.

```bash
curl -s "https://www.themealdb.com/api/json/v1/1/filter.php?c=Seafood" | python3 -c "
import sys,json
data = json.load(sys.stdin)
for m in data.get('meals', []) or []:
    print(f\"{m['idMeal']}  {m['strMeal']}\")
"
```

## Filter by Ingredient

Find meals you can make with what you have.

```bash
curl -s "https://www.themealdb.com/api/json/v1/1/filter.php?i=chicken_breast" | python3 -c "
import sys,json
data = json.load(sys.stdin)
for m in data.get('meals', []) or []:
    print(f\"{m['idMeal']}  {m['strMeal']}\")
"
```

## Random Recipe

```bash
curl -s "https://www.themealdb.com/api/json/v1/1/random.php" | python3 -c "
import sys,json,re
m = json.load(sys.stdin)['meals'][0]
print(f\"# {m['strMeal']}\")
print(f\"Category: {m['strCategory']}  |  Area: {m['strArea']}\")
print()
print('## Ingredients')
for i in range(1,21):
    ing = m.get(f'strIngredient{i}','').strip()
    meas = m.get(f'strMeasure{i}','').strip()
    if ing:
        print(f'- {meas} {ing}')
print()
print('## Instructions')
print(m['strInstructions'])
"
```

## YouTube Link

If a YouTube video is available (`strYoutube`), extract the video ID and build a URL:

```
https://www.youtube.com/watch?v=VIDEO_ID
```

Parse from response: `re.search(r'(?<=v=)[^&#]+', m['strYoutube']).group()`

## Workflow

1. Search by name/ingredient/category → get meal IDs
2. Lookup by ID for full recipe details
3. If `strYoutube` exists, offer to show the cooking video
4. Parse ingredients and measurements into a clean shopping list

## Coverage

- ~400 meals in database
- 20 categories, ~30 areas (cuisines)
- Not all meals have YouTube videos or source URLs
