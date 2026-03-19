---
name: scraper
description: Extract clean readable content from any webpage. Invoke when user asks "read this website", "scrape this page", "extract content from URL", "what does this article say", or "summarize this page". Uses r.jina.ai reader API — no installation required.
version: 1.0.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [web, scraping, content-extraction, reader, URLs]
prerequisites:
  commands: [curl]
---

# Web Scraper

Converts a webpage to Markdown for easier reading by the LLM.

## Requirements

- **Tool**: `r.jina.ai` (Free Reader API) - No installation needed, just use `curl`.

## Cross-Platform Method (Python)

Works on Windows, Linux, and Mac.

1.  **Run Script**:
    ```bash
    python workspace/skills/scraper/scripts/scrape.py "https://example.com"
    ```

## Commands (Bash/Linux/Mac)

### Read Page (Markdown)

Fetches the URL and converts it to clean Markdown.

```bash
curl -s "https://r.jina.ai/https://example.com"
```

### Read Page (Text Only)

Fetches the URL and returns plain text.

```bash
curl -s -H "Accept: text/plain" "https://r.jina.ai/https://example.com"
```

## Usage

Ghost uses this to "read" documentation, news articles, or blog posts that are otherwise too cluttered with HTML/JS for simple analysis.
