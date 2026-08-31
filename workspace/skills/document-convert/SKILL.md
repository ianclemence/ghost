---
name: document-convert
description: Convert office and text documents to clean Markdown the agent can read — Word, PowerPoint, Excel, OpenDocument, RTF, EPUB, CSV, and PDF. Invoke when the user shares or points at a document and asks to "read this", "summarize this file", "extract the text", "what's in this PDF", or "turn this into notes". Uses anydoc (Python) — free, on-device, no API key.
version: 1.0.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [documents, pdf, docx, pptx, xlsx, convert, markdown]
prerequisites:
  commands: [python]
---

# Document Conversion

Turn any office document into clean Markdown so the agent can read it into
memory, summarize it, or answer questions about it. Uses
[anydoc](https://github.com/firecrawl/anydoc) — a small Rust library with a
Python binding. It converts locally: no API key, no account, nothing leaves the
device (unless a scanned PDF needs hosted OCR, which we avoid by default).

## One-time setup

```bash
pip install firecrawl-anydoc
```

## Convert a file to Markdown

```bash
python3 - <<'PY'
import anydoc
md = anydoc.to_markdown("/path/to/file.docx")   # docx, pptx, xlsx, odt, ods, odp, rtf, epub, csv, pdf
print(md)
PY
```

For a file you want to keep, write the markdown somewhere the user can see:

```bash
python3 - <<'PY'
import anydoc, pathlib
src = "/path/to/file.docx"
md = anydoc.to_markdown(src)
out = pathlib.Path("/path/to/notes.md")
out.write_text(md, encoding="utf-8")
print("wrote", out)
PY
```

## What to do

1. If the user attached or referenced a document, convert it with anydoc.
2. Then do what they asked: summarize, extract key points, answer a question,
   or save it into their knowledge base / memory.
3. Keep the conversion local. Do not send documents to external OCR or
   conversion services.

## Rules

- If `firecrawl-anydoc` isn't installed, install it and retry, then tell the
  user it's ready. Don't ask them to do it.
- If a PDF is scanned/image-only, anydoc errors with `NeedsOcr`. Say so
  honestly and offer the user a different path — don't silently skip content.
- Respect document ownership; only convert documents the user gave you.
