package main

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/llm"
)

// respondToTitleRequest handles the daemon's prompt-only naming request without
// adding it to conversation captures or advancing a tool-call fixture.
func respondToTitleRequest(t *testing.T, w http.ResponseWriter, request llm.Request) bool {
	t.Helper()
	if len(request.Messages) == 0 || request.Messages[0].Role != "system" ||
		!strings.HasPrefix(request.Messages[0].Content, "Name this session based on") {
		return false
	}
	if request.Stream || len(request.Tools) != 0 || len(request.Messages) != 2 ||
		request.Messages[1].Role != "user" || strings.TrimSpace(request.Messages[1].Content) == "" {
		t.Errorf("title request must be nonstreaming and prompt-only: %+v", request)
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"Fixture session title"},"finish_reason":"stop"}]}`)
	return true
}
