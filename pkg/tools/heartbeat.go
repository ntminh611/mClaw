package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ntminh611/mclaw/pkg/heartbeat"
)

// HeartbeatTool allows the AI agent to manage heartbeat notes
type HeartbeatTool struct {
	service *heartbeat.HeartbeatService
}

func NewHeartbeatTool() *HeartbeatTool {
	return &HeartbeatTool{}
}

func (t *HeartbeatTool) SetHeartbeatService(hs *heartbeat.HeartbeatService) {
	t.service = hs
}

func (t *HeartbeatTool) Name() string {
	return "heartbeat"
}

func (t *HeartbeatTool) Description() string {
	return `Manage heartbeat notes. The bot reviews these periodically and acts on them. Actions:
- "add": Add a new note. Requires: content. Optional: category (reminder, task, note, instruction).
- "list": List all heartbeat notes.
- "remove": Remove a note by ID. Requires: note_id.
- "enable": Enable a note. Requires: note_id.
- "disable": Disable a note. Requires: note_id.
Use this for periodic reminders, tasks, or instructions the bot should check regularly.`
}

func (t *HeartbeatTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"description": "Action to perform: add, list, remove, enable, disable",
				"enum":        []string{"add", "list", "remove", "enable", "disable"},
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "Content of the note (required for add)",
			},
			"category": map[string]interface{}{
				"type":        "string",
				"description": "Category: reminder, task, note, instruction (default: note)",
				"enum":        []string{"reminder", "task", "note", "instruction"},
			},
			"note_id": map[string]interface{}{
				"type":        "string",
				"description": "Note ID (required for remove/enable/disable)",
			},
		},
		"required": []string{"action"},
	}
}

func (t *HeartbeatTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	if t.service == nil {
		return "Error: Heartbeat service not available", nil
	}

	action, _ := args["action"].(string)

	switch action {
	case "add":
		return t.addNote(args)
	case "list":
		return t.listNotes()
	case "remove":
		return t.removeNote(args)
	case "enable":
		return t.enableNote(args, true)
	case "disable":
		return t.enableNote(args, false)
	default:
		return fmt.Sprintf("Unknown action: %s. Use: add, list, remove, enable, disable", action), nil
	}
}

func (t *HeartbeatTool) addNote(args map[string]interface{}) (string, error) {
	content, _ := args["content"].(string)
	if content == "" {
		return "Error: 'content' is required for add", nil
	}

	category, _ := args["category"].(string)

	note, err := t.service.AddNote(content, category)
	if err != nil {
		return fmt.Sprintf("Error adding note: %v", err), nil
	}

	return fmt.Sprintf("✓ Added heartbeat note (ID: %s)\n  Category: %s\n  Content: %s",
		note.ID, note.Category, note.Content), nil
}

func (t *HeartbeatTool) listNotes() (string, error) {
	notes := t.service.ListNotes(true)

	if len(notes) == 0 {
		return "No heartbeat notes.", nil
	}

	type noteInfo struct {
		ID        string `json:"id"`
		Content   string `json:"content"`
		Category  string `json:"category"`
		Enabled   bool   `json:"enabled"`
		CreatedAt string `json:"created_at"`
	}

	var result []noteInfo
	for _, note := range notes {
		result = append(result, noteInfo{
			ID:        note.ID,
			Content:   note.Content,
			Category:  note.Category,
			Enabled:   note.Enabled,
			CreatedAt: time.UnixMilli(note.CreatedAtMS).Format("2006-01-02 15:04"),
		})
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	return fmt.Sprintf("Heartbeat notes (%d):\n%s", len(result), string(data)), nil
}

func (t *HeartbeatTool) removeNote(args map[string]interface{}) (string, error) {
	noteID, _ := args["note_id"].(string)
	if noteID == "" {
		return "Error: 'note_id' is required for remove", nil
	}

	if t.service.RemoveNote(noteID) {
		return fmt.Sprintf("✓ Removed note %s", noteID), nil
	}
	return fmt.Sprintf("Note %s not found", noteID), nil
}

func (t *HeartbeatTool) enableNote(args map[string]interface{}, enable bool) (string, error) {
	noteID, _ := args["note_id"].(string)
	if noteID == "" {
		return "Error: 'note_id' is required", nil
	}

	note := t.service.EnableNote(noteID, enable)
	if note == nil {
		return fmt.Sprintf("Note %s not found", noteID), nil
	}

	status := "enabled"
	if !enable {
		status = "disabled"
	}
	return fmt.Sprintf("✓ Note '%s' %s", note.Content[:min(50, len(note.Content))], status), nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
