package tui

import "strings"

func stripAll(s string) string {
	out := strings.Builder{}
	i := 0
	for i < len(s) {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' && s[i] != 'h' && s[i] != 'l' {
				i++
			}
			i++
			continue
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
}
