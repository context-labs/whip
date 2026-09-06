package tui

import (
	"encoding/json"
	"fmt"
	"github.com/context-labs/whip/internal/protocol"
	"strings"
)

func renderRuntimeControl(operation, output string) (string, bool, error) {
	decode := func(value any) error { return json.Unmarshal([]byte(output), value) }
	switch operation {
	case "history.compact":
		var value protocol.CompactionResult
		if err := decode(&value); err != nil {
			return "", true, err
		}
		text := fmt.Sprintf("compacted through message %d", value.Cutoff)
		if value.Model != "" {
			text += " using " + value.Model
		}
		return text, true, nil
	case "history.rewind":
		var value protocol.RewindResult
		if err := decode(&value); err != nil {
			return "", true, err
		}
		return fmt.Sprintf("rewound to message %d; restored %d workspace file(s)", value.Cut, value.RestoredFiles), true, nil
	case "compaction.configure":
		var value protocol.CompactionSettingsResult
		if err := decode(&value); err != nil {
			return "", true, err
		}
		if value.BuiltinDefault {
			return "automatic compaction restored to built-in defaults", true, nil
		}
		return "compaction model: " + strings.TrimSpace(value.Model+" "+value.Provider), true, nil
	case "history.compact.retry":
		var value protocol.CompactionRetryResult
		if err := decode(&value); err != nil {
			return "", true, err
		}
		if !value.Undone {
			return "no compaction to retry", true, nil
		}
		return fmt.Sprintf("compaction %d undone; raw history restored", value.Sequence), true, nil

	case "browser.status", "browser.set_driver":
		var value protocol.BrowserStatusResult
		if err := decode(&value); err != nil {
			return "", true, err
		}
		if !value.Enabled {
			return "browser automation is disabled", true, nil
		}
		return "browser driver: " + value.Driver, true, nil
	case "computer.status", "computer.allow", "computer.deny":
		var value protocol.ComputerStatusResult
		if err := decode(&value); err != nil {
			return "", true, err
		}
		if !value.Enabled {
			return "computer automation is disabled", true, nil
		}
		allowed := append(append([]string{}, value.Allowed...), value.SessionAllowed...)
		denied := append(append([]string{}, value.Denied...), value.SessionDenied...)
		return fmt.Sprintf("computer access: default deny=%t\nallowed: %s\ndenied: %s", value.DefaultDeny, strings.Join(allowed, ", "), strings.Join(denied, ", ")), true, nil
	case "mcp.import.status", "mcp.import.configure":
		var value protocol.MCPImportStatusResult
		if err := decode(&value); err != nil {
			return "", true, err
		}
		return fmt.Sprintf("MCP imports: Claude %t · Codex %t", value.Claude, value.Codex), true, nil
	case "schedule.create", "schedule.delete":
		var value protocol.ScheduleResult
		if err := decode(&value); err != nil {
			return "", true, err
		}
		action := "created"
		if operation == "schedule.delete" {
			action = "cancelled"
		}
		return fmt.Sprintf("schedule %d %s", value.ScheduleID, action), true, nil
	case "permission.rules":
		var value protocol.PermissionRulesResult
		if err := decode(&value); err != nil {
			return "", true, err
		}
		var lines []string
		for _, rule := range value.Rules {
			lines = append(lines, fmt.Sprintf("%s  %s  %s  (%s, %s)", rule.ID, rule.Operation, rule.Rule, rule.PrincipalID, rule.CreatedAt))
		}
		for _, rule := range value.Global {
			lines = append(lines, "global  "+rule)
		}
		if len(lines) == 0 {
			return "(no permission rules)", true, nil
		}
		return strings.Join(lines, "\n"), true, nil
	}
	return "", false, nil
}
