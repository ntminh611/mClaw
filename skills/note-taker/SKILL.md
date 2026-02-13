---
name: note-taker
description: "Smart note-taking system. Create, organize, search, and link notes with tags and categories."
metadata: {"nanobot":{"emoji":"📝","requires":{"bins":["date"]}}}
---

# Note Taker

Organize notes in `{workspace}/notes/` as Markdown files.

## Directory Structure
```
{workspace}/notes/
├── index.md          # Table of contents (auto-generated)
├── daily/            # Daily notes
│   └── 2026-02-13.md
├── topics/           # Topic-based notes
│   ├── project-x.md
│   └── ideas.md
└── meetings/         # Meeting notes
    └── 2026-02-13-standup.md
```

## File Format

```markdown
---
title: Project X Notes
tags: [project, development, golang]
created: 2026-02-13
updated: 2026-02-13
---

# Project X Notes

## Key Points
- Point one
- Point two

## Action Items
- [ ] Task from this note

## Related
- [[daily/2026-02-13]] - discussed in daily
- [[topics/ideas]] - originated from brainstorm
```

## Operations

### Create note
```bash
# Daily note
date +%Y-%m-%d  # for filename
```
Write to appropriate subdirectory. Auto-create directory if needed.

### Quick capture
Append to today's daily note (`daily/YYYY-MM-DD.md`). Create if doesn't exist.

### Search notes
```bash
# Search all notes for keyword
grep -ril "keyword" {workspace}/notes/ 2>/dev/null
```

### List recent notes
```bash
# Last 10 modified notes
find {workspace}/notes/ -name "*.md" -type f -printf "%T@ %p\n" 2>/dev/null | sort -rn | head -10 | cut -d' ' -f2-
# macOS:
find {workspace}/notes/ -name "*.md" -type f -exec stat -f "%m %N" {} \; 2>/dev/null | sort -rn | head -10 | cut -d' ' -f2-
```

### Update index
After creating/modifying notes, regenerate `index.md` with links to all notes grouped by category.

## Tips
- Use `[[wiki-links]]` for cross-referencing
- Always add tags in frontmatter for searchability
- Daily notes: capture quick thoughts, link to topic notes for details
- Meeting notes: include date, attendees, decisions, action items
- Keep notes concise—prefer bullet points over paragraphs
