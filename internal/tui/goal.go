package tui

import (
	"fmt"
	"strings"
)

const goalMetToken = "GOAL_MET"

func goalContinuePrompt(goal string) string {
	return fmt.Sprintf("[goal check] The session goal is:\n%s\n\nIf it is fully accomplished and verified, begin your reply with %s. Otherwise keep working.", goal, goalMetToken)
}

func goalMet(final string) bool {
	head := strings.TrimSpace(final)
	if len(head) > 200 {
		head = head[:200]
	}
	index := strings.Index(head, goalMetToken)
	if index < 0 {
		return false
	}
	if end := index + len(goalMetToken); end < len(head) {
		char := head[end]
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' {
			return false
		}
	}
	prefix := strings.ToLower(strings.TrimSpace(head[:index]))
	for _, hedge := range []string{"toward", "until", "before", "to reach", "approaching"} {
		if strings.HasSuffix(prefix, hedge) {
			return false
		}
	}
	return true
}
