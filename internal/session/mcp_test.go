package session

import (
	"errors"
	"testing"
)

func TestMCPSelectionInheritanceAndImmutableCeiling(t *testing.T) {
	base := Configuration{Model: ModelSelection{Provider: "test", Name: "model"}}
	all, err := Resolve(base, DefinitionDocument{}, ConfigPatch{})
	if err != nil || all.MCPServers == nil || !all.MCPServers.All {
		t.Fatal(all, err)
	}
	selection := &MCPSelection{Servers: []string{"example.with.dots", "second"}}
	root, err := Resolve(all, DefinitionDocument{}, ConfigPatch{MCPServers: selection})
	if err != nil {
		t.Fatal(err)
	}
	selection.Servers[0] = "tampered"
	if root.MCPServers.Servers[0] != "example.with.dots" {
		t.Fatal("selection aliases caller")
	}
	child, err := Resolve(root, DefinitionDocument{}, ConfigPatch{MCPServers: &MCPSelection{}})
	if err != nil || child.MCPServers.All || child.MCPServers.Allowed() == nil || len(child.MCPServers.Allowed()) != 0 {
		t.Fatal("none inherited", child, err)
	}
	if err := NarrowBindings(root, child); err != nil {
		t.Fatal(err)
	}
	if err := UpdateBindings(root, root.Clone()); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []*MCPSelection{nil, {All: true}, {Servers: []string{"future"}}} {
		changed := root.Clone()
		changed.MCPServers = candidate
		if !errors.Is(UpdateBindings(root, changed), ErrInvalid) {
			t.Fatal("ceiling broadened", candidate)
		}
	}
	for _, candidate := range []MCPSelection{{All: true, Servers: []string{"one"}}, {Servers: []string{"one", "one"}}, {Servers: []string{"\x00"}}} {
		if !errors.Is(candidate.Validate(), ErrInvalid) {
			t.Fatal("invalid selection admitted", candidate)
		}
	}
}
