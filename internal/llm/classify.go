package llm

import (
	"regexp"
	"strings"
)

// Providers and gateways often deliver a failure inside a 200 stream as an
// error chunk, or wrap an upstream status in prose. Status codes decide when
// present; these lists decide when only the message does. They are the
// families opencode and pi match, trimmed to what OpenAI-compatible gateways
// send.
type errorClass int

const (
	classUnknown errorClass = iota
	classTransient
	classPermanent
)

var transientProviderText = regexp.MustCompile(`(?i)` + strings.Join([]string{
	`rate.?limit`, `too many requests`, `\b(429|500|502|503|504|520|522|524)\b`,
	`overloaded`, `service.?unavailable`, `server.?error`, `internal.?error`,
	`time[d]?.?out`, `no next token`, `connection (reset|refused|lost|closed)`,
	`upstream`, `temporarily unavailable`, `try (your request )?again`,
	`provider returned error`, `stream ended (before|without)`, `resource.?exhausted`,
	`at capacity`,
}, "|"))

var permanentProviderText = regexp.MustCompile(`(?i)` + strings.Join([]string{
	`invalid.?api.?key`, `unauthorized`, `authentication`, `insufficient.?(quota|balance|credits?|funds)`,
	`billing`, `quota exceeded`, `usage limit`, `unsupported model`, `model.?not.?found`,
	`invalid request`, `not supported`, `context.?length`, `maximum context`, `prompt.?too.?long`,
}, "|"))

// classify reads a provider's own wording. Permanent wins over transient so
// "rate limit exceeded for this billing plan" is not repeated.
func classify(message string) errorClass {
	if permanentProviderText.MatchString(message) {
		return classPermanent
	}
	if transientProviderText.MatchString(message) {
		return classTransient
	}
	return classUnknown
}

// providerError classifies a failure the provider reported in words. Before
// the first delta an unknown message is worth exactly one repeat (nothing was
// generated); after it only a message that reads as transient is regenerated.
func providerError(message string, emitted bool) error {
	err := &providerMessageError{message: message}
	switch classify(message) {
	case classTransient:
		return err
	case classPermanent:
		return nonRetryable{err}
	}
	if emitted {
		return nonRetryable{err}
	}
	return preTokenError{err}
}

type providerMessageError struct{ message string }

func (e *providerMessageError) Error() string { return "api error: " + e.message }
