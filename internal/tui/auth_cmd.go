package tui

import "strings"

// Credential-bearing legacy commands never enter terminal input history.
func authCommandText(text string) bool {
	return strings.HasPrefix(text, "/auth ") || strings.HasPrefix(text, "/connect ")
}
