package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/tools/bashrun"
)

const (
	maxPayloadBytes = 256 << 10
	maxOutputBytes  = 64 << 10
	maxEnvBytes     = 64 << 10
)

type payload struct {
	Version              int    `json:"version"`
	Event                Event  `json:"event"`
	EventType            Event  `json:"event_type"`
	SessionID            string `json:"session_id,omitempty"`
	MatcherContext       string `json:"matcher_context,omitempty"`
	ToolName             string `json:"tool_name,omitempty"`
	ToolInput            any    `json:"tool_input,omitempty"`
	ToolResponse         string `json:"tool_response,omitempty"`
	ToolError            string `json:"tool_error,omitempty"`
	Message              string `json:"message,omitempty"`
	LastAssistantMessage string `json:"last_assistant_message,omitempty"`
	WorkingDir           string `json:"working_dir,omitempty"`
	ToolCallID           string `json:"tool_call_id,omitempty"`
}

type commandDecision struct {
	Decision          string `json:"decision"`
	Reason            string `json:"reason"`
	AdditionalContext string `json:"additionalContext"`
}

type actionResult struct {
	blocked           bool
	reason            string
	additionalContext string
	failure           string
}

// Run executes matching commands serially in discovery order. Manager is
// immutable after Load, so concurrent tool calls may safely share it.
func (m *Manager) Run(ctx context.Context, req Request) Outcome {
	out := Outcome{Failures: []string{}}
	if m == nil || m.disabled {
		return out
	}
	for _, a := range m.actions[req.Event] {
		if !a.matcher.Match(req.MatcherContext) {
			continue
		}
		out.Ran++
		result := runAction(ctx, a, req)
		if result.failure != "" {
			out.Failures = append(out.Failures, a.source+": "+result.failure)
			if req.Event == PreToolUse && a.onFailureBlock {
				out.Blocked = true
				out.Reason = fmt.Sprintf("policy hook %s could not complete: %s", a.source, result.failure)
				break
			}
		}
		if result.additionalContext != "" {
			combined, ok := joinContext(out.AdditionalContext, result.additionalContext)
			if !ok {
				failure := fmt.Sprintf("additional context exceeds %d bytes across hook chain", maxOutputBytes)
				out.Failures = append(out.Failures, a.source+": "+failure)
				if req.Event == PreToolUse && a.onFailureBlock {
					out.Blocked = true
					out.Reason = fmt.Sprintf("policy hook %s could not complete: %s", a.source, failure)
					break
				}
			} else {
				out.AdditionalContext = combined
			}
		}
		if result.blocked && canBlock(req.Event) {
			out.Blocked = true
			out.Reason = result.reason
			if out.Reason == "" {
				out.Reason = "blocked by " + a.source
			}
			break
		}
	}
	return out
}

func runAction(ctx context.Context, a action, req Request) actionResult {
	data, err := json.Marshal(makePayload(req))
	if err != nil {
		return actionResult{failure: "encode payload: " + err.Error()}
	}
	if len(data) > maxPayloadBytes {
		return actionResult{failure: fmt.Sprintf("payload exceeds %d bytes", maxPayloadBytes)}
	}
	data = append(data, '\n')
	env := hookEnv(a, req)
	if envBytes(os.Environ(), env) > maxEnvBytes {
		return actionResult{failure: fmt.Sprintf("environment exceeds %d bytes", maxEnvBytes)}
	}
	res := bashrun.Run(ctx, bashrun.Options{
		Command:        a.command,
		Stdin:          data,
		Env:            env,
		Dir:            req.WorkingDir,
		Timeout:        a.timeout,
		SeparateOutput: true,
		MaxOutputBytes: maxOutputBytes,
	})
	return interpret(res)
}

func makePayload(req Request) payload {
	var input any
	if len(req.ToolInput) > 0 {
		if err := json.Unmarshal(req.ToolInput, &input); err != nil {
			input = map[string]string{"_raw": string(req.ToolInput)}
		}
	}
	return payload{
		Version:              1,
		Event:                req.Event,
		EventType:            req.Event,
		SessionID:            req.SessionID,
		MatcherContext:       req.MatcherContext,
		ToolName:             req.ToolName,
		ToolInput:            input,
		ToolResponse:         req.ToolResponse,
		ToolError:            req.ToolError,
		Message:              req.Message,
		LastAssistantMessage: req.LastAssistantMessage,
		WorkingDir:           req.WorkingDir,
		ToolCallID:           req.ToolCallID,
	}
}

func hookEnv(a action, req Request) []string {
	return []string{
		"PLUGIN_ROOT=" + a.pluginRoot,
		"WHIP_HOOK_EVENT=" + string(req.Event),
		"WHIP_PROJECT_DIR=" + req.WorkingDir,
		"WHIP_SESSION_ID=" + req.SessionID,
		"WHIP_TOOL_NAME=" + req.ToolName,
	}
}

func envBytes(groups ...[]string) int {
	values := make(map[string]string)
	loose := 0
	for _, env := range groups {
		for _, item := range env {
			key, value, ok := strings.Cut(item, "=")
			if !ok {
				loose += len(item) + 1
				continue
			}
			values[key] = value
		}
	}
	total := loose
	for key, value := range values {
		total += len(key) + len(value) + 2 // '=' plus environment NUL
	}
	return total
}

func interpret(res bashrun.Result) actionResult {
	stderr := strings.TrimSpace(res.Stderr)
	stdout := strings.TrimSpace(res.Stdout)
	if res.ExitCode == 2 {
		reason := stderr
		if d, ok := decodeDecision(stdout); ok && d.Reason != "" {
			reason = d.Reason
		}
		return actionResult{blocked: true, reason: reason}
	}
	if res.TimedOut {
		return actionResult{failure: "timed out"}
	}
	if res.Killed {
		return actionResult{failure: res.Exit}
	}
	if res.Truncated {
		return actionResult{failure: fmt.Sprintf("output exceeds %d bytes", maxOutputBytes)}
	}
	if !utf8.ValidString(res.Stdout) {
		return actionResult{failure: "stdout is not valid utf-8"}
	}
	if stdout == "" {
		if res.ExitCode == 0 {
			return actionResult{}
		}
		return actionResult{failure: exitFailure(res, stderr)}
	}
	decision, ok := decodeDecision(stdout)
	if !ok {
		return actionResult{failure: "stdout is not one json decision object"}
	}
	switch strings.ToLower(decision.Decision) {
	case "block", "deny":
		return actionResult{
			blocked:           true,
			reason:            decision.Reason,
			additionalContext: decision.AdditionalContext,
		}
	case "allow":
		if res.ExitCode != 0 {
			return actionResult{failure: exitFailure(res, stderr)}
		}
		return actionResult{additionalContext: decision.AdditionalContext}
	case "":
		if res.ExitCode == 0 && decision.AdditionalContext != "" {
			return actionResult{additionalContext: decision.AdditionalContext}
		}
	}
	return actionResult{failure: "stdout contains an unrecognized decision"}
}

func decodeDecision(stdout string) (commandDecision, bool) {
	if stdout == "" || !strings.HasPrefix(stdout, "{") {
		return commandDecision{}, false
	}
	var decision commandDecision
	if err := json.Unmarshal([]byte(stdout), &decision); err != nil {
		return commandDecision{}, false
	}
	return decision, true
}

func exitFailure(res bashrun.Result, stderr string) string {
	if stderr != "" {
		return fmt.Sprintf("exit %d: %s", res.ExitCode, stderr)
	}
	if res.Exit != "" {
		return res.Exit
	}
	return fmt.Sprintf("exit %d", res.ExitCode)
}

func canBlock(event Event) bool {
	return event == UserPromptSubmit || event == PreToolUse || event == Stop
}

func joinContext(current, next string) (string, bool) {
	if current == "" {
		return next, len(next) <= maxOutputBytes
	}
	if len(next) > maxOutputBytes-2 || len(current) > maxOutputBytes-2-len(next) {
		return current, false
	}
	return current + "\n\n" + next, true
}
