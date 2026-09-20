package tools

import (
	"context"
	"strings"
	"testing"
)

func TestContextCheckGatePreservesCallingAgentPolicy(t *testing.T) {
	if got := CheckGate(t.Context(), "custom", "echo safe"); got != "" {
		t.Fatalf("embedded caller without services: %s", got)
	}
	for _, tc := range []struct {
		name     string
		gate     Gate
		headless bool
		want     string
	}{
		{"embedded", nil, false, ""},
		{"headless", nil, true, "headless execution"},
		{"rejected", func(context.Context, GateRequest) (GateDecision, string) { return GateReject, "" }, false, "the user rejected"},
		{"redirected", func(context.Context, GateRequest) (GateDecision, string) { return GateReject, "use read-only mode" }, false, "use read-only mode"},
		{"approved", func(context.Context, GateRequest) (GateDecision, string) { return GateAllowOnce, "" }, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			services := NewServices()
			services.SetGate(tc.gate)
			services.headlessPermissions = tc.headless
			got := CheckGate(WithServices(t.Context(), services), "custom", "echo safe")
			if tc.want == "" && got != "" || tc.want != "" && !strings.Contains(got, tc.want) {
				t.Fatalf("policy result = %q, want %q", got, tc.want)
			}
		})
	}
}
