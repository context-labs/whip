package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

func TestNativeSkillsImportPublishSelectAndExplicitFirstUse(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(t.TempDir())
	writeSkill(t, filepath.Join(home, ".codex", "skills"), "review", "Review carefully")
	source := filepath.Join(home, ".codex", "skills", "review", "SKILL.md")
	f, err := os.OpenFile(source, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\nONLY_AUTHORIZED_BODY\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	captureStdout(t, func() {
		if err := skillsCLI([]string{"import"}); err != nil {
			t.Fatal(err)
		}
	})
	requests := make(chan model.Request, 4)
	r := nativeRunFixture(t, func(_ context.Context, request model.Request, _ func(model.Chunk)) (model.Response, error) {
		requests <- request
		return model.Response{Parts: []session.Part{{Type: "text", Text: "done"}}}, nil
	})
	c, err := connectNativeRuntime(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	refs, err := r.Builtins()
	if err != nil {
		t.Fatal(err)
	}
	request := session.TreeCreationRequest{ID: "before-publication", Definition: refs[0], WorkingDirectory: t.TempDir(), Overrides: session.ConfigPatch{AutomaticTitle: new(false)}}
	before, err := r.CreateRoot(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"publish", "personal", filepath.Join(home, ".agents", "skills")}, {"defaults", "personal"}} {
		captureStdout(t, func() {
			if err := skillsCLI(args); err != nil {
				t.Fatal(err)
			}
		})
	}
	listing := captureStdout(t, func() {
		if err := skillsCLI([]string{"list"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(listing, "$review") || strings.Contains(listing, "ONLY_AUTHORIZED_BODY") {
		t.Fatal("metadata disclosure", listing)
	}
	retry, err := r.CreateRoot(t.Context(), request)
	if err != nil || retry.Root.ID != before.Root.ID || len(retry.Root.Config.Instructions.SkillRoots) != 0 {
		t.Fatal("creation retry recaptured defaults", retry, err)
	}
	request.ID = "after-publication"
	created, err := r.CreateRoot(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	owner := created.Root
	if !slices.Equal(owner.Config.Instructions.SkillRoots, []string{"personal"}) {
		t.Fatal("builtin did not capture default roots", owner.Config)
	}
	if grants, err := r.Grants(t.Context(), owner.ID, "", 10); err != nil || len(grants) != 0 {
		t.Fatal("setup granted access", grants, err)
	}
	run := func(id string) protocol.Admission {
		t.Helper()
		command, err := c.PrepareInput("sessions.submit", protocol.SubmitParams{Source: "user", SessionID: protocol.ID(owner.ID), Identity: protocol.RequestIdentity{ClientID: "skill-cli", RequestID: protocol.ID(id)}, Parts: []protocol.Part{{Type: "text", Text: "$review"}}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := command.Send(t.Context()); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		result, err := command.Wait(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	if unexpanded := run("unexpanded"); unexpanded.Turn == nil || unexpanded.Turn.State != "succeeded" {
		t.Fatal("literal unavailable skill should remain ordinary input", unexpanded)
	}
	select {
	case request := <-requests:
		if strings.Contains(request.Instructions, "ONLY_AUTHORIZED_BODY") || strings.Contains(request.Instructions, "Review carefully") {
			t.Fatal("ungranted skill bytes reached provider")
		}
	case <-time.After(time.Second):
		t.Fatal("missing literal prompt")
	}

	captureStdout(t, func() {
		if err := skillsCLI([]string{"allow", string(owner.ID), "personal"}); err != nil {
			t.Fatal(err)
		}
	})
	if done := run("allowed"); done.Turn == nil || done.Turn.State != "succeeded" {
		t.Fatal(done)
	}
	select {
	case request := <-requests:
		if !strings.Contains(request.Instructions, "ONLY_AUTHORIZED_BODY") {
			t.Fatal("body not captured")
		}
	case <-time.After(time.Second):
		t.Fatal("missing provider request")
	}
	captureStdout(t, func() {
		if err := skillsCLI([]string{"defaults", "--none"}); err != nil {
			t.Fatal(err)
		}
	})
	retained, err := r.Session(t.Context(), owner.ID)
	if err != nil || !slices.Equal(retained.Config.Instructions.SkillRoots, []string{"personal"}) {
		t.Fatal("existing policy changed", retained, err)
	}
	if err := skillsCLI([]string{"allow", string(before.Root.ID), "personal"}); err == nil {
		t.Fatal("granted unselected source")
	}
}
