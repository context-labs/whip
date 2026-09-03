package tui

import (
	"strings"
)

// computerUseInstruction wraps a task with the explicit steer to use
// computer_exec — split out so the message shape is testable off-darwin.
func computerUseInstruction(task string) string {
	return "The user asked for this to be done with computer-use. Use the computer_exec tool to accomplish it.\n\nTask: " + strings.TrimSpace(task)
}
