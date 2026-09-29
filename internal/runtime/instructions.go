package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/instruction"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

// Instructions captures external sources once for this turn. Already captured
// bytes survive later file edits or authority revocation; the next turn checks again.
func (r *Runtime) Instructions(ctx context.Context, turn session.Turn, policy session.Instructions) (string, error) {
	current, err := r.store.ConfigurationSession(ctx, turn.SessionID, turn.ConfigRevision)
	if err != nil {
		return "", err
	}
	tree, err := r.store.Tree(ctx, current.TreeID)
	if err != nil {
		return "", err
	}
	input, err := r.store.TurnInput(ctx, turn.ID)
	if err != nil {
		return "", err
	}
	if current.ParentID == nil && current.Config.Run != nil && current.Config.Run.System != "" {
		text := current.Config.Run.System
		contribution, err := r.turnStart(ctx, current, turn, input)
		if err != nil {
			return "", err
		}
		if contribution != "" {
			text += "\n\n--- Turn-start hook context ---\n" + contribution
		}
		if len(text) > session.MaxInstructionBytes {
			return "", errors.New("composed instructions exceed 1 MiB")
		}
		digest := sha256.Sum256([]byte(text))
		manifest := session.InstructionManifest{Bytes: int64(len(text)), SHA256: hex.EncodeToString(digest[:]), Sources: []session.InstructionSource{}}
		return text, r.store.SaveInstructionManifest(ctx, turn.ID, manifest)
	}
	var invoked []string
	if input != nil {
		invoked = instruction.InvokedNames(input.Parts)
	}
	catalogNeeded := policy.DiscoverSkills || len(invoked) > 0
	var authorities []store.InstructionReadAuthority
	ids := append([]string(nil), policy.SkillRoots...)
	if len(policy.ProjectFiles) > 0 || catalogNeeded {
		ids = append(ids, "")
	}
	for _, id := range ids {
		if id != "" && !catalogNeeded {
			continue
		}
		authority, err := r.store.InstructionReadAuthority(ctx, turn.ID, id)
		if err != nil {
			return "", err
		}
		if authority != nil {
			authorities = append(authorities, *authority)
		}
	}
	if policy.ProjectRoot != nil && (len(policy.ProjectFiles) > 0 || catalogNeeded) {
		authority, err := r.store.ProjectInstructionReadAuthority(ctx, turn.ID, *policy.ProjectRoot)
		if err != nil {
			return "", err
		}
		if authority != nil {
			authorities = append(authorities, *authority)
		}
	}
	roots, closeRoots, err := r.instructionRoots(ctx, current.WorkingDirectory, policy, authorities, catalogNeeded)
	if err != nil {
		return "", err
	}
	defer closeRoots()
	captured, err := instruction.Load(ctx, roots, policy, invoked)
	if err != nil {
		return "", err
	}
	text, err := r.invokedInstructions(ctx, turn.ID, roots, &captured)
	if err != nil {
		return "", err
	}
	if policy.StandingInstructions {
		standing, err := r.standingInstructions(ctx, turn.ID)
		if err != nil {
			return "", err
		}
		if len(standing.Sources) > session.MaxInstructionSources-len(captured.Sources) {
			return "", errors.New("instruction source manifest exceeds bounds")
		}
		captured.Sources = append(captured.Sources, standing.Sources...)
		if standing.Text != "" {
			framed := "\n\n--- Standing user instructions ---\n" + standing.Text
			if len(framed) > session.MaxInstructionBytes-len(text) {
				return "", errors.New("composed instructions exceed 1 MiB")
			}
			text += framed
		}
	}
	text += "\n\n" + environmentInstructions(current, turn) + "\n\n" + executionInstructions(current, tree)
	contribution, err := r.turnStart(ctx, current, turn, input)
	if err != nil {
		return "", err
	}
	if contribution != "" {
		text += "\n\n--- Turn-start hook context ---\n" + contribution
	}
	if len(text) > session.MaxInstructionBytes {
		return "", errors.New("composed instructions exceed 1 MiB")
	}
	digest := sha256.Sum256([]byte(text))
	manifest := session.InstructionManifest{Bytes: int64(len(text)), SHA256: hex.EncodeToString(digest[:]), Sources: captured.Sources}
	if err := r.store.SaveInstructionManifest(ctx, turn.ID, manifest); err != nil {
		return "", err
	}
	return text, nil
}

// InstructionManifest inspects immutable source metadata without reopening files.
func (r *Runtime) InstructionManifest(ctx context.Context, turn session.TurnID) (*session.InstructionManifest, error) {
	return r.store.InstructionManifest(ctx, turn)
}

// Each selected body is separately admitted. Discovery, a previous completion
// result, and previously granted permission for a single tool read confer none.
func (r *Runtime) invokedInstructions(ctx context.Context, turn session.TurnID, roots []instruction.Root, captured *instruction.Snapshot) (string, error) {
	var text strings.Builder
	text.WriteString(captured.Text)
	for _, selected := range captured.Selected {
		if len(captured.Sources) >= session.MaxInstructionSources {
			return "", errors.New("instruction source manifest exceeds bounds")
		}
		id := ""
		if selected.Source.RootID != nil {
			id = *selected.Source.RootID
		}
		var root *os.Root
		for _, candidate := range roots {
			if candidate.ID == id && candidate.Scope() == selected.Source.Scope {
				root = candidate.FS
				break
			}
		}
		var authority *store.InstructionReadAuthority
		var err error
		if selected.Source.Scope == "project" {
			authority, err = r.store.ProjectInstructionReadAuthority(ctx, turn, id)
		} else {
			authority, err = r.store.InstructionReadAuthority(ctx, turn, id)
		}
		if err != nil {
			return "", err
		}
		if root == nil || authority == nil || (id == "" && authority.Resource != root.Name()) {
			return "", fmt.Errorf("%w: skill invocation requires read authority", session.ErrInvalid)
		}
		body, source, err := instruction.ReadSkill(ctx, root, selected)
		if err != nil {
			return "", err
		}
		framed := "\n\n--- Explicit skill: " + strconv.Quote(selected.Name) + " from " + strconv.Quote(source.Scope+":"+id+"/"+source.Path) + " ---\n" + body
		if len(framed) > session.MaxInstructionBytes-text.Len() {
			return "", errors.New("composed instructions exceed 1 MiB")
		}
		text.WriteString(framed)
		captured.Sources = append(captured.Sources, source)
	}
	return text.String(), nil
}

// Skills returns current, authorized metadata for human inspection and completion.
// Each page is a fresh view; its name cursor does not promise a historical snapshot.
func (r *Runtime) Skills(ctx context.Context, id session.SessionID, prefix, after string, limit int) ([]instruction.Skill, *string, error) {
	if limit < 1 || limit > 100 || len(prefix) > 64 || len(after) > 64 || !utf8.ValidString(prefix+after) || strings.ContainsRune(prefix+after, 0) {
		return nil, nil, fmt.Errorf("%w: invalid skill page bounds", session.ErrInvalid)
	}
	owner, authorities, err := r.store.SessionInstructions(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	result := []instruction.Skill{}
	roots, closeRoots, err := r.instructionRoots(ctx, owner.WorkingDirectory, owner.Config.Instructions, authorities, true)
	if err != nil {
		return nil, nil, err
	}
	defer closeRoots()
	catalog, err := instruction.Catalog(ctx, roots)
	if err != nil {
		return nil, nil, err
	}
	for _, skill := range catalog.Skills {
		if skill.Name <= after || !strings.HasPrefix(skill.Name, prefix) {
			continue
		}
		if len(result) == limit {
			return result, new(result[len(result)-1].Name), nil
		}
		result = append(result, skill)
	}
	return result, nil, nil
}

// instructionRoots resolves logical names only against the explicit host registry.
// Registry selection confers no authority; absent read authority causes no filesystem probes.
func (r *Runtime) instructionRoots(ctx context.Context, cwd string, policy session.Instructions, authorities []store.InstructionReadAuthority, catalog bool) ([]instruction.Root, func(), error) {
	if err := policy.Validate(); err != nil {
		return nil, nil, err
	}
	var skillRoots map[string]string
	if len(policy.SkillRoots) > 0 {
		current, err := r.configuration.Snapshot(ctx)
		if err != nil {
			return nil, nil, err
		}
		skillRoots = current.Host.SkillRoots
	}
	var roots []instruction.Root
	closeRoots := func() {
		for _, root := range roots {
			_ = root.FS.Close()
		}
	}
	open := func(id, path string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		root, err := os.OpenRoot(path)
		if err != nil {
			var pathError *os.PathError
			if id != "" && errors.As(err, &pathError) {
				return fmt.Errorf("open host skill root %q: %w", id, pathError.Err)
			}
			return err
		}
		roots = append(roots, instruction.Root{ID: id, FS: root})
		return nil
	}
	for _, id := range policy.SkillRoots {
		path, ok := skillRoots[id]
		if !ok {
			closeRoots()
			return nil, nil, fmt.Errorf("%w: unknown host skill root %q", session.ErrInvalid, id)
		}
		if !catalog {
			continue
		}
		for _, authority := range authorities {
			if authority.Capability == "skills.read" && authority.Resource == id {
				if err := open(id, path); err != nil {
					closeRoots()
					return nil, nil, err
				}
				break
			}
		}
	}
	if policy.ProjectRoot != nil {
		id := *policy.ProjectRoot
		path, ok := r.host.ProjectRoots[id]
		if !ok {
			closeRoots()
			return nil, nil, fmt.Errorf("%w: unknown project instruction root %q", session.ErrInvalid, id)
		}
		if len(policy.ProjectFiles) > 0 || catalog {
			for _, authority := range authorities {
				if authority.Capability != "instructions.read" || authority.Resource != "project:"+id {
					continue
				}
				project, err := openProjectInstructions(ctx, id, path, cwd)
				if err != nil {
					closeRoots()
					return nil, nil, err
				}
				if project != nil {
					roots = append(roots, *project)
					return roots, closeRoots, nil
				}
				break
			}
		}
	}
	if len(policy.ProjectFiles) > 0 || catalog {
		for _, authority := range authorities {
			if authority.Capability == "files.read" {
				if err := open("", authority.Resource); err != nil {
					closeRoots()
					return nil, nil, err
				}
				break
			}
		}
	}
	return roots, closeRoots, nil
}

func (r *Runtime) standingInstructions(ctx context.Context, turn session.TurnID) (instruction.Snapshot, error) {
	path := r.host.StandingInstructionsFile
	if path == "" {
		// No published source means discovery is disabled; never fall back to
		// a home-directory filename or probe a path the host has not selected.
		return instruction.Snapshot{}, nil
	}
	authority, err := r.store.StandingInstructionReadAuthority(ctx, turn)
	if err != nil || authority == nil {
		return instruction.Snapshot{}, err
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		if pathError, ok := errors.AsType[*os.PathError](err); ok {
			return instruction.Snapshot{}, fmt.Errorf("open standing instruction directory: %w", pathError.Err)
		}
		return instruction.Snapshot{}, err
	}
	defer func() { _ = root.Close() }()
	return instruction.LoadStanding(ctx, root, filepath.Base(path))
}
