package ui

import (
	"image/color"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/context-labs/whip/internal/tui/theme"
)

// Every component renders under both built-in themes; the styled output is
// pinned byte-for-byte (a token change shows up as a diff) and the stripped
// output separately (layout regressions read without escape codes).
// Regenerate deliberately with: go test ./internal/tui/ui -update
func TestComponentGoldens(t *testing.T) {
	themes := map[string]*theme.Theme{
		"dark":  theme.Resolve(theme.Dark(), color.RGBA{R: 0x1e, G: 0x1e, B: 0x1e, A: 0xff}, colorprofile.TrueColor),
		"light": theme.Resolve(theme.Light(), color.RGBA{R: 0xfa, G: 0xfa, B: 0xfa, A: 0xff}, colorprofile.TrueColor),
	}
	src := "package main\n\nimport \"fmt\"\n\n// greet says hello\nfunc greet(name string) {\n\tfmt.Printf(\"hi %s: %d\\n\", name, 42)\n}\n"
	for name, th := range themes {
		th := th
		t.Run(name, func(t *testing.T) {
			cases := map[string]string{
				"panel":           Panel{Title: "Agents", Width: 32}.Render(th, "root\n  ├ worker-1  running\n  └ worker-2  idle"),
				"panel-focused":   Panel{Title: "Agents", Width: 32, Focused: true}.Render(th, "root\n  ├ worker-1  running"),
				"panel-border":    Panel{Title: "Confirm", Width: 32, Bordered: true, Focused: true}.Render(th, "Run `rm -rf build`?\n\n[y] yes  [n] no"),
				"codeblock":       CodeBlock{Lang: "go", Source: src, Width: 40, LineNumbers: true, MaxLines: 6}.Render(th),
				"statusbar":       StatusBar{Left: Muted(th, " ~/src/whip · main · sonnet"), Right: Kbd(th, "ctrl+p") + Muted(th, " commands"), Width: 60}.Render(th),
				"statusbar-tight": StatusBar{Left: Muted(th, "a very long left side that will need truncating to fit"), Right: Muted(th, "? help"), Width: 40}.Render(th),
				"toast":           Toast{Text: "config saved", Kind: Success, Width: 30}.Render(th),
				"toast-error":     Toast{Text: "daemon unreachable: connection refused on the socket", Kind: Error, Width: 30}.Render(th),
				"text":            Heading(th, "Heading") + "\n" + Label(th, "LABEL") + " " + Muted(th, "muted") + " " + Kbd(th, "ctrl+x"),
				"list": strings.Join(List{Title: "Commands", Hint: "esc", Search: true, Sel: 1, Width: 44, Empty: "No results found",
					Groups: []ListGroup{{Title: "Session", Items: []ListItem{{Left: "New session", Right: "/new"}, {Left: "Resume session", Right: "/resume"}}}, {Title: "Display", Items: []ListItem{{Left: "Theme: auto", Right: "/theme auto"}}}},
					Footer: []string{"enter", "select", "type", "to filter"}}.Render(th), "\n"),
				"list-window": strings.Join(List{Title: "Sessions", Hint: "esc", Sel: 5, Width: 40, Window: 3,
					Groups: []ListGroup{{Title: "Today", Items: []ListItem{{Left: "one", Right: "1m"}, {Left: "two", Right: "2m"}, {Left: "three", Right: "3m"}, {Left: "four", Right: "4m"}, {Left: "five", Right: "5m"}, {Left: "six", Right: "6m"}, {Left: "seven", Right: "7m"}}}}}.Render(th), "\n"),
				"list-swatch": strings.Join(List{Title: "Themes", Hint: "esc", Search: true, Sel: 1, Width: 40,
					Groups: []ListGroup{{Title: "Themes", Items: []ListItem{{Left: "auto", Swatch: []color.Color{th.Primary, th.Accent, th.Success, th.Error}}, {Left: "tokyonight", Swatch: []color.Color{lipgloss.Color("#82aaff"), lipgloss.Color("#ff966c"), lipgloss.Color("#c3e88d"), lipgloss.Color("#ff757f")}}}}}}.Render(th), "\n"),
				"list-empty": strings.Join(List{Title: "Select model", Hint: "esc", Search: true, Query: "zzz", Width: 40, Empty: "No results found"}.Render(th), "\n"),
			}
			for cname, out := range cases {
				t.Run(cname, func(t *testing.T) {
					golden.RequireEqual(t, []byte(out))
				})
				t.Run(cname+"-plain", func(t *testing.T) {
					golden.RequireEqual(t, []byte(ansi.Strip(out)))
				})
			}
		})
	}
}

// Fill must back-fill cells the body's own resets left on the terminal
// background, and never grow past the requested width.
func TestFillPaintsEveryCell(t *testing.T) {
	th := theme.Resolve(theme.Dark(), nil, colorprofile.TrueColor)
	out := Fill("ab\x1b[0m\ncd", 6, th.Surface.Panel)
	for _, ln := range ansi.Strip(out) {
		_ = ln
	}
	plain := ansi.Strip(out)
	if plain != "ab    \ncd    " {
		t.Fatalf("fill padded wrong: %q", plain)
	}
}
