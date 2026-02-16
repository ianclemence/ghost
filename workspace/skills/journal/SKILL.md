---
name: "journal"
description: "Logs daily notes, thoughts, or events to a markdown file. Invoke when user says 'Log that...', 'Write in my journal', or 'Note to self'."
---

# Journal / Note Taker

Appends text to a daily markdown file in the `workspace/journal` directory.

## Commands

### Log Entry

Appends a timestamped entry to today's file (e.g., `2024-05-21.md`).

**Windows (PowerShell):**

```powershell
$date = Get-Date -Format "yyyy-MM-dd"; $time = Get-Date -Format "HH:mm"; $entry = "I felt tired today."; Add-Content -Path "workspace\journal\$date.md" -Value "- **$time**: $entry"
```

**Linux / Pi:**

```bash
mkdir -p workspace/journal
echo "- **$(date +%H:%M)**: I felt tired today." >> workspace/journal/$(date +%Y-%m-%d).md
```

## Usage

Ghost will automatically replace the "I felt tired today" part with whatever the user dictates.
