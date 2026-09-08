package theme

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// MaxJSONBytes bounds imported and host-resident theme files.
	MaxJSONBytes = 64 * 1024
	// MaxCustomThemes bounds discovery work, including invalid directory entries.
	MaxCustomThemes = 128
)

// Metadata identifies a built-in or host-resident custom theme. Selection is local.
type Metadata struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Dark   bool   `json:"dark"`
	Source string `json:"source"`
}

// CatalogError reports one invalid file without hiding other themes.
type CatalogError struct {
	File    string `json:"file"`
	Message string `json:"message"`
}

// CatalogResult is bounded discovery, not a persisted selection or preferences file.
type CatalogResult struct {
	Themes    []Metadata     `json:"themes"`
	Errors    []CatalogError `json:"errors"`
	Truncated bool           `json:"truncated"`
}

// Catalog lists built-ins and up to MaxCustomThemes directory entries from customDir.
// customDir is the themes directory itself (normally WHIP_HOME/themes).
func Catalog(customDir string) (CatalogResult, error) {
	result := CatalogResult{Themes: []Metadata{}, Errors: []CatalogError{}}
	if _, errs := Embedded(); len(errs) > 0 {
		return result, errors.Join(errs...)
	}
	for _, s := range Builtins() {
		result.Themes = append(result.Themes, Metadata{ID: s.Name, Name: s.Label(), Dark: s.Dark, Source: "builtin"})
	}
	specs, errs, truncated, err := loadCustom(customDir)
	if err != nil {
		return result, err
	}
	result.Errors = errs
	result.Truncated = truncated
	for _, s := range specs {
		result.Themes = append(result.Themes, Metadata{ID: s.Name, Name: s.Label(), Dark: s.Dark, Source: "custom"})
	}
	return result, nil
}

// Resolve looks up a built-in or a bounded host catalog entry by its declared
// name. Names are never interpreted as filesystem paths.
func Resolve(name, customDir string) (Resolved, error) {
	if spec, ok := Builtin(name); ok {
		return ResolveSpec(spec)
	}
	specs, _, truncated, err := loadCustom(customDir)
	if err != nil {
		return Resolved{}, err
	}
	for _, s := range specs {
		if s.Name == name {
			return ResolveSpec(s)
		}
	}
	if truncated {
		return Resolved{}, fmt.Errorf("theme %q is not in the bounded catalog; keep at most %d entries in the themes directory", name, MaxCustomThemes)
	}
	return Resolved{}, fmt.Errorf("theme %q not found", name)
}

// ResolveJSON validates a custom theme supplied explicitly by a client. It never
// reads or writes the filesystem and cannot replace a built-in theme.
func ResolveJSON(data []byte) (Resolved, error) {
	spec, err := parseCustom(data, "imported.json")
	if err != nil {
		return Resolved{}, err
	}
	return ResolveSpec(spec)
}

// BuiltinCatalog resolves every shipped theme, preserving the TUI's menu order.
func BuiltinCatalog() ([]Resolved, error) {
	if _, errs := Embedded(); len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	specs := Builtins()
	result := make([]Resolved, 0, len(specs))
	for _, s := range specs {
		r, err := ResolveSpec(s)
		if err != nil {
			return nil, fmt.Errorf("theme %s: %w", s.Name, err)
		}
		result = append(result, r)
	}
	return result, nil
}

func parseCustom(data []byte, file string) (Spec, error) {
	if len(data) > MaxJSONBytes {
		return Spec{}, fmt.Errorf("theme %s exceeds %d bytes", file, MaxJSONBytes)
	}
	spec, err := parseSpec(data, file)
	if err != nil {
		return Spec{}, err
	}
	if spec.Name == "auto" || spec.Name == "neutral" {
		return Spec{}, fmt.Errorf("theme %s: %q is a reserved appearance name", file, spec.Name)
	}
	if _, ok := Builtin(spec.Name); ok {
		return Spec{}, fmt.Errorf("theme %s: %q is a built-in name; pick another", file, spec.Name)
	}
	return spec, nil
}

func loadCustom(dir string) ([]Spec, []CatalogError, bool, error) {
	specs, issues := []Spec{}, []CatalogError{}
	if dir == "" {
		return specs, issues, false, nil
	}
	root, err := os.OpenRoot(dir)
	if errors.Is(err, os.ErrNotExist) {
		return specs, issues, false, nil
	}
	if err != nil {
		return specs, issues, false, fmt.Errorf("open themes directory: %w", err)
	}
	defer func() { _ = root.Close() }()
	directory, err := root.Open(".")
	if err != nil {
		return specs, issues, false, err
	}
	defer directory.Close()
	entries, err := directory.ReadDir(MaxCustomThemes + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return specs, issues, false, err
	}
	truncated := len(entries) > MaxCustomThemes
	if truncated {
		entries = entries[:MaxCustomThemes]
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	seen := map[string]bool{}
	for _, entry := range entries {
		file := entry.Name()
		if !strings.HasSuffix(file, ".json") {
			continue
		}
		addError := func(err error) {
			message := err.Error()
			if len(message) > 512 {
				message = strings.ToValidUTF8(message[:509], "") + "..."
			}
			issues = append(issues, CatalogError{File: file, Message: message})
		}
		if !entry.Type().IsRegular() {
			addError(errors.New("theme must be a regular JSON file"))
			continue
		}
		data, err := readThemeFile(root, file)
		if err != nil {
			addError(err)
			continue
		}
		spec, err := parseCustom(data, filepath.Base(file))
		if err != nil {
			addError(err)
			continue
		}
		if seen[spec.Name] {
			addError(fmt.Errorf("duplicate theme name %q", spec.Name))
			continue
		}
		seen[spec.Name] = true
		specs = append(specs, spec)
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
	return specs, issues, truncated, nil
}

func readThemeFile(root *os.Root, name string) ([]byte, error) {
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// Verify after opening as well: directory entries can change during discovery.
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("theme must be a regular JSON file")
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxJSONBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxJSONBytes {
		return nil, fmt.Errorf("theme exceeds %d bytes", MaxJSONBytes)
	}
	return data, nil
}
