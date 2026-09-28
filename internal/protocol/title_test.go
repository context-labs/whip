package protocol

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/session"
)

func TestAutomaticTitlePolicyPreservesExplicitFalse(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		patch := ConfigPatch{AutomaticTitle: &enabled}
		raw, err := json.Marshal(patch)
		if err != nil || !strings.Contains(string(raw), `"automatic_title":`) {
			t.Fatal("policy omitted", string(raw), err)
		}
		var decoded ConfigPatch
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		domain, err := decoded.Domain()
		if err != nil || domain.AutomaticTitle == nil || *domain.AutomaticTitle != enabled {
			t.Fatal("policy became inheritance", domain, err)
		}
		config, err := ConfigurationFromDomain(session.Configuration{AutomaticTitle: enabled})
		if err != nil || config.AutomaticTitle != enabled {
			t.Fatal("configuration lost naming policy", config, err)
		}
	}
}

func TestAutomaticTitleEvidenceBoundsIdentityAndOwnership(t *testing.T) {
	value := session.AutomaticTitleDecision{
		TreeID: "tree", SessionID: "root", InputID: new(session.InputID("input")),
		ConfigRevision: 9007199254740993, ExpectedRevision: 9007199254740994,
		Enabled: true, Model: session.ModelSelection{Provider: "provider", Name: "title", Temperature: new(0.0)},
		Source: strings.Repeat("🌍", 300), Reason: "eligible", CreatedAt: time.Now(),
	}
	wire := AutomaticTitleDecisionFromDomain(value)
	if wire.ReceiptIdentity == nil || *wire.ReceiptIdentity != (RequestIdentity{ClientID: "automatic-title", RequestID: "tree"}) {
		t.Fatal("missing deterministic receipt identity", wire)
	}
	*wire.Model.Temperature = 1
	*wire.InputID = "changed"
	if *value.Model.Temperature != 0 || *value.InputID != "input" {
		t.Fatal("projection shared mutable values")
	}
	for _, length := range []int{0, 300, 301} {
		wire.Source = strings.Repeat("🌍", length)
		raw, err := json.Marshal(wire)
		if err != nil {
			t.Fatal(err)
		}
		if valid := Validate("AutomaticTitleDecision", raw) == nil; valid != (length <= 300) {
			t.Fatal("source bound", length, valid)
		}
	}
	value.Reason = "disabled"
	if AutomaticTitleDecisionFromDomain(value).ReceiptIdentity != nil {
		t.Fatal("disabled intent advertised helper work")
	}
	for _, text := range []string{strings.Repeat("🌍", 80), "Title", "", strings.Repeat("🌍", 81), "two\nlines", "control\u0085", "two\u2028lines", "title\n", "title\r", "title\u2029", " leading", "trailing ", "title\u00a0"} {
		candidate := AutomaticTitleResultFromDomain(session.AutomaticTitleResult{TreeID: "tree", AttemptID: "attempt", Text: text, Applied: true, CreatedAt: time.Now()})
		raw, err := json.Marshal(candidate)
		if err != nil {
			t.Fatal(err)
		}
		if (Validate("AutomaticTitleResult", raw) == nil) != (session.ValidateAutomaticTitle(text) == nil) {
			t.Fatalf("candidate contract differs for %q", text)
		}
	}
}
