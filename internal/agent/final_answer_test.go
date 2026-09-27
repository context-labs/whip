package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/tools"
)

// A turn that hits the cap ends with a tools-disabled answer. That answer must
// pass the same final-message check a no-tools reply does, with the one
// correction round the check may ask for.
func TestFinalAnswerRunsCheckFinalWithOneCorrection(t *testing.T) {
	t.Parallel()
	var finals []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request llm.Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		switch {
		case len(request.Tools) > 0:
			fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"echo","arguments":"{\"s\":\"work\"}"}}]}}]}`+"\n\n")
		default:
			// The correction notice rides as an ephemeral system message
			// ahead of the tools-disabled nudge.
			var system []string
			for _, message := range request.Messages {
				if message.Role == "system" {
					system = append(system, message.Content)
				}
			}
			joined := strings.Join(system, "\n")
			finals = append(finals, joined)
			answer := "not json"
			if strings.Contains(joined, "correct") {
				answer = `{"ok":true}`
			}
			fmt.Fprintf(w, `data: {"choices":[{"delta":{"content":%q},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`+"\n\n", answer)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	ag := NewRuntime(llm.New(server.URL, "key"), "model", 1024, "system", tools.NewServices())
	defer ag.Services.Close()
	ag.Tools = []tools.Tool{echoTool()}
	ag.MaxTurns = 1
	var notice string
	checks := 0
	ev := Events{
		EphemeralNotices: func() string { return notice },
		CheckFinal: func(text string) (bool, error) {
			checks++
			if text == `{"ok":true}` {
				return false, nil
			}
			if checks > 1 {
				return false, fmt.Errorf("output_invalid: %q", text)
			}
			notice = "please correct: reply with one JSON value"
			return true, nil
		},
	}
	final, err := ag.Turn(t.Context(), "go", ev)
	if err != nil || final != `{"ok":true}` {
		t.Fatalf("capped turn = %q, %v", final, err)
	}
	if checks != 2 || len(finals) != 2 {
		t.Fatalf("checks=%d final calls=%d", checks, len(finals))
	}
	if strings.Contains(finals[0], "correct") || !strings.Contains(finals[1], "correct") {
		t.Fatalf("correction notice not delivered on the second final call: %q", finals)
	}
}

func TestFinalAnswerFailsTheTurnOnASecondMiss(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request llm.Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if len(request.Tools) > 0 {
			fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"echo","arguments":"{\"s\":\"work\"}"}}]}}]}`+"\n\n")
		} else {
			fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"still not json"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`+"\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	ag := NewRuntime(llm.New(server.URL, "key"), "model", 1024, "system", tools.NewServices())
	defer ag.Services.Close()
	ag.Tools = []tools.Tool{echoTool()}
	ag.MaxTurns = 1
	checks := 0
	_, err := ag.Turn(t.Context(), "go", Events{CheckFinal: func(string) (bool, error) {
		checks++
		if checks > 1 {
			return false, errors.New("output_invalid")
		}
		return true, nil
	}})
	if err == nil || !strings.Contains(err.Error(), "output_invalid") || checks != 2 {
		t.Fatalf("second miss should fail the turn: err=%v checks=%d", err, checks)
	}
}
