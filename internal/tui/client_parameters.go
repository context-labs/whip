package tui

import (
	"errors"
	"strconv"
	"strings"

	bubbletea "charm.land/bubbletea/v2"
	"github.com/context-labs/whip/internal/protocol"
)

// submitClientCLI parses terminal syntax before constructing the wire payload.
func (m *model) submitClientCLI(operation, text string) (bubbletea.Model, bubbletea.Cmd) {
	method, payload, err := clientCLIParameters(operation, text)
	if err != nil {
		m.append(errStyle.Render(err.Error()))
		return m, nil
	}
	return m.submitClientAction(method, payload, "")
}

func clientCLIParameters(operation, text string) (string, any, error) {
	fields := strings.Fields(text)
	fail := func(message string) (string, any, error) { return "", nil, errors.New(message) }
	switch operation {
	case "goal.from-context":
		window := 0
		if text != "" {
			var err error
			window, err = strconv.Atoi(text)
			if err != nil || window < 2 {
				return fail("goal context window must be at least 2")
			}
		}
		return operation, protocol.GoalContextParams{Window: window}, nil
	case "history.rewind":
		cut, err := strconv.Atoi(text)
		if err != nil || cut < 1 {
			return fail("rewind requires a positive conversation index")
		}
		return operation, protocol.RewindParams{Cut: cut}, nil
	case "session.open", "agent.control", "agent.delete", "capability.revoke", "permission.forget":
		return operation, protocol.IDParams{ID: text}, nil
	case "session.rename":
		return operation, protocol.TitleParams{Title: text}, nil
	case "session.fork":
		return operation, protocol.ForkParams{Title: text}, nil
	case "workspace.set":
		return operation, protocol.PathParams{Path: text}, nil
	case "goal.run", "goal.set":
		return operation, protocol.TextParams{Text: text}, nil
	case "compaction.configure":
		value := protocol.CompactionParams{}
		if len(fields) > 2 {
			return fail("compaction requires model and optional provider")
		}
		if len(fields) > 0 && fields[0] != "off" {
			value.Model = fields[0]
			if len(fields) > 1 {
				value.Provider = fields[1]
			}
		}
		return operation, value, nil
	case "schedule":
		if len(fields) == 0 || len(fields) == 1 && fields[0] == "list" {
			return "schedule.list", protocol.EmptyParams{}, nil
		}
		if len(fields) == 2 && fields[0] == "cancel" {
			id, err := strconv.Atoi(fields[1])
			if err != nil || id < 1 {
				return fail("schedule ID must be positive")
			}
			return "schedule.delete", protocol.ScheduleDeleteParams{ScheduleID: id}, nil
		}
		if len(fields) < 3 || fields[0] != "@every" && fields[0] != "@at" {
			return fail("schedule requires @every <duration> or @at <time> and a prompt")
		}
		return "schedule.create", protocol.ScheduleCreateParams{Schedule: strings.Join(fields[:2], " "), Prompt: strings.Join(fields[2:], " ")}, nil
	case "mcp":
		if len(fields) == 0 || len(fields) == 1 && (fields[0] == "list" || fields[0] == "status") {
			return "mcp.status", protocol.EmptyParams{}, nil
		}
		if fields[0] == "import" {
			if len(fields) == 1 || len(fields) == 2 && fields[1] == "status" {
				return "mcp.import.status", protocol.EmptyParams{}, nil
			}
			if len(fields) != 3 || fields[1] != "claude" && fields[1] != "codex" || fields[2] != "on" && fields[2] != "off" {
				return fail("mcp import requires claude|codex and on|off")
			}
			return "mcp.import.configure", protocol.MCPImportParams{Source: fields[1], Enabled: fields[2] == "on"}, nil
		}
		action := "reconnect"
		if len(fields) > 2 {
			return fail("mcp requires a server and reconnect|enable|disable")
		}
		if len(fields) == 2 {
			action = fields[1]
		}
		if action != "reconnect" && action != "enable" && action != "disable" {
			return fail("mcp action must be reconnect, enable or disable")
		}
		return "mcp." + action, protocol.MCPServerParams{Name: fields[0]}, nil
	case "lsp":
		if len(fields) > 1 || len(fields) == 1 && fields[0] != "list" && fields[0] != "status" {
			return fail("lsp supports status only")
		}
		return "lsp.status", protocol.EmptyParams{}, nil
	case "browser":
		if len(fields) == 0 || len(fields) == 1 && fields[0] == "status" {
			return "browser.status", protocol.EmptyParams{}, nil
		}
		if len(fields) == 2 && fields[0] == "driver" {
			fields = fields[1:]
		}
		if len(fields) != 1 || fields[0] != "rod" && fields[0] != "chromedp" {
			return fail("browser driver must be rod or chromedp")
		}
		return "browser.set_driver", protocol.BrowserDriverParams{Driver: fields[0]}, nil
	case "computer":
		if len(fields) == 0 || len(fields) == 1 && fields[0] == "status" {
			return "computer.status", protocol.EmptyParams{}, nil
		}
		if len(fields) < 2 || fields[0] != "allow" && fields[0] != "deny" {
			return fail("computer action requires allow|deny and an app")
		}
		return "computer." + fields[0], protocol.ComputerAppParams{App: strings.Join(fields[1:], " ")}, nil
	default:
		if text != "" {
			return fail("this operation takes no arguments")
		}
		return operation, protocol.EmptyParams{}, nil
	}
}
