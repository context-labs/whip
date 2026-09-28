package protocol

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/lsp"
)

func TestLanguageServerProjectionBoundsAndPrivateFailures(t *testing.T) {
	projected := LanguageServersFromDomain([]lsp.Status{{Name: "custom", State: "failed", Root: "/workspace", Err: "private-command-and-environment"}})
	raw, err := json.Marshal(projected)
	if err != nil || strings.Contains(string(raw), "private-command") {
		t.Fatal(string(raw), err)
	}
	if err := Validate("LanguageServersResult", raw); err != nil {
		t.Fatal(err)
	}
	if err := Validate("LanguageServersResult", []byte(`{"items":[{"name":"custom","state":"restart","workspace_root":null,"failure":null}]}`)); err == nil {
		t.Fatal("invented server state accepted")
	}
}
