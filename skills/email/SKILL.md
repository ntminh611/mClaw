---
name: email
description: "Read, compose, and manage emails. Summarize inbox, draft replies, search messages."
metadata: {"nanobot":{"emoji":"📧","requires":{"bins":["curl"]}}}
---

# Email

Manage email via CLI tools or workspace drafts.

## Option 1: himalaya (recommended CLI email client)

### Check inbox
```bash
himalaya list --max-width 120 2>/dev/null || echo "himalaya not installed"
```

### Read a message
```bash
himalaya read <id>
```

### Search
```bash
himalaya search "subject:invoice from:company.com"
```

### Send
```bash
himalaya send <<EOF
From: user@example.com
To: recipient@example.com
Subject: Re: Meeting tomorrow

Hi,

Thanks for the update. I'll be there at 2pm.

Best regards
EOF
```

## Option 2: Draft mode (no email client needed)

When no email client is available, compose drafts in workspace:

### Save draft
Write to `{workspace}/drafts/email-YYYY-MM-DD-subject.md`:

```markdown
---
to: recipient@example.com
cc: 
subject: Re: Meeting tomorrow
---

Hi,

Thanks for the update. I'll be there at 2pm.

Best regards
```

User can copy/paste into their email client.

## Option 3: neomutt / mustreak
```bash
# Check if available
which neomutt mutt 2>/dev/null
```

## Email Composition Tips
- Match language to the context (formal/informal)
- Keep subject lines concise (<60 chars)
- For Vietnamese users: default to Vietnamese unless writing to international contacts
- Always ask before actually sending
- Offer to translate if needed

## Install himalaya
```bash
# macOS
brew install himalaya
# Linux
cargo install himalaya
# Config: ~/.config/himalaya/config.toml
```
