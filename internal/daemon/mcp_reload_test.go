package daemon

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/mcp"
	"github.com/context-labs/whip/internal/session"
)

func TestMCPReloadPreservesPermissionPolicyAndRetainedChildren(t *testing.T) {
	for _, mode := range []string{"deny", "headless", "automatic", "ask"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("WHIP_HOME", t.TempDir())
			url, effects := localMCPFixture(t, "guidance")
			store, root, runtime := mcpRuntimeFixture(t, url, mode == "deny")
			originalFactory := root.factory
			root.factory = func(ctx context.Context, meta session.Meta, history []llm.Message) (Components, error) {
				parts, err := originalFactory(ctx, meta, history)
				if err != nil {
					return parts, err
				}
				// Production factories begin in interactive mode. Replacement must
				// carry forward the session's selected policy before restoring children.
				parts.Runner.(*AgentSession).agent.Services.SetExternalPermissions(true)
				servers := map[string]mcp.ServerConfig{"local": {URL: url, Source: "fixture import", Origin: "claude", StartupTimeout: 2, ToolTimeout: 2}}
				if mode == "deny" {
					servers = mcp.FromConfigMap(map[string]config.MCPServer{"local": {URL: url, StartupTimeout: 2, ToolTimeout: 2}})
				}
				parts.MCP = mcp.NewManager(servers)
				return parts, nil
			}
			call, err := root.mcpManager().ResolveTool("local", "mutate")
			if err != nil {
				t.Fatal(err)
			}
			childID := root.ID() + ":retained"
			if _, err := store.AdmitAgent(t.Context(), session.AgentAdmission{
				RootID: root.ID(), ParentAgentID: root.AgentID(), ChildAgentID: childID, Name: "retained",
				Capabilities: []session.CapabilityDelegation{{
					ID: "mcp:" + childID, Issuer: root.authority.MCP, AgentID: childID,
					Operations: []string{"mcp.call"}, MCP: []capability.MCPSelector{call.MCPSelector},
				}},
			}); err != nil {
				t.Fatal(err)
			}
			if err := runtime.restoreChildren(t.Context()); err != nil {
				t.Fatal(err)
			}
			operation, payload := "permission.mode", clientActionPayload{ExternalPermissions: mode == "ask"}
			switch mode {
			case "deny":
				operation, payload = "tool.configure", clientActionPayload{DenyPermissions: true}
			case "headless":
				operation, payload = "run.configure", clientActionPayload{Headless: true}
			}
			if result := clientCommand(t, root, "human", "policy", operation, payload); result.Status != "succeeded" {
				t.Fatalf("policy command=%+v", result)
			}
			for _, stage := range []string{"before", "after"} {
				if stage == "after" {
					if result := clientCommand(t, root, "human", "reload", "session.reload", clientActionPayload{}); result.Status != "succeeded" {
						t.Fatalf("reload=%+v", result)
					}
					waitMCPReady(t, root.mcpManager())
					runtime = root.runtime.(*RecursiveRuntime)
				}
				runtime.mu.RLock()
				child := runtime.agents[childID]
				runtime.mu.RUnlock()
				if child == nil {
					t.Fatal("reload lost retained child")
				}
				for _, node := range []*AgentSession{runtime.rootNode, child} {
					if external := node.agent.Services.ExternalPermissionsEnabled(); external != (mode == "ask") {
						t.Fatalf("%s %s external=%v in %s mode", stage, node.id, external, mode)
					}
					if mode == "ask" {
						rejectMCPReloadCall(t, store, root, node, stage+node.id)
					} else if err := mcpCell(t, node, "mutate"); (err == nil) != (mode == "automatic") {
						t.Fatalf("%s %s %s call error=%v", stage, node.id, mode, err)
					}
				}
			}
			want := int32(0)
			if mode == "automatic" {
				want = 4
			}
			if effects.Load() != want {
				t.Fatalf("%s effects=%d want=%d", mode, effects.Load(), want)
			}
			if pending, err := store.ListPendingPermissions(t.Context(), root.ID()); err != nil || len(pending) != 0 {
				t.Fatalf("pending=%+v err=%v", pending, err)
			}
		})
	}
}

func rejectMCPReloadCall(t *testing.T, store *session.Store, root *Session, node *AgentSession, commandID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := node.kernel.Exec(ctx, `mcp.call(server="local", tool="mutate", arguments={})`)
		done <- err
	}()
	pending := waitMCPPermission(t, store, root)
	if pending.AgentID != node.id {
		t.Fatalf("permission owner=%s want=%s", pending.AgentID, node.id)
	}
	payload := json.RawMessage(`{"decision":"deny"}`)
	digest, err := requestDigest("root", root.ID(), "permission.decide", payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := root.DecidePermissionCommand(t.Context(), session.CommandAdmission{
		ClientID: "human", CommandID: commandID, RequestDigest: digest,
		Payload: session.RuntimePayload{Data: payload, MediaType: "application/json", Source: "permission decision"},
	}, pending.ID, capability.Decision{PrincipalID: "human", Reason: "reject"}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("rejected MCP call succeeded")
		}
	case <-ctx.Done():
		t.Fatal("rejected MCP call remained pending")
	}
}

func TestMCPSpawnCopiesDenialAppliedDuringAdmission(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	url, effects := localMCPFixture(t, "guidance")
	_, root, runtime := mcpRuntimeFixture(t, url, true)
	type spawnContextKey struct{}
	ctx, cancel := context.WithTimeout(context.WithValue(t.Context(), spawnContextKey{}, true), 5*time.Second)
	defer cancel()
	entered, deny := make(chan struct{}), make(chan struct{})
	actorDone := make(chan error, 1)
	go func() {
		actorDone <- root.routeControl(ctx, func(actorCtx context.Context) error {
			close(entered)
			select {
			case <-deny:
			case <-ctx.Done():
				return ctx.Err()
			}
			_, err := root.applyClientCommand(actorCtx, "tool.configure", json.RawMessage(`{"deny_permissions":true}`))
			return err
		})
	}()
	<-entered
	type spawnResult struct {
		value any
		err   error
	}
	spawnDone := make(chan spawnResult, 1)
	go func() {
		value, err := runtime.spawn(ctx, runtime.rootNode, "child", "finish", map[string]any{"report": "notice"})
		spawnDone <- spawnResult{value: value, err: err}
	}()
	// The child has cloned its services when its admission reaches the actor.
	// Hold that admission while the actor applies the tree-wide deny policy.
	for {
		queued := false
		root.supervisor.mu.Lock()
		for _, event := range root.supervisor.events {
			queued = queued || event.kind == workerControl && event.controlCtx.Value(spawnContextKey{}) == true
		}
		root.supervisor.mu.Unlock()
		if queued {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("child admission did not reach the actor")
		case <-time.After(time.Millisecond):
		}
	}
	close(deny)
	if err := <-actorDone; err != nil {
		t.Fatal(err)
	}
	result := <-spawnDone
	if result.err != nil {
		t.Fatal(result.err)
	}
	id := result.value.(map[string]any)["id"].(string)
	runtime.mu.RLock()
	child := runtime.agents[id]
	runtime.mu.RUnlock()
	if err := mcpCell(t, child, "mutate"); err == nil || effects.Load() != 0 {
		t.Fatalf("child retained pre-denial consent: effects=%d err=%v", effects.Load(), err)
	}
}
