---
name: email
description: "Read, compose, and manage emails. Summarize inbox, draft replies, search messages."
metadata: {"nanobot":{"emoji":"📧","requires":{"bins":["curl"]}}}
---

# Email

Manage email via `himalaya` (IMAP/SMTP) or draft mode.

## Option 1: himalaya (IMAP/SMTP client)

```bash
himalaya list --max-width 120
himalaya read <id>
himalaya search "subject:invoice from:company.com"
himalaya send <<EOF
From: user@example.com
To: recipient@example.com
Subject: Re: Meeting tomorrow

Hi, thanks for the update.
EOF
```

## Option 2: Draft mode (no email client needed)

Compose drafts in `{workspace}/drafts/email-YYYY-MM-DD-subject.md`:

```markdown
---
to: recipient@example.com
subject: Re: Meeting tomorrow
---

Hi, thanks for the update. I'll be there at 2pm.
```

User can copy/paste into their email client.

## Email Composition Tips
- Match language to context (formal/informal)
- Keep subject lines concise (<60 chars)
- For Vietnamese users: default to Vietnamese unless writing to international contacts
- Always ask before actually sending
- Offer to translate if needed
