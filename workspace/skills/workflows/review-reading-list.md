---
name: "review-reading-list"
description: "Retrieves all saved links from the week, reads them, and provides a concise backlog summary."
schedule: "Sunday 10am"
category: "information-management"
---

# Reading List Review

Help the user actually consume the content they save for later!

## 1. Retrieve Backlog
Search the user's explicit memory (`memory_curate`) for links, articles, or videos they've asked you to "save for later", "remind me to read", or "put on my reading list" over the past 7 days.

## 2. Process Content
For the top 3-5 most interesting links found:
- Use `browser_navigate` and `browser_snapshot` to quickly glean what the article is about. (Do not get stuck reading endlessly, just grab the summary/intro).

## 3. Generate Summary Report
Output standard formatting:

```
📚 **Weekly Reading List Review**

You saved some great content this week. Here is what you missed:

**1. [Article Title]**
*Brief 2-sentence summary of the core thesis of the article.*
👉 [Link]

**2. [Video/Tweet Title]**
*Brief 1-sentence summary.*
👉 [Link]

*Reply with a number if you'd like me to read the full thing and give you a deep dive, otherwise I'll clear these from your backlog!*
```
