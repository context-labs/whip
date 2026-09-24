package daemon

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/rlm"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
)

func TestReportModeRestoresIdentityAndCompletionBehavior(t *testing.T) {
	for _, mode := range []string{"", "notice", "inline", "message"} {
		name := mode
		if name == "" {
			name = "default"
		}
		t.Run(name, func(t *testing.T) {
			var answer atomic.Value
			answer.Store(strings.Repeat("a", 512))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				streamText(w, answer.Load().(string))
			}))
			defer server.Close()
			path := filepath.Join(t.TempDir(), "sessions.db")
			store := openStore(t, path)
			rootID := createRoot(t, store)
			makeOwner := func(store *session.Store) (*Daemon, *Session, *RecursiveRuntime) {
				var runtime *RecursiveRuntime
				owner, err := New(store, func(_ context.Context, meta session.Meta, history []llm.Message) (Components, error) {
					value := agent.NewRuntime(llm.New(server.URL, "key"), "model", 1024, "", tools.NewServices())
					value.ModelName, value.Provider, value.WorkingDir = meta.Model, meta.Provider, meta.CWD
					limits := rlm.DefaultLimits()
					var err error
					runtime, err = NewRecursiveRuntime(RecursiveRuntimeOptions{
						Agent: value, History: history, Limits: limits, Kernels: rlm.NewManager(limits.MaxWorkers), KernelCommand: recursiveKernelCommand,
					})
					if err != nil {
						return Components{}, err
					}
					return Components{Runner: runtime.RootSession(), Runtime: runtime, Bind: runtime.Bind}, nil
				})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = owner.Close() })
				root, err := owner.Open(rootID)
				if err != nil {
					t.Fatal(err)
				}
				return owner, root, runtime
			}
			owner, root, runtime := makeOwner(store)
			spawned, err := runtime.rootNode.host.Call(t.Context(), "agents", "spawn", map[string]any{
				"name": "worker", "prompt": "answer once", "report": mode,
			})
			if err != nil {
				t.Fatal(err)
			}
			childID := spawned.(map[string]any)["id"].(string)
			expected := mode
			if expected == "" {
				expected = "notice"
			}
			checkIdentity := func() *AgentSession {
				t.Helper()
				runtime.mu.RLock()
				child := runtime.agents[childID]
				runtime.mu.RUnlock()
				if child == nil {
					t.Fatalf("child %q was not restored", childID)
				}
				waitAgentIdle(t, child)
				if child.report != expected {
					t.Fatalf("child report = %q, want %q", child.report, expected)
				}
				messages := child.agent.MessagesSnapshot()
				identity := messages[0].Content
				if start := strings.LastIndex(identity, "\n\nIdentity:"); start >= 0 {
					identity = identity[start:]
				}
				fragment := "160-byte preview"
				switch expected {
				case "inline":
					fragment = "up to 4 KiB of your last text"
				case "message":
					fragment = "no completion notice when you succeed"
				}
				if !strings.Contains(identity, fragment) {
					t.Fatalf("identity disagrees with report %q: %s", expected, identity)
				}
				inspected, err := runtime.rootNode.host.Call(t.Context(), "agents", "inspect", map[string]any{"id": childID})
				if err != nil || inspected.(map[string]any)["report"] != expected {
					t.Fatalf("inspect lost report: %+v, %v", inspected, err)
				}
				snapshot, err := root.Snapshot(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				for _, record := range snapshot.Agents {
					if record.ID == childID && record.Report == expected {
						return child
					}
				}
				t.Fatal("root reconnect snapshot lost child report mode")
				return child
			}
			for _, stage := range []string{"spawned", "reopened"} {
				if stage == "reopened" {
					if err := owner.Close(); err != nil {
						t.Fatal(err)
					}
					answer.Store(strings.Repeat("b", 512))
					store = openStore(t, path)
					owner, root, runtime = makeOwner(store)
					if _, err := runtime.rootNode.host.Call(t.Context(), "agents", "submit", map[string]any{
						"id": childID, "text": "answer again", "delivery": "queued",
					}); err != nil {
						t.Fatal(err)
					}
				}
				child := checkIdentity()
				waitAgentIdle(t, child)
				if expected == "message" {
					// Exercise suppression synchronously as well: an absence assertion
					// must not pass just because the worker has not posted its mail yet.
					child.postCompletionNotice("succeeded", answer.Load().(string), nil)
					mail, err := store.ListMailboxMessages(t.Context(), rootID, rootID, "all", childID, 100)
					if err != nil {
						t.Fatal(err)
					}
					if len(mail) != 0 {
						t.Fatalf("%s message-mode success sent automatic mail: %+v", stage, mail)
					}
					continue
				}
				text := answer.Load().(string)
				wantPreview := text
				if expected == "notice" {
					wantPreview = text[:agentNoticePreviewBytes] + "…"
				}
				mail := reportWaitNotice(t, store, rootID, childID, wantPreview)
				found := false
				for _, message := range mail {
					read, err := store.ReadMailboxMessage(t.Context(), rootID, rootID, message.ID)
					if err != nil {
						t.Fatal(err)
					}
					if !strings.HasSuffix(string(read.Body.Inline), "last text: "+wantPreview) {
						continue
					}
					found = true
					if message.Kind != session.MessageKindAgentCompleted || (message.EvidenceReferenceID != "") != (expected == "notice") {
						t.Fatalf("%s notice metadata disagrees with report: %+v", stage, message)
					}
				}
				if !found {
					t.Fatalf("%s completion preview lost for %q: %+v", stage, expected, mail)
				}
			}
			child := checkIdentity()
			child.postCompletionNotice("failed", "", errors.New("expected test failure"))
			mail, err := store.ListMailboxMessages(t.Context(), rootID, rootID, "all", childID, 100)
			if err != nil {
				t.Fatal(err)
			}
			for _, message := range mail {
				if message.Kind == session.MessageKindAgentFailed {
					return
				}
			}
			t.Fatal("restored report mode hid failure notice")
		})
	}
}

func reportWaitNotice(t *testing.T, store *session.Store, rootID, childID, preview string) []session.MailboxMessage {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mail, err := store.ListMailboxMessages(t.Context(), rootID, rootID, "all", childID, 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, message := range mail {
			if strings.HasSuffix(message.Excerpt, "last text: "+preview) {
				return mail
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("child completion notice did not arrive")
	return nil
}
