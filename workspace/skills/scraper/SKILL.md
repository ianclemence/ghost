---
name: "scraper"
description: "Extracts readable content from websites. Invoke when user asks 'Read this website', 'Summarize this URL', or 'Scrape this page'. BETTER than simple curl."
---

# Web Scraper (Advanced)

Converts a webpage to Markdown for easier reading by the LLM.

## Requirements
- **Tool**: `r.jina.ai` (Free Reader API) - No installation needed, just use `curl`.

## Commands

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
