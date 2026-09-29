package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/context-labs/whip/internal/skills"
)

// skillsCLI separates copying files, native publication, selection and authority.
func skillsCLI(args []string) error {
	if len(args) > 0 && args[0] == "import" {
		return skillsImportCLI(args[1:])
	}
	return nativeSkillsCLI(args)
}

func skillsImportCLI(args []string) error {
	dryRun := false
	for _, a := range args {
		if a == "--dry-run" {
			dryRun = true
		} else {
			return errors.New("usage: whipcode skills import [--dry-run]")
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dest := filepath.Join(home, ".agents", "skills")

	// Copying is explicit local-user work. Only the destination and this project
	// participate in dedup; retired ambient runtime directories confer nothing.
	existing := map[string]bool{}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	for _, s := range skills.Scan(filepath.Join(cwd, ".agents", "skills"), dest) {
		existing[s.Name] = true
	}

	type candidate struct {
		name, srcDir string
	}
	var importable, skipped []candidate
	for _, dir := range skills.ForeignDirs() {
		for _, s := range skills.Scan(dir) {
			c := candidate{name: s.Name, srcDir: filepath.Dir(s.Path)}
			// One check dedups both against whip's existing skills and across
			// the foreign sources themselves (first dir — codex — wins over
			// claude for a duplicate name): the claim lands in existing.
			if existing[s.Name] {
				skipped = append(skipped, c)
				continue
			}
			existing[s.Name] = true
			importable = append(importable, c)
		}
	}

	if len(importable) == 0 && len(skipped) == 0 {
		fmt.Println("nothing found to import (looked in: " + strings.Join(skills.ForeignDirs(), ", ") + ")")
		return nil
	}
	sort.Slice(skipped, func(i, j int) bool { return skipped[i].name < skipped[j].name })
	for _, c := range skipped {
		fmt.Printf("○ %-24s already present — leaving %s alone\n", c.name, c.srcDir)
	}
	if len(importable) == 0 {
		fmt.Println("nothing new to import")
		return nil
	}

	sort.Slice(importable, func(i, j int) bool { return importable[i].name < importable[j].name })
	if dryRun {
		fmt.Printf("would import %d skill(s) into %s:\n", len(importable), dest)
		for _, c := range importable {
			fmt.Printf("  %-24s from %s\n", c.name, c.srcDir)
		}
		return nil
	}
	var imported, failed []string
	for _, c := range importable {
		// The frontmatter name becomes the destination directory. A spec-
		// invalid name with path separators (../, ../../pwned) would escape
		// ~/.agents/skills — validate() only warns, so enforce it here: the
		// name must be a single path component matching the spec regex.
		if filepath.Base(c.name) != c.name || !skills.ValidName(c.name) {
			fmt.Fprintf(os.Stderr, "✗ %-24s invalid skill name %q (path traversal guard)\n", c.name, c.name)
			failed = append(failed, c.name)
			continue
		}
		dst := filepath.Join(dest, c.name)
		// copyDir owns cleanup only after its exclusive directory creation succeeds.
		if err := copyDir(c.srcDir, dst); err != nil {
			fmt.Fprintf(os.Stderr, "✗ %-24s %v\n", c.name, err)
			failed = append(failed, c.name)
			continue
		}
		fmt.Printf("✓ %-24s → %s\n", c.name, dst)
		imported = append(imported, c.name)
	}
	fmt.Printf("imported %d skill(s) into %s — files only; not published, selected or authorized\n", len(imported), dest)
	fmt.Printf("Next: whipcode skills publish personal %q, then whipcode skills defaults personal. Create/open a new session and explicitly use skills allow <session-id> personal before invoking a skill.\n", dest)
	if len(failed) > 0 {
		return fmt.Errorf("%d skill(s) failed to copy: %s", len(failed), strings.Join(failed, ", "))
	}
	return nil
}

// copyDir recursively copies src into dst (created if missing). os.ReadDir's
// IsDir is lstat-based, so a symlinked subdirectory falls through to copyFile
// and fails with EISDIR — symlinked dirs are not importable (documented, not
// silently half-copied). Destination must not already exist — the dedup pass
// guarantees the name is free, and refusing to clobber keeps a racing user
// edit safe.
func copyDir(src, dst string) (err error) {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", src)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	// Mkdir, unlike MkdirAll(dst), proves this call created the destination.
	// A concurrent importer/user creating it first must never be cleaned up.
	if err := os.Mkdir(dst, 0o750); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, os.RemoveAll(dst))
		}
	}()
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		s := filepath.Join(src, e.Name())
		d := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := copyDir(s, d); err != nil {
				return err
			}
			continue
		}
		if err := copyFile(s, d); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src) //nolint:gosec // G304: copying the caller-chosen skill dir is the function's contract
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm()) //nolint:gosec // G304: dst is inside whip's own ~/.agents/skills (validated by the dedup pass)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
