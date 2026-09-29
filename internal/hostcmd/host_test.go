package hostcmd

import (
	"bytes"
	"testing"
)

func TestExplicitRuntimeSelection(t *testing.T) {
	for _, args := range [][]string{nil, {"-directory", t.TempDir(), "-web-listen", "127.0.0.1:0"}, {"-directory", t.TempDir(), "-web-terminals"}, {"-scripted"}, {"-directory", t.TempDir(), "-scripted-delay", "1s"}, {"-directory", t.TempDir(), "-scripted", "-scripted-delay", "-1s"}} {
		var out, diagnostics bytes.Buffer
		if err := Run(t.Context(), args, &out, &diagnostics); err == nil {
			t.Fatalf("accepted missing/invalid runtime selection: %v", args)
		}
		if out.Len() != 0 {
			t.Fatal("announced readiness for invalid invocation")
		}
	}
	var out, diagnostics bytes.Buffer
	if err := Run(t.Context(), []string{"-help"}, &out, &diagnostics); err != nil {
		t.Fatal(err)
	}
}
