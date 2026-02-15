---
name: calendar
description: "View and manage calendar events. List upcoming events, create new ones, check availability."
metadata: {"nanobot":{"emoji":"📅","requires":{"bins":["curl"]}}}
---

# Calendar

Manage calendar events via `gcalcli` or ICS files.

## Option 1: gcalcli

```bash
gcalcli agenda --nocolor --tsv 2>/dev/null || echo "gcalcli not installed"
gcalcli agenda "today" "tomorrow" --nocolor
gcalcli agenda "today" "next monday" --nocolor
gcalcli add --title "Meeting" --when "2026-02-14 14:00" --duration 60 --noprompt
gcalcli search "standup" --nocolor
```

## Option 2: ICS file management

For simple use, save `.ics` files to `{workspace}/calendar/` — user can import to any calendar app.

## Tips
- Always confirm timezone with user (default: system timezone)
- When creating events, confirm details before adding
- For recurring events, mention the recurrence pattern
- Show events in a clean table format
