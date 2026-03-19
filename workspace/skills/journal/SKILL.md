---
name: journal
description: Append and search a dated markdown journal. Invoke when user says "log that", "note to self", "write in my journal", "remember that I...", "search my journal for X", or "what did I write about Y". Files live at workspace/journal/YYYY-MM-DD.md.
version: 1.1.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [journal, notes, daily-notes, memory, logging]
prerequisites:
  commands: []
---

# Journal

Appends entries to daily markdown files. Files: `workspace/journal/YYYY-MM-DD.md`.

## File Format

```
## HH:mm

Entry content here. Can be multiple lines.

---
## HH:mm

Next entry later the same day.
```

## Append Entry

```bash
# Get current date and time
DATE=$(date +%Y-%m-%d)
TIME=$(date +%H:%M)
FILE="workspace/journal/${DATE}.md"

# Create file with header if it doesn't exist
if [ ! -f "$FILE" ]; then
  echo "# Journal — $DATE\n" > "$FILE"
fi

# Append entry
cat >> "$FILE" << EOF

## $TIME

User's note goes here.

EOF
```

PowerShell:

```powershell
$date = Get-Date -Format "yyyy-MM-dd"
$time = Get-Date -Format "HH:mm"
$file = "workspace/journal/${date}.md"
if (-not (Test-Path $file)) {
    New-Item -Path $file -Value "# Journal — $date`n`n" -Force | Out-Null
}
"## $time`n`nUser's note goes here.`n" | Add-Content $file
```

## Search Past Entries

```bash
# Search all journal files for a keyword
grep -r "keyword" workspace/journal/ | python3 -c "
import sys
for line in sys.stdin:
    path, rest = line.strip().split(':', 1)
    date = path.split('/')[-1].replace('.md','')
    print(f'[{date}] {rest}')
"
```

```bash
# Full-text search with ripgrep (richer output)
rg -i "keyword" workspace/journal/ --pretty
```

```bash
# List all journal entries on a specific date
rg -N "^##" workspace/journal/2026-03-19.md
```

## Read Recent Entries

```bash
# Last 3 entries from today
tail -20 "workspace/journal/$(date +%Y-%m-%d).md"
```

```bash
# All entries from this week
for f in $(ls workspace/journal/$(date +%Y-%m-%d --date="7 days ago" 2>/dev/null | sed 's/-//g')-*.md 2>/dev/null); do
    echo "=== $f ==="
    cat "$f"
done
```

## Delete or Edit

Journal entries are append-only by design. To edit:
1. Read the file
2. Use `write_file` to overwrite with corrections

To delete a specific entry, overwrite the file excluding that entry's block.

## Directory Path

Always: `workspace/journal/` (relative to Ghost workspace root). Confirm the workspace path with `pwd` if targeting from a different context.

## Wikilink Integration

Entries in `workspace/journal/` can reference `[[knowledge-base/notes/...]]` notes. Use the knowledge-base skill to create and manage cross-references.
