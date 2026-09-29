package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestNativeWebLinksAreExplicitBoundedAndSafeAcrossWrapping(t *testing.T) {
	for _, width := range []int{18, 45, 100} {
		rendered := nativeMarkdown("Read [the complete guide](https://example.test/guide?q=hello#part), then <http://example.test/a-long-autolink>.\n\n[host](file:///private/secret) [mail](mailto:user@example.test) [unsafe](javascript:alert).\n\n\x1b]8;;https://attacker.test\aordinary\x1b]8;;\a", width)
		if !strings.Contains(rendered, "\x1b]8;;https://example.test/guide?q=hello#part") || !strings.Contains(rendered, "\x1b]8;;http://example.test/a-long-autolink") {
			t.Fatalf("web links missing at width %d: %q", width, rendered)
		}
		for _, forbidden := range []string{"\x1b]8;;file:", "\x1b]8;;mailto:", "\x1b]8;;javascript:", "\x1b]8;;https://attacker.test"} {
			if strings.Contains(rendered, forbidden) {
				t.Fatalf("unsafe or raw hyperlink retained: %q", rendered)
			}
		}
		plain := ansi.Strip(rendered)
		if strings.Contains(plain, "https://example.test/guide") || strings.ContainsRune(plain, '\x1b') || !strings.Contains(plain, "ordinary") {
			t.Fatalf("duplicate href or controls in copied display: %q", plain)
		}
		for line := range strings.Lines(rendered) {
			if ansi.StringWidth(strings.TrimSuffix(line, "\n")) > width {
				t.Fatalf("wide hyperlink at %d: %q", width, line)
			}
		}
	}
}

func TestNativeWebLinkPolicyRejectsOtherSchemesCredentialsAndControlCharacters(t *testing.T) {
	for _, value := range []string{"https:///empty-host", "//example.test/path", "javascript:alert(1)", "file:///tmp/a", "mailto:a@example.test", "https://user:secret@example.test/", "https://example.test/a\x1b]52;c;text\a", "https://example.test/a\nmore", "https://example.test/with space", "https://example.test/%zz"} {
		if got := nativeWebLink(value); got != "" {
			t.Fatalf("unsafe destination %q became %q", value, got)
		}
	}
	for _, value := range []string{"https://example.test/with%20space?a=1&b=2#part", "http://[::1]:8000/path", "HTTPS://example.test/界"} {
		if got := nativeWebLink(value); got == "" {
			t.Fatalf("valid web destination refused: %q", value)
		}
	}
}
