package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/context-labs/whip/internal/session"
)

// PublishSkillRoot adds an explicit immutable name-to-directory declaration.
// It neither selects the source for a session nor grants permission to read it.
func (a *Authority) PublishSkillRoot(ctx context.Context, expected, id, path string) (Snapshot, error) {
	if err := session.ValidateID(id); err != nil {
		return Snapshot{}, err
	}
	if err := session.ValidateText(path, 4096); err != nil {
		return Snapshot{}, err
	}
	if !filepath.IsAbs(path) {
		return Snapshot{}, fmt.Errorf("%w: skill root must be absolute", session.ErrInvalid)
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("%w: skill root unavailable", session.ErrInvalid)
	}
	root, err := os.OpenRoot(canonical)
	if err != nil {
		return Snapshot{}, fmt.Errorf("%w: skill root must be a directory", session.ErrInvalid)
	}
	if err := root.Close(); err != nil {
		return Snapshot{}, err
	}
	return a.Update(ctx, expected, func(host *Host) error {
		if prior, exists := host.SkillRoots[id]; exists && prior != canonical {
			return fmt.Errorf("%w: a published skill root cannot be rebound; use a new name", ErrRevisionConflict)
		}
		if host.SkillRoots == nil {
			host.SkillRoots = map[string]string{}
		}
		host.SkillRoots[id] = canonical
		return nil
	})
}

// SetDefaultSkillRoots selects discovery sources for newly resolved builtins.
// Existing session configurations and custom definition policies stay unchanged.
func (a *Authority) SetDefaultSkillRoots(ctx context.Context, expected string, roots []string) (Snapshot, error) {
	if err := (session.Instructions{SkillRoots: roots}).Validate(); err != nil {
		return Snapshot{}, err
	}
	return a.Update(ctx, expected, func(host *Host) error {
		for _, id := range roots {
			if _, exists := host.SkillRoots[id]; !exists {
				return fmt.Errorf("%w: unknown skill root %q", session.ErrInvalid, id)
			}
		}
		host.Defaults.Instructions.SkillRoots = slices.Clone(roots)
		return nil
	})
}
