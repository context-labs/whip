package runtime

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func waitSchedule(t *testing.T, r *Runtime, owner session.SessionID, id session.ScheduleID) session.Schedule {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		value, err := r.Schedule(ctx, owner, id)
		if err != nil {
			t.Fatal(err)
		}
		if value.Latest != nil {
			return value
		}
		select {
		case <-ctx.Done():
			t.Fatalf("schedule never admitted: %+v runtime=%v", value, r.Err())
		case <-ticker.C:
		}
	}
}

func TestSchedulesFairAdmissionWithSaturatedWorker(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	provider := providerFunc(func(ctx context.Context, request model.Request) (model.Response, error) {
		if request.Messages[0].Parts[0].Text == "block" {
			close(entered)
			select {
			case <-ctx.Done():
				return model.Response{}, ctx.Err()
			case <-release:
			}
		}
		return model.Response{Parts: []session.Part{{Type: "text", Text: "done"}}}, nil
	})
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	r, err := Open(t.Context(), directory, provider, Options{Workers: 1, PollInterval: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	busy := createTest(t, r)
	blocked := createTest(t, r)
	healthy := createTest(t, r)
	if _, err := r.SetResource(t.Context(), blocked.ID, 1, session.ResourceLimit{Kind: session.ResourceQueuedInputs, Limit: new(int64(0))}); err != nil {
		t.Fatal(err)
	}
	submitTest(t, r, busy.ID, "block")
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not start")
	}
	for i := range 17 {
		_, err := r.CreateSchedule(t.Context(), blocked.ID, session.ScheduleID(fmt.Sprintf("blocked_%02d", i)), session.ScheduleSpec{Expression: "@at 2000-01-01T00:00:00Z", Parts: []session.Part{{Type: "text", Text: "blocked"}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = r.CreateSchedule(t.Context(), healthy.ID, "healthy", session.ScheduleSpec{Expression: "@at 2001-01-01T00:00:00.000000001Z", Parts: []session.Part{{Type: "text", Text: "healthy"}}})
	if err != nil {
		t.Fatal(err)
	}
	admitted := waitSchedule(t, r, healthy.ID, "healthy")
	input, err := r.store.Input(t.Context(), admitted.Latest.InputID)
	if err != nil || input.State != session.Queued || input.Source != session.ScheduledInput {
		t.Fatalf("saturated admission %+v %v", input, err)
	}
	for i := range 17 {
		value, err := r.Schedule(t.Context(), blocked.ID, session.ScheduleID(fmt.Sprintf("blocked_%02d", i)))
		if err != nil || value.Latest != nil || value.NextDue == nil {
			t.Fatalf("blocked slot changed %+v %v", value, err)
		}
	}
	close(release)
	identity := session.RequestIdentity{ClientID: admitted.Latest.ClientID, RequestID: admitted.Latest.RequestID}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for {
		value, err := r.Admission(ctx, identity)
		if err != nil {
			t.Fatal(err)
		}
		if terminal(value) {
			if value.Turn.State != session.Succeeded {
				t.Fatal(value.Turn)
			}
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
}

func TestSchedulesRestartUnloadedStoppedOwner(t *testing.T) {
	path := t.TempDir()
	r := openTest(t, path, model.Scripted{})
	owner := createTest(t, r)
	created, err := r.CreateSchedule(t.Context(), owner.ID, "restart", session.ScheduleSpec{Expression: "@at 2000-01-01T00:00:00.123456789Z", Parts: []session.Part{{Type: "text", Text: "after restart"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.SetLifecycle(t.Context(), owner.ID, session.Stopped); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openTest(t, path, model.Scripted{})
	if err := reopened.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.admitSchedules(t.Context(), scheduleScan{}); err != nil {
		t.Fatal(err)
	}
	before, err := reopened.Schedule(t.Context(), owner.ID, "restart")
	if err != nil || before.Latest != nil || !before.NextDue.Equal(*created.Schedule.NextDue) {
		t.Fatalf("stopped slot %+v %v", before, err)
	}
	if _, err := reopened.SetLifecycle(t.Context(), owner.ID, session.Active); err != nil {
		t.Fatal(err)
	}
	after := waitSchedule(t, reopened, owner.ID, "restart")
	if after.Latest.ScheduledFor.Nanosecond() != 123456789 {
		t.Fatal("restart rounded slot")
	}
}

func TestBothEnginesSchedulesRemainOwnedByInvoker(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			code := `created=schedules.create(expression="@at 2500-01-01T00:00:00.123456789Z",parts=[{"type":"text","text":"future"}])
listed=schedules.list(limit=10)
if len(listed["items"]) != 1 or listed["items"][0]["id"] != created["id"]: fail("wrong owner")
schedules.cancel(id=created["id"])
print("schedule owned")`
			if engine == session.QuickJS {
				code = `var created=await schedules.create({expression:"@at 2500-01-01T00:00:00.123456789Z",parts:[{type:"text",text:"future"}]}); var listed=await schedules.list({limit:10}); if(listed.items.length!==1||listed.items[0].id!==created.id) throw Error("wrong owner"); await schedules.cancel({id:created.id}); print("schedule owned");`
			}
			r := openEngineTest(t, t.TempDir(), cellProvider(map[string]string{"manage": code}))
			root := createEngineSession(t, r, engine)
			for _, name := range []string{"create", "list", "cancel"} {
				if _, err := r.CreateGrant(t.Context(), session.Grant{ID: session.GrantID("schedules_" + name), SessionID: root.ID, Capability: "schedules." + name, Resource: string(root.TreeID)}); err != nil {
					t.Fatal(err)
				}
			}
			runCellTurn(t, r, root.ID, "manage", "schedule owned\n")
			child, err := r.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "child_manage"}, store.ChildRequest{ParentID: root.ID, Parts: []session.Part{{Type: "text", Text: "manage"}}})
			if err != nil {
				t.Fatal(err)
			}
			finished := waitTestWithin(t, r, "child_manage", terminal, 30*time.Second)
			if finished.Turn.State != session.Succeeded {
				t.Fatalf("child schedule %+v", finished.Turn)
			}
			for _, owner := range []session.SessionID{root.ID, child.Session.ID} {
				page, err := r.Schedules(t.Context(), owner, session.ScheduleList{Limit: 10})
				if err != nil || len(page.Items) != 1 || page.Items[0].SessionID != owner || page.Items[0].CancelledAt == nil {
					t.Fatalf("ownership %+v %v", page, err)
				}
			}
		})
	}
}
