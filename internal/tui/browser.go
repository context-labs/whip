package tui

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/context-labs/whip/internal/browser"
)

// browserStepLabel extracts the step label from browser_exec args: the
// first line of `code` that starts with "# " (the convention the tool
// description teaches — the model writes a plain-language label for the
// user, max 60 chars). Returns "" when absent.
func browserStepLabel(argsJSON string) string {
	var a struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &a); err != nil || a.Code == "" {
		return ""
	}
	for line := range strings.SplitSeq(a.Code, "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "#"); ok {
			return strings.TrimSpace(after)
		}
		if line != "" {
			break // first non-comment line: no label
		}
	}
	return ""
}

// switchBrowserDriver changes the browser automation driver (rod ↔
// chromedp) and invalidates open sessions so the next browser_exec reopens
// on the new driver. Env-pinned (WHIP_BROWSER_DRIVER) wins — the switch
// reports that instead of pretending to apply.
func (m *model) switchBrowserDriver(d string) {
	if os.Getenv("WHIP_BROWSER_DRIVER") != "" {
		m.append(dimStyle.Render("◎ browser driver pinned by WHIP_BROWSER_DRIVER=" + os.Getenv("WHIP_BROWSER_DRIVER") + " — unset it to switch"))
		return
	}
	var manager *browser.Manager
	if m.agent != nil && m.agent.Services != nil {
		manager = m.agent.Services.Browser()
	}
	if manager != nil {
		manager.SwitchDriver(d)
	}
	m.append(dimStyle.Render("◎ browser driver: " + m.browserDriver() + " (open browser sessions re-open on next use)"))
}

func (m *model) browserDriver() string {
	if m.agent != nil && m.agent.Services != nil {
		if manager := m.agent.Services.Browser(); manager != nil {
			return manager.Driver()
		}
	}
	return browser.DefaultDriver()
}
