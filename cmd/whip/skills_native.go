package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

const skillsUsage = `usage: whipcode skills <list|import|publish|defaults|allow>
  import [--dry-run]             copy foreign skill files; no host publication or grant
  publish <root-id> <absolute-directory>   publish a named source; no grant
  defaults <root-id>...          select sources for new builtin sessions (--none clears)
  list                          inspect published sources and selected global metadata
  allow <root-session-id> <root-id>  explicitly grant standing skills.read to that root
Create a new session after defaults, then allow its selected root before sending $skill.
Existing sessions and custom definitions keep their instruction policy. Child access requires delegation`

func nativeSkillsCLI(args []string) error {
	if len(args) == 0 {
		return errors.New(skillsUsage)
	}
	switch args[0] {
	case "list":
		if len(args) != 1 {
			return errors.New(skillsUsage)
		}
	case "publish":
		if len(args) != 3 {
			return errors.New(skillsUsage)
		}
	case "defaults":
		if len(args) < 2 {
			return errors.New(skillsUsage)
		}
	case "allow":
		if len(args) != 3 {
			return errors.New(skillsUsage)
		}
	default:
		return errors.New(skillsUsage)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, err := connectNativeRuntime(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	var roots protocol.HostSkillRoots
	if err := c.Call(ctx, "host.skills.roots", protocol.EmptyParams{}, &roots); err != nil {
		return err
	}
	switch args[0] {
	case "list":
		fmt.Println("Published skill sources (metadata only; selection grants no access):")
		for _, root := range roots.Roots {
			fmt.Printf("%-24s %q  selected=%t\n", root.ID, root.Path, slices.Contains(roots.Defaults, root.ID))
		}
		if len(roots.Roots) == 0 {
			fmt.Println("No named roots published. Use skills import, then skills publish and skills defaults.")
		}
		var result protocol.HostSkillsResult
		if err := c.Call(ctx, "host.skills.complete", protocol.HostSkillsParams{Scope: "global", Limit: 1024}, &result); err != nil {
			return err
		}
		for _, candidate := range result.Candidates {
			fmt.Printf("%-24s %q\n", candidate.Text, candidate.Description)
		}
		if result.Truncated {
			fmt.Println("Skill metadata truncated at 1024 candidates.")
		}
	case "publish":
		var result protocol.HostSkillRoots
		if err := c.Call(ctx, "host.skills.publish", protocol.PublishSkillRootParams{ExpectedRevision: roots.Revision, ID: protocol.ID(args[1]), Path: args[2]}, &result); err != nil {
			return fmt.Errorf("publication not confirmed; use skills list before another edit: %w", err)
		}
		fmt.Printf("Published %s. Use skills defaults to select it for new builtin sessions. No grant created.\n", args[1])
	case "defaults":
		ids := []protocol.ID{}
		if len(args) != 2 || args[1] != "--none" {
			for _, id := range args[1:] {
				ids = append(ids, protocol.ID(id))
			}
		}
		var result protocol.HostSkillRoots
		if err := c.Call(ctx, "host.skills.set_defaults", protocol.SetDefaultSkillRootsParams{ExpectedRevision: roots.Revision, Roots: ids}, &result); err != nil {
			return fmt.Errorf("default selection not confirmed; use skills list before another edit: %w", err)
		}
		fmt.Println("Skill roots selected for new builtin sessions. Existing sessions and grants are unchanged.")
	case "allow":
		return allowSkillRoot(ctx, c, roots, protocol.ID(args[1]), protocol.ID(args[2]))
	}
	return nil
}

func allowSkillRoot(ctx context.Context, c *client.Client, roots protocol.HostSkillRoots, id, rootID protocol.ID) error {
	var owner protocol.Session
	if err := c.Call(ctx, "sessions.get", protocol.SessionParams{SessionID: id}, &owner); err != nil {
		return err
	}
	if owner.ID != id || owner.ParentID != nil {
		return errors.New("skills allow requires an existing root session; children need explicit delegated grants")
	}
	if !slices.ContainsFunc(roots.Roots, func(root protocol.HostSkillRoot) bool { return root.ID == rootID }) || !slices.Contains(owner.Configuration.Instructions.SkillRoots, string(rootID)) {
		return errors.New("skill root is not published and selected by this session; create a new session after skills defaults or explicitly edit its instruction policy")
	}
	grantID := protocol.ID(rand.Text())
	fmt.Fprintf(os.Stderr, "Creating standing skills.read grant %s for session %s and named root %s.\n", grantID, id, rootID)
	var grant protocol.Grant
	if err := c.Call(ctx, "grants.create", protocol.CreateGrantParams{ID: grantID, SessionID: id, Capability: "skills.read", Resource: string(rootID)}, &grant); err != nil {
		return fmt.Errorf("grant %s outcome not confirmed; inspect this session's grants before retrying: %w", grantID, err)
	}
	if grant.ID != grantID || grant.SessionID != id || grant.Capability != "skills.read" || grant.Resource != string(rootID) {
		return errors.New("grant response ownership mismatch")
	}
	fmt.Printf("Granted %s. The session can read skill files from %s; neighboring files and scripts are not authorized.\n", grant.ID, rootID)
	return nil
}
