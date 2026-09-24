package theme_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/theme"
)

func TestUserThemeLoaderIsolatesBrokenFiles(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	dir := filepath.Join(home, "themes")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"a-custom.json": `{"dark":true,"palette":{"primary":"#123456"}}`,
		"z-custom.json": `{"displayName":"Custom Light"}`,
		"blank.json":    "",
		"unknown.json":  `{"palette":{"invalidToken":"#123456"}}`,
		"trailing.json": `{} {}`,
		"color.json":    `{"palette":{"primary":"url(external)"}}`,
		"reserved.json": `{"name":"dark"}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "not-a-file.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	loaded, problems := theme.Load(home)
	if len(loaded) != 2 || len(problems) != 6 {
		t.Fatalf("broken file hid valid themes: %+v, %v", loaded, problems)
	}
	if loaded[0].Name != "a-custom" || loaded[0].Palette.Primary != "#123456" || loaded[0].Palette.Text != theme.Dark().Palette.Text {
		t.Fatalf("dark overrides or defaults lost: %+v", loaded[0])
	}
	if loaded[1].Name != "z-custom" || loaded[1].Label() != "Custom Light" || loaded[1].Palette.Text != theme.Light().Palette.Text {
		t.Fatalf("light defaults or fallback filename lost: %+v", loaded[1])
	}
	foundHint := false
	for _, err := range problems {
		if strings.Contains(err.Error(), "unknown.json") && strings.Contains(err.Error(), "allowed keys:") {
			foundHint = true
		}
	}
	if !foundHint {
		t.Fatalf("unknown token lacks an actionable correction: %v", problems)
	}
}
