package rlm

import (
	"regexp"
	"strings"
)

var keywordArgument = regexp.MustCompile(`\b([A-Za-z_][A-Za-z_0-9]*)=`)

func IdentityBlockForEngine(engineID string, identity Identity) string {
	text := IdentityBlock(identity)
	if engineID == EngineQuickJS {
		return javascriptExamples(text)
	}
	return text
}

// Only rewrite actual host-call examples, leaving policy prose and local APIs
// unchanged. This also covers shared mailbox, citation and identity guidance.
func javascriptExamples(text string) string {
	var result strings.Builder
	for i := 0; i < len(text); {
		matched := false
		for module, operations := range moduleRegistry {
			for _, operation := range operations {
				prefix := module + "." + operation + "("
				if !strings.HasPrefix(text[i:], prefix) {
					continue
				}
				start, end, depth := i+len(prefix), i+len(prefix), 1
				var quote byte
				for end < len(text) && depth > 0 {
					c := text[end]
					if quote != 0 {
						if c == '\\' {
							end++
						} else if c == quote {
							quote = 0
						}
					} else {
						switch c {
						case '\'', '"':
							quote = c
						case '(':
							depth++
						case ')':
							depth--
						}
					}
					end++
				}
				if depth != 0 {
					continue
				}
				args := text[start : end-1]
				// Explicit JavaScript examples were already selected above.
				if strings.HasPrefix(strings.TrimSpace(args), "{") {
					result.WriteString(text[i:end])
					i = end
					matched = true
					break
				}
				args = keywordArgument.ReplaceAllString(args, "$1: ")
				args = strings.NewReplacer("True", "true", "False", "false", "None", "null").Replace(args)
				if args == "id" {
					args = "id: id"
				}
				result.WriteString("await " + prefix + "{" + args + "})")
				i = end
				matched = true
				break
			}
			if matched {
				break
			}
		}
		if !matched {
			result.WriteByte(text[i])
			i++
		}
	}
	return result.String()
}
