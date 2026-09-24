package computer

import (
	"reflect"
	"testing"
)

func TestPolicyStateIsSortedAndIndependent(t *testing.T) {
	policy := NewPolicy([]string{"Safari", "Chrome"}, []string{"Terminal"}, true)
	policy.Approve("Preview")
	policy.Deny("Finder")
	state := policy.State()
	if !state.DefaultDeny || !reflect.DeepEqual(state.Allowed, []string{"chrome", "safari"}) || !reflect.DeepEqual(state.SessionDenied, []string{"finder"}) {
		t.Fatalf("state=%+v", state)
	}
	state.Allowed[0] = "changed"
	if err := policy.Check("Chrome"); err != nil {
		t.Fatal(err)
	}
	if policy.State().Allowed[0] != "chrome" {
		t.Fatal("snapshot mutated policy")
	}
}
