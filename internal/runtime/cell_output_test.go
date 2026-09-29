package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func TestCellOutputBoundsUnicodeAndLateCallbacks(t *testing.T) {
	r := &Runtime{cellOutputs: map[session.SessionID]*CellOutputPreview{}}
	turn := session.Turn{ID: "turn", SessionID: "owner", HistoryRevision: 9007199254740993}
	emit, end := r.beginCellOutput(turn, "cell", "message", "call")
	emit("hello\n")
	before := *r.cellOutputs[turn.SessionID]
	emit("hello\n")
	if !reflect.DeepEqual(before, *r.cellOutputs[turn.SessionID]) {
		t.Fatal("identical output advanced revision")
	}
	emit(strings.Repeat("x", maxCellOutputBytes-1) + "界")
	live := *r.cellOutputs[turn.SessionID]
	if len(live.Text) != maxCellOutputBytes-1 || !utf8.ValidString(live.Text) || !live.Truncated || live.Revision != 2 {
		t.Fatal("invalid bounded output", len(live.Text), live.Truncated, live.Revision)
	}
	emit("changed after truncation")
	if !reflect.DeepEqual(live, *r.cellOutputs[turn.SessionID]) {
		t.Fatal("truncated prefix changed")
	}
	next, finish := r.beginCellOutput(turn, "next", "next_message", "next_call")
	next("new cell")
	emit("stale callback")
	end()
	if r.cellOutputs[turn.SessionID].CellID != "next" || r.cellOutputs[turn.SessionID].Text != "new cell" {
		t.Fatal("old lifetime replaced new output")
	}
	finish()
	next("after completion")
	if len(r.cellOutputs) != 0 {
		t.Fatal("late callback revived output")
	}
	malformed, closeMalformed := r.beginCellOutput(turn, "invalid", "message", "call")
	malformed("ok\xff" + strings.Repeat("x", maxCellOutputBytes))
	if got := r.cellOutputs[turn.SessionID]; got.Text != "ok" || !got.Truncated {
		t.Fatal("invalid UTF-8 escaped bound", got)
	}
	closeMalformed()
}

func TestCellOutputCapacityAndConcurrentRetirement(t *testing.T) {
	r := &Runtime{cellOutputs: map[session.SessionID]*CellOutputPreview{}}
	var ends []func()
	for index := range 64 {
		_, end := r.beginCellOutput(session.Turn{SessionID: session.SessionID(fmt.Sprintf("owner_%d", index))}, "cell", "message", "call")
		ends = append(ends, end)
	}
	overflow, endOverflow := r.beginCellOutput(session.Turn{SessionID: "overflow"}, "cell", "message", "call")
	overflow("not retained")
	endOverflow()
	if len(r.cellOutputs) != 64 || r.cellOutputs["overflow"] != nil {
		t.Fatal("capacity exceeded")
	}
	emit, end := r.beginCellOutput(session.Turn{SessionID: "owner_0"}, "replacement", "message", "call")
	var workers sync.WaitGroup
	workers.Go(func() {
		for range 100 {
			emit("bounded")
		}
	})
	workers.Go(ends[0])
	workers.Wait()
	if r.cellOutputs["owner_0"].CellID != "replacement" {
		t.Fatal("stale retirement removed replacement at capacity")
	}
	workers.Go(func() {
		for range 100 {
			emit("bounded more")
		}
	})
	workers.Go(end)
	workers.Wait()
	for _, end := range ends {
		end()
	}
	if len(r.cellOutputs) != 0 {
		t.Fatal("retained ended output")
	}
}

func TestCellOutputSuppressesSettledAndForeignHistoryBeforeRetirement(t *testing.T) {
	r := openTest(t, t.TempDir(), model.Scripted{})
	root := createTest(t, r)
	submitTest(t, r, root.ID, "output")
	claimed, err := r.store.Claim(t.Context(), root.ID)
	if err != nil {
		t.Fatal(err)
	}
	message, err := r.store.AppendMessage(t.Context(), claimed.Turn.ID, session.MessageDraft{ID: "call_message", Role: session.Assistant, Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "call", Name: "execute", Arguments: json.RawMessage(`{"code":"print(1)"}`)}}}})
	if err != nil {
		t.Fatal(err)
	}
	cell, dispatch, err := r.store.BeginCell(t.Context(), session.CellSpec{ID: "cell", TurnID: claimed.Turn.ID, CallMessageID: message.ID, CallID: "call"})
	if err != nil || !dispatch {
		t.Fatal(cell, dispatch, err)
	}
	wrong := claimed.Turn
	wrong.HistoryRevision++
	bad, endBad := r.beginCellOutput(wrong, cell.ID, message.ID, "call")
	bad("retired")
	if got, err := r.CellOutput(t.Context(), root.ID); err != nil || got != nil {
		t.Fatal("foreign history visible", got, err)
	}
	endBad()
	emit, end := r.beginCellOutput(claimed.Turn, cell.ID, message.ID, "call")
	defer end()
	emit("live")
	got, err := r.CellOutput(t.Context(), root.ID)
	if err != nil || got == nil || got.CellID != cell.ID || got.Text != "live" {
		t.Fatal(got, err)
	}
	got.Text = "caller mutation"
	if next, err := r.CellOutput(t.Context(), root.ID); err != nil || next.Text != "live" {
		t.Fatal("read aliased mutable preview", next, err)
	}
	if _, err := r.store.SettleCell(t.Context(), cell.ID, session.CellSucceeded, session.ToolResult{CallID: "call", Output: "committed"}, nil); err != nil {
		t.Fatal(err)
	}
	emit("late settlement callback")
	if got, err := r.CellOutput(t.Context(), root.ID); err != nil || got != nil {
		t.Fatal("settled preview remained visible", got, err)
	}
	if _, err := r.CellOutput(t.Context(), "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestBothEnginesCellOutputBeforeQuestionAndAfterSettlementCancellationRestart(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			code := `print("first")` + "\n" + `print("before 界")` + "\n" + `user.ask(question="Choose", options=[{"label":"A"},{"label":"B"}])` + "\n" + `print("after")`
			if engine == session.QuickJS {
				code = `console.log("first"); console.log("before 界"); await user.ask({question:"Choose",options:[{label:"A"},{label:"B"}]}); console.log("after");`
			}
			complete := cellProvider(map[string]string{"settle": code, "cancel": code, "restart": code})
			var calls atomic.Int64
			provider := providerFunc(func(ctx context.Context, request model.Request) (model.Response, error) {
				calls.Add(1)
				return complete(ctx, request)
			})
			directory := t.TempDir()
			r := openEngineTest(t, directory, provider)
			for _, outcome := range []string{"settle", "cancel", "restart"} {
				owner := createEngineSession(t, r, engine)
				submitTest(t, r, owner.ID, outcome)
				question := awaitRuntimeQuestion(t, r, owner.ID, outcome)
				live, err := r.CellOutput(t.Context(), owner.ID)
				if err != nil || live == nil || live.Text != "first\nbefore 界\n" || live.CellID != question.CellID || live.TurnID != question.TurnID || live.SessionID != owner.ID || live.HistoryRevision != owner.HistoryRevision || live.Truncated {
					t.Fatal("output hidden during human wait", live, err)
				}
				cell, err := r.Cell(t.Context(), question.CellID)
				if err != nil || cell.State != session.CellRunning || cell.ResultMessageID != nil || live.CallMessageID != cell.CallMessageID || live.CallID != cell.CallID {
					t.Fatal("preview did not identify running canonical cell", live, cell, err)
				}
				foreign := createEngineSession(t, r, engine)
				if got, err := r.CellOutput(t.Context(), foreign.ID); err != nil || got != nil {
					t.Fatal("cross-session output", got, err)
				}
				switch outcome {
				case "settle":
					if _, err := r.AnswerQuestion(t.Context(), owner.ID, question.OperationID, []session.QuestionAnswer{{Answer: []string{"A"}}}); err != nil {
						t.Fatal(err)
					}
					result := questionCellResult(t, r, question, outcome)
					if result.Output != "first\nbefore 界\nafter\n" {
						t.Fatal("committed output changed", result.Output)
					}
				case "cancel":
					if _, err := r.CancelTurn(t.Context(), question.TurnID); err != nil {
						t.Fatal(err)
					}
					waitTest(t, r, outcome, terminal)
				case "restart":
					epoch := r.ProcessEpoch()
					if err := r.Close(); err != nil {
						t.Fatal(err)
					}
					r.previewMu.Lock()
					retained := len(r.cellOutputs)
					r.previewMu.Unlock()
					if retained != 0 {
						t.Fatal("close retained live output")
					}
					before := calls.Load()
					r = openEngineTest(t, directory, provider)
					if r.ProcessEpoch() == epoch || calls.Load() != before {
						t.Fatal("restart revived output or replayed model")
					}
				}
				if got, err := r.CellOutput(t.Context(), owner.ID); err != nil || got != nil {
					t.Fatal("closed cell output remained live", got, err)
				}
			}
		})
	}
}
