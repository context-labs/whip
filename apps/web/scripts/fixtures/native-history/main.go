//go:build unix

// The browser harness invokes this only between joined runtime processes. It
// seeds synthetic historical evidence through the current store API; it does
// not execute or claim to have executed the historical host effects.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/content"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
	"golang.org/x/sys/unix"
)

type child struct {
	ID   session.SessionID `json:"id"`
	Name string            `json:"name"`
}
type evidence struct {
	RootID            session.SessionID `json:"root_id"`
	RootMessages      int               `json:"root_messages"`
	Children          []child           `json:"children"`
	ChildMessages     int               `json:"child_messages"`
	LargeContent      string            `json:"large_content"`
	LargeContentBytes int               `json:"large_content_bytes"`
	CellID            session.CellID    `json:"cell_id"`
	Operations        int               `json:"operations"`
}

func main() {
	directory := flag.String("directory", "", "owned fixture runtime directory")
	root := flag.String("root", "", "empty fixture root")
	nonce := flag.String("nonce", "", "one-use fixture marker")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	value, err := seed(ctx, *directory, session.SessionID(*root), *nonce)
	if err == nil {
		err = json.NewEncoder(os.Stdout).Encode(value)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func seed(ctx context.Context, directory string, rootID session.SessionID, nonce string) (result evidence, err error) {
	// This helper cannot be pointed at an installed or user runtime by accident.
	parent := filepath.Dir(directory)
	if !filepath.IsAbs(directory) || filepath.Base(directory) != "state" || !strings.HasPrefix(filepath.Base(parent), "whip-web-native-") || len(nonce) != 36 {
		return result, errors.New("expected disposable native web fixture directory and marker")
	}
	owned, err := os.OpenRoot(parent)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, owned.Close()) }()
	const marker = "history-seed-owner"
	data, err := owned.ReadFile(marker)
	if err != nil || string(data) != nonce {
		return result, errors.New("fixture marker mismatch")
	}
	lock, err := owned.OpenFile("state/runtime.lock", os.O_RDWR, 0)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, lock.Close()) }()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return result, errors.New("fixture runtime must be stopped before seeding")
	}
	db, err := store.Open(ctx, filepath.Join(directory, "state.db"))
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	root, err := db.Session(ctx, rootID)
	if err != nil {
		return result, err
	}
	if root.ParentID != nil || filepath.Clean(root.WorkingDirectory) != filepath.Clean(parent) {
		return result, errors.New("root does not belong to fixture directory")
	}
	snapshot, err := db.HistorySnapshot(ctx, root.ID)
	if err != nil {
		return result, err
	}
	if snapshot.MessageCount != 0 {
		return result, errors.New("fixture root is not empty; seeding is never replayed")
	}
	if err := owned.Remove(marker); err != nil {
		return result, err
	}
	result = evidence{RootID: root.ID, RootMessages: 10000, Children: []child{}, ChildMessages: 100, LargeContent: "fixture-large-content", LargeContentBytes: 1400000, CellID: "fixture-history-cell", Operations: 128}
	bodies, err := content.New(directory)
	if err != nil {
		return result, err
	}
	body, err := bodies.Put([]byte(strings.Repeat("Large content stays on the host.\n", 50000)[:result.LargeContentBytes]))
	if err != nil {
		return result, err
	}
	if _, err := db.RegisterContent(ctx, session.ContentReference{ID: result.LargeContent, SessionID: root.ID, Digest: body.Digest, Size: body.Size, MediaType: "text/plain"}); err != nil {
		return result, err
	}
	// 4,998 genuine admitted user/assistant pairs, then one four-message cell turn.
	for index := range 4998 {
		turn, err := prompt(ctx, db, root.ID, fmt.Sprintf("root-%05d", index), rootText(index*2+1))
		if err != nil {
			return result, err
		}
		draft := session.MessageDraft{ID: session.MessageID(fmt.Sprintf("root-answer-%05d", index)), Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: rootText(index*2 + 2)}}}
		if index == 4990 {
			draft.Parts = append(draft.Parts, session.Part{Type: "content", ReferenceID: result.LargeContent})
		}
		if _, err := db.Finish(ctx, turn.ID, session.Succeeded, nil, []session.MessageDraft{draft}); err != nil {
			return result, err
		}
	}
	if err := cellHistory(ctx, db, root.ID, result.CellID); err != nil {
		return result, err
	}
	for index := range 100 {
		name := fmt.Sprintf("perf-child-%03d", index)
		def, err := db.RegisterDefinition(ctx, session.DefinitionDocument{ID: name, Name: name, Defaults: session.ConfigPatch{ReportMode: new(session.ReportMessage)}})
		if err != nil {
			return result, err
		}
		admitted, err := db.SpawnChild(ctx, session.RequestIdentity{ClientID: "history-fixture", RequestID: name}, store.ChildRequest{ParentID: root.ID, Definition: &def.Ref, Parts: []session.Part{{Type: "text", Text: name + " message 001. Child-only transcript stays separate from the root."}}})
		if err != nil {
			return result, err
		}
		childID := admitted.Session.ID
		claimed, err := db.Claim(ctx, childID)
		if err != nil {
			return result, err
		}
		messages := make([]session.MessageDraft, 99)
		for messageIndex := range messages {
			messages[messageIndex] = session.MessageDraft{ID: session.MessageID(fmt.Sprintf("%s-message-%03d", name, messageIndex+2)), Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: fmt.Sprintf("%s message %03d. Child-only transcript stays separate from the root.", name, messageIndex+2)}}}
		}
		if _, err := db.Finish(ctx, claimed.Turn.ID, session.Succeeded, nil, messages); err != nil {
			return result, err
		}
		if _, err := db.SetLifecycle(ctx, childID, session.Stopped); err != nil {
			return result, err
		}
		result.Children = append(result.Children, child{ID: childID, Name: name})
	}
	snapshot, err = db.HistorySnapshot(ctx, root.ID)
	if err != nil {
		return result, err
	}
	if snapshot.MessageCount != int64(result.RootMessages) {
		return result, fmt.Errorf("root message count %d", snapshot.MessageCount)
	}
	return result, nil
}

func rootText(index int) string {
	return fmt.Sprintf("Root message %05d. **Retained history** with selectable text.\n\n%s", index, strings.Repeat("Bounded history stays on the execution host. ", 8))
}

func prompt(ctx context.Context, db *store.Store, owner session.SessionID, key, text string) (session.Turn, error) {
	if _, err := db.Admit(ctx, session.RequestIdentity{ClientID: "history-fixture", RequestID: key}, store.Submission{SessionID: owner, Source: session.UserInput, Parts: []session.Part{{Type: "text", Text: text}}}); err != nil {
		return session.Turn{}, err
	}
	claimed, err := db.Claim(ctx, owner)
	return claimed.Turn, err
}

func cellHistory(ctx context.Context, db *store.Store, owner session.SessionID, id session.CellID) error {
	turn, err := prompt(ctx, db, owner, "history-cell", rootText(9997))
	if err != nil {
		return err
	}
	call, err := db.AppendMessage(ctx, turn.ID, session.MessageDraft{ID: "history-call", Role: session.Assistant, Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "history-call", Name: "execute", Arguments: json.RawMessage(`{"code":"files.read(path=\"source/example.ts\")"}`)}}}})
	if err != nil {
		return err
	}
	_, dispatch, err := db.BeginCell(ctx, session.CellSpec{ID: id, TurnID: turn.ID, CallMessageID: call.ID, CallID: "history-call"})
	if err != nil {
		return err
	}
	if !dispatch {
		return errors.New("fixture cell was already dispatched")
	}
	for index := range 128 {
		key := fmt.Sprintf("history-operation-%03d", index)
		path := fmt.Sprintf("source/file-%03d.ts", index)
		arguments, err := json.Marshal(map[string]string{"path": path})
		if err != nil {
			return err
		}
		operation, err := db.AdmitOperation(ctx, session.OperationSpec{ID: session.OperationID(key), CellID: id, RequestID: key, Capability: "files.read", Resource: path, Arguments: arguments})
		if err != nil {
			return err
		}
		if _, err := db.ResolvePermission(ctx, operation.ID, true); err != nil {
			return err
		}
		dispatch, err := db.DispatchOperation(ctx, operation.ID)
		if err != nil {
			return err
		}
		if !dispatch {
			return errors.New("fixture operation was not dispatched")
		}
		if _, err := db.SettleOperation(ctx, operation.ID, session.OperationResult{State: session.OperationSucceeded, Value: json.RawMessage(`{"output":"synthetic historical file evidence"}`)}); err != nil {
			return err
		}
	}
	if _, err := db.SettleCell(ctx, id, session.CellSucceeded, session.ToolResult{CallID: "history-call", Output: `{"value":null,"output":"128 files read","steps":128}`}, nil); err != nil {
		return err
	}
	_, err = db.Finish(ctx, turn.ID, session.Succeeded, nil, []session.MessageDraft{{ID: "history-finished", Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: rootText(10000)}}}})
	return err
}
