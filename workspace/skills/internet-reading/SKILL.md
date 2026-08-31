---
name: internet-reading
description: Read and search the web cleanly without API keys or accounts. Invoke when the user asks to "read this link", "what's on this page", "summarize this website", "what does this YouTube video say", "transcribe this video", "search YouTube", "what's new in this RSS feed", or "check this feed". Uses Jina Reader, yt-dlp, and feedparser — all free, no API keys, no logins.
version: 1.0.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [web, reading, search, youtube, rss, transcript]
prerequisites:
  commands: [curl, yt-dlp]
---

# Internet Reading

Read the internet the clean way: plain text in, plain text out. Three narrow
capabilities, all free and no login. This is opt-in: if `curl`, `yt-dlp`, or
`pip` are missing, tell the user what to install rather than guessing.

## 1. Read any web page as clean text (Jina Reader)

Free, no API key, no account:

```bash
curl -s "https://r.jina.ai/<full-url>"
```

Returns the page as lightweight markdown. For pages that need JS or block bots,
this is far more reliable than raw `curl` of the HTML.

- One request per page; don't hammer it.
- If it's a login wall or a paywall, say so — don't try to bypass it.

## 2. YouTube: transcript + search (yt-dlp)

Install once: `pip install yt-dlp` (or `pip install --user yt-dlp`).

Get a video transcript (subtitles):

```bash
yt-dlp --write-auto-subs --sub-lang en --skip-download -o "%(title)s" "https://www.youtube.com/watch?v=VIDEO_ID"
yt-dlp --list-subs "https://www.youtube.com/watch?v=VIDEO_ID"
```

Search YouTube (best-effort, no key):

```bash
yt-dlp "ytsearch5:your search terms" --print "%(title)s | https://www.youtube.com/watch?v=%(id)s" --skip-download
```

Then read the audio/content with the media or summarize skill when asked for a
summary. Respect the video's terms; don't re-upload.

## 3. RSS / Atom feeds (feedparser)

Install once: `pip install feedparser`.

```bash
python3 - <<'PY'
import feedparser
d = feedparser.parse("https://example.com/feed.xml")
for e in d.entries[:10]:
    print(e.get("title"), "—", e.get("link"))
PY
```

Use it to summarize what's new or watch a feed for a topic.

## Rules

- Never fake a result. If an API is blocked or a feed is empty, say so.
- Don't attempt to bypass paywalls, logins, or geo-blocks.
- Keep results short and useful — give the user the answer, not a wall of
  source text.
- These are read-only. Never post, comment, or take actions on the user's
  behalf.
