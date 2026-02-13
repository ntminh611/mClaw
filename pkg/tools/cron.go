package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ntminh611/mclaw/pkg/cron"
)

// CronTool allows the AI agent to create, list, remove, and manage scheduled jobs
type CronTool struct {
	cronService    *cron.CronService
	defaultChannel string
	defaultChatID  string
}

func NewCronTool() *CronTool {
	return &CronTool{}
}

func (t *CronTool) SetCronService(cs *cron.CronService) {
	t.cronService = cs
}

// SetContext sets the default channel and chatID for delivery
func (t *CronTool) SetContext(channel, chatID string) {
	t.defaultChannel = channel
	t.defaultChatID = chatID
}

func (t *CronTool) Name() string {
	return "cron"
}

func (t *CronTool) Description() string {
	return `Manage scheduled/recurring tasks (cron jobs). Actions:
- "add": Create a new scheduled job. Requires: name, message, schedule_type ("every" or "at"), interval_seconds (for "every") or run_at_iso (for "at"). Optional: deliver (bool), channel, to (chat_id).
- "list": List all active scheduled jobs.
- "remove": Remove a job by ID. Requires: job_id.
- "enable": Enable a disabled job. Requires: job_id.
- "disable": Disable a job. Requires: job_id.
When deliver=true, the job result will be sent to the specified channel/chat.`
}

func (t *CronTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"description": "Action to perform: add, list, remove, enable, disable",
				"enum":        []string{"add", "list", "remove", "enable", "disable"},
			},
			"name": map[string]interface{}{
				"type":        "string",
				"description": "Job name (required for add)",
			},
			"message": map[string]interface{}{
				"type":        "string",
				"description": "The prompt/message the agent will process when the job runs (required for add)",
			},
			"schedule_type": map[string]interface{}{
				"type":        "string",
				"description": "Schedule type: 'every' for recurring, 'at' for one-time",
				"enum":        []string{"every", "at"},
			},
			"interval_seconds": map[string]interface{}{
				"type":        "number",
				"description": "Interval in seconds for 'every' schedule type (e.g. 3600 = 1 hour)",
			},
			"run_at_iso": map[string]interface{}{
				"type":        "string",
				"description": "ISO 8601 datetime for 'at' schedule type (e.g. '2026-02-14T09:00:00+07:00')",
			},
			"deliver": map[string]interface{}{
				"type":        "boolean",
				"description": "Whether to deliver the result to a chat channel (default: true)",
			},
			"channel": map[string]interface{}{
				"type":        "string",
				"description": "Target channel for delivery (e.g. 'telegram')",
			},
			"to": map[string]interface{}{
				"type":        "string",
				"description": "Target chat/user ID for delivery",
			},
			"job_id": map[string]interface{}{
				"type":        "string",
				"description": "Job ID (required for remove/enable/disable)",
			},
		},
		"required": []string{"action"},
	}
}

func (t *CronTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	if t.cronService == nil {
		return "Error: Cron service not available", nil
	}

	action, _ := args["action"].(string)

	switch action {
	case "add":
		return t.addJob(args)
	case "list":
		return t.listJobs()
	case "remove":
		return t.removeJob(args)
	case "enable":
		return t.enableJob(args, true)
	case "disable":
		return t.enableJob(args, false)
	default:
		return fmt.Sprintf("Unknown action: %s. Use: add, list, remove, enable, disable", action), nil
	}
}

func (t *CronTool) addJob(args map[string]interface{}) (string, error) {
	name, _ := args["name"].(string)
	message, _ := args["message"].(string)
	scheduleType, _ := args["schedule_type"].(string)

	if name == "" {
		return "Error: 'name' is required for add", nil
	}
	if message == "" {
		return "Error: 'message' is required for add", nil
	}

	deliver := true // default
	if d, ok := args["deliver"].(bool); ok {
		deliver = d
	}
	channel, _ := args["channel"].(string)
	to, _ := args["to"].(string)

	// Auto-fill from current chat context if not specified
	if channel == "" {
		channel = t.defaultChannel
	}
	if to == "" {
		to = t.defaultChatID
	}

	var schedule cron.CronSchedule

	switch scheduleType {
	case "every":
		intervalF, ok := args["interval_seconds"].(float64)
		if !ok || intervalF <= 0 {
			return "Error: 'interval_seconds' must be a positive number for 'every' schedule", nil
		}
		everyMS := int64(intervalF) * 1000
		schedule = cron.CronSchedule{
			Kind:    "every",
			EveryMS: &everyMS,
		}

	case "at":
		runAtISO, _ := args["run_at_iso"].(string)
		if runAtISO == "" {
			return "Error: 'run_at_iso' is required for 'at' schedule", nil
		}
		runAt, err := time.Parse(time.RFC3339, runAtISO)
		if err != nil {
			return fmt.Sprintf("Error: invalid run_at_iso format: %v. Use ISO 8601 like '2026-02-14T09:00:00+07:00'", err), nil
		}
		atMS := runAt.UnixMilli()
		schedule = cron.CronSchedule{
			Kind: "at",
			AtMS: &atMS,
		}

	default:
		return "Error: 'schedule_type' must be 'every' or 'at'", nil
	}

	job, err := t.cronService.AddJob(name, schedule, message, deliver, channel, to)
	if err != nil {
		return fmt.Sprintf("Error adding job: %v", err), nil
	}

	nextRun := "N/A"
	if job.State.NextRunAtMS != nil {
		nextRun = time.UnixMilli(*job.State.NextRunAtMS).Format("2006-01-02 15:04:05")
	}

	return fmt.Sprintf("✓ Created cron job '%s' (ID: %s)\n  Schedule: %s\n  Next run: %s\n  Message: %s\n  Deliver: %v",
		job.Name, job.ID, scheduleType, nextRun, job.Payload.Message, job.Payload.Deliver), nil
}

func (t *CronTool) listJobs() (string, error) {
	jobs := t.cronService.ListJobs(true)

	if len(jobs) == 0 {
		return "No scheduled jobs.", nil
	}

	type jobInfo struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Enabled  bool   `json:"enabled"`
		Schedule string `json:"schedule"`
		NextRun  string `json:"next_run"`
		Message  string `json:"message"`
		Deliver  bool   `json:"deliver"`
	}

	var result []jobInfo
	for _, job := range jobs {
		schedule := "unknown"
		if job.Schedule.Kind == "every" && job.Schedule.EveryMS != nil {
			schedule = fmt.Sprintf("every %ds", *job.Schedule.EveryMS/1000)
		} else if job.Schedule.Kind == "at" && job.Schedule.AtMS != nil {
			schedule = fmt.Sprintf("at %s", time.UnixMilli(*job.Schedule.AtMS).Format("2006-01-02 15:04"))
		}

		nextRun := "not scheduled"
		if job.State.NextRunAtMS != nil {
			nextRun = time.UnixMilli(*job.State.NextRunAtMS).Format("2006-01-02 15:04")
		}

		result = append(result, jobInfo{
			ID:       job.ID,
			Name:     job.Name,
			Enabled:  job.Enabled,
			Schedule: schedule,
			NextRun:  nextRun,
			Message:  job.Payload.Message,
			Deliver:  job.Payload.Deliver,
		})
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	return fmt.Sprintf("Scheduled jobs (%d):\n%s", len(result), string(data)), nil
}

func (t *CronTool) removeJob(args map[string]interface{}) (string, error) {
	jobID, _ := args["job_id"].(string)
	if jobID == "" {
		return "Error: 'job_id' is required for remove", nil
	}

	if t.cronService.RemoveJob(jobID) {
		return fmt.Sprintf("✓ Removed job %s", jobID), nil
	}
	return fmt.Sprintf("Job %s not found", jobID), nil
}

func (t *CronTool) enableJob(args map[string]interface{}, enable bool) (string, error) {
	jobID, _ := args["job_id"].(string)
	if jobID == "" {
		return "Error: 'job_id' is required", nil
	}

	job := t.cronService.EnableJob(jobID, enable)
	if job == nil {
		return fmt.Sprintf("Job %s not found", jobID), nil
	}

	status := "enabled"
	if !enable {
		status = "disabled"
	}
	return fmt.Sprintf("✓ Job '%s' %s", job.Name, status), nil
}
