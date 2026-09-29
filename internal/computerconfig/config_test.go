package computerconfig

import (
	"strings"
	"testing"
)

func TestPolicyAvailabilityDoesNotInventConsentOrOverrideDeny(t *testing.T) {
	c, err := (Config{Enabled: true, DefaultDeny: true, Allow: []string{" TEXTEDIT "}, Deny: []string{"com.blocked.App"}}).Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if c.NeedsConsent([]string{"textedit"}) || !c.NeedsConsent([]string{"unlisted"}) {
		t.Fatal("availability eligibility changed")
	}
	if c.Check("unlisted") != nil {
		t.Fatal("unlisted app cannot receive one-off consent")
	}
	if c.Check("alias", "com.blocked.App") == nil {
		t.Fatal("alias bypassed hard denial")
	}
	c.Allow[0] = "com.blocked.app"
	if c.Check("com.blocked.app") == nil {
		t.Fatal("allow bypassed hard denial")
	}
	if (Config{}).Check("textedit") == nil {
		t.Fatal("zero policy enabled control")
	}
}

func TestComputerConfigBoundsAndCanonicalOwnership(t *testing.T) {
	allow := []string{"A"}
	source := Config{Allow: allow}
	normalized, err := source.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	allow[0] = "B"
	if normalized.Allow[0] != "a" {
		t.Fatal("aliased config")
	}
	for _, c := range []Config{{Allow: []string{"a", " A "}}, {Deny: []string{"a\u202e"}}, {HelperExecutable: "relative"}, {HelperExecutable: "/a/../b"}, {Allow: make([]string, 65)}, {Allow: []string{strings.Repeat("a", 257)}}} {
		if _, err := c.Normalize(); err == nil {
			t.Fatal("accepted invalid computer config", c)
		}
	}
}
