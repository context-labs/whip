package tui

import (
	"sort"
	"strings"
)

// registryEntry is the single source of truth for one slash command or
// keybind action: its name, one-line hint, optional keybind, and category.
// /help, the tab-completion table, and the ctrl+p palette all render from
// this registry, so adding a command is one entry and the three views can't
// drift apart. The dispatch switch in (*model).command remains the actual
// behavior — the registry is names+hints only.
type registryEntry struct {
	Name     string // "/model", or "!cmd" for the shell escape
	Hint     string // one line: "[args] — what it does"
	Keybind  string // optional: "ctrl+p", "esc esc", …
	Category string // Agent, Session, Display, App, Keys
}

// registry lists every user-facing slash command. Palette-only rows that
// don't dispatch through the switch (rewind, quit, thinking tokens) keep
// their hint/keybind in palette.go as constants, so a keybind or description
// still has exactly one home even when it's not a slash command.
var registry = []registryEntry{
	{Name: "/auth", Hint: "<provider> [key] — connect inference-net (browser login), openrouter (masked key), or codex (device login); /model lists the catalog; also: whip auth <provider>", Category: "Agent"},
	{Name: "/cd", Hint: "[dir] — change working directory (bare prints it)", Category: "Session"},
	{Name: "/clear", Hint: "— reset conversation", Category: "Session"},
	{Name: "/compact", Hint: "[model] [provider]|off — compact now, or pick the compaction model (off restores the default); retry undoes the last compaction, log lists them; compaction level: ctrl+p › Compaction level", Category: "Session"},
	{Name: "/computer-use", Hint: "[task] — drive this Mac (apps, open Chrome, screen); also: allow|deny <app>", Category: "Agent"},
	{Name: "/context-doctor", Hint: "— audit what a fresh session injects (skills, MCP, tool schemas) and its token cost", Category: "Session"},
	{Name: "/effort", Hint: "[level] — reasoning effort: off·low·medium·high (bare opens selector)", Category: "Agent"},
	{Name: "/fork", Hint: "[name] — copy the conversation into a new session (pick a point in the rewind picker with f)", Category: "Session"},
	{Name: "/goal", Hint: "<text> — keep working until the goal is met (resume | clear | rounds <n>|default [--global])", Category: "Session"},
	{Name: "/goal-from-context", Hint: "[n] — formulate a goal from the last n messages (default 8) and work until it's met", Category: "Session"},
	{Name: "/help", Hint: "— show all commands and keybindings", Category: "App"},
	{Name: "/mcp", Hint: "[name] [reconnect|enable|disable] — MCP servers: status, reconnect, toggle", Category: "Session"},
	{Name: "/me", Hint: "— edit your standing instructions (~/.whip/me.md) in $EDITOR", Category: "Agent"},
	{Name: "/memory", Hint: "[n] [session] — saved memories: list what's injected each turn, mark entry n done", Category: "Session"},
	{Name: "/model", Hint: "<name> [provider] — switch model (any provider-catalog model works; refresh pulls new announcements)", Category: "Agent"},
	{Name: "/mouse", Hint: "— toggle mouse capture (on = wheel scroll + clicks, drag to copy)", Category: "Display"},
	{Name: "/pwd", Hint: "— print working directory", Category: "Session"},
	{Name: "/quit", Hint: "— exit", Keybind: "ctrl+c ctrl+c", Category: "App"},
	{Name: "/rename", Hint: "[title] — retitle this session", Category: "Session"},
	{Name: "/report", Hint: "— bug-report bundle: prefilled GitHub-issue link + copy-pastable environment snippet (terminal, theme, versions)", Category: "App"},
	{Name: "/resume", Hint: "[id] — resume a previous session", Category: "Session"},
	{Name: "/schedule", Hint: "@every 10m|<@at time> <prompt> — schedule a wakeup turn; list | cancel <n>", Category: "Session"},
	{Name: "/tasks", Hint: "[id] — background subagents: focus the dock, or open one subagent's live view", Keybind: "ctrl+t", Category: "Session"},
	{Name: "/theme", Hint: "[light|dark|auto] — color scheme (bare opens the switcher)", Category: "Display"},
	{Name: "!cmd", Hint: "— run a shell command locally; output lands in the transcript and the conversation", Category: "App"},
}

func sortEntries(es []registryEntry) {
	sort.Slice(es, func(i, j int) bool { return es[i].Name < es[j].Name })
}

// slashRegistry returns the registry entries that name a slash command,
// sorted by name (the canonical order for help and completion).
func slashRegistry() []registryEntry {
	var out []registryEntry
	for _, e := range registry {
		if strings.HasPrefix(e.Name, "/") {
			out = append(out, e)
		}
	}
	sortEntries(out)
	return out
}

// registryFind returns the entry for a slash command name (nil for "!cmd"
// and unknown names).
func registryFind(name string) *registryEntry {
	for i := range registry {
		if registry[i].Name == name {
			return &registry[i]
		}
	}
	return nil
}

// dispatches reports whether the command switch routes name to a real
// handler rather than the unknown-command error. It is a probe — meant for
// tests — that runs the bare command on a scratch model and checks the
// transcript.
func (m *model) dispatches(name string) bool {
	before := len(m.blocks)
	m.command(name)
	for _, b := range m.blocks[before:] {
		if strings.Contains(b.text, "unknown command") {
			return false
		}
	}
	return true
}

// helpText renders /help from the registry plus the palette's keybind hints:
// slash commands first (sorted), then the keybindings roster. Nothing here is
// hand-maintained anymore — every line comes from one of the two tables.
func helpText() string {
	var b strings.Builder
	for _, e := range slashRegistry() {
		b.WriteString(e.Name + " " + e.Hint + "\n")
	}
	b.WriteString(palHintRewind + " — " + palDescRewind + "\n")
	b.WriteString("!cmd " + registryFind("!cmd").Hint + "\n")
	b.WriteString("tab — complete")
	for _, hint := range []string{
		"ctrl+k — clear the conversation",
		"ctrl+t — focus the subagents dock (↑/↓ select, enter opens, esc backs out)",
		palHintThinking + " — toggle thinking tokens",
		"ctrl+e — expand the last tool result",
		"ctrl+j / shift+enter — newline",
		"ctrl+v — paste image",
		"esc — interrupt the agent",
		"esc esc (idle) — " + palDescRewind + " (↑/↓ browse, enter rewinds, f forks)",
		"while busy with queued messages: ↑/↓ select, del removes",
		"PgUp/PgDn — scroll · wheel — scroll · drag — select/copy text",
		palHintQuit + " — quit",
	} {
		b.WriteString(" · " + hint)
	}
	return b.String()
}
