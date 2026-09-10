package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/rlm"
	"github.com/context-labs/whip/internal/session"
)

// loadPersistedScratch follows the same migration preference as the kernel:
// new opaque checkpoints are authoritative, with legacy tagged scratch fallback.
func loadPersistedScratch(store *session.Store, ctx context.Context, rootID, agentID string) (string, []byte, error) {
	envelope, image, err := store.LoadAgentCheckpoint(ctx, rootID, agentID)
	if err != nil {
		return "", nil, err
	}
	if len(envelope) == 0 {
		return store.LoadAgentScratch(ctx, rootID, agentID)
	}
	var metadata struct {
		Manifest json.RawMessage `json:"manifest"`
	}
	if err := json.Unmarshal(envelope, &metadata); err != nil {
		return "", nil, err
	}
	return string(image), metadata.Manifest, nil
}

func TestScratchCorruptDurableManifestPreservesCheckpoint(t *testing.T) {
	store, root, runtime := openRecursiveRuntime(t, llm.New("http://127.0.0.1:1", "key"), 1)
	node := runtime.rootNode
	if _, err := node.kernel.Exec(t.Context(), "good = 42"); err != nil {
		t.Fatal(err)
	}
	envelope, image, err := store.LoadAgentCheckpoint(t.Context(), root.ID(), node.id)
	if err != nil {
		t.Fatal(err)
	}
	var corrupt map[string]json.RawMessage
	if err := json.Unmarshal(envelope, &corrupt); err != nil {
		t.Fatal(err)
	}
	corrupt["manifest"] = json.RawMessage(`"corrupt"`)
	broken, err := json.Marshal(corrupt)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAgentCheckpoint(t.Context(), root.ID(), node.id, broken, image); err != nil {
		t.Fatal(err)
	}
	if err := node.kernel.Suspend(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := node.kernel.Exec(t.Context(), "good"); err == nil {
			t.Fatal("corrupt manifest was ignored")
		}
		if node.kernel.Started() || runtime.kernels.Active() != 0 {
			t.Fatal("failed restore retained a worker or its reservation")
		}
		kept, body, err := store.LoadAgentCheckpoint(t.Context(), root.ID(), node.id)
		if err != nil || !bytes.Equal(body, image) || !bytes.Equal(kept, broken) {
			t.Fatalf("failed restore changed the checkpoint: %v", err)
		}
	}
	if err := store.SaveAgentCheckpoint(t.Context(), root.ID(), node.id, envelope, image); err != nil {
		t.Fatal(err)
	}
	if result, err := node.kernel.Exec(t.Context(), "good == 42"); err != nil || result.Value != true {
		t.Fatalf("repaired checkpoint did not restore: result=%+v err=%v", result, err)
	}
}

func TestScratchStateRootAndChildSurviveDaemonRestart(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { streamText(w, "done") }))
	defer server.Close()
	client := llm.New(server.URL, "key")
	database := filepath.Join(t.TempDir(), "sessions.db")
	store := openStore(t, database)
	rootID := createRoot(t, store)
	owner, root, runtime := openPromptRuntime(t, store, rootID, client)
	childID := rootID + ":keeper"
	if _, err := store.AdmitAgent(t.Context(), session.AgentAdmission{
		RootID: rootID, ParentAgentID: root.AgentID(), ChildAgentID: childID, Name: "keeper",
		Capabilities: []session.CapabilityDelegation{{
			ID: "mcp:" + childID, Issuer: root.authority.MCP, AgentID: childID, Operations: []string{"mcp.call"},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.restoreChildren(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, node := range []*AgentSession{runtime.rootNode, runtime.agents[childID]} {
		code := fmt.Sprintf(`shared = [1]
container = {"items": shared}
big = 1 << 100
def remember():
    return shared
def record():
    return state.private_append(key="effects", value=1)
def stateful(x, cache=[]):
    cache.append(x)
    return cache
registry = [remember]
state.private_set(key="memory", value={"owner": %q, "big": 1 << 100})
state.private_set(key="effects", value=[])
True`, node.id)
		if result, err := node.kernel.Exec(t.Context(), code); err != nil || result.Value != true {
			t.Fatalf("initial %s result=%+v err=%v", node.id, result, err)
		}
		if _, err := node.kernel.Exec(t.Context(), "number = 1\ndef stale():\n    return number"); err != nil {
			t.Fatal(err)
		}
		if _, err := node.kernel.Exec(t.Context(), "number = 2"); err != nil {
			t.Fatal(err)
		}
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if runtime.kernels.Active() != 0 {
		t.Fatal("closed daemon retained worker reservations")
	}
	store = openStore(t, database)
	_, _, restored := openPromptRuntime(t, store, rootID, client)
	for _, node := range []*AgentSession{restored.rootNode, restored.agents[childID]} {
		if node == nil {
			t.Fatal("retained child was lost")
		}
		ctx, start, release, err := node.kernel.AcquireTurn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if start.Restore == nil {
			release()
			t.Fatal("restart did not report restored scratch")
		}
		failed := map[string]bool{}
		for _, item := range start.Restore.Failed {
			failed[item.Name] = true
		}
		for _, name := range []string{"registry", "stateful", "stale"} {
			if !failed[name] {
				release()
				t.Fatalf("%s did not report unsupported %s: %+v", node.id, name, start.Restore)
			}
		}
		code := fmt.Sprintf(`shared.append(42)
memory = state.private_get(key="memory")["value"]
unchanged = state.private_get(key="effects")["value"] == []
record()
unchanged and container["items"] == [1, 42] and remember() == [1, 42] and big == 1 << 100 and memory == {"owner": %q, "big": 1 << 100} and state.private_get(key="effects")["value"] == [1]`, node.id)
		result, err := node.kernel.Exec(ctx, code)
		release()
		if err != nil || result.Value != true {
			t.Fatalf("restored %s result=%+v err=%v", node.id, result, err)
		}
	}
}

func TestScratchStateContractReachesProvider(t *testing.T) {
	requests := make(chan llm.Request, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var input llm.Request
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		requests <- input
		streamText(w, "done")
	}))
	defer server.Close()
	_, root, _ := openRecursiveRuntime(t, llm.New(server.URL, "key"), 1)
	receipt, err := root.Submit(t.Context(), "inspect the environment")
	if err != nil {
		t.Fatal(err)
	}
	if result := waitReceipt(t, receipt); result.Err != nil {
		t.Fatal(result.Err)
	}
	request := <-requests
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"mutable or nonliteral defaults", "unsaved checkpoint", "decoded value", "json.decode", "string-keyed dictionaries"} {
		if !strings.Contains(string(encoded), want) {
			t.Fatalf("actual provider request omitted %q", want)
		}
	}
}

func TestLegacyScratchMigratesToOpaqueCheckpoint(t *testing.T) {
	store, root, runtime := openRecursiveRuntime(t, llm.New("http://127.0.0.1:1", "key"), 1)
	node := runtime.rootNode
	legacy, err := rlm.NewKernel(rlm.KernelOptions{Command: recursiveKernelCommand, Scratch: scratchStore{node: node}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(t.Context(), "legacy_value = 42"); err != nil {
		t.Fatal(err)
	}
	legacy.Close()
	before, manifest, err := store.LoadAgentScratch(t.Context(), root.ID(), node.id)
	if err != nil || before == "" {
		t.Fatalf("legacy fixture=%q %v", before, err)
	}
	if result, err := node.kernel.Exec(t.Context(), "legacy_value == 42"); err != nil || result.Value != true {
		t.Fatalf("legacy restore=%+v %v", result, err)
	}
	if envelope, _, err := store.LoadAgentCheckpoint(t.Context(), root.ID(), node.id); err != nil || len(envelope) == 0 {
		t.Fatalf("new checkpoint missing: %v", err)
	}
	after, encoded, err := store.LoadAgentScratch(t.Context(), root.ID(), node.id)
	if err != nil || after != before || !bytes.Equal(manifest, encoded) {
		t.Fatal("legacy original was discarded")
	}
}
