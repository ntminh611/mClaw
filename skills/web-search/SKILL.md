---
name: web-search
description: "Search the web intelligently using Brave Search API. Summarize results, follow up with web_fetch for details."
metadata: {"nanobot":{"emoji":"🔍"}}
---

# Web Search

Use the `web_search` tool (Brave Search API) for finding information, then `web_fetch` or `browser` for detailed content.

## Strategy

### 1. Quick answers
Single search, summarize top results:
```
web_search(query="current Bitcoin price USD")
```

### 2. Research mode
Multiple searches with different angles, then fetch relevant pages:
```
web_search(query="Next.js vs Remix comparison 2026")
→ pick best URLs
web_fetch(url="https://best-article.com/comparison")
→ synthesize findings
```

### 3. Follow-up
If search results are shallow, fetch the most promising URL:
```
web_fetch(url="<top_result_url>", maxChars=30000)
```

For JavaScript-heavy sites (SPAs), use browser tool:
```
browser(url="<spa_url>", wait_seconds=3)
```

## Query Tips
- Be specific: "Python FastAPI JWT authentication tutorial" > "python auth"
- Add year for freshness: "best laptops 2026"
- Use quotes for exact phrases: `"error code 0x80070005"`
- Add site filter in query: "site:github.com fastapi template"

## Response Format
- Cite sources with URLs
- Summarize key points, don't just dump raw text
- If multiple conflicting answers, present both sides
- For Vietnamese users, translate key findings to Vietnamese

## Limitations
- Requires `tools.web.search.api_key` (Brave API key)
- Max 10 results per query
- For real-time data (stock, crypto), prefer specialized APIs
