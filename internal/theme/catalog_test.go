package theme_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/theme"
)

func TestHostCatalogIsolationAndPartialErrors(t *testing.T) {
	dir := t.TempDir()
	writeTheme(t, dir, "file-name.json", `{"name":"declared-name","dark":true,"palette":{"primary":"#abcdef"}}`)
	writeTheme(t, dir, "broken.json", `{"name":"broken","palette":{"text":"red"}}`)
	writeTheme(t, dir, "builtin.json", `{"name":"dark"}`)
	writeTheme(t, dir, "duplicate.json", `{"name":"declared-name","dark":true,"palette":{"primary":"#abcdef"}}`)
	writeTheme(t, dir, "huge.json", strings.Repeat("x", theme.MaxJSONBytes+1))
	outside := t.TempDir()
	writeTheme(t, outside, "secret.json", `{"name":"must-not-read"}`)
	if err := os.Symlink(filepath.Join(outside, "secret.json"), filepath.Join(dir, "escape.json")); err != nil {
		t.Fatal(err)
	}
	result, err := theme.Catalog(dir)
	if err != nil {
		t.Fatal(err)
	}
	if result.Truncated || len(result.Themes) != len(theme.Builtins())+1 || len(result.Errors) != 5 {
		t.Fatalf("catalog lost valid entries or errors: %+v", result)
	}
	last := result.Themes[len(result.Themes)-1]
	if last.ID != "declared-name" || last.Source != "custom" || !last.Dark {
		t.Fatalf("custom metadata: %+v", last)
	}
	resolved, err := theme.Resolve("declared-name", dir)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Colors.Primary != "#abcdef" {
		t.Fatal("lookup used filename instead of declared name")
	}
	for _, name := range []string{"must-not-read", "../secret.json", filepath.Join(outside, "secret.json"), "file-name"} {
		if _, err := theme.Resolve(name, dir); err == nil {
			t.Errorf("unexpected path/name resolution: %s", name)
		}
	}
}

func TestHostCatalogBoundsAndMissingDirectory(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "themes")
	catalog, err := theme.Catalog(missing)
	if err != nil || len(catalog.Themes) != len(theme.Builtins()) || len(catalog.Errors) != 0 || catalog.Truncated {
		t.Fatalf("missing optional directory: %+v, %v", catalog, err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("discovery created the optional directory")
	}
	dir := t.TempDir()
	for i := range theme.MaxCustomThemes + 5 {
		writeTheme(t, dir, fmt.Sprintf("theme-%03d.json", i), fmt.Sprintf(`{"name":"custom-%03d"}`, i))
	}
	catalog, err = theme.Catalog(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !catalog.Truncated || len(catalog.Themes) != len(theme.Builtins())+theme.MaxCustomThemes {
		t.Fatalf("unbounded discovery: %+v", catalog)
	}
	if _, err := theme.Resolve("not-loaded", dir); err == nil || !strings.Contains(err.Error(), "bounded catalog") {
		t.Fatalf("missing truncation notice: %v", err)
	}
}

func TestHostCatalogBoundsErrorText(t *testing.T) {
	dir := t.TempDir()
	writeTheme(t, dir, "broken.json", `{"palette":{"primary":"`+strings.Repeat("x", 10_000)+`"}}`)
	catalog, err := theme.Catalog(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Errors) != 1 || len(catalog.Errors[0].Message) > 512 {
		t.Fatalf("unbounded catalog error: %+v", catalog.Errors)
	}
}

func writeTheme(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
