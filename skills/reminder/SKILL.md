---
name: reminder
description: "Manage reminders and todo lists. Create, list, complete, and delete tasks. Supports due dates and priorities."
metadata: {"nanobot":{"emoji":"📋","requires":{"bins":["date"]}}}
---

# Reminder & Todo

Manage reminders and todo lists using workspace files. All data is stored in `{workspace}/memory/todos.md`.

## File Format

```markdown
# Todo List

## Active
- [ ] 🔴 [2026-02-14] Buy Valentine's gift
- [ ] 🟡 [2026-02-15] Review pull request
- [ ] 🟢 Clean desk

## Completed
- [x] ~~Submit report~~ ✅ 2026-02-13
```

Priority: 🔴 high · 🟡 medium · 🟢 low (default)

## Operations

### Add a task
Read the file, append under `## Active`, write back. Include priority emoji and optional due date.

### List tasks
Read and display the file. Group by priority or due date as requested.

### Complete a task
Move from `## Active` to `## Completed`, add ✅ and completion date, apply ~~strikethrough~~.

### Delete a task
Remove the line entirely.

### Due today / overdue
Parse `[YYYY-MM-DD]` dates, compare with current date:
```bash
date +%Y-%m-%d
```

## Reminder Check (on every conversation)
When memory is loaded, check `todos.md` for overdue items and notify user proactively.

## Tips
- Always preserve existing content when editing
- Sort active tasks: overdue first, then by date, then by priority
- Use Vietnamese or English based on user's language
