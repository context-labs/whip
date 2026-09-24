package tui

import (
	"encoding/json"
	"strings"
)

func browserStepLabel(argsJSON string) string {
	var args struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return ""
	}
	for line := range strings.SplitSeq(args.Code, "\n") {
		line = strings.TrimSpace(line)
		if label, ok := strings.CutPrefix(line, "#"); ok {
			return strings.TrimSpace(label)
		}
		if line != "" {
			break
		}
	}
	return ""
}
