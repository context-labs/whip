package runtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
	"github.com/context-labs/whip/internal/tool"
)

func TestHistoryPreparationPinsSnapshotAndRejectsForeignOwners(t *testing.T) {
	r := openTest(t, t.TempDir(), model.Scripted{})
	owner, other := createTest(t, r), createTest(t, r)
	submitTest(t, r, owner.ID, "original")
	turn, err := r.store.Claim(t.Context(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := r.PrepareCoordination(t.Context(), owner, tool.Invocation{Module: "context", Name: "inspect", Arguments: map[string]any{}})
	if err != nil || prepared.Capability != "context.inspect" || prepared.Resource != string(owner.TreeID) || prepared.Mutating || prepared.Acquire == nil || prepared.Apply != nil {
		t.Fatalf("inspection preparation=%+v err=%v", prepared, err)
	}
	var normalized historyPageRequest
	if err := json.Unmarshal(prepared.Arguments, &normalized); err != nil || normalized.ThroughSequence == nil || *normalized.ThroughSequence != 1 {
		t.Fatalf("snapshot was not frozen in operation intent: %s %v", prepared.Arguments, err)
	}
	if _, err := r.store.AppendMessage(t.Context(), turn.Turn.ID, session.MessageDraft{ID: "later", Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: "later message"}}}); err != nil {
		t.Fatal(err)
	}
	value, err := prepared.Run(t.Context(), "test_operation")
	if err != nil {
		t.Fatal(err)
	}
	page, ok := value.(session.HistoryMetadataPage)
	if !ok || len(page.Items) != 1 || page.ThroughSequence != 1 || page.NextAfter != nil {
		t.Fatalf("prepared inspection read future history: %+v", value)
	}
	foreign, err := r.PrepareCoordination(t.Context(), other, tool.Invocation{Module: "context", Name: "read", Arguments: map[string]any{"id": "later"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := foreign.Run(t.Context(), "test_operation"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("message identity granted foreign access: %v", err)
	}
	for _, call := range []tool.Invocation{
		{Module: "context", Name: "inspect", Arguments: map[string]any{"session_id": other.ID}},
		{Module: "context", Name: "inspect", Arguments: map[string]any{"limit": 101}},
		{Module: "context", Name: "inspect", Arguments: map[string]any{"after": "2", "through_sequence": "1"}},
		{Module: "context", Name: "inspect", Arguments: map[string]any{"through_sequence": "99"}},
		{Module: "context", Name: "read", Arguments: map[string]any{"id": "later", "offset": "-1"}},
		{Module: "context", Name: "read", Arguments: map[string]any{"id": "later", "length": 65537}},
		{Module: "context", Name: "read", Arguments: map[string]any{"id": "later", "offset": 9007199254740992.0}},
		{Module: "context", Name: "search", Arguments: map[string]any{"query": ""}},
		{Module: "context", Name: "history", Arguments: map[string]any{}},
	} {
		if _, err := r.PrepareCoordination(t.Context(), owner, call); !errors.Is(err, session.ErrInvalid) {
			t.Fatalf("invalid history call accepted: %+v %v", call, err)
		}
	}
}

func TestOwnHistoryThroughBothEnginesUsesExactBytesAndScopedOperations(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			var code string
			provider := providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
				last := request.Messages[len(request.Messages)-1]
				if last.Role == session.Tool {
					if last.Parts[0].Result.IsError || !strings.Contains(last.Parts[0].Result.Output, "history-evidence-ok") {
						return model.Response{}, fmt.Errorf("history inspection failed: %s", last.Parts[0].Result.Output)
					}
					return model.Response{Parts: []session.Part{{Type: "text", Text: "done"}}}, nil
				}
				return childControlCode(1, code), nil
			})
			r := openMailRuntime(t, t.TempDir(), provider)
			owner := createEngineSession(t, r, engine)
			submitTest(t, r, owner.ID, "seed-history")
			seed, err := r.store.Claim(t.Context(), owner.ID)
			if err != nil {
				t.Fatal(err)
			}
			parts := []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "old-call", Name: "execute", Arguments: json.RawMessage(`{"n":9007199254740993,"text":"history needle ` + strings.Repeat("界", 24000) + `"}`)}}}
			if _, err := r.store.Finish(t.Context(), seed.Turn.ID, session.Succeeded, nil, []session.MessageDraft{
				{ID: "old-code", Role: session.Assistant, Parts: parts},
				{ID: "old-result", Role: session.Tool, Parts: []session.Part{{Type: "tool_result", Result: &session.ToolResult{CallID: "old-call", Output: "already completed"}}}},
				{ID: "old-answer", Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: "seed complete"}}},
			}); err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(parts)
			if err != nil {
				t.Fatal(err)
			}
			first, second := base64.StdEncoding.EncodeToString(raw[:65536]), base64.StdEncoding.EncodeToString(raw[65536:])
			for _, capability := range []string{"context.inspect", "context.read", "context.search"} {
				if _, err := r.CreateGrant(t.Context(), session.Grant{ID: session.GrantID(strings.ReplaceAll(capability, ".", "_")), SessionID: owner.ID, Capability: capability, Resource: string(owner.TreeID)}); err != nil {
					t.Fatal(err)
				}
			}
			code = fmt.Sprintf(`view=context.inspect(limit=100)
hits=context.search(query="history needle",through_sequence=view["through_sequence"],limit=1)
if hits["matches"][0]["message"]["id"] != "old-code" or hits["matches"][0]["field"] != "arguments": fail("wrong source")
first=context.read(id="old-code",offset="0",length=65536)
second=context.read(id="old-code",offset=first["next_offset"],length=65536)
if first["data_base64"] != %q or second["data_base64"] != %q or second["next_offset"] != None: fail("changed exact bytes")
print("history-evidence-ok")`, first, second)
			if engine == session.QuickJS {
				code = fmt.Sprintf(`var view=await context.inspect({limit:100});
var hits=await context.search({query:"history needle",through_sequence:view.through_sequence,limit:1});
if(hits.matches[0].message.id!=="old-code"||hits.matches[0].field!=="arguments") throw Error("wrong source");
var first=await context.read({id:"old-code",offset:"0",length:65536});
var second=await context.read({id:"old-code",offset:first.next_offset,length:65536});
if(first.data_base64!==%q||second.data_base64!==%q||second.next_offset!==null) throw Error("changed exact bytes");
print("history-evidence-ok");`, first, second)
			}
			submitTest(t, r, owner.ID, "read-history")
			if err := r.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			finished := waitTestWithin(t, r, "read-history", terminal, 30*time.Second)
			if finished.Turn.State != session.Succeeded {
				history, _ := r.History(t.Context(), owner.ID, 0, 100)
				evidence, _ := json.Marshal(struct {
					Turn    *session.Turn
					History []session.Message
				}{finished.Turn, history})
				t.Fatalf("history execution failed: %s", evidence)
			}
			operations, err := r.Operations(t.Context(), finished.Turn.ID, "", 100)
			if err != nil || len(operations) != 4 {
				t.Fatalf("history bypassed ledger: %+v %v", operations, err)
			}
			for _, operation := range operations {
				if operation.State != session.OperationSucceeded || operation.GrantID == nil || operation.Resource != string(owner.TreeID) {
					t.Fatalf("history bypassed scoped authority: %+v", operation)
				}
			}
		})
	}
}
