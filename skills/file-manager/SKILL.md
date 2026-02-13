---
name: file-manager
description: "Manage files and directories. Search, organize, compress, convert, and analyze files."
metadata: {"nanobot":{"emoji":"📁","requires":{"bins":["find","tar"]}}}
---

# File Manager

Manage files using shell commands via the `exec` tool.

## Search

### Find files by name
```bash
find /path -name "*.go" -type f 2>/dev/null | head -20
```

### Find by content
```bash
grep -rl "search term" /path --include="*.md" 2>/dev/null | head -20
```

### Find large files
```bash
find /path -type f -size +10M -exec ls -lh {} \; 2>/dev/null | sort -k5 -rh | head -10
```

### Find recently modified
```bash
find /path -type f -mtime -7 -name "*.md" 2>/dev/null | head -20
```

## Disk Usage

### Directory sizes
```bash
du -sh /path/*/ 2>/dev/null | sort -rh | head -10
```

### Total size
```bash
du -sh /path 2>/dev/null
```

## Compress / Archive

### Create tar.gz
```bash
tar -czf archive.tar.gz -C /parent directory_name
```

### Create zip
```bash
zip -r archive.zip /path/to/directory
```

### Extract
```bash
tar -xzf archive.tar.gz -C /destination
unzip archive.zip -d /destination
```

## Organize

### Count files by extension
```bash
find /path -type f | sed 's/.*\.//' | sort | uniq -c | sort -rn | head -20
```

### Move files by type
```bash
# Example: organize downloads
mkdir -p ~/organized/{images,docs,videos,archives}
find /path -maxdepth 1 -name "*.jpg" -o -name "*.png" -exec mv {} ~/organized/images/ \;
find /path -maxdepth 1 -name "*.pdf" -o -name "*.docx" -exec mv {} ~/organized/docs/ \;
```

### Remove duplicates (by content, dry-run first)
```bash
# Find duplicate files (same size + md5)
find /path -type f -exec md5sum {} \; 2>/dev/null | sort | uniq -d -w32
```

## File Info

### File details
```bash
file /path/to/file
stat /path/to/file
wc -l /path/to/file  # line count
```

### Tree view
```bash
find /path -maxdepth 2 | head -50 | sed 's|[^/]*/|  |g'
# or if tree is available:
tree -L 2 /path 2>/dev/null || find /path -maxdepth 2 | head -50
```

## Safety
- Always confirm before deleting or moving files
- Use `ls` or `find` to preview before bulk operations
- Suggest `cp` before destructive operations
- Never use `rm -rf` without explicit user confirmation
