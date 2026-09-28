package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestStateJSONPreservesNumbersAndRejectsInvalidUnicodeAndDepth(t *testing.T) {
	for _, raw := range []string{`{"integer":9007199254740993123456789,"exponent":1e999,"negative":-0}`, `"🌏\ud83c\udf0f"`, `"\\ud800"`, strings.Repeat("[", 100) + "0" + strings.Repeat("]", 100)} {
		if err := ValidateStateJSON([]byte(raw)); err != nil {
			t.Fatalf("valid JSON %s: %v", raw, err)
		}
	}
	for _, raw := range []string{`"\ud800"`, `"\udfff"`, `"\ud800\u0000"`, `{"\ud800":true}`, `"\ud800x"`, `NaN`, "\"\xff\"", strings.Repeat("[", 101) + "0" + strings.Repeat("]", 101)} {
		if err := ValidateStateJSON([]byte(raw)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid JSON accepted: %q %v", raw, err)
		}
	}
}

func TestAppendStateJSONPreservesValuesAndBounds(t *testing.T) {
	for _, test := range []struct{ left, right, want string }{
		{` [ 9007199254740993123456789, -0 ] `, `[1e999,{"x":[]} ]`, `[9007199254740993123456789, -0,1e999,{"x":[]}]`},
		{`[]`, `[]`, `[]`},
		{`[]`, `[true]`, `[true]`},
		{`[false]`, `[]`, `[false]`},
		{`"a\n"`, `"\ud83c\udf0f\\"`, `"a\n\ud83c\udf0f\\"`},
	} {
		got, err := AppendStateJSON([]byte(test.left), []byte(test.right))
		if err != nil || string(got) != test.want {
			t.Fatalf("append %s + %s = %s, %v", test.left, test.right, got, err)
		}
	}
	for _, pair := range [][2]string{{`null`, `[]`}, {`[1]`, `"x"`}, {`"x"`, `[1]`}} {
		if _, err := AppendStateJSON([]byte(pair[0]), []byte(pair[1])); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid append", err)
		}
	}
	maximum := append([]byte{'"'}, bytes.Repeat([]byte{'a'}, MaxStateValueBytes-2)...)
	maximum = append(maximum, '"')
	if err := ValidateStateJSON(maximum); err != nil {
		t.Fatal(err)
	}
	if _, err := AppendStateJSON(maximum, []byte(`"x"`)); !errors.Is(err, ErrInvalid) {
		t.Fatal("oversized append accepted", err)
	}
	if !json.Valid(maximum) {
		t.Fatal("test JSON malformed")
	}
}

func FuzzStateJSON(f *testing.F) {
	for _, seed := range []string{`null`, `"\ud800"`, `"\ud83c\udf0f"`, `["a\\\"",9007199254740993]`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if err := ValidateStateJSON(data); err == nil && !json.Valid(data) {
			t.Fatal("invalid JSON accepted")
		}
	})
}
