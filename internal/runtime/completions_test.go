package runtime

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
	"github.com/context-labs/whip/internal/tool"
)

func completionTest(t *testing.T, r *Runtime, parent session.Session, key, text string, mode session.ReportMode) session.CompletionMetadata {
	t.Helper()
	child, err := r.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "completion", RequestID: key}, store.ChildRequest{
		ParentID: parent.ID, Parts: []session.Part{{Type: "text", Text: key}}, Overrides: session.ConfigPatch{ReportMode: &mode},
	})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := r.store.Claim(t.Context(), child.Session.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.store.Finish(t.Context(), claim.Turn.ID, session.Succeeded, nil, []session.MessageDraft{{ID: session.MessageID("answer_" + key), Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: text}}}})
	if err != nil {
		t.Fatal(err)
	}
	completion, err := r.store.PendingCompletion(t.Context(), parent.ID, child.Session.ID, claim.Turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	return completion.CompletionMetadata
}

func artifactBytesTest(t *testing.T, r *Runtime, owner session.Session, id string) []byte {
	t.Helper()
	var result []byte
	var offset int64
	for {
		prepared, err := r.PrepareCoordination(t.Context(), owner, tool.Invocation{Module: "artifacts", Name: "read", Arguments: map[string]any{"id": id, "offset": strconv.FormatInt(offset, 10), "length": 4093}})
		if err != nil || prepared.Capability != "artifacts.read" || prepared.Resource != string(owner.TreeID) || prepared.Mutating || prepared.Apply != nil {
			t.Fatalf("artifact authority: %+v %v", prepared, err)
		}
		release, err := prepared.Acquire(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		value, err := prepared.Run(t.Context())
		release()
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var page struct {
			Data []byte `json:"data"`
			Next *int64 `json:"next_offset,string"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			t.Fatal(err)
		}
		result = append(result, page.Data...)
		if page.Next == nil {
			return result
		}
		if *page.Next <= offset {
			t.Fatal("artifact cursor did not advance")
		}
		offset = *page.Next
	}
}

func TestCompletionPendingPagingPublicationRestartAndChildDeletion(t *testing.T) {
	directory := t.TempDir()
	r := openTest(t, directory, model.Scripted{})
	parent, stranger := createTest(t, r), createTest(t, r)
	text := strings.Repeat("héllo🙂\n", 12000)
	metadata := completionTest(t, r, parent, "child", text, session.ReportInline)
	before, err := r.Budgets(t.Context(), parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	items, err := r.ListPendingCompletions(t.Context(), parent.ID, "", 100)
	if err != nil || len(items) != 1 || items[0].TextBytes != int64(len(text)) {
		t.Fatalf("pending metadata=%+v %v", items, err)
	}
	var raw []byte
	var offset int64
	for {
		page, err := r.ReadPendingCompletion(t.Context(), parent.ID, metadata.ChildID, metadata.TurnID, offset, 3071)
		if err != nil || page.Completion.TurnID != metadata.TurnID || len(page.Data) > 3071 {
			t.Fatalf("pending page=%+v %v", page, err)
		}
		raw = append(raw, page.Data...)
		if page.NextOffset == nil {
			if page.TotalBytes != int64(len(raw)) {
				t.Fatal("incomplete pending JSON")
			}
			break
		}
		offset = *page.NextOffset
	}
	var completion session.Completion
	if err := json.Unmarshal(raw, &completion); err != nil || completion.Text != text {
		t.Fatal("pending inspection lost full evidence", err)
	}
	if _, err := r.ReadPendingCompletion(t.Context(), stranger.ID, metadata.ChildID, metadata.TurnID, 0, 1); !errors.Is(err, store.ErrConflict) {
		t.Fatal("pending evidence escaped parent scope", err)
	}
	if _, err := r.ReadPendingCompletion(t.Context(), parent.ID, metadata.ChildID, "other_turn", 0, 1); !errors.Is(err, store.ErrConflict) {
		t.Fatal("pending read ignored exact turn", err)
	}
	if err := r.DeleteSubtree(t.Context(), metadata.ChildID); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r = openTest(t, directory, model.Scripted{})
	if next, err := r.publishCompletions(t.Context(), ""); err != nil || next != "" {
		t.Fatalf("publication cursor=%s %v", next, err)
	}
	if _, err := r.ReadPendingCompletion(t.Context(), parent.ID, metadata.ChildID, metadata.TurnID, 0, 1); !errors.Is(err, store.ErrConflict) {
		t.Fatal("published token was not cleared", err)
	}
	mails, err := r.ListMail(t.Context(), parent.ID, "", "", 100)
	if err != nil || len(mails) != 1 || mails[0].Source.Kind != "completion" {
		t.Fatalf("canonical completion mail=%+v %v", mails, err)
	}
	mail, err := r.ReadMail(t.Context(), parent.ID, mails[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	var notice session.CompletionNotice
	if err := json.Unmarshal([]byte(mail.Body), &notice); err != nil || !notice.TextTruncated || len(notice.Preview) > 4096 || notice.EvidenceRef != "completion_"+string(metadata.TurnID) {
		t.Fatalf("completion notice=%+v %v", notice, err)
	}
	if actual := artifactBytesTest(t, r, parent, notice.EvidenceRef); !bytes.Equal(actual, raw) {
		t.Fatal("published evidence differs from exact pending snapshot")
	}
	reference, _, err := r.ReadContentRange(t.Context(), parent.ID, notice.EvidenceRef, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []struct {
		owner session.SessionID
		id    string
	}{{stranger.ID, notice.EvidenceRef}, {parent.ID, reference.Digest}, {metadata.ChildID, notice.EvidenceRef}} {
		if _, _, err := r.ReadContentRange(t.Context(), request.owner, request.id, 0, 1); !errors.Is(err, store.ErrNotFound) {
			t.Fatal("artifact escaped owner reference", err)
		}
	}
	if _, err := r.publishCompletions(t.Context(), ""); err != nil {
		t.Fatal(err)
	}
	after, err := r.Budgets(t.Context(), parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	for i := range before {
		if before[i].Used != after[i].Used {
			t.Fatalf("derived completion charged %s", before[i].Kind)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r = openTest(t, directory, model.Scripted{})
	if actual := artifactBytesTest(t, r, parent, notice.EvidenceRef); !bytes.Equal(actual, raw) {
		t.Fatal("startup collection lost published evidence")
	}
}

func TestCompletionBlockedPrefixDoesNotHideLaterParents(t *testing.T) {
	r := openTest(t, t.TempDir(), model.Scripted{})
	for i := range 6 {
		parent := createTest(t, r)
		completionTest(t, r, parent, fmt.Sprintf("child_%d", i), "finished", session.ReportNotice)
	}
	candidates, err := r.store.CompletionCandidates(t.Context(), "", 100)
	if err != nil || len(candidates) != 6 {
		t.Fatal("missing candidate fixture", err)
	}
	// All owners reference one real immutable body. The first four owners have
	// full logical content capacity without inflating test disk usage.
	body, err := r.content.Put(bytes.Repeat([]byte{'x'}, session.MaxContentBytes))
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range candidates[:4] {
		for i := range session.MaxSessionContentBytes / session.MaxContentBytes {
			_, err := r.store.RegisterContent(t.Context(), session.ContentReference{ID: fmt.Sprintf("pressure_%s_%d", candidate.ChildID, i), SessionID: candidate.ParentID, Digest: body.Digest, Size: body.Size, MediaType: "application/octet-stream"})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	next, err := r.publishCompletions(t.Context(), "")
	if err != nil || next != candidates[3].ChildID {
		t.Fatalf("blocked page did not advance=%s %v", next, err)
	}
	page, err := r.ReadPendingCompletion(t.Context(), candidates[0].ParentID, candidates[0].ChildID, candidates[0].TurnID, 0, maxEvidenceReadBytes)
	if err != nil || !strings.Contains(string(page.Data), "finished") {
		t.Fatal("pressure removed inspection access", err)
	}
	next, err = r.publishCompletions(t.Context(), next)
	if err != nil || next != "" {
		t.Fatalf("end of scan did not wrap=%s %v", next, err)
	}
	for i, candidate := range candidates {
		items, err := r.ListPendingCompletions(t.Context(), candidate.ParentID, "", 100)
		if err != nil || (i < 4 && len(items) != 1) || (i >= 4 && len(items) != 0) {
			t.Fatalf("candidate %d pending=%+v %v", i, items, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := r.publishCompletions(ctx, ""); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled publication continued", err)
	}
}

func TestCompletionReadValidationAndImmutableRanges(t *testing.T) {
	r := openTest(t, t.TempDir(), model.Scripted{})
	parent := createTest(t, r)
	metadata := completionTest(t, r, parent, "child", "result", session.ReportNotice)
	for _, call := range []tool.Invocation{
		{Module: "agents", Name: "pending_reports", Arguments: map[string]any{"limit": 101}},
		{Module: "agents", Name: "read_report", Arguments: map[string]any{"child_id": metadata.ChildID, "turn_id": metadata.TurnID, "length": 65537}},
		{Module: "agents", Name: "read_report", Arguments: map[string]any{"child_id": metadata.ChildID, "turn_id": metadata.TurnID, "offset": 1}},
		{Module: "agents", Name: "read_report", Arguments: map[string]any{"child_id": metadata.ChildID, "turn_id": metadata.TurnID, "parent_id": parent.ID}},
		{Module: "artifacts", Name: "read", Arguments: map[string]any{"id": "reference", "offset": "-1"}},
		{Module: "artifacts", Name: "put", Arguments: map[string]any{}},
	} {
		if _, err := r.PrepareCoordination(t.Context(), parent, call); !errors.Is(err, session.ErrInvalid) {
			t.Fatalf("invalid inspection accepted: %+v %v", call, err)
		}
	}
	prepared, err := r.PrepareCoordination(t.Context(), parent, tool.Invocation{Module: "agents", Name: "pending_reports", Arguments: map[string]any{}})
	if err != nil || prepared.Capability != "agents.pending_reports" || prepared.Resource != string(parent.TreeID) || prepared.Mutating {
		t.Fatalf("pending inspection authority=%+v %v", prepared, err)
	}
	if _, err := prepared.Run(t.Context()); err != nil {
		t.Fatal(err)
	}
	page, err := r.ReadPendingCompletion(t.Context(), parent.ID, metadata.ChildID, metadata.TurnID, 0, maxEvidenceReadBytes)
	if err != nil {
		t.Fatal(err)
	}
	end, err := r.ReadPendingCompletion(t.Context(), parent.ID, metadata.ChildID, metadata.TurnID, page.TotalBytes, 1)
	if err != nil || len(end.Data) != 0 || end.NextOffset != nil {
		t.Fatal("EOF range", end, err)
	}
	if _, err := r.ReadPendingCompletion(t.Context(), parent.ID, metadata.ChildID, metadata.TurnID, page.TotalBytes+1, 1); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("out-of-range read", err)
	}
	if _, err := r.publishCompletions(t.Context(), ""); err != nil {
		t.Fatal(err)
	}
	ref, _, err := r.ReadContentRange(t.Context(), parent.ID, "completion_"+string(metadata.TurnID), 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, data, err := r.ReadContentRange(t.Context(), parent.ID, ref.ID, ref.Size, 1); err != nil || len(data) != 0 {
		t.Fatal("artifact EOF", err)
	}
	if _, _, err := r.ReadContentRange(t.Context(), parent.ID, ref.ID, ref.Size+1, 1); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("artifact offset overflow", err)
	}
}

func TestCompletionEvidenceThroughBothEnginesAndScopedDispatcher(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			var code string
			provider := providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
				last := request.Messages[len(request.Messages)-1]
				if last.Role == session.Tool {
					if last.Parts[0].Result.IsError || !strings.Contains(last.Parts[0].Result.Output, "completion-evidence-ok") {
						return model.Response{}, fmt.Errorf("completion inspection failed: %s", last.Parts[0].Result.Output)
					}
					return model.Response{Parts: []session.Part{{Type: "text", Text: "done"}}}, nil
				}
				return childControlCode(1, code), nil
			})
			r := openMailRuntime(t, t.TempDir(), provider)
			parent := createEngineSession(t, r, engine)
			published := completionTest(t, r, parent, "published", strings.Repeat("complete published evidence ", 700), session.ReportNotice)
			if err := r.publishCompletion(t.Context(), published); err != nil {
				t.Fatal(err)
			}
			id := "completion_" + string(published.TurnID)
			reference, publishedJSON, err := r.ReadContent(t.Context(), parent.ID, id, session.MaxContentBytes)
			if err != nil {
				t.Fatal(err)
			}
			pending := completionTest(t, r, parent, "pending", strings.Repeat("complete pending evidence ", 700), session.ReportNotice)
			value, err := r.store.PendingCompletion(t.Context(), parent.ID, pending.ChildID, pending.TurnID)
			if err != nil {
				t.Fatal(err)
			}
			pendingJSON, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			large := bytes.Repeat([]byte{'p'}, session.MaxContentBytes)
			body, err := r.content.Put(large)
			if err != nil {
				t.Fatal(err)
			}
			for remaining, i := int64(session.MaxSessionContentBytes)-reference.Size, 0; remaining > 0; i++ {
				size := min(remaining, int64(session.MaxContentBytes))
				if size < body.Size {
					body, err = r.content.Put(large[:size])
					if err != nil {
						t.Fatal(err)
					}
				}
				_, err := r.store.RegisterContent(t.Context(), session.ContentReference{ID: fmt.Sprintf("capacity_%d", i), SessionID: parent.ID, Digest: body.Digest, Size: body.Size, MediaType: "application/octet-stream"})
				if err != nil {
					t.Fatal(err)
				}
				remaining -= size
			}
			for _, capability := range []string{"agents.pending_reports", "agents.read_report", "artifacts.read"} {
				if _, err := r.CreateGrant(t.Context(), session.Grant{ID: session.GrantID("grant_" + strings.ReplaceAll(capability, ".", "_")), SessionID: parent.ID, Capability: capability, Resource: string(parent.TreeID)}); err != nil {
					t.Fatal(err)
				}
			}
			publishedBase64 := base64.StdEncoding.EncodeToString(publishedJSON)
			pendingBase64 := base64.StdEncoding.EncodeToString(pendingJSON)
			code = fmt.Sprintf(`listed=agents.pending_reports()
if len(listed["items"]) != 1 or listed["items"][0]["child_id"] != %q: fail("wrong pending child")
pending=agents.read_report(child_id=%q,turn_id=%q,offset="0",length=65536)
if pending["data"] != %q or pending["next_offset"] != None: fail("truncated pending evidence")
published=artifacts.read(id=%q,offset="0",length=65536)
if published["data"] != %q or published["next_offset"] != None: fail("truncated published evidence")
print("completion-evidence-ok")`, pending.ChildID, pending.ChildID, pending.TurnID, pendingBase64, id, publishedBase64)
			if engine == session.QuickJS {
				code = fmt.Sprintf(`var listed=await agents.pending_reports({});
if(listed.items.length!==1||listed.items[0].child_id!==%q) throw Error("wrong pending child");
var pending=await agents.read_report({child_id:%q,turn_id:%q,offset:"0",length:65536});
if(pending.data!==%q||pending.next_offset!==null) throw Error("truncated pending evidence");
var published=await artifacts.read({id:%q,offset:"0",length:65536});
if(published.data!==%q||published.next_offset!==null) throw Error("truncated published evidence");
print("completion-evidence-ok");`, pending.ChildID, pending.ChildID, pending.TurnID, pendingBase64, id, publishedBase64)
			}
			submitTest(t, r, parent.ID, "inspect-completions")
			if err := r.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			admission := waitTestWithin(t, r, "inspect-completions", terminal, 30*time.Second)
			if admission.Turn.State != session.Succeeded {
				t.Fatalf("inspection failed: %+v runtime=%v", admission.Turn, r.Err())
			}
			operations, err := r.Operations(t.Context(), admission.Turn.ID, "", 100)
			if err != nil || len(operations) != 3 {
				t.Fatalf("inspection bypassed ledger: %+v %v", operations, err)
			}
			for _, operation := range operations {
				if operation.State != session.OperationSucceeded || operation.GrantID == nil || operation.Resource != string(parent.TreeID) {
					t.Fatalf("inspection bypassed authority: %+v", operation)
				}
			}
		})
	}
}
