package capability

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBrowserCallReferenceWireShape(t *testing.T) {
	call := BrowserCall{Grant: Reference{ID: "grant", Generation: 2}, Arguments: json.RawMessage(`{"code":"title()"}`)}
	raw, err := json.Marshal(call)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"grant":{"id":"grant","generation":2}`) ||
		!strings.Contains(string(raw), `"arguments":{"code":"title()"}`) {
		t.Fatalf("unexpected browser envelope: %s", raw)
	}
	var legacy Reference
	if err := json.Unmarshal([]byte(`{"ID":"old","Generation":1}`), &legacy); err != nil || legacy.ID != "old" || legacy.Generation != 1 {
		t.Fatalf("old persisted reference no longer decodes: %+v %v", legacy, err)
	}
}

func TestBrowserPermissionsHaveNoRememberedRule(t *testing.T) {
	for _, operation := range []string{"browser.open", "browser.attach", "browser.run", "browser.detach", "browser.allow_preview_port", "browser.future"} {
		if _, rules, ok := PermissionRule(operation, json.RawMessage(`{}`), ""); ok || len(rules) != 0 {
			t.Fatalf("%s can be remembered: %v", operation, rules)
		}
	}
}
