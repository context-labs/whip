package runtime

import (
	"errors"
	"fmt"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/session"
)

func TestBothEnginesShellForegroundJobsContentAndStop(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			codes := map[string]string{
				"foreground": `print(shell.run(command="printf hello")["output"])`,
				"large":      `s=shell.run(command="head -c 1100000 /dev/zero | tr '\\0' x"); print(s["bytes"]); print(s["retained_bytes"])`,
				"start":      `j=shell.start(command="printf background; sleep 30"); print("started")`,
				"poll":       `print(shell.poll(id=j["id"])["running"])`,
				"wait":       `w=shell.start(command="printf completed"); print(shell.wait(id=w["id"],timeout_ms=1000)["output"])`,
			}
			if engine == session.QuickJS {
				codes = map[string]string{
					"foreground": `console.log((await shell.run({command:"printf hello"})).output)`,
					"large":      `var s=await shell.run({command:"head -c 1100000 /dev/zero | tr '\\0' x"}); console.log(s.bytes); console.log(s.retained_bytes)`,
					"start":      `var j=await shell.start({command:"printf background; sleep 30"}); console.log("started")`,
					"poll":       `console.log((await shell.poll({id:j.id})).running)`,
					"wait":       `var w=await shell.start({command:"printf completed"}); console.log((await shell.wait({id:w.id,timeout_ms:1000})).output)`,
				}
			}
			r := openEngineTest(t, t.TempDir(), cellProvider(codes))
			root := createEngineSession(t, r, engine)
			for _, name := range []string{"run", "start", "poll", "wait"} {
				if _, err := r.CreateGrant(t.Context(), session.Grant{ID: session.GrantID("shell-" + name), SessionID: root.ID, Capability: "shell." + name, Resource: root.WorkingDirectory}); err != nil {
					t.Fatal(err)
				}
			}
			runCellTurn(t, r, root.ID, "foreground", "hello\n")
			runCellTurn(t, r, root.ID, "large", "1100000\n1048576\n")
			large, err := r.store.LatestCell(t.Context(), root.ID)
			if err != nil {
				t.Fatal(err)
			}
			operations, err := r.Operations(t.Context(), large.TurnID, "", 10)
			if err != nil || len(operations) != 1 {
				t.Fatal(operations, err)
			}
			if operations[0].State != session.OperationSucceeded || !strings.Contains(string(operations[0].Result.Value), `"truncated":true`) {
				t.Fatal(operations)
			}
			ref, data, err := r.ReadContent(t.Context(), root.ID, string(operations[0].ID)+"_output", session.MaxContentBytes)
			if err != nil || ref.Size != 1<<20 || len(data) != 1<<20 {
				t.Fatal(ref, len(data), err)
			}
			runCellTurn(t, r, root.ID, "wait", "completed\n")
			runCellTurn(t, r, root.ID, "start", "started\n")
			want := "True\n"
			if engine == session.QuickJS {
				want = "true\n"
			}
			runCellTurn(t, r, root.ID, "poll", want)
			scope, err := r.shells.Capture(string(root.ID))
			if err != nil {
				t.Fatal(err)
			}
			ids, err := scope.JobIDs()
			if err != nil {
				t.Fatal(err)
			}
			var pid int
			for _, id := range ids {
				job, err := scope.Job(id)
				if err != nil {
					t.Fatal(err)
				}
				if job.Running() {
					pid = job.PID()
				}
			}
			if pid == 0 {
				t.Fatal("no persistent job")
			}
			if _, err := r.SetLifecycle(t.Context(), root.ID, session.Stopped); err != nil {
				t.Fatal(err)
			}
			if err := syscall.Kill(-pid, 0); !errors.Is(err, syscall.ESRCH) {
				t.Fatal("stop retained shell group", pid, err)
			}
			if scope.Context().Err() == nil {
				t.Fatal("stop retained prepared authority")
			}
		})
	}
}

func TestShellInputValidation(t *testing.T) {
	for name, args := range map[string]map[string]any{"run": {"command": "echo no", "timeout": 121}, "start": {"command": "echo no", "timeout": 86401}, "wait": {"id": "job", "timeout_ms": 25001}, "tail": {"id": "job", "bytes": 8193}, "list": {"id": "hidden"}} {
		if _, err := parseShellRequest(name, args); err == nil {
			t.Fatal(name, "accepted invalid args")
		}
	}
}

func TestShellPermissionPrecedesStartAndDenialLeavesNoJob(t *testing.T) {
	r := openEngineTest(t, t.TempDir(), cellProvider(map[string]string{"shell": `shell.start(command="sleep 30")`}))
	root := createEngineSession(t, r, session.Starlark)
	submitTest(t, r, root.ID, "shell")
	deadline := time.Now().Add(10 * time.Second)
	var operation session.OperationID
	for time.Now().Before(deadline) {
		permissions, err := r.Permissions(t.Context(), root.ID, "", 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(permissions) == 1 {
			operation = permissions[0].OperationID
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if operation == "" {
		t.Fatal("shell did not wait for permission")
	}
	scope, err := r.shells.Capture(string(root.ID))
	if err != nil {
		t.Fatal(err)
	}
	if ids, err := scope.JobIDs(); err != nil || len(ids) != 0 {
		t.Fatal(ids, err)
	}
	if _, err := r.ResolvePermission(t.Context(), operation, false); err != nil {
		t.Fatal(err)
	}
	finished := waitTestWithin(t, r, "shell", terminal, 30*time.Second)
	operations, err := r.Operations(t.Context(), finished.Turn.ID, "", 10)
	if err != nil || len(operations) != 1 || operations[0].State != session.OperationDenied {
		t.Fatal(fmt.Sprint(operations), err)
	}
	if ids, err := scope.JobIDs(); err != nil || len(ids) != 0 {
		t.Fatal(ids, err)
	}
}

func TestBothEnginesInteractiveShellHumanInputIsTransient(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			code := `print(shell.run(command="stty -echo; printf ready; read value; printf 'len:%s' ${#value}", interactive=True)["output"])`
			if engine == session.QuickJS {
				code = `console.log((await shell.run({command:"stty -echo; printf ready; read value; printf 'len:%s' ${#value}", interactive:true})).output)`
			}
			r := openEngineTest(t, t.TempDir(), cellProvider(map[string]string{"interactive": code}))
			root := createEngineSession(t, r, engine)
			if _, err := r.CreateGrant(t.Context(), session.Grant{ID: "shell-interactive", SessionID: root.ID, Capability: "shell.run", Resource: root.WorkingDirectory}); err != nil {
				t.Fatal(err)
			}
			submitTest(t, r, root.ID, "interactive")
			var operation session.OperationID
			deadline := time.Now().Add(10 * time.Second)
			for time.Now().Before(deadline) {
				view, err := r.ShellInteraction(t.Context(), root.ID, 0)
				if err != nil {
					t.Fatal(err)
				}
				if view != nil && strings.Contains(string(view.Output), "ready") {
					operation = session.OperationID(view.OperationID)
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if operation == "" {
				t.Fatal("interactive shell never published its prompt")
			}
			if err := r.ShellInput(t.Context(), root.ID, "foreign", 1, []byte("do-not-type")); err == nil {
				t.Fatal("foreign input accepted")
			}
			if err := r.ShellInput(t.Context(), root.ID, operation, 1, []byte("private-input\n")); err != nil {
				t.Fatal(err)
			}
			finished := waitTestWithin(t, r, "interactive", terminal, 30*time.Second)
			if finished.Turn.State != session.Succeeded {
				t.Fatal(finished)
			}
			operations, err := r.Operations(t.Context(), finished.Turn.ID, "", 10)
			if err != nil || len(operations) != 1 || operations[0].State != session.OperationSucceeded || !strings.Contains(string(operations[0].Result.Value), "readylen:13") || strings.Contains(string(operations[0].Result.Value), "private-input") || strings.Contains(string(operations[0].Arguments), "private-input") {
				t.Fatal("input was persisted or execution failed", operations, err)
			}
			if view, err := r.ShellInteraction(t.Context(), root.ID, 0); err != nil || view != nil {
				t.Fatal("terminal command retained input authority", view, err)
			}
			if err := r.ShellInput(t.Context(), root.ID, operation, 2, []byte("stale")); err == nil {
				t.Fatal("completed operation accepted input")
			}
		})
	}
}

func TestBothEnginesTurnCancellationStopsForegroundButRetainsJob(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			code := `j=shell.start(command="sleep 30"); shell.run(command="printf begun; sleep 30", interactive=True)`
			if engine == session.QuickJS {
				code = `var j=await shell.start({command:"sleep 30"}); await shell.run({command:"printf begun; sleep 30",interactive:true})`
			}
			r := openEngineTest(t, t.TempDir(), cellProvider(map[string]string{"cancel-shell": code}))
			root := createEngineSession(t, r, engine)
			for _, verb := range []string{"run", "start"} {
				if _, err := r.CreateGrant(t.Context(), session.Grant{ID: session.GrantID("cancel-" + verb), SessionID: root.ID, Capability: "shell." + verb, Resource: root.WorkingDirectory}); err != nil {
					t.Fatal(err)
				}
			}
			submitTest(t, r, root.ID, "cancel-shell")
			deadline := time.Now().Add(10 * time.Second)
			var operation session.OperationID
			for time.Now().Before(deadline) {
				view, err := r.ShellInteraction(t.Context(), root.ID, 0)
				if err != nil {
					t.Fatal(err)
				}
				if view != nil && strings.Contains(string(view.Output), "begun") {
					operation = session.OperationID(view.OperationID)
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if operation == "" {
				t.Fatal("foreground never started")
			}
			op, err := r.Operation(t.Context(), operation)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := r.CancelTurn(t.Context(), op.TurnID); err != nil {
				t.Fatal(err)
			}
			waitTestWithin(t, r, "cancel-shell", terminal, 30*time.Second)
			op, err = r.Operation(t.Context(), operation)
			if err != nil || op.State != session.OperationUncertain || op.Result == nil || !strings.Contains(string(op.Result.Value), "begun") {
				t.Fatal("lost uncertain partial effects", op, err)
			}
			scope, err := r.shells.Capture(string(root.ID))
			if err != nil {
				t.Fatal(err)
			}
			ids, err := scope.JobIDs()
			if err != nil || len(ids) != 1 {
				t.Fatal(ids, err)
			}
			job, err := scope.Job(ids[0])
			if err != nil || !job.Running() {
				t.Fatal("turn cancellation killed a session job", err)
			}
			if view, err := r.ShellInteraction(t.Context(), root.ID, 0); err != nil || view != nil {
				t.Fatal(view, err)
			}
			if _, err := r.SetLifecycle(t.Context(), root.ID, session.Stopped); err != nil {
				t.Fatal(err)
			}
			if job.Running() {
				t.Fatal("session stop retained job")
			}
		})
	}
}
