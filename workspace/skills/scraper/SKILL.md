---
name: scraper
description: Extract clean text content from any webpage. Invoke when user asks to "read this website", "scrape this page", "extract content from URL", "what does this article say", or "summarize this page". Uses r.jina.ai reader mode — no API key, no installation required.
version: 1.1.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [web, scraping, content-extraction, reader, URLs]
prerequisites:
  commands: [curl]
---

# Web Scraper

Extracts clean, readable text from any URL using r.jina.ai.

## Quick Reference

| Task | Command |
|------|---------|
| Extract content | `curl -s https://r.jina.ai/https://example.com` |
| Extract + markdown | `curl -s https://r.jina.ai/m/https://example.com` |
| Batch (newline-sep) | `curl -s -X POST https://r.jina.ai/m/ -H "Content-Type: text/plain" -d "url1\nurl2"` |
| Meta description only | `curl -s https://r.jina.ai/meta/https://example.com` |

## Single URL

```bash
curl -s "https://r.jina.ai/https://example.com"
```

Returns: clean article text, no HTML, no ads, no trackers.

## Markdown Output

```bash
curl -s "https://r.jina.ai/m/https://example.com"
```

Returns: Markdown-formatted content. Use this when structure (headings, lists, code blocks) matters.

## Batch Extraction

```bash
curl -s -X POST "https://r.jina.ai/m/" \
  -H "Content-Type: text/plain" \
  -d "https://news.ycombinator.com/news
https://example.com/article"
```

Each URL on its own line. Results separated by `\n---\n`.

## Parse Response

```bash
curl -s "https://r.jina.ai/https://example.com" | python3 -c "
import sys
lines = sys.stdin.read().strip().split('\n')
print(lines[0][:200] if lines else 'No content')
"
```

## Limitations

- JavaScript-heavy sites (React/Vue SPAs) may render poorly — r.jina.ai handles most but not all
- Rate limits: avoid scraping the same site repeatedly in short bursts
- Some paywalled or login-protected content cannot be extracted
- `r.jina.ai` may return empty for sites that block crawlers

## robots.txt

Check and respect robots.txt before scraping:
```bash
curl -s https://example.com/robots.txt | grep -i "disallow"
```

## Timeout

Set a timeout to avoid hanging on slow sites:
```bash
curl -s --max-time 10 "https://r.jina.ai/https://slow-site.com"
```

## JavaScript-Heavy Sites

If r.jina.ai returns thin content, try with a different approach:

```bash
# Try with screenshot mode (if available)
curl -s "https://r.jina.ai/screenshot/https://example.com"
```

Or fall back to `python3 -c "$(curl -s url)"` for basic HTML parsing with BeautifulSoup.
