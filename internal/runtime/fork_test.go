package runtime

import (
	"reflect"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/session"
)

func TestForkStartsEmptyREPLAndPreservesSourceAcrossRestart(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			codes := map[string]string{"seed": "x = 41\nprint(x)", "probe": "print(x)", "source": "x += 1\nprint(x)", "fresh": "x = 7\nprint(x)", "retry": "x += 1\nprint(x)"}
			if engine == session.QuickJS {
				codes = map[string]string{"seed": "var x = 41; console.log(x)", "probe": "console.log(x)", "source": "x += 1; console.log(x)", "fresh": "var x = 7; console.log(x)", "retry": "x += 1; console.log(x)"}
			}
			directory := t.TempDir()
			r := openEngineTest(t, directory, cellProvider(codes))
			source := createEngineSession(t, r, engine)
			runCellTurn(t, r, source.ID, "seed", "41\n")
			snapshot, err := r.HistorySnapshot(t.Context(), source.ID)
			if err != nil {
				t.Fatal(err)
			}
			request := session.ForkRequest{ID: "fork", SessionID: source.ID, ExpectedHistoryRevision: snapshot.Revision, ExpectedConfigRevision: source.ConfigRevision, ObservedThrough: snapshot.ThroughSequence, KeepThrough: snapshot.ThroughSequence}
			fork, err := r.Fork(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			if cell, err := r.store.LatestCell(t.Context(), fork.Root.ID); err != nil || cell != nil {
				t.Fatal("fork acquired a source checkpoint", cell, err)
			}
			// The source remains alive with its original globals; no cell is replayed.
			runCellTurn(t, r, source.ID, "source", "42\n")
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			r = openEngineTest(t, directory, cellProvider(codes))
			submitTest(t, r, fork.Root.ID, "probe")
			waitTestWithin(t, r, "probe", terminal, 30*time.Second)
			cell, err := r.store.LatestCell(t.Context(), fork.Root.ID)
			if err != nil || cell == nil || cell.State != session.CellFailed {
				t.Fatal("fork restored source globals after restart", cell, err)
			}
			runCellTurn(t, r, fork.Root.ID, "fresh", "7\n")
			retry, err := r.Fork(t.Context(), request)
			if err != nil || !reflect.DeepEqual(fork.Fork, retry.Fork) || retry.Root.ID != fork.Root.ID {
				t.Fatal("retry changed destination or receipt", retry, err)
			}
			runCellTurn(t, r, fork.Root.ID, "retry", "8\n")
		})
	}
}
