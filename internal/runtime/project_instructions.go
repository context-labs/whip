package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/instruction"
)

// openProjectInstructions is called only after project instruction authority
// admits membership metadata and source reads. Canonical paths identify the
// candidate chain; descriptor-relative reads remain confined to the opened root.
// A nil root means the selected boundary does not contain this workspace.
func openProjectInstructions(ctx context.Context, id, boundary, cwd string) (_ *instruction.Root, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(boundary)
	if err != nil {
		return nil, errors.New("project instruction boundary is unavailable")
	}
	keep := false
	defer func() {
		if !keep {
			_ = root.Close()
		}
	}()
	canonicalBoundary, err := filepath.EvalSymlinks(boundary)
	if err != nil {
		return nil, errors.New("project instruction boundary cannot be resolved")
	}
	canonicalCWD, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return nil, errors.New("project instruction workspace cannot be resolved")
	}
	if len(canonicalBoundary) > 4096 || len(canonicalCWD) > 4096 || !utf8.ValidString(canonicalBoundary+canonicalCWD) {
		return nil, errors.New("project instruction paths exceed bounds")
	}
	relative, err := filepath.Rel(canonicalBoundary, canonicalCWD)
	if err != nil || !filepath.IsLocal(relative) {
		return nil, nil //nolint:nilnil // An unrelated configured boundary supplies no project sources.
	}
	var directories []string
	for path := relative; ; path = filepath.Dir(path) {
		if len(directories) == instruction.MaxProjectDirectories {
			return nil, errors.New("project instruction chain exceeds 128 directories")
		}
		if strings.ContainsRune(path, '\\') {
			return nil, errors.New("project instruction paths must use canonical separators")
		}
		directories = append(directories, filepath.ToSlash(path))
		if path == "." {
			break
		}
	}
	slices.Reverse(directories)
	openedBoundary, err := root.Stat(".")
	if err != nil {
		return nil, errors.New("project instruction boundary cannot be inspected")
	}
	resolvedBoundary, err := os.Stat(canonicalBoundary)
	if err != nil || !os.SameFile(openedBoundary, resolvedBoundary) {
		return nil, errors.New("project instruction boundary changed during resolution")
	}
	workspace, err := os.OpenRoot(cwd)
	if err != nil {
		return nil, errors.New("project instruction workspace is unavailable")
	}
	defer func() { _ = workspace.Close() }()
	openedCWD, err := workspace.Stat(".")
	if err != nil {
		return nil, errors.New("project instruction workspace cannot be inspected")
	}
	confinedCWD, err := root.Stat(relative)
	if err != nil || !os.SameFile(openedCWD, confinedCWD) {
		return nil, errors.New("project instruction workspace changed during resolution")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	keep = true
	return &instruction.Root{ID: id, FS: root, ProjectDirectories: directories}, nil
}
