---
name: calendar
description: "View and manage Google Calendar events. List upcoming events, create new ones, check availability."
metadata: {"nanobot":{"emoji":"📅","requires":{"bins":["curl"]}}}
---

# Calendar

Interact with Google Calendar using the CalDAV/REST API or `gcalcli` if available.

## Option 1: gcalcli (recommended if installed)

### List upcoming events
```bash
gcalcli agenda --nocolor --tsv 2>/dev/null || echo "gcalcli not installed"
```

### Today's schedule
```bash
gcalcli agenda "today" "tomorrow" --nocolor
```

### This week
```bash
gcalcli agenda "today" "next monday" --nocolor
```

### Add event
```bash
gcalcli add --title "Meeting with Team" --when "2026-02-14 14:00" --duration 60 --where "Office" --noprompt
```

### Search events
```bash
gcalcli search "standup" --nocolor
```

## Option 2: Google Calendar API (via curl)

Requires OAuth2 token. If not available, suggest user to install `gcalcli`:

```bash
# Install gcalcli
pip install gcalcli
# First run will open browser for OAuth
gcalcli list
```

## Option 3: ICS file management

For simple use, manage `.ics` files in workspace:

### Create ICS event
```
BEGIN:VCALENDAR
VERSION:2.0
BEGIN:VEVENT
DTSTART:20260214T140000
DTEND:20260214T150000
SUMMARY:Meeting with Team
LOCATION:Office
DESCRIPTION:Weekly sync
END:VEVENT
END:VCALENDAR
```

Save to `{workspace}/calendar/` and user can import to any calendar app.

## Quick Date/Time Reference
```bash
# Current time
date "+%Y-%m-%d %H:%M %Z"
# Day of week
date "+%A"
# This week range
date -v-$(date +%u)d "+%Y-%m-%d"  # Monday (macOS)
```

## Tips
- Always confirm timezone with user (default: user's system timezone)
- When creating events, confirm details before adding
- For recurring events, mention the recurrence pattern
- Show events in a clean table format
