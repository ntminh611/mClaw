package tools

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

// BrowserTool uses headless Chrome (chromedp) to fetch JS-rendered pages.
type BrowserTool struct {
	timeout         time.Duration
	chromeAvailable bool
}

func NewBrowserTool(timeout time.Duration) *BrowserTool {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	// Detect Chrome/Chromium
	available := false
	chromePaths := []string{
		"google-chrome",
		"google-chrome-stable",
		"chromium",
		"chromium-browser",
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
	}
	for _, p := range chromePaths {
		if _, err := exec.LookPath(p); err == nil {
			available = true
			break
		}
	}

	if available {
		log.Printf("[tools] Browser tool: Chrome/Chromium detected ✓")
	} else {
		log.Printf("[tools] Browser tool: Chrome/Chromium not found — browser tool disabled")
	}

	return &BrowserTool{timeout: timeout, chromeAvailable: available}
}

func (t *BrowserTool) Name() string {
	return "browser"
}

func (t *BrowserTool) Description() string {
	if !t.chromeAvailable {
		return "Browser tool (UNAVAILABLE — Chrome/Chromium not installed). Use web_fetch instead."
	}
	return "Open a URL in a headless browser, wait for JavaScript to render, and extract the page text. Use this for JS-heavy sites (SPAs, dynamic content) where web_fetch returns empty/useless content."
}

func (t *BrowserTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{
				"type":        "string",
				"description": "URL to open in browser",
			},
			"wait_seconds": map[string]interface{}{
				"type":        "integer",
				"description": "Extra seconds to wait for JS rendering (default: 2, max: 10)",
				"minimum":     0.0,
				"maximum":     10.0,
			},
		},
		"required": []string{"url"},
	}
}

func (t *BrowserTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	if !t.chromeAvailable {
		return "Browser tool is unavailable: Chrome/Chromium is not installed on this system. " +
			"For best results with JavaScript-heavy websites (SPAs, dynamic content), install Chrome or Chromium:\n" +
			"  • Ubuntu/Debian: sudo apt install chromium-browser\n" +
			"  • macOS: brew install --cask chromium\n" +
			"  • Or run: ./setup.sh\n\n" +
			"For now, use the web_fetch tool instead — it works for most websites without a browser.", nil
	}

	urlStr, ok := args["url"].(string)
	if !ok {
		return "", fmt.Errorf("url is required")
	}

	if !strings.HasPrefix(urlStr, "http://") && !strings.HasPrefix(urlStr, "https://") {
		return "", fmt.Errorf("only http/https URLs are allowed")
	}

	waitSeconds := 2
	if ws, ok := args["wait_seconds"].(float64); ok {
		waitSeconds = int(ws)
		if waitSeconds > 10 {
			waitSeconds = 10
		}
		if waitSeconds < 0 {
			waitSeconds = 0
		}
	}

	// Create headless Chrome context with timeout
	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx,
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.UserAgent("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"),
	)
	defer allocCancel()

	chromeCtx, chromeCancel := chromedp.NewContext(allocCtx)
	defer chromeCancel()

	timeoutCtx, timeoutCancel := context.WithTimeout(chromeCtx, t.timeout)
	defer timeoutCancel()

	var pageText string
	var pageTitle string

	err := chromedp.Run(timeoutCtx,
		chromedp.Navigate(urlStr),
		chromedp.Sleep(time.Duration(waitSeconds)*time.Second),
		chromedp.Title(&pageTitle),
		chromedp.Text("body", &pageText, chromedp.ByQuery),
	)
	if err != nil {
		return "", fmt.Errorf("browser failed: %w", err)
	}

	// Clean up whitespace
	lines := strings.Split(pageText, "\n")
	var cleanLines []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			cleanLines = append(cleanLines, line)
		}
	}
	pageText = strings.Join(cleanLines, "\n")

	// Truncate if too long
	maxChars := 50000
	truncated := len(pageText) > maxChars
	if truncated {
		pageText = pageText[:maxChars]
	}

	result := fmt.Sprintf("Title: %s\nURL: %s\nTruncated: %v\nLength: %d\n\n%s",
		pageTitle, urlStr, truncated, len(pageText), pageText)

	return result, nil
}
