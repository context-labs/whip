package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

func TestStandingInstructionConfigurationFailuresDoNotDispatch(t *testing.T) {
	for _, mode := range []string{"unconfigured", "missing file", "missing directory"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			r := openTest(t, t.TempDir(), providerFunc(func(context.Context, model.Request) (model.Response, error) {
				calls.Add(1)
				return model.Response{Parts: []session.Part{{Type: "text", Text: "unexpected"}}}, nil
			}))
			owner := createTest(t, r)
			policy := session.Instructions{Text: "configured", StandingInstructions: true}
			if _, err := r.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Instructions: &policy}); err != nil {
				t.Fatal(err)
			}
			path := ""
			if mode != "unconfigured" {
				parent := t.TempDir()
				if mode == "missing directory" {
					parent = filepath.Join(parent, "private-absent-directory")
				}
				path = filepath.Join(parent, "private-standing.md")
				r.host.StandingInstructionsFile = path
				if _, err := r.CreateGrant(t.Context(), session.Grant{ID: "standing-read", SessionID: owner.ID, Capability: "instructions.read", Resource: "standing"}); err != nil {
					t.Fatal(err)
				}
			}
			if err := r.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			submitTest(t, r, owner.ID, "standing-failure")
			done := waitTest(t, r, "standing-failure", terminal)
			attempts, err := r.ModelAttempts(t.Context(), done.Turn.ID, "", 100)
			if err != nil || done.Turn.State != session.Failed || done.Turn.Failure == nil || calls.Load() != 0 || len(attempts) != 0 {
				t.Fatalf("standing failure dispatched work: turn=%+v calls=%d attempts=%+v err=%v", done.Turn, calls.Load(), attempts, err)
			}
			manifest, err := r.InstructionManifest(t.Context(), done.Turn.ID)
			if err != nil || manifest != nil {
				t.Fatalf("failed capture retained a manifest: %+v %v", manifest, err)
			}
			if path != "" {
				if strings.Contains(*done.Turn.Failure, filepath.Dir(path)) {
					t.Fatalf("failure exposed host path: %s", *done.Turn.Failure)
				}
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("missing standing file was seeded: %v", err)
				}
				if mode == "missing directory" {
					if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("missing standing directory was created: %v", err)
					}
				}
			}
			if err := r.Err(); err != nil {
				t.Fatal("capture failure faulted the runtime", err)
			}
		})
	}
}

func TestStandingInstructionMissingAuthorityNeverProbesSource(t *testing.T) {
	for _, mode := range []string{"no grant", "workspace grant", "wrong standing resource", "missing directory"} {
		t.Run(mode, func(t *testing.T) {
			requests := make(chan model.Request, 1)
			r := openTest(t, t.TempDir(), providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
				requests <- request
				return model.Response{Parts: []session.Part{{Type: "text", Text: "done"}}}, nil
			}))
			owner := createTest(t, r)
			policy := session.Instructions{Text: "configured literal", StandingInstructions: true}
			if _, err := r.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Instructions: &policy}); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "standing.md")
			if mode == "missing directory" {
				path = filepath.Join(t.TempDir(), "absent", "standing.md")
			} else {
				writeInstructionFile(t, path, string([]byte{0xff, 0}))
			}
			r.host.StandingInstructionsFile = path
			if mode == "workspace grant" || mode == "wrong standing resource" {
				capability, resource := "files.read", owner.WorkingDirectory
				if mode == "wrong standing resource" {
					capability, resource = "instructions.read", owner.WorkingDirectory
				}
				if _, err := r.CreateGrant(t.Context(), session.Grant{ID: "wrong-read", SessionID: owner.ID, Capability: capability, Resource: resource}); err != nil {
					t.Fatal(err)
				}
			}
			if err := r.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			submitTest(t, r, owner.ID, "ungranted-standing")
			done := waitTest(t, r, "ungranted-standing", terminal)
			if done.Turn.State != session.Succeeded {
				t.Fatalf("ungranted source was probed: %+v", done.Turn)
			}
			request := nextInstructionRequest(t, requests)
			if !strings.HasPrefix(request.Instructions, policy.Text) || strings.Contains(request.Instructions, "--- Standing user instructions ---") || strings.Contains(request.Instructions, path) {
				t.Fatal("ungranted source changed model instructions")
			}
			manifest, err := r.InstructionManifest(t.Context(), done.Turn.ID)
			if err != nil || manifest == nil || len(manifest.Sources) != 0 {
				t.Fatalf("ungranted source left audit evidence: %+v %v", manifest, err)
			}
			attempts, err := r.ModelAttempts(t.Context(), done.Turn.ID, "", 100)
			if err != nil || len(attempts) != 1 {
				t.Fatalf("ordinary turn did not complete once: %+v %v", attempts, err)
			}
			permissions, err := r.Permissions(t.Context(), owner.ID, "", 100)
			if err != nil || len(permissions) != 0 {
				t.Fatalf("automatic capture created a permission prompt: %+v %v", permissions, err)
			}
		})
	}
}

func TestStandingInstructionCaptureAuditsRawFileAndComposesFilteredText(t *testing.T) {
	for _, tc := range []struct{ name, raw, filtered string }{
		{"rules", "# private comment\r\n\r\n  Prefer exact evidence.  \r\n\t# another comment\n  Keep Unicode café.\t\n", "Prefer exact evidence.\nKeep Unicode café."},
		{"comments only", "# private comment\n  # another comment\r\n\n\t\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := make(chan model.Request, 1)
			r := openTest(t, t.TempDir(), providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
				requests <- request
				return model.Response{Parts: []session.Part{{Type: "text", Text: "done"}}}, nil
			}))
			owner := createTest(t, r)
			policy := session.Instructions{Text: "configured literal", StandingInstructions: true}
			if _, err := r.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Instructions: &policy}); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "operator-standing.md")
			writeInstructionFile(t, path, tc.raw)
			r.host.StandingInstructionsFile = path
			if _, err := r.CreateGrant(t.Context(), session.Grant{ID: "standing-read", SessionID: owner.ID, Capability: "instructions.read", Resource: "standing"}); err != nil {
				t.Fatal(err)
			}
			if err := r.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			submitTest(t, r, owner.ID, "captured-standing")
			done := waitTest(t, r, "captured-standing", terminal)
			if done.Turn.State != session.Succeeded {
				t.Fatalf("standing capture failed: %+v", done.Turn)
			}
			request := nextInstructionRequest(t, requests)
			want := policy.Text
			if tc.filtered != "" {
				want += "\n\n--- Standing user instructions ---\n" + tc.filtered
			}
			if !strings.HasPrefix(request.Instructions, want+"\nThe execute tool runs ") || strings.Contains(request.Instructions, "private comment") || strings.Contains(request.Instructions, path) {
				t.Fatal("standing composition lost filtering, ordering or host path privacy")
			}
			manifest, err := r.InstructionManifest(t.Context(), done.Turn.ID)
			if err != nil || manifest == nil || len(manifest.Sources) != 1 {
				t.Fatalf("standing capture lost raw audit: %+v %v", manifest, err)
			}
			source := manifest.Sources[0]
			rawDigest := sha256.Sum256([]byte(tc.raw))
			if source.Kind != "standing_instructions" || source.Scope != "host" || source.RootID == nil || *source.RootID != "standing" || source.Path != filepath.Base(path) || source.Bytes != int64(len(tc.raw)) || source.SHA256 != hex.EncodeToString(rawDigest[:]) {
				t.Fatalf("standing source did not audit the exact raw file: %+v", source)
			}
			composedDigest := sha256.Sum256([]byte(request.Instructions))
			if manifest.Bytes != int64(len(request.Instructions)) || manifest.SHA256 != hex.EncodeToString(composedDigest[:]) {
				t.Fatal("manifest digest does not identify provider instructions")
			}
			persisted, err := os.ReadFile(path)
			if err != nil || string(persisted) != tc.raw {
				t.Fatalf("capture changed the operator file: %v", err)
			}
		})
	}
}
