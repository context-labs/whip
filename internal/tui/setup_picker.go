package tui

import (
	"cmp"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/tui/ui"
)

type setupConnectionMethod struct {
	entry         protocol.ProviderEntry
	method, label string
}

// Family entries are presentation only. Their representative ID remains a real
// route; selecting the family resolves an actual API or subscription connection.
func (s *providerSetup) entries() []protocol.ProviderEntry {
	var entries []protocol.ProviderEntry
	for _, entry := range s.list.Providers {
		if entry.Family == "openai" {
			if slices.ContainsFunc(entries, func(e protocol.ProviderEntry) bool { return e.Family == entry.Family }) {
				continue
			}
			entry.Name = "OpenAI"
			for _, member := range s.list.Providers {
				if member.Family == entry.Family && member.ID == "openai" {
					entry.ID, entry.Methods = member.ID, member.Methods
				}
			}
		}
		if setupProviderScore(entry, s.input.Value()) > 0 {
			entries = append(entries, entry)
		}
	}
	slices.SortStableFunc(entries, func(a, b protocol.ProviderEntry) int {
		if rank := cmp.Compare(setupProviderScore(b, s.input.Value()), setupProviderScore(a, s.input.Value())); rank != 0 {
			return rank
		}
		if rank := cmp.Compare(setupProviderRank(a), setupProviderRank(b)); rank != 0 {
			return rank
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return entries
}

func setupProviderRank(entry protocol.ProviderEntry) int {
	if strings.EqualFold(entry.Category, "popular") {
		switch entry.ID {
		case "inference-net":
			return 0
		case "openrouter":
			return 1
		default:
			return 2
		}
	}
	return 3
}

func setupProviderScore(entry protocol.ProviderEntry, query string) int {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return 1
	}
	name := strings.ToLower(entry.Name + " " + entry.ID)
	if entry.Family == "openai" {
		name += " chatgpt subscription codex api key"
	}
	if query == strings.ToLower(entry.Name) || query == entry.ID {
		return 4
	}
	if strings.HasPrefix(name, query) {
		return 3
	}
	if strings.Contains(name, query) {
		return 2
	}
	if strings.Contains(strings.ToLower(entry.Category), query) {
		return 1
	}
	return 0
}

func (s *providerSetup) selectedProviderID() string {
	entries := s.entries()
	if s.mode == "providers" && s.selected < len(entries) {
		return entries[s.selected].ID
	}
	if s.mode == "providers" && len(s.list.Providers) > 0 && s.selected == len(entries) {
		return "@other"
	}
	return ""
}

func (s *providerSetup) restoreProviderSelection(id string) {
	entries := s.entries()
	if id == "@other" {
		s.selected = len(entries)
		return
	}
	for i, entry := range entries {
		if entry.ID == id {
			s.selected = i
			return
		}
	}
}

func (s *providerSetup) familyEntries(entry protocol.ProviderEntry) []protocol.ProviderEntry {
	var entries []protocol.ProviderEntry
	for _, member := range s.list.Providers {
		if member.ID == entry.ID || entry.Family != "" && member.Family == entry.Family {
			entries = append(entries, member)
		}
	}
	return entries
}

func (s *providerSetup) chooseProvider(entry protocol.ProviderEntry, manage bool) tea.Cmd {
	entries := s.familyEntries(entry)
	if !manage {
		available := slices.DeleteFunc(slices.Clone(entries), func(e protocol.ProviderEntry) bool { return !setupCanAttempt(e) })
		if len(available) == 1 {
			return s.connect(available[0])
		}
	}
	s.methods = nil
	for _, member := range entries {
		if manage || setupCanAttempt(member) || member.Status.Disabled {
			label := member.Name
			switch member.ID {
			case "openai":
				label = "API key"
			case "openai-codex":
				label = "ChatGPT subscription"
			}
			s.methods = append(s.methods, setupConnectionMethod{entry: member, label: label})
			continue
		}
		for _, method := range member.Methods {
			label := "API key"
			if method == "login" {
				label = "Sign in with browser"
				if member.ID == "openai-codex" {
					label = "ChatGPT subscription"
				}
			} else if method != "api_key" {
				continue
			}
			s.methods = append(s.methods, setupConnectionMethod{member, method, label})
		}
	}
	if len(s.methods) == 1 {
		method := s.methods[0]
		if manage || method.entry.Status.Disabled {
			s.provider = method.entry.ID
			return s.readProvider("manage")
		}
		return s.connectMethod(method.entry, method.method)
	}
	if len(s.methods) == 0 {
		s.provider = entry.ID
		return s.readProvider("manage")
	}
	s.manageMethods, s.mode, s.selected = manage, "methods", 0
	s.provider = entry.ID
	s.input.Reset()
	return nil
}

func setupConnectionMark(entry protocol.ProviderEntry) string {
	if !entry.Status.Disabled && entry.Status.Available != nil && *entry.Status.Available {
		return "✓"
	}
	if entry.Custom && entry.Status.Configured || entry.Status.Disabled ||
		entry.Status.AuthState == "unchecked" || entry.Status.AuthState == "configuration_error" ||
		entry.Status.AuthState == "setup_required" {
		return "!"
	}
	return ""
}

func (s *providerSetup) providerRows(width, height int) []string {
	entries := s.entries()
	var groups []ui.ListGroup
	for _, entry := range entries {
		category := "Providers"
		if strings.EqualFold(entry.Category, "popular") {
			category = "Popular"
		}
		if len(groups) == 0 || groups[len(groups)-1].Title != category {
			groups = append(groups, ui.ListGroup{Title: category})
		}
		hint, mark := "", setupConnectionMark(entry)
		for _, member := range s.familyEntries(entry) {
			if setupConnectionMark(member) == "✓" {
				mark = "✓"
			}
		}
		if entry.Recommended {
			hint = "Recommended"
		} else if entry.Family == "openai" {
			hint = "Subscription or API key"
		}
		groups[len(groups)-1].Items = append(groups[len(groups)-1].Items, ui.ListItem{Left: entry.Name, Right: hint, Mark: mark})
	}
	groups = append(groups, ui.ListGroup{Items: []ui.ListItem{{Left: "Custom endpoint"}}})
	detail := s.message
	if s.busy {
		detail = "Checking connections…"
	}
	footer := []string{"enter", "select", "ctrl+e", "manage"}
	if width < 45 {
		footer = []string{"enter", "select"}
	}
	return s.listRows(ui.List{
		Title: "Connect a provider", Hint: "esc", Search: true, SearchView: s.searchView(width - 2),
		Groups: groups, Sel: s.selected, Footer: footer, Gutter: true, Width: width, Height: height,
	}, detail)
}

func (s *providerSetup) methodRows(width, height int) []string {
	items := make([]ui.ListItem, len(s.methods))
	for i, method := range s.methods {
		items[i] = ui.ListItem{Left: method.label, Mark: setupConnectionMark(method.entry)}
	}
	title := "Connect " + s.provider
	if entry := s.entry(); entry != nil {
		title = "Connect " + entry.Name
		if entry.Family == "openai" {
			title = "Connect OpenAI"
		}
	}
	if s.manageMethods {
		title = "Manage OpenAI connections"
	}
	return s.listRows(ui.List{
		Title: title, Hint: "esc", Groups: []ui.ListGroup{{Items: items}}, Sel: s.selected,
		Footer: []string{"enter", "select"}, Gutter: true, Width: width, Height: height,
	}, s.message)
}

// Bubbles reserves one extra cell for the cursor beyond Width. Theme the entire
// input (including its padding) and reserve that cell before placing it in a row.
func setupInputView(input textinput.Model, placeholder string, width int) string {
	th := currentTheme()
	input.Prompt, input.Placeholder = "", placeholder
	input.SetWidth(max(width-1, 1))
	styles := input.Styles()
	styles.Focused.Text, styles.Blurred.Text = th.On(th.Text, th.Surface.Panel), th.On(th.Text, th.Surface.Panel)
	styles.Focused.Placeholder, styles.Blurred.Placeholder = th.On(th.Muted, th.Surface.Panel), th.On(th.Muted, th.Surface.Panel)
	styles.Focused.Prompt, styles.Blurred.Prompt = th.On(th.Text, th.Surface.Panel), th.On(th.Text, th.Surface.Panel)
	styles.Cursor.Color, styles.Cursor.Blink = th.Primary, false
	input.SetStyles(styles)
	return input.View()
}

func (s *providerSetup) keyRows(width, height int) []string {
	input := setupInputView(s.input, "API key", width-4)
	detail := ""
	if s.busy {
		detail = "Connecting…"
	} else if s.message != "" {
		th := currentTheme()
		input += "\n" + th.On(th.Error, th.Surface.Panel).Render(wrap(s.message, max(width-4, 1)))
	}
	return setupPromptRows("API key", "", input, []string{"enter", "submit"}, detail, width, height)
}

func (s *providerSetup) loginRows(width, height int) []string {
	th := currentTheme()
	muted := th.On(th.Muted, th.Surface.Panel)
	name := s.provider
	if entry := s.entry(); entry != nil {
		name = entry.Name
	}
	if s.provider == "openai-codex" {
		name = "ChatGPT"
	}
	content := []string{}
	keys := []string{}
	roomy := height == 0 || height >= 14
	if s.login.VerificationURL != "" {
		if roomy {
			content = append(content, muted.Render("Open this link in your browser:"))
		}
		link := th.On(th.Text, th.Surface.Panel).Render(wrap(s.login.VerificationURL, max(width-4, 1)))
		content = append(content, link)
		keys = append(keys, "o", "open browser")
	}
	if s.login.UserCode != "" {
		code := th.On(th.Primary, th.Surface.Panel).Bold(true).Render(s.login.UserCode)
		content = append(content, "", muted.Render("Code  ")+code)
		keys = append(keys, "c", "copy code")
	}
	if width < 48 && len(keys) == 4 {
		keys = []string{"o", "open", "c", "copy"}
	}
	if roomy || len(content) == 0 {
		content = append(content, "", muted.Render("Waiting for sign-in…"))
	}
	detail := s.message
	if detail == "" {
		detail = s.loginFeedback
	}
	if detail == "" && s.provider == "openai-codex" {
		detail = "If needed, enable device-code authorization in ChatGPT Security settings."
	}
	return setupPromptRows(
		"Sign in to "+name,
		"",
		strings.Join(content, "\n"),
		keys,
		detail,
		width,
		height,
	)
}

func (s *providerSetup) projectNameRows(width, height int) []string {
	detail := s.message
	if s.busy {
		detail = "Creating project…"
	}
	return setupPromptRows(
		"Create a project",
		"",
		setupInputView(s.input, "Project name", width-4),
		[]string{"enter", "create"},
		detail,
		width,
		height,
	)
}

func setupPromptRows(title, label, input string, keys []string, detail string, width, height int) []string {
	th := currentTheme()
	bg := th.Surface.Panel
	text, muted := th.On(th.Text, bg), th.On(th.Muted, bg)
	pad := func(line string) string {
		return ui.PadRow(text.Render("  ")+ansi.Truncate(line, max(width-4, 1), ""), width, bg)
	}
	blank := ui.PadRow("", width, bg)
	title = ansi.Truncate(title, max(width-9, 1), "…")
	gap := max(width-4-ansi.StringWidth(title)-3, 1)
	rows := []string{blank, pad(text.Bold(true).Render(title) + text.Render(strings.Repeat(" ", gap)) + muted.Render("esc")), blank}
	if label != "" {
		rows = append(rows, pad(muted.Render(label)), blank)
	}
	for line := range strings.SplitSeq(input, "\n") {
		rows = append(rows, pad(line))
	}
	rows = append(rows, blank)
	if height == 0 || height >= 14 {
		rows = append(rows, blank)
	}
	if len(keys) > 2 && ansi.StringWidth(ui.Hints(th, bg, keys...)) > max(width-4, 1) {
		keys = keys[:2]
	}
	rows = append(rows, pad(ui.Hints(th, bg, keys...)))
	if detail != "" {
		lines := strings.Split(wrap(detail, max(width-4, 1)), "\n")
		if height > 0 {
			lines = lines[:min(len(lines), max(height-len(rows)-1, 0))]
		}
		for _, line := range lines {
			rows = append(rows, pad(muted.Render(line)))
		}
	}
	rows = append(rows, blank)
	// Give content priority over vertical spacing in short terminals.
	for i := len(rows) - 1; height > 0 && len(rows) > height && i >= 0; i-- {
		if strings.TrimSpace(ansi.Strip(rows[i])) == "" {
			rows = slices.Delete(rows, i, i+1)
		}
	}
	return rows
}
