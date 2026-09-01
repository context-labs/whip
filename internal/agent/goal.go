package agent

import (
	"fmt"
	"strings"
)

const goalMetToken = "GOAL_MET"

func GoalContinuePrompt(goal string) string {
	return fmt.Sprintf(`[goal check] The session goal is:
%s

If the goal is FULLY accomplished and you have VERIFIED it with your tools (builds pass, tests pass, behavior confirmed), your reply MUST begin with the literal token %s as the very first characters — no preamble, no "Verified:" line, nothing before it. Example first line: "%s — shipped and verified."

Otherwise do not use the token %s at all — keep working toward the goal right now with your tools. If any part is incomplete, unverified, or you are unsure, that means keep working. Do not stop to ask questions; make reasonable assumptions and proceed.`, goal, goalMetToken, goalMetToken, goalMetToken)
}

func GoalMet(final string) bool {
	const window = 200
	head := strings.TrimSpace(final)
	if len(head) > window {
		head = head[:window]
	}
	i := strings.Index(head, goalMetToken)
	if i < 0 {
		return false
	}
	if i+len(goalMetToken) < len(head) {
		c := head[i+len(goalMetToken)]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' {
			return false
		}
	}
	prefix := strings.ToLower(head[:i])
	for _, hedge := range []string{"toward", "until", "before", "to reach", "approaching"} {
		if strings.HasSuffix(strings.TrimSpace(prefix), hedge) {
			return false
		}
	}
	return true
}
