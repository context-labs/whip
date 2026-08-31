package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	maxConfigBytes  = 1 << 20
	maxCommandBytes = 32 << 10
	maxActions      = 128
	maxTimeout      = 10 * time.Minute
)

type configFlavor int

const (
	flavorPlugin configFlavor = iota
	flavorProject
)

// LoadOptions control hook discovery. User plugins are always included;
// IncludeProject gates every project-owned executable configuration.
type LoadOptions struct {
	WorkingDir     string
	HomeDir        string
	IncludeProject bool
}

// Manager is an immutable, concurrent-safe set of normalized hook commands.
type Manager struct {
	actions        map[Event][]action
	warnings       []string
	disabled       bool
	projectEnabled bool
}

// Load discovers user plugins first, project plugins second, and the project's
// hook file last. Files and plugin directories are sorted
// so command order is stable across filesystems.
func Load(opts LoadOptions) *Manager {
	m := &Manager{
		actions:        make(map[Event][]action),
		projectEnabled: opts.IncludeProject,
	}
	if os.Getenv("WHIP_DISABLE_HOOKS") == "1" {
		m.disabled = true
		return m
	}
	if opts.WorkingDir == "" {
		opts.WorkingDir, _ = os.Getwd()
	}
	if opts.HomeDir == "" {
		opts.HomeDir, _ = os.UserHomeDir()
	}

	seen := map[string]bool{}
	if opts.HomeDir != "" {
		m.loadPlugins(filepath.Join(opts.HomeDir, ".agents", "plugins"), "user plugin", seen)
	}
	if opts.IncludeProject && opts.WorkingDir != "" {
		m.loadPlugins(filepath.Join(opts.WorkingDir, ".agents", "plugins"), "project plugin", seen)
		m.loadFile(
			filepath.Join(opts.WorkingDir, ".whip", "hooks.json"),
			"project hook",
			opts.WorkingDir,
			flavorProject,
			seen,
		)
	}
	return m
}

// Entries returns loaded commands in execution order.
func (m *Manager) Entries() []Entry {
	if m == nil {
		return []Entry{}
	}
	out := make([]Entry, 0, m.Count())
	for _, event := range eventOrder {
		for _, a := range m.actions[event] {
			out = append(out, Entry{
				Event:   event,
				Source:  a.source,
				Matcher: a.matcher.String(),
				Command: a.command,
			})
		}
	}
	return out
}

// Warnings returns a defensive copy of discovery and validation problems.
func (m *Manager) Warnings() []string {
	if m == nil {
		return []string{}
	}
	return append([]string{}, m.warnings...)
}

// Count returns the number of runnable commands.
func (m *Manager) Count() int {
	if m == nil {
		return 0
	}
	n := 0
	for _, actions := range m.actions {
		n += len(actions)
	}
	return n
}

// Disabled reports whether WHIP_DISABLE_HOOKS=1 suppressed discovery.
func (m *Manager) Disabled() bool { return m != nil && m.disabled }

// ProjectEnabled reports whether project hook files were trusted and loaded.
func (m *Manager) ProjectEnabled() bool { return m != nil && m.projectEnabled }

func (m *Manager) loadPlugins(base, label string, seen map[string]bool) {
	entries, err := os.ReadDir(base)
	if err != nil {
		if !os.IsNotExist(err) {
			m.warn("%s: %v", base, err)
		}
		return
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		root := filepath.Join(base, entry.Name())
		m.loadFile(
			filepath.Join(root, "hooks", "hooks.json"),
			label+" "+entry.Name(),
			root,
			flavorPlugin,
			seen,
		)
	}
}

type rawRule struct {
	Matcher *string     `json:"matcher"`
	Hooks   []rawAction `json:"hooks"`
}

type rawAction struct {
	Type      string `json:"type"`
	Command   string `json:"command"`
	Timeout   int    `json:"timeout"`
	Async     bool   `json:"async"`
	OnFailure string `json:"on_failure"`
}

func (m *Manager) loadFile(path, source, pluginRoot string, flavor configFlavor, seen map[string]bool) {
	clean := filepath.Clean(path)
	if seen[clean] {
		return
	}
	seen[clean] = true
	info, err := os.Stat(clean)
	if err != nil {
		if !os.IsNotExist(err) {
			m.warn("%s: %v", clean, err)
		}
		return
	}
	if info.Size() > maxConfigBytes {
		m.warn("%s: config exceeds %d bytes", clean, maxConfigBytes)
		return
	}
	data, err := os.ReadFile(clean)
	if err != nil {
		m.warn("%s: %v", clean, err)
		return
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		m.warn("%s: invalid json: %v", clean, err)
		return
	}
	events := root
	if wrapped, ok := root["hooks"]; ok {
		if err := json.Unmarshal(wrapped, &events); err != nil {
			m.warn("%s: hooks must be an object: %v", clean, err)
			return
		}
	}

	keys := make([]string, 0, len(events))
	for key := range events {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		event, ok := parseEvent(key)
		if !ok {
			if key != "version" {
				m.warn("%s: unsupported event %q", clean, key)
			}
			continue
		}
		var rules []rawRule
		if err := json.Unmarshal(events[key], &rules); err != nil {
			m.warn("%s: event %s must be an array: %v", clean, event, err)
			continue
		}
		for ruleIndex, rule := range rules {
			match, err := compileMatcher(rule.Matcher, flavor)
			if err != nil {
				m.warn("%s: %s rule %d: %v", clean, event, ruleIndex+1, err)
				continue
			}
			for actionIndex, raw := range rule.Hooks {
				if m.Count() >= maxActions {
					m.warn("hook limit reached (%d); remaining commands skipped", maxActions)
					return
				}
				a, ok := normalizeAction(
					raw,
					event,
					match,
					source,
					pluginRoot,
					flavor,
				)
				if !ok {
					m.warn("%s: %s rule %d action %d is invalid", clean, event, ruleIndex+1, actionIndex+1)
					continue
				}
				m.actions[event] = append(m.actions[event], a)
			}
		}
	}
}

func normalizeAction(
	raw rawAction,
	event Event,
	match matcher,
	source string,
	pluginRoot string,
	flavor configFlavor,
) (action, bool) {
	typ := raw.Type
	if typ == "" {
		typ = "command"
	}
	if typ != "command" || raw.Async || raw.Command == "" || len(raw.Command) > maxCommandBytes {
		return action{}, false
	}
	if raw.Timeout < 0 {
		return action{}, false
	}
	timeout := 30 * time.Second
	if flavor == flavorProject {
		timeout = 60 * time.Second
	}
	if raw.Timeout > 0 {
		seconds := min(raw.Timeout, int(maxTimeout/time.Second))
		timeout = time.Duration(seconds) * time.Second
	}
	onFailureBlock := false
	if event == PreToolUse {
		switch raw.OnFailure {
		case "", "allow":
		case "block":
			onFailureBlock = true
		default:
			return action{}, false
		}
	}
	return action{
		event:          event,
		source:         source,
		pluginRoot:     pluginRoot,
		command:        raw.Command,
		timeout:        timeout,
		matcher:        match,
		onFailureBlock: onFailureBlock,
	}, true
}

func parseEvent(name string) (Event, bool) {
	normalized := strings.ToLower(strings.ReplaceAll(name, "_", ""))
	for _, event := range eventOrder {
		if strings.ToLower(string(event)) == normalized {
			return event, true
		}
	}
	return "", false
}

type matcher struct {
	raw string
	re  *regexp.Regexp
}

func (m matcher) Match(s string) bool { return m.re == nil || m.re.MatchString(s) }

func (m matcher) String() string {
	if m.raw == "" {
		return "<all>"
	}
	return m.raw
}

func compileMatcher(raw *string, flavor configFlavor) (matcher, error) {
	if raw == nil || *raw == "" {
		return matcher{}, nil
	}
	pattern := *raw
	if flavor == flavorProject && pattern == "*" {
		return matcher{raw: pattern}, nil
	}
	if flavor == flavorProject && len(pattern) >= 2 && strings.HasPrefix(pattern, "/") && strings.HasSuffix(pattern, "/") {
		pattern = pattern[1 : len(pattern)-1]
	} else if flavor == flavorProject && !strings.ContainsAny(pattern, `\.^$|?*+()[]{}`) {
		pattern = "^(?:" + regexp.QuoteMeta(pattern) + ")$"
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return matcher{}, fmt.Errorf("invalid matcher %q: %w", *raw, err)
	}
	return matcher{raw: *raw, re: re}, nil
}

func (m *Manager) warn(format string, args ...any) {
	m.warnings = append(m.warnings, fmt.Sprintf(format, args...))
}
