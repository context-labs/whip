package instruction

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/session"
)

func workspace(t *testing.T) (string, *os.Root) {
	t.Helper()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return dir, root
}

func writeFile(t *testing.T, dir, path, text string) {
	t.Helper()
	path = filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadCapturesOnlyAuthorizedRulesAndSkillMetadata(t *testing.T) {
	dir, root := workspace(t)
	writeFile(t, dir, "AGENTS.md", "project rules π")
	writeFile(t, dir, "docs/RULES.md", "nested rules")
	visible := "---\nname: visible\ndescription: >-\n  catalog description\n  second line\n---\n"
	disabled := "---\ndescription: |\n  hidden description\ndisable-model-invocation: true\n---\n"
	writeFile(t, dir, ".agents/skills/z-visible/SKILL.md", visible+strings.Repeat("PRIVATE_BODY\xff", 100000))
	writeFile(t, dir, ".agents/skills/a-disabled/SKILL.md", disabled+"NEVER_IN_PROMPT")
	writeFile(t, dir, ".agents/skills/ignored.md", "not an immediate skill directory")
	policy := session.Instructions{Text: "captured instructions", ProjectFiles: []string{"AGENTS.md", "missing.md", "docs/RULES.md"}, DiscoverSkills: true}
	snapshot, err := Load(t.Context(), []Root{{FS: root}}, policy, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"captured instructions", "project rules π", "nested rules", "catalog description second line", "<location>.agents/skills/z-visible/SKILL.md</location>"} {
		if !strings.Contains(snapshot.Text, want) {
			t.Fatalf("missing %q in instructions", want)
		}
	}
	for _, absent := range []string{dir, "PRIVATE_BODY", "hidden description", "NEVER_IN_PROMPT"} {
		if strings.Contains(snapshot.Text, absent) {
			t.Fatalf("instructions exposed %q", absent)
		}
	}
	wantPaths := []string{"AGENTS.md", "docs/RULES.md", ".agents/skills/a-disabled/SKILL.md", ".agents/skills/z-visible/SKILL.md"}
	wantBodies := []string{"project rules π", "nested rules", disabled, visible}
	if len(snapshot.Sources) != len(wantPaths) {
		t.Fatalf("sources=%+v", snapshot.Sources)
	}
	for i, source := range snapshot.Sources {
		hash := sha256.Sum256([]byte(wantBodies[i]))
		if source.Path != wantPaths[i] || source.Bytes != int64(len(wantBodies[i])) || source.SHA256 != hex.EncodeToString(hash[:]) || source.Validate() != nil {
			t.Fatalf("incorrect source evidence: %+v", source)
		}
	}
	again, err := Load(t.Context(), []Root{{FS: root}}, policy, nil)
	if err != nil || !reflect.DeepEqual(snapshot, again) {
		t.Fatalf("assembly was nondeterministic: %v", err)
	}
	writeFile(t, dir, "AGENTS.md", "changed next turn")
	next, err := Load(t.Context(), []Root{{FS: root}}, policy, nil)
	if err != nil || next.Sources[0].SHA256 == snapshot.Sources[0].SHA256 || strings.Contains(snapshot.Text, "changed next turn") {
		t.Fatal("refresh mutated previous snapshot or retained stale file")
	}
}

func TestLoadWithoutFilesystemAuthorityDoesNotProbeSources(t *testing.T) {
	policy := session.Instructions{Text: "only captured text", ProjectFiles: []string{"AGENTS.md"}, DiscoverSkills: true}
	snapshot, err := Load(t.Context(), nil, policy, nil)
	if err != nil || snapshot.Text != policy.Text || len(snapshot.Sources) != 0 {
		t.Fatalf("source-free snapshot=%+v err=%v", snapshot, err)
	}
	_, root := workspace(t)
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	// No configured sources means even a closed root need not be consulted.
	snapshot, err = Load(t.Context(), []Root{{FS: root}}, session.Instructions{Text: "static"}, nil)
	if err != nil || snapshot.Text != "static" || len(snapshot.Sources) != 0 {
		t.Fatalf("static policy probed root: %+v %v", snapshot, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Load(ctx, nil, policy, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled load=%v", err)
	}
}

func TestLoadRejectsInvalidPresentSources(t *testing.T) {
	for _, kind := range []string{"directory", "fifo", "invalid UTF8", "NUL", "oversize", "dangling", "outside symlink", "outside directory", "unreadable"} {
		t.Run(kind, func(t *testing.T) {
			dir, root := workspace(t)
			outside := t.TempDir()
			writeFile(t, outside, "AGENTS.md", "OUTSIDE_SECRET")
			path := filepath.Join(dir, "AGENTS.md")
			var err error
			switch kind {
			case "directory":
				err = os.Mkdir(path, 0o700)
			case "fifo":
				err = syscall.Mkfifo(path, 0o600)
			case "invalid UTF8":
				writeFile(t, dir, "AGENTS.md", "\xff")
			case "NUL":
				writeFile(t, dir, "AGENTS.md", "\x00")
			case "oversize":
				writeFile(t, dir, "AGENTS.md", strings.Repeat("x", maxSourceBytes+1))
			case "dangling":
				err = os.Symlink("missing", path)
			case "outside symlink":
				err = os.Symlink(filepath.Join(outside, "AGENTS.md"), path)
			case "outside directory":
				err = os.Symlink(outside, filepath.Join(dir, "escape"))
				path = filepath.Join(dir, "escape", "AGENTS.md")
			case "unreadable":
				if os.Geteuid() == 0 {
					t.Skip("root bypasses file permission bits")
				}
				writeFile(t, dir, "AGENTS.md", "unreadable")
				err = os.Chmod(path, 0)
			}
			if err != nil {
				t.Fatal(err)
			}
			relative, err := filepath.Rel(dir, path)
			if err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() {
				_, err := Load(t.Context(), []Root{{FS: root}}, session.Instructions{ProjectFiles: []string{relative}}, nil)
				result <- err
			}()
			select {
			case err := <-result:
				if err == nil {
					t.Fatal("invalid present source accepted")
				}
			case <-time.After(time.Second):
				t.Fatal("source open blocked before descriptor validation")
			}
		})
	}
}

func TestLoadRootDescriptorSurvivesDirectoryRetarget(t *testing.T) {
	dir, root := workspace(t)
	writeFile(t, dir, "AGENTS.md", "original workspace")
	out := t.TempDir()
	writeFile(t, out, "AGENTS.md", "OUTSIDE_SECRET")
	moved := dir + "-moved"
	if err := os.Rename(dir, moved); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(moved) })
	if err := os.Symlink(out, dir); err != nil {
		t.Fatal(err)
	}
	snapshot, err := Load(t.Context(), []Root{{FS: root}}, session.Instructions{ProjectFiles: []string{"AGENTS.md"}}, nil)
	if err != nil || !strings.Contains(snapshot.Text, "original workspace") || strings.Contains(snapshot.Text, "OUTSIDE_SECRET") {
		t.Fatalf("root path retarget escaped descriptor: %+v %v", snapshot, err)
	}
}

func TestLoadSymlinkRetargetCannotEscapeRoot(t *testing.T) {
	dir, root := workspace(t)
	writeFile(t, dir, "inside.md", "inside workspace")
	out := t.TempDir()
	writeFile(t, out, "outside.md", "OUTSIDE_SECRET")
	if err := os.Symlink("inside.md", filepath.Join(dir, "rules.md")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	var writers sync.WaitGroup
	writers.Go(func() {
		for i := 0; ctx.Err() == nil; i++ {
			target := "inside.md"
			if i%2 == 0 {
				target = filepath.Join(out, "outside.md")
			}
			temporary := filepath.Join(dir, "swap")
			if os.Symlink(target, temporary) != nil {
				return
			}
			if os.Rename(temporary, filepath.Join(dir, "rules.md")) != nil {
				return
			}
		}
	})
	defer func() { cancel(); writers.Wait() }()
	for range 100 {
		snapshot, err := Load(t.Context(), []Root{{FS: root}}, session.Instructions{ProjectFiles: []string{"rules.md"}}, nil)
		if err == nil && (!strings.Contains(snapshot.Text, "inside workspace") || strings.Contains(snapshot.Text, "OUTSIDE_SECRET")) {
			t.Fatalf("retarget escaped root: %+v", snapshot)
		}
	}
}

func TestLoadBoundsIncludeFramingAndOptionalDiscovery(t *testing.T) {
	dir, root := workspace(t)
	writeFile(t, dir, "AGENTS.md", strings.Repeat("x", maxSourceBytes))
	if _, err := Load(t.Context(), []Root{{FS: root}}, session.Instructions{ProjectFiles: []string{"AGENTS.md"}}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(t.Context(), []Root{{FS: root}}, session.Instructions{Text: strings.Repeat("s", session.MaxInstructionBytes-maxSourceBytes), ProjectFiles: []string{"AGENTS.md"}}, nil); err == nil {
		t.Fatal("composed bound omitted project framing")
	}
	if _, err := Load(t.Context(), nil, session.Instructions{Text: strings.Repeat("s", session.MaxInstructionBytes)}, nil); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, ".agents/skills/broken/SKILL.md", "bad frontmatter")
	if _, err := Load(t.Context(), []Root{{FS: root}}, session.Instructions{}, nil); err != nil {
		t.Fatal("disabled discovery read malformed skill", err)
	}
	if _, err := Load(t.Context(), []Root{{FS: root}}, session.Instructions{DiscoverSkills: true}, nil); err == nil {
		t.Fatal("malformed metadata silently omitted")
	}
}

func TestLoadSkillBoundsAndNonregularMetadata(t *testing.T) {
	for _, kind := range []string{"metadata bytes", "skill count", "entry count", "catalog bytes", "fifo", "directory fifo"} {
		t.Run(kind, func(t *testing.T) {
			dir, root := workspace(t)
			skillDir := filepath.Join(dir, skillDirectory)
			if err := os.MkdirAll(skillDir, 0o700); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "metadata bytes":
				writeFile(t, dir, ".agents/skills/one/SKILL.md", "---\n#"+strings.Repeat("x", maxSourceBytes)+"\n---\n")
			case "skill count":
				for i := range maxSkills + 1 {
					writeFile(t, dir, fmt.Sprintf(".agents/skills/skill-%d/SKILL.md", i), "---\ndescription: okay\n---\n")
				}
			case "entry count":
				for i := range maxEntries + 1 {
					writeFile(t, dir, fmt.Sprintf(".agents/skills/entry-%d", i), "")
				}
			case "catalog bytes":
				for i := range 210 {
					writeFile(t, dir, fmt.Sprintf(".agents/skills/skill-%d/SKILL.md", i), "---\ndescription: '"+strings.Repeat("&", 1024)+"'\n---\n")
				}
			case "fifo":
				if err := os.Mkdir(filepath.Join(skillDir, "one"), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := syscall.Mkfifo(filepath.Join(skillDir, "one", "SKILL.md"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "directory fifo":
				if err := os.Remove(skillDir); err != nil {
					t.Fatal(err)
				}
				if err := syscall.Mkfifo(skillDir, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := Load(t.Context(), []Root{{FS: root}}, session.Instructions{DiscoverSkills: true}, nil); err == nil {
				t.Fatal("invalid or excessive catalog accepted")
			}
		})
	}
}

func TestMetadataReadConsumesOnlyFrontmatter(t *testing.T) {
	frontmatter := "---\nname: complete\ndescription: metadata\n---\n"
	data, err := readMetadata(t.Context(), bytes.NewBufferString(frontmatter+strings.Repeat("body", 100000)))
	if err != nil || string(data) != frontmatter {
		t.Fatalf("metadata=%q err=%v", data, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := readMetadata(ctx, strings.NewReader(frontmatter)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled metadata read=%v", err)
	}
}

func TestProjectReadRejectsSizeChangesAfterDescriptorValidation(t *testing.T) {
	for _, change := range []string{"truncate", "grow", "short read"} {
		t.Run(change, func(t *testing.T) {
			dir, root := workspace(t)
			writeFile(t, dir, "AGENTS.md", "complete original instructions")
			file, err := root.OpenFile("AGENTS.md", os.O_RDONLY|syscall.O_NONBLOCK, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			before, err := file.Stat()
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "truncate":
				writeFile(t, dir, "AGENTS.md", "short")
			case "grow":
				writeFile(t, dir, "AGENTS.md", "complete original instructions with more data")
			case "short read":
				// Even unchanged descriptor size cannot authorize partial bytes.
				if _, err := file.Seek(1, 0); err != nil {
					t.Fatal(err)
				}
			}
			data, err := readComplete(file, before, maxSourceBytes)
			if err == nil || data != nil || !strings.Contains(err.Error(), "size changed") {
				t.Fatalf("changed read was labelled complete: bytes=%q err=%v", data, err)
			}
		})
	}
}
