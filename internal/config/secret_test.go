package config

import (
	"strings"
	"testing"
)

func TestResolveSecretEnvVar(t *testing.T) {
	t.Setenv("WHIP_SECRET_TEST", "s3cr3t")
	for _, ref := range []string{"$WHIP_SECRET_TEST", "${WHIP_SECRET_TEST}"} {
		got, err := ResolveSecret(ref)
		if err != nil || got != "s3cr3t" {
			t.Fatalf("%s: got %q err %v", ref, got, err)
		}
	}
}

func TestResolveSecretUnsetVar(t *testing.T) {
	for _, ref := range []string{"$WHIP_SECRET_UNSET", "${WHIP_SECRET_UNSET}"} {
		got, err := ResolveSecret(ref)
		if err == nil || !strings.Contains(err.Error(), "WHIP_SECRET_UNSET") {
			t.Fatalf("%s: expected unset-var error naming the var, got %q err %v", ref, got, err)
		}
		if got != "" {
			t.Fatalf("%s: expected empty on error, got %q", ref, got)
		}
	}
}

func TestResolveSecretCommand(t *testing.T) {
	got, err := ResolveSecret("!printf secret-value")
	if err != nil || got != "secret-value" {
		t.Fatalf("!printf: got %q err %v", got, err)
	}
	// Failing command errors; the reference, not stderr secrets, is in the message.
	if _, err := ResolveSecret("!exit 1"); err == nil {
		t.Fatal("!exit 1: expected error")
	}
}

func TestResolveSecretLiteral(t *testing.T) {
	got, err := ResolveSecret("sk-literal-key-123")
	if err != nil || got != "sk-literal-key-123" {
		t.Fatalf("literal: got %q err %v", got, err)
	}
	// A trailing/embedded $ is not a reference.
	got, err = ResolveSecret("price is $5")
	if err != nil || got != "price is $5" {
		t.Fatalf("embedded $: got %q err %v", got, err)
	}
}

func TestProviderHoldsReferenceNotValue(t *testing.T) {
	t.Setenv("WHIP_SECRET_TEST", "resolved-value")

	p := Provider{Name: "test", BaseURL: "https://other.example.com", APIKey: "${WHIP_SECRET_TEST}"}
	if p.APIKey != "${WHIP_SECRET_TEST}" {
		t.Fatalf("config must hold the raw reference, got %q", p.APIKey)
	}
	k, err := p.ResolveKey()
	if err != nil || k != "resolved-value" {
		t.Fatalf("ResolveKey: got %q err %v", k, err)
	}
	// Key() degrades unresolvable references to "" (missing-key path).
	p.APIKey = "$WHIP_SECRET_UNSET"
	if k := p.Key(); k != "" {
		t.Fatalf("unset ref should yield empty key, got %q", k)
	}
	if _, err := p.ResolveKey(); err == nil || !strings.Contains(err.Error(), "WHIP_SECRET_UNSET") {
		t.Fatalf("ResolveKey should name the unset var: %v", err)
	}
	// apiKeyEnv still wins over an apiKey reference.
	p.APIKeyEnv = "WHIP_SECRET_TEST"
	if k, _ := p.ResolveKey(); k != "resolved-value" {
		t.Fatalf("apiKeyEnv precedence: got %q", k)
	}
}

func TestExpandTemplate(t *testing.T) {
	t.Setenv("WHIP_TMPL_KEY", "tok")
	t.Setenv("WHIP_TMPL_EMPTY", "")
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"no refs", "plain literal", "plain literal", false},
		{"bare ref embedded", "Bearer $WHIP_TMPL_KEY", "Bearer tok", false},
		{"braced ref embedded", "Bearer ${WHIP_TMPL_KEY}!", "Bearer tok!", false},
		{"default on unset", "${WHIP_TMPL_UNSET:-fallback}", "fallback", false},
		{"default on empty", "${WHIP_TMPL_EMPTY:-fallback}", "fallback", false},
		{"dash default keeps set-empty", "${WHIP_TMPL_EMPTY-fallback}", "", false},
		{"dash default on unset", "${WHIP_TMPL_UNSET-fallback}", "fallback", false},
		{"set var ignores default", "${WHIP_TMPL_KEY:-fallback}", "tok", false},
		{"dollar digits literal", "price is $5", "price is $5", false},
		{"dollar dollar escape", "pa$$word", "pa$word", false},
		{"trailing dollar literal", "ends with $", "ends with $", false},
		{"unset without default errors", "Bearer $WHIP_TMPL_UNSET", "", true},
		{"unterminated brace errors", "Bearer ${WHIP_TMPL_KEY", "", true},
		{"nested default ref", "${WHIP_TMPL_UNSET:-$WHIP_TMPL_KEY}", "tok", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExpandTemplate(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ExpandTemplate(%q): expected error, got %q", tt.in, got)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("ExpandTemplate(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
			}
		})
	}
}

func TestResolveEnvMap(t *testing.T) {
	t.Setenv("WHIP_ENV_KEY", "resolved")
	env, err := ResolveEnvMap(map[string]string{
		"SET":       "$WHIP_ENV_KEY",     // whole ref: resolves
		"MISSING":   "$WHIP_ENV_UNSET",   // whole ref to nothing: dropped, never "MISSING="
		"TEMPLATE":  "x-$WHIP_ENV_KEY-y", // template: expands in place
		"LITERAL":   "no refs here",      // untouched
		"DOLLARCAD": "pa$$word",          // escaped literal survives
		"CMDFAIL":   "!exit 1",           // failing command: dropped
	})
	if err != nil {
		t.Fatal(err)
	}
	if env["SET"] != "resolved" || env["TEMPLATE"] != "x-resolved-y" || env["LITERAL"] != "no refs here" || env["DOLLARCAD"] != "pa$word" {
		t.Errorf("resolved env wrong: %v", env)
	}
	if _, ok := env["MISSING"]; ok {
		t.Error("unresolvable whole-ref must be dropped, not spawned as KEY=")
	}
	if _, ok := env["CMDFAIL"]; ok {
		t.Error("failing !cmd must be dropped")
	}
	if got, err := ResolveEnvMap(nil); err != nil || got != nil {
		t.Errorf("nil in, nil out: %v %v", got, err)
	}
}

func TestResolveHeader(t *testing.T) {
	t.Setenv("WHIP_HDR_TOKEN", "bearer-tok")
	if got, err := ResolveHeader("Bearer $WHIP_HDR_TOKEN"); err != nil || got != "Bearer bearer-tok" {
		t.Errorf("template header = %q, %v", got, err)
	}
	if got, err := ResolveHeader("$WHIP_HDR_TOKEN"); err != nil || got != "bearer-tok" {
		t.Errorf("whole-ref header = %q, %v", got, err)
	}
	// an unresolvable template errors so the caller drops the header instead
	// of sending "Bearer " upstream
	if _, err := ResolveHeader("Bearer $WHIP_HDR_UNSET"); err == nil {
		t.Error("unset template ref must error")
	}
	// a literal with $ stays literal
	if got, err := ResolveHeader("pa$$word"); err != nil || got != "pa$word" {
		t.Errorf("literal = %q, %v", got, err)
	}
}
