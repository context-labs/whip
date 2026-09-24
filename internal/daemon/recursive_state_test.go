package daemon

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/llm"
	sessionstore "github.com/context-labs/whip/internal/session"
)

func TestStructuredStateSubprocessRoundTrip(t *testing.T) {
	_, _, runtime := openRecursiveRuntime(t, llm.New("http://127.0.0.1:1", "key"), 1)
	code := `
a = state.private_set(key="record", value={"integer": 123456789012345678901234567890, "float": 1.0, "null": None, "list": [True, "x"]})
b = state.private_get(key="record")
print(a["value"] == b["value"], b["value"]["integer"] == 123456789012345678901234567890, type(b["value"]["float"]))
c = state.private_cas(key="record", version=b["version"], value=[])
d = state.private_append(key="record", value={"n": 9007199254740993})
print(d["value"][0]["n"] == 9007199254740993)
print(json.decode('{"n": 123456789012345678901234567890}')["n"] == 123456789012345678901234567890)
state.blackboard_set(key="shared", value=None)
print(state.blackboard_get(key="shared")["value"] == None)
huge = int("9" * 400)
state.private_set(key="huge", value=huge)
print(state.private_get(key="huge")["value"] == huge)
`
	result, err := runtime.rootNode.kernel.Exec(t.Context(), code)
	if err != nil || !strings.Contains(result.Output, "True True float") || strings.Count(result.Output, "True") != 6 { //nolint:dupword // the fixture intentionally prints two consecutive boolean results
		t.Fatalf("result=%+v err=%v", result, err)
	}
	for _, code := range []string{
		`state.private_set(key="missing")`,
		`state.private_set(key="bad", value=b"bytes")`,
		`state.private_set(key="bad", value=(1, 2))`,
		`state.private_set(key="bad", value=lambda: 1)`,
		`state.private_set(key="bad", value=float("inf"))`,
		`state.private_set(key="bad", value="\xff")`,
		`state.private_set(key="bad", value={"\xff":1})`,
		"cycle=[]\ncycle.append(cycle)\nstate.private_set(key=\"bad\", value=cycle)",
		`state.private_cas(key="record", version=1, value="conflict")`,
	} {
		if _, err := runtime.rootNode.kernel.Exec(t.Context(), code); err == nil {
			t.Errorf("accepted %s", code)
		}
	}
}

func TestStructuredStatePagesAndLargeHandles(t *testing.T) {
	_, _, runtime := openRecursiveRuntime(t, llm.New("http://127.0.0.1:1", "key"), 1)
	host := runtime.rootNode.host
	for i := range 25 {
		if _, err := host.Call(t.Context(), "state", "private_set", map[string]any{"key": fmt.Sprintf("k%02d", i), "value": strings.Repeat("x", 3000)}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := host.Call(t.Context(), "state", "private_list", map[string]any{"limit": 100})
	if err != nil {
		t.Fatal(err)
	}
	first := page.(map[string]any)
	encoded, err := json.Marshal(first)
	if err != nil || len(encoded) > 64<<10 {
		t.Fatalf("encoded page size=%d err=%v", len(encoded), err)
	}
	if !first["truncated"].(bool) || len(first["items"].([]any)) == 0 {
		t.Fatalf("page=%v", first)
	}
	next := first["next"].(map[string]any)
	page, err = host.Call(t.Context(), "state", "private_list", next)
	if err != nil || page.(map[string]any)["truncated"] != false {
		t.Fatalf("next=%v err=%v", page, err)
	}
	large := strings.Repeat("y", sessionstore.InlineValueLimit+1)
	value, err := host.Call(t.Context(), "state", "blackboard_set", map[string]any{"key": "large", "value": large})
	if err != nil {
		t.Fatal(err)
	}
	record := value.(map[string]any)
	if record["handle"] == "" || record["media_type"] != "application/json" || record["value"] != nil {
		t.Fatalf("record=%v", record)
	}
	result, err := runtime.rootNode.kernel.Exec(t.Context(), `r = state.blackboard_get(key="large")
parts = []
for offset in range(0, r["size"], 8192):
    parts.append(context.read(handle=r["handle"], offset=offset, length=8192)["text"])
print(len(json.decode("".join(parts))))`)
	if err != nil || !strings.Contains(result.Output, strconv.Itoa(len(large))) {
		t.Fatalf("large result=%+v err=%v", result, err)
	}
	for i := range 4 {
		if _, err := host.Call(t.Context(), "state", "blackboard_set", map[string]any{"key": "history", "value": i}); err != nil {
			t.Fatal(err)
		}
	}
	page, err = host.Call(t.Context(), "state", "blackboard_history", map[string]any{"key": "history", "limit": 2, "after_version": 1})
	if err != nil {
		t.Fatal(err)
	}
	if page.(map[string]any)["next"].(map[string]any)["after_version"] != int64(3) {
		t.Fatalf("history=%v", page)
	}
}

func TestStructuredStatePageDefaultsAndValidation(t *testing.T) {
	_, _, runtime := openRecursiveRuntime(t, llm.New("http://127.0.0.1:1", "key"), 1)
	host := runtime.rootNode.host
	for i := range 25 {
		if _, err := host.Call(t.Context(), "state", "private_set", map[string]any{"key": fmt.Sprintf("k%02d", i), "value": nil}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := host.Call(t.Context(), "state", "private_list", nil)
	if err != nil || len(page.(map[string]any)["items"].([]any)) != 20 {
		t.Fatalf("default=%v err=%v", page, err)
	}
	for _, limit := range []any{0, 101, 1.5, "2"} {
		if _, err := host.Call(t.Context(), "state", "private_list", map[string]any{"limit": limit}); err == nil {
			t.Fatalf("accepted limit=%v", limit)
		}
	}
	if _, err := host.Call(t.Context(), "state", "private_set", map[string]any{"key": "bytes", "value": []byte("x")}); err == nil {
		t.Fatal("accepted direct bytes")
	}
}

func TestStructuredStateRejectsMalformedValues(t *testing.T) {
	for _, value := range []any{"\xff", map[string]any{"\xff": nil}, json.Number("1e999"), json.Number("not-a-number")} {
		if _, err := runtimeStatePayload(value); err == nil {
			t.Fatalf("accepted %#v", value)
		}
	}
	for _, data := range []string{"null null", "1 trailing", "\"\xff\""} { //nolint:dupword // two JSON values intentionally test rejection of trailing content
		if _, err := stateResult(sessionstore.StateValue{Key: "corrupt", Payload: sessionstore.RuntimeValue{Inline: []byte(data)}}, nil); err == nil {
			t.Fatalf("accepted corrupt state %q", data)
		}
	}
}
