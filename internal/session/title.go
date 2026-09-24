package session

import (
	"strings"

	"github.com/context-labs/whip/internal/llm"
)

const provisionalTitleRunes = 64

// ProvisionalTitle derives the initial session title from the first message the
// user authored. The returned title is whitespace-normalized and at most 64
// runes, including the ellipsis.
func ProvisionalTitle(messages []llm.Message) string {
	for _, message := range messages {
		if message.Role != "user" || !message.Authored {
			continue
		}
		title := strings.Join(strings.Fields(message.TextContent()), " ")
		runes := []rune(title)
		if len(runes) > provisionalTitleRunes {
			return string(runes[:provisionalTitleRunes-1]) + "…"
		}
		return title
	}
	return ""
}
