package mcpconfig

import (
	"encoding/json"
	"strings"
)

func (c Server) Valid() string {
	if len(c.Command) > 128 || len(c.Env) > 128 || len(c.Headers) > 64 || len(c.URL) > 8192 || len(c.Cwd) > 4096 || len(c.Note) > 4096 || c.StartupTimeout < 0 || c.StartupTimeout > 300 || c.ToolTimeout < 0 || c.ToolTimeout > 300 {
		return "configuration exceeds bounds"
	}
	raw, err := json.Marshal(c)
	if err != nil || len(raw) > 128<<10 {
		return "configuration exceeds byte limit"
	}
	for index, part := range c.Command {
		if index == 0 && len(part) == 0 || len(part) > 16384 || strings.ContainsRune(part, 0) {
			return "invalid command argument"
		}
	}
	for key, value := range c.Env {
		if len(key) == 0 || len(key) > 256 || len(value) > 16384 || strings.ContainsRune(key+value, 0) {
			return "invalid environment declaration"
		}
	}
	for key, value := range c.Headers {
		if len(key) == 0 || len(key) > 256 || len(value) > 16384 || strings.ContainsAny(key+value, "\r\n\x00") {
			return "invalid header declaration"
		}
	}

	switch {
	case c.URL != "" && len(c.Command) > 0:
		return "both command and url set"
	case c.URL == "" && len(c.Command) == 0:
		return "neither command nor url set"
	case c.URL != "" && !strings.HasPrefix(c.URL, "http://") && !strings.HasPrefix(c.URL, "https://"):
		return "url must start with http:// or https://"
	}
	return ""
}
