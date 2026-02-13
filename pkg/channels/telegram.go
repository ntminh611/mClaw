package channels

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/ntminh611/mclaw/pkg/bus"
	"github.com/ntminh611/mclaw/pkg/config"
	"github.com/ntminh611/mclaw/pkg/cron"
	"github.com/ntminh611/mclaw/pkg/heartbeat"
	"github.com/ntminh611/mclaw/pkg/session"
	"github.com/ntminh611/mclaw/pkg/voice"
)

type TelegramChannel struct {
	*BaseChannel
	bot              *tgbotapi.BotAPI
	config           config.TelegramConfig
	chatIDs          map[string]int64
	updates          tgbotapi.UpdatesChannel
	transcriber      *voice.GroqTranscriber
	cronService      *cron.CronService
	heartbeatService *heartbeat.HeartbeatService
	sessionManager   *session.SessionManager
	modelName        string
	placeholders     sync.Map // chatID -> messageID
	stopThinking     sync.Map // chatID -> chan struct{}
}

func NewTelegramChannel(cfg config.TelegramConfig, bus *bus.MessageBus) (*TelegramChannel, error) {
	bot, err := tgbotapi.NewBotAPI(cfg.Token)
	if err != nil {
		return nil, fmt.Errorf("failed to create telegram bot: %w", err)
	}

	base := NewBaseChannel("telegram", cfg, bus, cfg.AllowFrom)

	return &TelegramChannel{
		BaseChannel:  base,
		bot:          bot,
		config:       cfg,
		chatIDs:      make(map[string]int64),
		transcriber:  nil,
		placeholders: sync.Map{},
		stopThinking: sync.Map{},
	}, nil
}

func (c *TelegramChannel) SetTranscriber(transcriber *voice.GroqTranscriber) {
	c.transcriber = transcriber
}

func (c *TelegramChannel) SetCronService(cs *cron.CronService) {
	c.cronService = cs
}

func (c *TelegramChannel) SetHeartbeatService(hs *heartbeat.HeartbeatService) {
	c.heartbeatService = hs
}

func (c *TelegramChannel) SetSessionManager(sm *session.SessionManager) {
	c.sessionManager = sm
}

func (c *TelegramChannel) SetModelName(model string) {
	c.modelName = model
}

func (c *TelegramChannel) Start(ctx context.Context) error {
	log.Printf("Starting Telegram bot (polling mode)...")

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 30

	updates := c.bot.GetUpdatesChan(u)
	c.updates = updates

	c.setRunning(true)

	botInfo, err := c.bot.GetMe()
	if err != nil {
		return fmt.Errorf("failed to get bot info: %w", err)
	}
	log.Printf("Telegram bot @%s connected", botInfo.UserName)

	// Register bot commands menu
	commands := tgbotapi.NewSetMyCommands(
		tgbotapi.BotCommand{Command: "start", Description: "Start the bot"},
		tgbotapi.BotCommand{Command: "help", Description: "Show available commands"},
		tgbotapi.BotCommand{Command: "reset", Description: "Clear conversation history"},
		tgbotapi.BotCommand{Command: "status", Description: "Show bot status"},
		tgbotapi.BotCommand{Command: "cron", Description: "List cron jobs"},
		tgbotapi.BotCommand{Command: "heartbeat", Description: "Show heartbeat status"},
	)
	if _, err := c.bot.Request(commands); err != nil {
		log.Printf("Failed to set bot commands: %v", err)
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case update, ok := <-updates:
				if !ok {
					log.Printf("Updates channel closed, reconnecting...")
					return
				}
				if update.Message != nil {
					c.handleMessage(update)
				}
			}
		}
	}()

	return nil
}

func (c *TelegramChannel) Stop(ctx context.Context) error {
	log.Println("Stopping Telegram bot...")
	c.setRunning(false)

	if c.updates != nil {
		c.bot.StopReceivingUpdates()
		c.updates = nil
	}

	return nil
}

func (c *TelegramChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return fmt.Errorf("telegram bot not running")
	}

	chatID, err := parseChatID(msg.ChatID)
	if err != nil {
		return fmt.Errorf("invalid chat ID: %w", err)
	}

	// Stop thinking animation
	if stop, ok := c.stopThinking.Load(msg.ChatID); ok {
		close(stop.(chan struct{}))
		c.stopThinking.Delete(msg.ChatID)
	}

	// Split long messages into chunks (Telegram limit ~4096 chars)
	const maxLen = 4000
	content := msg.Content
	chunks := splitMessage(content, maxLen)

	for i, chunk := range chunks {
		// Small delay between chunks to avoid rate limiting
		if i > 0 {
			time.Sleep(500 * time.Millisecond)
		}

		htmlContent := markdownToTelegramHTML(chunk)
		tgMsg := tgbotapi.NewMessage(chatID, htmlContent)
		tgMsg.ParseMode = tgbotapi.ModeHTML

		if err := c.sendWithRetry(tgMsg); err != nil {
			// Fallback to plain text
			tgMsg = tgbotapi.NewMessage(chatID, chunk)
			tgMsg.ParseMode = ""
			if err := c.sendWithRetry(tgMsg); err != nil {
				log.Printf("Failed to send chunk: %v", err)
			}
		}
	}

	return nil
}

// sendWithRetry sends a Telegram message with retry on rate limit (429)
func (c *TelegramChannel) sendWithRetry(msg tgbotapi.Chattable) error {
	maxRetries := 2
	for attempt := 0; attempt <= maxRetries; attempt++ {
		_, err := c.bot.Send(msg)
		if err == nil {
			return nil
		}

		errStr := err.Error()
		// Check for rate limit (Too Many Requests)
		if strings.Contains(errStr, "Too Many Requests") || strings.Contains(errStr, "retry after") {
			waitSeconds := 3
			if idx := strings.Index(errStr, "retry after "); idx >= 0 {
				fmt.Sscanf(errStr[idx+len("retry after "):], "%d", &waitSeconds)
			}
			if waitSeconds > 10 {
				waitSeconds = 10
			}
			log.Printf("[telegram] Rate limited, waiting %ds before retry (attempt %d/%d)", waitSeconds, attempt+1, maxRetries)
			time.Sleep(time.Duration(waitSeconds) * time.Second)
			continue
		}

		return err
	}
	return fmt.Errorf("failed after %d retries due to rate limiting", maxRetries)
}

// splitMessage splits text into chunks of maxLen, preferring to split at newlines
func splitMessage(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}

	var chunks []string
	for len(text) > 0 {
		if len(text) <= maxLen {
			chunks = append(chunks, text)
			break
		}

		// Find a good split point (last newline before maxLen)
		splitAt := maxLen
		if idx := strings.LastIndex(text[:maxLen], "\n"); idx > maxLen/2 {
			splitAt = idx + 1
		}

		chunks = append(chunks, strings.TrimRight(text[:splitAt], "\n "))
		text = text[splitAt:]
	}

	return chunks
}

func (c *TelegramChannel) handleMessage(update tgbotapi.Update) {
	message := update.Message
	if message == nil {
		return
	}

	user := message.From
	if user == nil {
		return
	}

	senderID := fmt.Sprintf("%d", user.ID)
	if user.UserName != "" {
		senderID = fmt.Sprintf("%d|%s", user.ID, user.UserName)
	}

	chatID := message.Chat.ID
	c.chatIDs[senderID] = chatID

	// Handle bot commands first
	if message.IsCommand() {
		c.handleCommand(message)
		return
	}

	content := ""
	mediaPaths := []string{}

	if message.Text != "" {
		content += message.Text
	}

	if message.Caption != "" {
		if content != "" {
			content += "\n"
		}
		content += message.Caption
	}

	if message.Photo != nil && len(message.Photo) > 0 {
		photo := message.Photo[len(message.Photo)-1]
		photoPath := c.downloadPhoto(photo.FileID)
		if photoPath != "" {
			mediaPaths = append(mediaPaths, photoPath)
			if content != "" {
				content += "\n"
			}
			content += fmt.Sprintf("[image: %s]", photoPath)
		}
	}

	if message.Voice != nil {
		voicePath := c.downloadFile(message.Voice.FileID, ".ogg")
		if voicePath != "" {
			mediaPaths = append(mediaPaths, voicePath)

			transcribedText := ""
			if c.transcriber != nil && c.transcriber.IsAvailable() {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()

				result, err := c.transcriber.Transcribe(ctx, voicePath)
				if err != nil {
					log.Printf("Voice transcription failed: %v", err)
					transcribedText = fmt.Sprintf("[voice: %s (transcription failed)]", voicePath)
				} else {
					transcribedText = fmt.Sprintf("[voice transcription: %s]", result.Text)
					log.Printf("Voice transcribed successfully: %s", result.Text)
				}
			} else {
				transcribedText = fmt.Sprintf("[voice: %s]", voicePath)
			}

			if content != "" {
				content += "\n"
			}
			content += transcribedText
		}
	}

	if message.Audio != nil {
		audioPath := c.downloadFile(message.Audio.FileID, ".mp3")
		if audioPath != "" {
			mediaPaths = append(mediaPaths, audioPath)
			if content != "" {
				content += "\n"
			}
			content += fmt.Sprintf("[audio: %s]", audioPath)
		}
	}

	if message.Document != nil {
		docPath := c.downloadFile(message.Document.FileID, "")
		if docPath != "" {
			mediaPaths = append(mediaPaths, docPath)
			if content != "" {
				content += "\n"
			}
			content += fmt.Sprintf("[file: %s]", docPath)
		}
	}

	if content == "" {
		content = "[empty message]"
	}

	log.Printf("Telegram message from %s: %s...", senderID, truncateString(content, 50))

	// Thinking indicator — use typing action only (lightweight, not rate-limited)
	c.bot.Send(tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping))

	stopChan := make(chan struct{})
	c.stopThinking.Store(fmt.Sprintf("%d", chatID), stopChan)

	// Keep sending typing action until response is ready
	go func(cid int64, stop <-chan struct{}) {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				c.bot.Send(tgbotapi.NewChatAction(cid, tgbotapi.ChatTyping))
			}
		}
	}(chatID, stopChan)

	metadata := map[string]string{
		"message_id": fmt.Sprintf("%d", message.MessageID),
		"user_id":    fmt.Sprintf("%d", user.ID),
		"username":   user.UserName,
		"first_name": user.FirstName,
		"is_group":   fmt.Sprintf("%t", message.Chat.Type != "private"),
	}

	c.HandleMessage(senderID, fmt.Sprintf("%d", chatID), content, mediaPaths, metadata)
}

func (c *TelegramChannel) handleCommand(message *tgbotapi.Message) {
	chatID := message.Chat.ID
	cmd := message.Command()

	var text string

	switch cmd {
	case "start":
		model := c.modelName
		if model == "" {
			model = "unknown"
		}
		text = "🤖 <b>MClaw AI Assistant</b>\n\n" +
			"I'm your personal AI assistant. Just send me a message and I'll help!\n\n" +
			fmt.Sprintf("Model: <code>%s</code>\n", model) +
			"Type /help for available commands."

	case "help":
		text = "📋 <b>Available Commands</b>\n\n" +
			"/start — Start the bot\n" +
			"/help — Show this help\n" +
			"/reset — Clear conversation history\n" +
			"/status — Show bot status\n" +
			"/cron — List scheduled jobs\n" +
			"/heartbeat — Heartbeat status\n\n" +
			"Or just send me any message to chat!"

	case "reset":
		senderID := fmt.Sprintf("%d", message.From.ID)
		sessionKey := fmt.Sprintf("telegram:%s", senderID)
		if c.sessionManager != nil {
			c.sessionManager.ClearHistory(sessionKey)
			text = "🗑 <b>Session cleared!</b>\n\nConversation history has been reset. Let's start fresh!"
		} else {
			text = "⚠️ Session manager not available."
		}

	case "status":
		model := c.modelName
		if model == "" {
			model = "unknown"
		}
		lines := []string{
			"📊 <b>Bot Status</b>\n",
			fmt.Sprintf("🤖 Model: <code>%s</code>", model),
			fmt.Sprintf("📡 Channel: Telegram (running: %t)", c.IsRunning()),
		}

		if c.cronService != nil {
			status := c.cronService.Status()
			lines = append(lines, fmt.Sprintf("⏰ Cron: %d jobs", status["jobs"]))
		}

		if c.heartbeatService != nil {
			lines = append(lines, fmt.Sprintf("💓 Heartbeat: %t", c.heartbeatService.IsRunning()))
		}

		if c.transcriber != nil && c.transcriber.IsAvailable() {
			lines = append(lines, "🎤 Voice: enabled (Groq)")
		} else {
			lines = append(lines, "🎤 Voice: disabled")
		}

		text = strings.Join(lines, "\n")

	case "cron":
		if c.cronService == nil {
			text = "⚠️ Cron service not available."
			break
		}

		jobs := c.cronService.ListJobs(true)
		if len(jobs) == 0 {
			text = "⏰ <b>Cron Jobs</b>\n\nNo scheduled jobs."
			break
		}

		lines := []string{fmt.Sprintf("⏰ <b>Cron Jobs</b> (%d total)\n", len(jobs))}
		for _, job := range jobs {
			status := "✅"
			if !job.Enabled {
				status = "❌"
			}
			lines = append(lines, fmt.Sprintf("%s <b>%s</b> [%s]", status, job.Name, job.ID))
			lines = append(lines, fmt.Sprintf("   Schedule: %s", job.Schedule.Kind))
			if job.State.LastStatus != "" {
				lines = append(lines, fmt.Sprintf("   Last: %s", job.State.LastStatus))
			}
		}
		text = strings.Join(lines, "\n")

	case "heartbeat":
		if c.heartbeatService == nil {
			text = "⚠️ Heartbeat service not available."
			break
		}

		running := c.heartbeatService.IsRunning()
		status := "🔴 Stopped"
		if running {
			status = "🟢 Running"
		}
		text = fmt.Sprintf("💓 <b>Heartbeat</b>\n\nStatus: %s", status)

	default:
		text = fmt.Sprintf("Unknown command: /%s\nType /help for available commands.", cmd)
	}

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "HTML"
	if _, err := c.bot.Send(msg); err != nil {
		log.Printf("Failed to send command response: %v", err)
	}
}

func (c *TelegramChannel) downloadPhoto(fileID string) string {
	file, err := c.bot.GetFile(tgbotapi.FileConfig{FileID: fileID})
	if err != nil {
		log.Printf("Failed to get photo file: %v", err)
		return ""
	}

	return c.downloadFileWithInfo(&file, ".jpg")
}

func (c *TelegramChannel) downloadFileWithInfo(file *tgbotapi.File, ext string) string {
	if file.FilePath == "" {
		return ""
	}

	url := file.Link(c.bot.Token)
	log.Printf("File URL: %s", url)

	mediaDir := filepath.Join(os.TempDir(), "mclaw_media")
	if err := os.MkdirAll(mediaDir, 0755); err != nil {
		log.Printf("Failed to create media directory: %v", err)
		return ""
	}

	localPath := filepath.Join(mediaDir, file.FilePath[:min(16, len(file.FilePath))]+ext)

	if err := c.downloadFromURL(url, localPath); err != nil {
		log.Printf("Failed to download file: %v", err)
		return ""
	}

	return localPath
}

func (c *TelegramChannel) downloadFromURL(url, localPath string) error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status: %d", resp.StatusCode)
	}

	out, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	log.Printf("File downloaded successfully to: %s", localPath)
	return nil
}

func (c *TelegramChannel) downloadFile(fileID, ext string) string {
	file, err := c.bot.GetFile(tgbotapi.FileConfig{FileID: fileID})
	if err != nil {
		log.Printf("Failed to get file: %v", err)
		return ""
	}

	if file.FilePath == "" {
		return ""
	}

	url := file.Link(c.bot.Token)
	log.Printf("File URL: %s", url)

	mediaDir := filepath.Join(os.TempDir(), "mclaw_media")
	if err := os.MkdirAll(mediaDir, 0755); err != nil {
		log.Printf("Failed to create media directory: %v", err)
		return ""
	}

	localPath := filepath.Join(mediaDir, fileID[:16]+ext)

	if err := c.downloadFromURL(url, localPath); err != nil {
		log.Printf("Failed to download file: %v", err)
		return ""
	}

	return localPath
}

func parseChatID(chatIDStr string) (int64, error) {
	var id int64
	_, err := fmt.Sscanf(chatIDStr, "%d", &id)
	return id, err
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

func markdownToTelegramHTML(text string) string {
	if text == "" {
		return ""
	}

	codeBlocks := extractCodeBlocks(text)
	text = codeBlocks.text

	inlineCodes := extractInlineCodes(text)
	text = inlineCodes.text

	text = regexp.MustCompile(`^#{1,6}\s+(.+)$`).ReplaceAllString(text, "$1")

	text = regexp.MustCompile(`^>\s*(.*)$`).ReplaceAllString(text, "$1")

	text = escapeHTML(text)

	text = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`).ReplaceAllString(text, `<a href="$2">$1</a>`)

	text = regexp.MustCompile(`\*\*(.+?)\*\*`).ReplaceAllString(text, "<b>$1</b>")

	text = regexp.MustCompile(`__(.+?)__`).ReplaceAllString(text, "<b>$1</b>")

	reItalic := regexp.MustCompile(`_([^_]+)_`)
	text = reItalic.ReplaceAllStringFunc(text, func(s string) string {
		match := reItalic.FindStringSubmatch(s)
		if len(match) < 2 {
			return s
		}
		return "<i>" + match[1] + "</i>"
	})

	text = regexp.MustCompile(`~~(.+?)~~`).ReplaceAllString(text, "<s>$1</s>")

	text = regexp.MustCompile(`^[-*]\s+`).ReplaceAllString(text, "• ")

	for i, code := range inlineCodes.codes {
		escaped := escapeHTML(code)
		text = strings.ReplaceAll(text, fmt.Sprintf("\x00IC%d\x00", i), fmt.Sprintf("<code>%s</code>", escaped))
	}

	for i, code := range codeBlocks.codes {
		escaped := escapeHTML(code)
		text = strings.ReplaceAll(text, fmt.Sprintf("\x00CB%d\x00", i), fmt.Sprintf("<pre><code>%s</code></pre>", escaped))
	}

	return text
}

type codeBlockMatch struct {
	text  string
	codes []string
}

func extractCodeBlocks(text string) codeBlockMatch {
	re := regexp.MustCompile("```[\\w]*\\n?([\\s\\S]*?)```")
	matches := re.FindAllStringSubmatch(text, -1)

	codes := make([]string, 0, len(matches))
	for _, match := range matches {
		codes = append(codes, match[1])
	}

	i := 0
	text = re.ReplaceAllStringFunc(text, func(m string) string {
		placeholder := fmt.Sprintf("\x00CB%d\x00", i)
		i++
		return placeholder
	})

	return codeBlockMatch{text: text, codes: codes}
}

type inlineCodeMatch struct {
	text  string
	codes []string
}

func extractInlineCodes(text string) inlineCodeMatch {
	re := regexp.MustCompile("`([^`]+)`")
	matches := re.FindAllStringSubmatch(text, -1)

	codes := make([]string, 0, len(matches))
	for _, match := range matches {
		codes = append(codes, match[1])
	}

	i := 0
	text = re.ReplaceAllStringFunc(text, func(m string) string {
		placeholder := fmt.Sprintf("\x00IC%d\x00", i)
		i++
		return placeholder
	})

	return inlineCodeMatch{text: text, codes: codes}
}

func escapeHTML(text string) string {
	text = strings.ReplaceAll(text, "&", "&amp;")
	text = strings.ReplaceAll(text, "<", "&lt;")
	text = strings.ReplaceAll(text, ">", "&gt;")
	return text
}
