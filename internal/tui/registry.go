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
	{Name: "/auth", Hint: "<provider> [key] — connect a provider (bare = guided login)", Category: "Agent"},
	{Name: "/agents", Hint: "[list|stop <id>|delete <id>|budget <id> <kind> <limit>|revoke <capability-id>] — inspect or control recursive agents", Category: "Session"},
	{Name: "/browser", Hint: "[status|driver rod|driver chromedp] — inspect or select browser automation", Category: "Session"},
	{Name: "/cd", Hint: "[dir] — change working directory (bare prints it)", Category: "Session"},
	{Name: "/clear", Hint: "— reset conversation", Category: "Session"},
	{Name: "/compact", Hint: "[model]|off|retry|log — compact the conversation now", Category: "Session"},
	{Name: "/computer-use", Hint: "[task] — drive this Mac; allow|deny <app>", Category: "Agent"},
	{Name: "/context-doctor", Hint: "— audit fresh-session injections and their token cost", Category: "Session"},
	{Name: "/effort", Hint: "[level] — reasoning effort: off·low·medium·high", Category: "Agent"},
	{Name: "/export", Hint: "[path] — write the transcript to a markdown file", Category: "Session"},
	{Name: "/fork", Hint: "[name] — copy the conversation into a new session", Category: "Session"},
	{Name: "/goal", Hint: "<text> — keep working until the goal is met (resume | clear)", Category: "Session"},
	{Name: "/goal-from-context", Hint: "[n] — form a goal from recent messages and pursue it", Category: "Session"},
	{Name: "/help", Hint: "— show all commands and keybindings", Category: "App"},
	{Name: "/lsp", Hint: "[status] — inspect language servers", Category: "Session"},
	{Name: "/mcp", Hint: "[name] [reconnect|enable|disable] | import <claude|codex> <on|off> — MCP status and imports", Category: "Session"},
	{Name: "/me", Hint: "— edit your standing instructions in $EDITOR", Category: "Agent"},
	{Name: "/memory", Hint: "[n] — list saved memories; mark entry n done", Category: "Session"},
	{Name: "/model", Hint: "<name> [provider] — switch model (refresh pulls the catalog)", Category: "Agent"},
	{Name: "/model-for-session", Hint: "<name> — switch model for this session only", Category: "Agent"},
	{Name: "/mouse", Hint: "— toggle mouse capture", Category: "Display"},
	{Name: "/pwd", Hint: "— print working directory", Category: "Session"},
	{Name: "/quit", Hint: "— exit", Keybind: "ctrl+c ctrl+c", Category: "App"},
	{Name: "/rename", Hint: "[title] — retitle this session", Category: "Session"},
	{Name: "/report", Hint: "— bug report: issue link + environment snippet", Category: "App"},
	{Name: "/resume", Hint: "[id] — resume a previous session", Category: "Session"},
	{Name: "/rewind", Hint: "[message-index] — rewind conversation and workspace state", Category: "Session"},
	{Name: "/schedule", Hint: "@every 10m|@at <time> <prompt> — schedule a wakeup; list | cancel", Category: "Session"},
	{Name: "/theme", Hint: "[light|dark|auto] — color scheme", Category: "Display"},
	{Name: "/ui-mode", Hint: "[default|opencode] — switch rendering mode", Category: "Display"},
	{Name: "/repl", Hint: "— toggle the live Starlark REPL panel in the opencode sidebar (ctrl+x r)", Category: "Display"},
	{Name: "!cmd", Hint: "— run a shell command; output joins the conversation", Category: "App"},
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
		"ctrl+t — focus the recursive agents dock (↑/↓ select, enter opens, esc returns to root)",
		palHintThinking + " — toggle thinking tokens",
		"ctrl+e — expand the last tool result",
		"ctrl+j / shift+enter — newline",
		"ctrl+v — paste image",
		"esc — interrupt the agent",
		"esc esc (idle) — " + palDescRewind + " (↑/↓ browse, enter rewinds, f forks)",
		"while busy: enter steers the running turn (lands as the next turn if it ends first)",
		"PgUp/PgDn — scroll · wheel — scroll · drag — select/copy text",
		palHintQuit + " — quit",
	} {
		b.WriteString(" · " + hint)
	}
	return b.String()
}
