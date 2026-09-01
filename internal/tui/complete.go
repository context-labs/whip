package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// cand is one completion candidate with an optional description.
type cand struct {
	Text string
	Desc string
}

// commands is the tab-completion table, built from the registry so a command
// can never be dispatchable but uncompletable (or vice versa).
var commands = completionTable()

// completionTable derives the tab-completion table from the registry, so a
// command can never be dispatchable but uncompletable (or vice versa).
func completionTable() []cand {
	reg := slashRegistry()
	out := make([]cand, 0, len(reg))
	for _, e := range reg {
		out = append(out, cand{e.Name, e.Hint})
	}
	return out
}

// execNow lists commands the menu runs immediately on enter (they act
// sensibly with no arguments); others insert themselves for arguments.
var execNow = map[string]bool{
	"/clear": true, "/compact": true, "/computer-use": true, "/computer": true, "/context-doctor": true, "/effort": true, "/goal": true, "/goal-from-context": true, "/help": true,
	"/mcp": true, "/model": true, "/mouse": true, "/pwd": true, "/quit": true, "/report": true, "/resume": true, "/subagents": true, "/tasks": true,
}

// completions splits val into an untouched head and candidates for its last
// token: slash commands, /model or /effort arguments, $skills, or filesystem
// paths. A newline (ctrl+j or pasted text) also starts a fresh token, so
// $skills and @files complete on later lines too. nil efforts uses the
// default /effort candidates.
func completions(val string, models, providers, skillCands, efforts []cand) (head string, cands []cand) {
	if efforts == nil {
		efforts = effortCands
	}
	i := strings.LastIndexAny(val, " \n")
	head, token := val[:i+1], val[i+1:]
	fields := strings.Fields(head)
	switch {
	case strings.HasPrefix(val, "/") && len(fields) == 0:
		cands = filterPrefix(commands, token)
	case len(fields) == 1 && (fields[0] == "/model" || fields[0] == "/model-for-session"):
		cands = filterFuzzy(append([]cand{{"refresh", "refetch provider model catalogs"}}, models...), token)
	case len(fields) == 2 && (fields[0] == "/model" || fields[0] == "/model-for-session") && fields[1] != "refresh":
		cands = filterFuzzy(providers, token)
	case len(fields) == 1 && fields[0] == "/effort":
		cands = filterPrefix(efforts, token)
	case len(fields) == 1 && fields[0] == "/compact":
		cands = filterPrefix(append([]cand{{"off", "compact with the current model"}}, models...), token)
	case len(fields) == 2 && fields[0] == "/compact":
		cands = filterPrefix(providers, token)
	case strings.HasPrefix(token, "$"): // codex-style skill invocation
		cands = filterPrefix(skillCands, token)
	case strings.HasPrefix(token, "@"):
		// @file mentions: path-like queries (with a separator, ~, or leading
		// dot) complete like paths; bare words fuzzy-match the recursive
		// index so "@roadmap" finds docs/roadmap.md without the full path.
		if q := token[1:]; isPathQuery(q) {
			for _, c := range mentionPathMatches(q) {
				cands = append(cands, cand{"@" + c.Text, c.Desc})
			}
		} else {
			for _, f := range fuzzyFiles(q, menuRows) {
				cands = append(cands, cand{"@" + f, ""})
			}
		}
	case strings.HasPrefix(val, "/"): // other slash-command args: nothing to complete
	default:
		cands = pathMatches(token)
	}
	sort.Slice(cands, func(a, b int) bool { return cands[a].Text < cands[b].Text })
	return head, cands
}

func filterPrefix(all []cand, prefix string) []cand {
	var out []cand
	for _, c := range all {
		if strings.HasPrefix(c.Text, prefix) {
			out = append(out, c)
		}
	}
	return out
}

// filterFuzzy matches candidates tiered like fuzzyFiles (substring, then
// subsequence), best tier first. An empty prefix keeps the original order.
func filterFuzzy(all []cand, q string) []cand {
	if q == "" {
		return append([]cand(nil), all...)
	}
	type hit struct {
		c    cand
		tier int
	}
	var hits []hit
	for _, c := range all {
		if tier := matchTier(c.Text, q); tier >= 0 {
			hits = append(hits, hit{c, tier})
		}
	}
	sort.SliceStable(hits, func(a, b int) bool { return hits[a].tier < hits[b].tier })
	out := make([]cand, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.c)
	}
	return out
}

// isPathQuery reports whether an @mention query looks like a path (has a
// separator, ~, or leading dot) and should use plain glob completion rather
// than the recursive fuzzy index.
func isPathQuery(q string) bool {
	return q == "" || strings.ContainsAny(q, "/\\") || strings.HasPrefix(q, "~") || strings.HasPrefix(q, ".")
}

// mentionPathMatches globs an @mention path query against the mention root
// (not the process cwd): absolute and ~ queries glob as-is; relative ones are
// joined to the root and returned root-relative, with dirs keeping their
// trailing slash.
func mentionPathMatches(q string) []cand {
	if filepath.IsAbs(q) || q == "~" || strings.HasPrefix(q, "~/") {
		return pathMatches(q)
	}
	root, err := currentRoot()
	if err != nil {
		return nil
	}
	var out []cand
	for _, c := range pathMatches(filepath.Join(root, q)) {
		dir := strings.HasSuffix(c.Text, "/")
		if rel, err := filepath.Rel(root, strings.TrimSuffix(c.Text, "/")); err == nil {
			c.Text = filepath.ToSlash(rel)
			if dir {
				c.Text += "/"
			}
			out = append(out, c)
		}
	}
	return out
}

func pathMatches(prefix string) []cand {
	p := prefix
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = home + p[1:]
		}
	}
	matches, _ := filepath.Glob(p + "*")
	var out []cand
	for _, m := range matches {
		if fi, err := os.Stat(m); err == nil && fi.IsDir() {
			out = append(out, cand{m + "/", "dir"})
		} else {
			out = append(out, cand{m, ""})
		}
	}
	return out
}
