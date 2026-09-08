package daemon

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

func TestRecursiveTreeMessagesAndSubscriptionsRespectRelativeScope(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	store, root, runtime := openRecursiveRuntime(t, llm.New("http://unused.invalid", ""), 1)
	_, _, release, err := runtime.rootNode.kernel.AcquireTurn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	host := runtime.rootNode.host
	spawned := hostBehaviorCall(t, host, "agents", "spawn", map[string]any{"name": "worker", "prompt": "wait for work", "report": "message", "budgets": map[string]any{"tokens": float64(1000)}}).(map[string]any)
	childID := spawned["id"].(string)
	runtime.mu.RLock()
	child := runtime.agents[childID]
	runtime.mu.RUnlock()
	budgets, err := root.InspectBudgets(t.Context(), root.ID(), childID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, budget := range budgets {
		if budget.Kind == session.BudgetTokens {
			found = budget.Limit != nil && *budget.Limit == 1000
		}
	}
	if !found {
		t.Fatalf("child budget not applied: %+v", budgets)
	}
	parent := hostBehaviorCall(t, child.host, "agents", "inspect", map[string]any{"id": root.ID()}).(map[string]any)
	if parent["id"] != root.ID() {
		t.Fatalf("child cannot inspect parent: %+v", parent)
	}
	relatives := hostBehaviorCall(t, host, "agents", "list", nil).(session.AgentRelatives)
	if len(relatives.Children) != 1 || relatives.Children[0].ID != childID {
		t.Fatalf("relatives=%+v", relatives)
	}
	message := hostBehaviorCall(t, host, "messages", "send", map[string]any{"recipient": "worker", "subject": "task", "body": "by relative name", "delivery": "next_turn"}).(map[string]any)
	if message["recipient"] != childID {
		t.Fatalf("name was not resolved to child: %+v", message)
	}
	received := hostBehaviorCall(t, child.host, "messages", "read", map[string]any{"id": message["id"]}).(map[string]any)
	if received["body"] != "by relative name" {
		t.Fatalf("message body changed: %+v", received)
	}
	for _, args := range []map[string]any{{"id": message["id"]}, {"id": message["id"], "until": "tomorrow"}} {
		if _, err := child.host.Call(t.Context(), "messages", "defer", args); err == nil {
			t.Fatalf("invalid deferral accepted: %+v", args)
		}
	}
	deferred := hostBehaviorCall(t, child.host, "messages", "defer", map[string]any{"id": message["id"], "seconds": float64(3600)}).(map[string]any)
	if deferred["available_at"] == "" {
		t.Fatalf("deferral lacks deadline: %+v", deferred)
	}
	subscription := hostBehaviorCall(t, child.host, "state", "subscribe", map[string]any{"key": "progress"}).(session.BlackboardSubscription)
	listed := hostBehaviorCall(t, child.host, "state", "subscriptions", nil).([]session.BlackboardSubscription)
	if len(listed) != 1 || listed[0].ID != subscription.ID {
		t.Fatalf("subscription not listed: %+v", listed)
	}
	hostBehaviorCall(t, host, "state", "blackboard_set", map[string]any{"key": "progress", "value": []any{}})
	appended := hostBehaviorCall(t, host, "state", "blackboard_append", map[string]any{"key": "progress", "value": "compiled"}).(map[string]any)
	if !reflect.DeepEqual(appended["value"], []any{"compiled"}) {
		t.Fatalf("structured append lost element: %+v", appended)
	}
	version := appended["version"].(int64)
	hostBehaviorCall(t, host, "state", "blackboard_cas", map[string]any{"key": "progress", "version": version, "value": []any{"tested"}})
	if _, err := host.Call(t.Context(), "state", "blackboard_cas", map[string]any{"key": "progress", "version": version, "value": "stale"}); err == nil {
		t.Fatal("stale blackboard CAS overwrote newer value")
	}
	if _, err := host.Call(t.Context(), "state", "cancel_subscription", map[string]any{"id": subscription.ID}); !errors.Is(err, session.ErrSubscriptionAccess) {
		t.Fatalf("root cancelled child's subscription: %v", err)
	}
	hostBehaviorCall(t, child.host, "state", "cancel_subscription", map[string]any{"id": subscription.ID})
	if listed := hostBehaviorCall(t, child.host, "state", "subscriptions", nil).([]session.BlackboardSubscription); len(listed) != 0 {
		t.Fatalf("cancelled subscription remains: %+v", listed)
	}
	pending, err := store.ListMailboxMessages(t.Context(), root.ID(), childID, "pending", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	changed := 0
	for _, mail := range pending {
		if mail.Kind == session.MessageKindStateChanged && mail.Subject == "progress" {
			changed++
		}
	}
	if changed != 1 {
		t.Fatalf("subscription failed to coalesce changes: %+v", pending)
	}
	grandchild := hostBehaviorCall(t, child.host, "agents", "spawn", map[string]any{"name": "grandchild", "prompt": "wait", "report": "message"}).(map[string]any)
	grandchildID := grandchild["id"].(string)
	if _, err := host.Call(t.Context(), "agents", "submit", map[string]any{"id": grandchildID, "text": "skip parent"}); !errors.Is(err, session.ErrAgentAccess) {
		t.Fatalf("submission escaped direct child scope: %v", err)
	}
	hostBehaviorCall(t, host, "agents", "delete", map[string]any{"id": childID})
	for _, id := range []string{childID, grandchildID} {
		record, err := store.LoadAgent(t.Context(), root.ID(), id)
		if err != nil || record.Status != "deleted" {
			t.Fatalf("descendant not terminalized: %+v %v", record, err)
		}
		runtime.mu.RLock()
		remaining := runtime.agents[id]
		runtime.mu.RUnlock()
		if remaining != nil {
			t.Fatalf("deleted runtime remains: %s", id)
		}
	}
}

func TestRecursiveMalformedRequestsDoNotAdmitWork(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	_, root, runtime := openRecursiveRuntime(t, llm.New("http://unused.invalid", ""), 1)
	host := runtime.rootNode.host
	for _, test := range []struct {
		module, operation string
		args              map[string]any
	}{
		{"models", "call", nil},
		{"models", "batch", map[string]any{"prompts": []any{}}},
		{"agents", "spawn", nil},
		{"agents", "spawn", map[string]any{"prompt": "invalid", "capabilities": "read"}},
		{"agents", "spawn", map[string]any{"prompt": "invalid", "model": "unknown"}},
		{"agents", "spawn", map[string]any{"prompt": "invalid", "report": "typo"}},
		{"agents", "wait", nil},
		{"agents", "wait", map[string]any{"ids": []any{1}}},
		{"agents", "inspect", nil},
		{"agents", "inspect", map[string]any{"id": "foreign"}},
		{"agents", "submit", nil},
		{"agents", "submit", map[string]any{"id": root.ID(), "text": "invalid", "delivery": "typo"}},
		{"agents", "delete", map[string]any{"id": root.ID()}},
		{"messages", "send", nil},
		{"messages", "send", map[string]any{"recipient": "parent", "body": "invalid"}},
		{"messages", "send", map[string]any{"recipient": "foreign", "body": "invalid"}},
		{"messages", "read", map[string]any{"id": "foreign"}},
		{"messages", "ack", map[string]any{"ids": []any{false}}},
		{"state", "blackboard_cas", map[string]any{"key": "progress", "version": -1, "value": "invalid"}},
		{"mcp", "list_servers", nil},
	} {
		t.Run(test.module+"."+test.operation, func(t *testing.T) {
			if _, err := host.Call(t.Context(), test.module, test.operation, test.args); err == nil {
				t.Fatalf("invalid operation accepted: %+v", test.args)
			}
		})
	}
	batch := hostBehaviorCall(t, host, "models", "batch", map[string]any{"prompts": []any{17, false}}).([]map[string]any)
	if len(batch) != 2 || !strings.Contains(batch[0]["error"].(string), "not a string") || !strings.Contains(batch[1]["error"].(string), "not a string") {
		t.Fatalf("malformed batch lost positional errors: %+v", batch)
	}
	children, err := root.LoadRetainedAgents(t.Context())
	if err != nil || len(children) != 0 {
		t.Fatalf("invalid spawn retained children: %+v %v", children, err)
	}
}
