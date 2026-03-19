---
name: nano-pdf
description: Edit existing PDF files with natural-language instructions. Invoke when user asks to "change the title in this PDF", "fix the typo on page 3", "edit text in a PDF", or "modify page X of a PDF document". Uses nano-pdf CLI with uv/pip installation.
version: 1.0.0
author: community
license: MIT
metadata:
  ghost:
    tags: [PDF, Documents, Editing, NLP, Productivity]
    homepage: https://pypi.org/project/nano-pdf/
prerequisites:
  commands: [nano-pdf]
---

# nano-pdf

Edit PDFs using natural-language instructions. Point it at a page and describe what to change.

## Prerequisites

```bash
# Install with uv (recommended — already available in Hermes)
uv pip install nano-pdf

# Or with pip
pip install nano-pdf
```

## Usage

```bash
nano-pdf edit <file.pdf> <page_number> "<instruction>"
```

## Examples

```bash
# Change a title on page 1
nano-pdf edit deck.pdf 1 "Change the title to 'Q3 Results' and fix the typo in the subtitle"

# Update a date on a specific page
nano-pdf edit report.pdf 3 "Update the date from January to February 2026"

# Fix content
nano-pdf edit contract.pdf 2 "Change the client name from 'Acme Corp' to 'Acme Industries'"
```

## Notes

- Page numbers may be 0-based or 1-based depending on version — if the edit hits the wrong page, retry with ±1
- Always verify the output PDF after editing (use `read_file` to check file size, or open it)
- The tool uses an LLM under the hood — requires an API key (check `nano-pdf --help` for config)
- Works well for text changes; complex layout modifications may need a different approach
