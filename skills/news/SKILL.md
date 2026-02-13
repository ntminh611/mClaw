---
name: news
description: "Get latest news from RSS feeds and news APIs. Summarize headlines, read articles, track topics."
metadata: {"nanobot":{"emoji":"📰","requires":{"bins":["curl"]}}}
---

# News

Fetch news from free sources using `web_fetch` or `curl`.

## RSS Feeds (primary, no API key)

### Tech News
```bash
# Hacker News top stories
curl -s "https://hacker-news.firebaseio.com/v0/topstories.json" | python3 -c "import json,sys; ids=json.load(sys.stdin)[:10]; [print(json.dumps(json.loads(open('/dev/stdin','r').read()))) for _ in ['']]" 2>/dev/null
```

Simpler approach — use `web_fetch`:
```
web_fetch(url="https://hn.algolia.com/api/v1/search?tags=front_page&hitsPerPage=10")
```

### Vietnamese News
```
web_fetch(url="https://vnexpress.net/rss/tin-moi-nhat.rss")
web_fetch(url="https://tuoitre.vn/rss/tin-moi-nhat.rss")
web_fetch(url="https://thanhnien.vn/rss/home.rss")
```

### Global News
```
web_fetch(url="https://feeds.bbci.co.uk/news/rss.xml")
web_fetch(url="https://rss.nytimes.com/services/xml/rss/nyt/HomePage.xml")
```

### Tech / Dev
```
web_fetch(url="https://dev.to/feed")
web_fetch(url="https://techcrunch.com/feed/")
```

## Summarization Strategy

1. Fetch RSS feed → extract titles and links
2. For interesting articles, use `web_fetch` to get full content
3. Summarize in user's preferred language
4. Group by topic if multiple articles

## Response Format

```
📰 **Tin tức hôm nay** (13/02/2026)

**🌍 Quốc tế**
1. [Title](url) — 2-line summary
2. [Title](url) — 2-line summary

**🇻🇳 Việt Nam**
1. [Title](url) — 2-line summary

**💻 Công nghệ**
1. [Title](url) — 2-line summary
```

## Tips
- Default to Vietnamese news for Vietnamese users
- Offer to deep-dive into any article
- Cache results mentally—don't re-fetch within same conversation
- For breaking news, use `web_search` with recent time filter
