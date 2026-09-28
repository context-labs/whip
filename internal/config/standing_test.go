package config

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestStandingInstructionFileExplicitRoundTripAndBounds(t *testing.T) {
	directory := t.TempDir()
	host := Default()
	if host.StandingInstructionsFile != "" {
		t.Fatal("fresh host invents standing file")
	}
	host.StandingInstructionsFile = filepath.Join(directory, "not-created", "me.md")
	host.Defaults.Instructions.StandingInstructions = true
	if err := Save(directory, host); err != nil {
		t.Fatal("publication must not inspect filesystem", err)
	}
	loaded, err := Load(directory)
	if err != nil || loaded.Version != Version || loaded.StandingInstructionsFile != host.StandingInstructionsFile || !loaded.Defaults.Instructions.StandingInstructions {
		t.Fatalf("standing config roundtrip=%+v %v", loaded, err)
	}
	for _, valid := range []string{"", directory, "/個人/me.md", "/" + strings.Repeat("a", 4095)} {
		host.StandingInstructionsFile = valid
		if err := host.Validate(); err != nil {
			t.Fatalf("syntactically valid path %q rejected: %v", valid, err)
		}
	}
	for _, bad := range []string{"/", " ", "me.md", "../me.md", "/a/../me.md", "/a//me.md", "/a/", `/tmp/me\rules.md`, "/bad\x00.md", "/bad\xff.md", "/" + strings.Repeat("a", 4096)} {
		host.StandingInstructionsFile = bad
		if err := host.Validate(); !errors.Is(err, session.ErrInvalid) {
			t.Fatalf("invalid standing path %q accepted: %v", bad, err)
		}
	}
}
