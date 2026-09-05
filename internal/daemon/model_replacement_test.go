package daemon

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

func TestModelReplacementRejectsRunningDescendantBeforeConstruction(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-release:
			streamText(w, "child completed")
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer unblock()
	store, root, runtime := openRecursiveRuntime(t, llm.New(server.URL, "key"), 1)
	spawned, err := runtime.rootNode.host.Call(t.Context(), "agents", "spawn", map[string]any{"name": "worker", "prompt": "work", "report": "message"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("child model call did not start")
	}
	var constructions atomic.Int32
	root.factory = func(context.Context, session.Meta, []llm.Message) (Components, error) {
		constructions.Add(1)
		return Components{}, errors.New("replacement factory must not run")
	}
	result := clientCommand(t, root, "tui", "change-model", "session.model", map[string]string{"args": "replacement provider"})
	if result.Status != "failed" || !strings.Contains(result.Error, "agent or client operation is running") || constructions.Load() != 0 {
		t.Fatalf("replacement with running child = %+v, constructions=%d", result, constructions.Load())
	}
	unblock()
	childID := spawned.(map[string]any)["id"].(string)
	runtime.mu.RLock()
	child := runtime.agents[childID]
	runtime.mu.RUnlock()
	waitAgentIdle(t, child)
	stored, err := store.LoadAgentTranscript(t.Context(), root.ID(), childID)
	if err != nil || len(stored) < 2 || stored[len(stored)-1].Content != "child completed" {
		t.Fatalf("rejected replacement interrupted child = %+v, %v", stored, err)
	}
}
