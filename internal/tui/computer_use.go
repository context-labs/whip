package tui

import (
	"strings"

	"github.com/context-labs/whip/internal/computer"
)

// computerUseCommand implements /computer-use (alias /computer):
//
//	/computer-use              status: platform, policy, approved apps
//	/computer-use allow <app>  approve an app for this session
//	/computer-use deny <app>   block an app for this session
//	/computer-use <text>       submit <text> with an explicit instruction to
//	                           use computer_exec (the user said "use the
//	                           computer" — the model shouldn't have to guess)
func (m *model) computerUseCommand(args []string, text string) {
	if !computer.Available() {
		m.append(errStyle.Render("computer-use is macOS-only for now — browser_exec drives browsers on any platform"))
		return
	}
	if len(args) > 0 && args[0] == "allow" && len(args) > 1 {
		app := strings.Join(args[1:], " ")
		policy := m.agent.Services.ComputerPolicy()
		if policy == nil {
			m.append(errStyle.Render("no computer policy installed"))
			return
		}
		policy.Approve(app)
		m.append(dimStyle.Render("◎ computer-use: " + app + " approved for this session"))
		return
	}
	if len(args) > 0 && args[0] == "deny" && len(args) > 1 {
		// session deny: add to the deny map via a fresh policy check path
		policy := m.agent.Services.ComputerPolicy()
		if policy == nil {
			m.append(errStyle.Render("no computer policy installed"))
			return
		}
		policy.Deny(strings.Join(args[1:], " "))
		m.append(dimStyle.Render("◎ computer-use: " + strings.Join(args[1:], " ") + " denied for this session"))
		return
	}
	if len(args) > 0 {
		// /computer-use <text> — submit with an explicit computer-use steer.
		m.submitTurn(computerUseInstruction(strings.Join(args, " ")), true)
		return
	}
	// bare: status
	apps := "none"
	if policy := m.agent.Services.ComputerPolicy(); policy != nil {
		apps = policy.Summary()
	}
	m.append(dimStyle.Render("◎ computer-use: macOS ✓ · approved apps: " + apps + " · /computer-use <task> to drive the desktop, allow/deny <app> to manage consent"))
}

// computerUseInstruction wraps a task with the explicit steer to use
// computer_exec — split out so the message shape is testable off-darwin.
func computerUseInstruction(task string) string {
	return "The user asked for this to be done with computer-use. Use the computer_exec tool (drive the user's Mac — apps, the already-open Chrome via AppleScript, the screen) to accomplish it; do not fall back to browser_exec or shell unless computer_exec can't express the step.\n\nTask: " + task
}
