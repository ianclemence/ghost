---
name: organizer
description: Sort messy directories into categorized subfolders by file type. Invoke when user asks "organize my downloads", "sort files", "tidy up folder", "move files into folders", or "categorize this directory". SAFETY: never touch project roots or system directories without explicit confirmation.
version: 1.1.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [files, organization, directory, cleanup, productivity]
prerequisites:
  commands: [python]
---

# File Organizer

Sorts files in a directory into subfolders by extension. Dry-run by default.

## Category Mapping

| Category | Extensions |
|----------|------------|
| Images | jpg, jpeg, png, gif, webp, svg, bmp, ico, heic |
| Videos | mp4, mkv, avi, mov, wmv, flv, webm |
| Audio | mp3, wav, flac, aac, ogg, m4a, opus |
| Documents | pdf, doc, docx, txt, rtf, odt, xls, xlsx, ppt, pptx |
| Archives | zip, tar, gz, rar, 7z, bz2 |
| Code | py, js, ts, go, java, c, cpp, h, sh, rs, rb, php |
| Data | json, xml, csv, yaml, yml, toml, sql, db |
| Books | epub, mobi, azw3, djvu |
| Web | html, htm, css, js (single files) |

## Preview / Dry Run (Always Do First)

Always preview before moving anything:

```bash
python3 -c "
import os, shutil
from pathlib import Path

src = Path('workspace/downloads')  # <-- change this
categories = {
    'Images': ['.jpg','.jpeg','.png','.gif','.webp','.svg','.bmp'],
    'Videos': ['.mp4','.mkv','.avi','.mov','.wmv'],
    'Audio': ['.mp3','.wav','.flac','.aac','.ogg'],
    'Documents': ['.pdf','.doc','.docx','.txt','.rtf'],
    'Archives': ['.zip','.tar','.gz','.rar','.7z'],
}

for f in src.iterdir():
    if f.is_file():
        ext = f.suffix.lower()
        cat = 'Other'
        for c, exts in categories.items():
            if ext in exts:
                cat = c
                break
        print(f'{cat}/{f.name}')
"
```

## Execute Sort

```bash
python3 -c "
import os, shutil
from pathlib import Path

src = Path('workspace/downloads')  # <-- change this
log_file = Path('workspace/organizer_log.txt')

categories = {
    'Images': ['.jpg','.jpeg','.png','.gif','.webp','.svg','.bmp'],
    'Videos': ['.mp4','.mkv','.avi','.mov','.wmv'],
    'Audio': ['.mp3','.wav','.flac','.aac','.ogg'],
    'Documents': ['.pdf','.doc','.docx','.txt','.rtf'],
    'Archives': ['.zip','.tar','.gz','.rar','.7z'],
    'Code': ['.py','.js','.ts','.go','.sh','.rb'],
    'Data': ['.json','.xml','.csv','.yaml'],
}

log = []
for f in src.iterdir():
    if f.is_file():
        ext = f.suffix.lower()
        cat = 'Other'
        for c, exts in categories.items():
            if ext in exts:
                cat = c
                break
        dest = src / cat
        dest.mkdir(exist_ok=True)
        if f != dest / f.name:  # skip if already in right place
            shutil.move(str(f), str(dest / f.name))
            log.append(f'{f.name} -> {cat}/')
            print(f'OK: {f.name} -> {cat}/')

with open(log_file, 'a') as l:
    l.write('\n'.join(log) + '\n')
"
```

## Undo (Roll Back)

The script logs moves to `workspace/organizer_log.txt`. To undo:

```bash
python3 -c "
from pathlib import Path
log = Path('workspace/organizer_log.txt')
if log.exists():
    for line in reversed(open(log).readlines()):
        line = line.strip()
        if '->' in line:
            src_name, dest_cat = line.split(' -> ')
            dest_cat = dest_cat.rstrip('/')
            src = Path('workspace/downloads') / dest_cat / src_name
            dst = Path('workspace/downloads') / src_name
            if src.exists():
                src.rename(dst)
                print(f'Restored: {src_name}')
"
```

## Custom Categories

Pass additional extensions via a dict. Edit the `categories` dict in the script to add user-specific extensions (e.g., `.blend` for Blender, `.psd` for Photoshop).

## Safety Rules

1. **Always dry-run first** — inspect output before executing the move
2. **Never organize `workspace/` root** — only subdirectories like `workspace/downloads/`, `workspace/temp/`
3. **Confirm before touching** hidden files (`.` prefix) or system directories
4. **Skip open files** — if a file is locked/in-use, skip and report
