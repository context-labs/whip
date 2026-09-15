package config

import (
	"testing"
)

func TestReadWriteJSONRoundTrip(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	type state struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	if err := WriteJSON("state.json", state{Name: "abe", Count: 3}); err != nil {
		t.Fatal(err)
	}
	var got state
	if err := ReadJSON("state.json", &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "abe" || got.Count != 3 {
		t.Fatalf("round-trip = %+v", got)
	}
}

// TestParseJSONCEdgeCases pins the stripper's tricky cases: comment markers
// and commas inside string literals must survive, escapes must not end a
// string early, and malformed sources must be reported rather than silently
// producing broken JSON.
func TestParseJSONCEdgeCases(t *testing.T) {
	ok := []struct {
		name, src, want string
	}{
		{"escaped quote", `{"a": "say \"hi\", ok"}`, `say "hi", ok`},
		{"escaped backslash", `{"a": "c:\\path\\"}`, `c:\path\`},
		{"comment marker in string", `{"a": "http://x/y"}`, "http://x/y"},
		{"division-like slash", `{"a": "1"} // 2/3`, "1"},
		{"block comment", `{/* skip */ "a": "1"}`, "1"},
		{"trailing comma", `{"a": "1",}`, "1"},
		{"comma inside string", `{"a": ", }"}`, ", }"},
	}
	for _, c := range ok {
		t.Run(c.name, func(t *testing.T) {
			var v struct {
				A string `json:"a"`
			}
			if err := ParseJSONC([]byte(c.src), &v); err != nil {
				t.Fatalf("parse %s: %v", c.src, err)
			}
			if v.A != c.want {
				t.Fatalf("got %q, want %q", v.A, c.want)
			}
		})
	}

	// a lone slash that starts neither comment form is left alone
	if got, err := stripJSONC([]byte(`{"a":"1"}/`)); err != nil || string(got) != `{"a":"1"}/` {
		t.Fatalf("bare slash: %q %v", got, err)
	}

	bad := map[string]string{
		"unterminated block comment": `{"a": 1} /* nope`,
		"unterminated string":        `{"a": "nope}`,
	}
	for name, src := range bad {
		t.Run(name, func(t *testing.T) {
			var v map[string]any
			if err := ParseJSONC([]byte(src), &v); err == nil {
				t.Fatalf("%s should be an error", name)
			}
		})
	}
}

func TestReadJSONMissingFileErrors(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	var v map[string]any
	if err := ReadJSON("nope.json", &v); err == nil {
		t.Fatal("missing file should be an error the caller treats as empty")
	}
}
