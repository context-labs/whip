package process

import "unicode/utf8"

func noticeText(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	end := limit - 3
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}
	return text[:end] + "..."
}
