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

// skillsCLI implements `whip skills <list|import>`.
//
//	list                    skill names, descriptions, and where they load from
//	import [--dry-run]      copy skills from other harnesses' dirs (codex,
//	                        claude-code) into ~/.agents/skills, skipping
//	                        anything whip already has
//
// Skills are directories with a SKILL.md, so "import" is a recursive copy.
// Dedup is by skill name: a name present in any of whip's dirs (project
// .agents/skills, ~/.whip/skills, ~/.agents/skills) is never overwritten —
// the repo-level copy always wins at scan time too, so copying over a
// user-level skill would silently shadow nothing and confuse everyone.
func skillsCLI(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: whip skills <list|import>")
	}
	switch args[0] {
	case "list":
		return skillsListCLI()
	case "import":
		return skillsImportCLI(args[1:])
	default:
		return fmt.Errorf("unknown skills subcommand %q (list|import)", args[0])
	}
}

func skillsListCLI() error {
	dirs := skills.DefaultDirs()
	sk, problems := skills.ScanDetailed(dirs...)
	if len(sk) == 0 && len(problems) == 0 {
		fmt.Println("no skills found (looked in: " + strings.Join(dirs, ", ") + ")")
		return nil
	}
	for _, s := range sk {
		warn := ""
		if s.Warning != "" {
			warn = " ⚠ " + s.Warning
		}
		fmt.Printf("%-24s %s%s\n", s.Name, filepath.Dir(filepath.Dir(s.Path)), warn)
	}
	for _, p := range problems {
		fmt.Fprintf(os.Stderr, "skills: %s: %s\n", p.Path, p.Err)
	}
	return nil
}

func skillsImportCLI(args []string) error {
	dryRun := false
	for _, a := range args {
		if a == "--dry-run" {
			dryRun = true
		} else {
			return errors.New("usage: whip skills import [--dry-run]")
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dest := filepath.Join(home, ".agents", "skills")

	// Names whip already loads (project + user dirs) are the dedup set.
	existing := map[string]bool{}
	for _, s := range skills.Scan(skills.DefaultDirs()...) {
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
	for _, c := range importable {
		dst := filepath.Join(dest, c.name)
		if err := copyDir(c.srcDir, dst); err != nil {
			return fmt.Errorf("import %s: %w", c.name, err)
		}
		fmt.Printf("✓ %-24s → %s\n", c.name, dst)
	}
	fmt.Printf("imported %d skill(s) into %s — available on next whip launch\n", len(importable), dest)
	return nil
}

// copyDir recursively copies src into dst (created if missing). Symlinks are
// followed (an imported skill should be real files, not a link back into
// another harness's dir). Destination must not already exist — the dedup
// pass guarantees the name is free, and refusing to clobber keeps a racing
// user edit safe.
func copyDir(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", src)
	}
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("%s already exists", dst)
	}
	if err := os.MkdirAll(dst, 0o750); err != nil { // 0o750: imported skills are user config, not world-readable (gosec G301)
		return err
	}
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
