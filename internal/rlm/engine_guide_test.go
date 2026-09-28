package rlm

import (
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/engine/process"
)

func TestEngineDescriptorsAndJavaScriptGuide(t *testing.T) {
	descriptor, err := ResolveEngine(process.EngineQuickJS)
	if err != nil || descriptor.Language != "javascript" || len(descriptor.Build) != 64 || len(descriptor.GuideSHA256) != 64 || len(descriptor.BridgeSHA256) != 64 {
		t.Fatalf("descriptor=%+v %v", descriptor, err)
	}
	if _, err := ResolveEngine("node"); err == nil {
		t.Fatal("unbundled engine accepted")
	}
	guide, err := RuntimeGuide(process.EngineQuickJS, ModuleNames(), nil, nil, "/workspace", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"await files.read({path:", "BigInt", "Proxy construction is disabled", "complete JavaScript heap", "100,000", "40 MiB"} {
		if !strings.Contains(guide, want) {
			t.Fatalf("guide missing %q", want)
		}
	}
	for _, bad := range []string{"bounded Starlark runtime", "accept keyword arguments only", "context.history(seq=", "include_grants=True"} {
		if strings.Contains(guide, bad) {
			t.Fatalf("JavaScript guide retains %q", bad)
		}
	}
}
