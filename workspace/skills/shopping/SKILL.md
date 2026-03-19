---
name: shopping
description: Maintain a persistent shopping list. Invoke when user says "add X to my shopping list", "what do I need to buy", "remove X from the list", "clear my shopping list", or "check if I have X on my list". Persists to workspace/data/shopping_list.txt.
version: 1.1.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [shopping, todo, list, productivity]
prerequisites:
  commands: []
---

# Shopping List

Persistent shopping list in `workspace/data/shopping_list.txt`.

## File Format

One item per line:

```
milk
eggs
bread
```

## Add Item

```bash
ITEM="milk"
FILE="workspace/data/shopping_list.txt"
mkdir -p "$(dirname "$FILE")"
if [ -f "$FILE" ] && grep -qxF "$ITEM" "$FILE"; then
    echo "Already on list: $ITEM"
else
    echo "$ITEM" >> "$FILE"
    echo "Added: $ITEM"
fi
```

PowerShell:

```powershell
$item = "milk"
$file = "workspace/data/shopping_list.txt"
$dir = Split-Path $file -Parent
if (-not (Test-Path $dir)) { New-Item -Path $dir -ItemType Directory -Force | Out-Null }
if ((Test-Path $file) -and (Get-Content $file -Raw) -match "(?m)^$item$") {
    Write-Output "Already on list: $item"
} else {
    Add-Content -Path $file -Value $item
    Write-Output "Added: $item"
}
```

## Remove Item

```bash
ITEM="milk"
FILE="workspace/data/shopping_list.txt"
if [ -f "$FILE" ]; then
    if grep -qxF "$ITEM" "$FILE"; then
        grep -vxF "$ITEM" "$FILE" > "${FILE}.tmp" && mv "${FILE}.tmp" "$FILE"
        echo "Removed: $ITEM"
    else
        echo "Not on list: $ITEM"
    fi
fi
```

PowerShell:

```powershell
$item = "milk"
$file = "workspace/data/shopping_list.txt"
if (Test-Path $file) {
    $lines = Get-Content $file | Where-Object { $_ -ne $item }
    if ($lines -join "`n" -ne (Get-Content $file -Raw -Raw).Trim()) {
        $lines | Set-Content $file
        Write-Output "Removed: $item"
    } else {
        Write-Output "Not on list: $item"
    }
}
```

## Check if Item Exists

```bash
grep -qxF "milk" "workspace/data/shopping_list.txt" && echo "Yes" || echo "No"
```

## View List

```bash
cat "workspace/data/shopping_list.txt"
```

## Clear List

```bash
> "workspace/data/shopping_list.txt"
```

## Count Items

```bash
wc -l < "workspace/data/shopping_list.txt"
```
