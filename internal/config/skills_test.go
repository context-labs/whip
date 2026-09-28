package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestHostSkillRootsExplicitValidationAndRoundTrip(t *testing.T) {
	directory := t.TempDir()
	host := Default()
	host.SkillRoots = map[string]string{"missing": filepath.Join(directory, "not-created"), "team": filepath.Join(directory, "regular-file")}
	if err := os.WriteFile(host.SkillRoots["team"], []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	host.Defaults.Instructions.SkillRoots = []string{"team", "missing"}
	// Registry validation is purely syntactic. Opening and checking the declared
	// directory belongs to the authorized runtime reader, never config loading.
	if err := Save(directory, host); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(directory)
	if err != nil || loaded.Version != Version || !reflect.DeepEqual(loaded.SkillRoots, host.SkillRoots) || !reflect.DeepEqual(loaded.Defaults.Instructions.SkillRoots, host.Defaults.Instructions.SkillRoots) {
		t.Fatalf("host roots roundtrip=%+v %v", loaded, err)
	}
	loaded.SkillRoots["team"] = "/mutated"
	loaded.Defaults.Instructions.SkillRoots[0] = "mutated"
	again, err := Load(directory)
	if err != nil || !reflect.DeepEqual(again.SkillRoots, host.SkillRoots) || !reflect.DeepEqual(again.Defaults.Instructions.SkillRoots, host.Defaults.Instructions.SkillRoots) {
		t.Fatal("loaded config aliases prior caller", err)
	}
	maxRoots := make(map[string]string, session.MaxSkillRoots)
	for i := range session.MaxSkillRoots {
		maxRoots[fmt.Sprintf("root_%d", i)] = "/skills"
	}
	host.SkillRoots = maxRoots
	if err := host.Validate(); err != nil {
		t.Fatal("exact root count rejected", err)
	}
	host.SkillRoots["extra"] = "/skills"
	if err := host.Validate(); !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("too many roots accepted: %v", err)
	}
}

func TestHostSkillRootsRejectInvalidDeclarations(t *testing.T) {
	for _, path := range []string{"", "relative", "/a/../skills", "/a//skills", "/a/", "/bad\x00path", "/bad\xffpath", "/" + strings.Repeat("a", 4096)} {
		host := Default()
		host.SkillRoots["team"] = path
		if err := host.Validate(); !errors.Is(err, session.ErrInvalid) {
			t.Fatalf("invalid path %q accepted: %v", path, err)
		}
	}
	for _, id := range []string{"", "../root", "bad root", strings.Repeat("a", 129)} {
		host := Default()
		host.SkillRoots[id] = "/skills"
		if err := host.Validate(); !errors.Is(err, session.ErrInvalid) {
			t.Fatalf("invalid root ID %q accepted: %v", id, err)
		}
	}
	for _, version := range []int{1, 2, 3} {
		host := Default()
		host.Version = version
		if err := host.Validate(); !errors.Is(err, session.ErrInvalid) {
			t.Fatalf("old config version %d accepted: %v", version, err)
		}
	}
	host := Default()
	host.SkillRoots["unicode"] = "/é/skills"
	if err := host.Validate(); err != nil {
		t.Fatal(err)
	}
}
