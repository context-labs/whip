package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

// Discovery reads the catalogs the manager already holds; it never touches a
// server. A catalog is hundreds of tools at most, so every query is a linear
// scan over the standard library, with no index to keep current.
const (
	SearchDefaultLimit = 20
	SearchMaxLimit     = 100
	summaryLimit       = 160
	suggestionLimit    = 3
)

// Match is one search hit: a tool located by server and name with a
// one-line summary and no schema; Describe has the schema.
type Match struct {
	Server  string `json:"server"`
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Summary string `json:"summary"`
	rank    int    // lower is better: the sum over query tokens of the field each one hit
}

// Search ranks the cached catalogs against a whitespace-separated query.
// Every token must appear in the tool's name, title, description or top-level
// schema property names; a name hit outranks a title hit outranks the rest.
// An empty server searches every ready server; a named server that is not
// ready is an error, as it is for ListTools.
func (m *Manager) Search(serverName, query string, limit int) ([]Match, error) {
	tokens := searchTokens(query)
	if len(tokens) == 0 {
		return nil, errors.New("MCP search requires a query")
	}
	if limit <= 0 {
		limit = SearchDefaultLimit
	}
	limit = min(limit, SearchMaxLimit)
	names := []string{serverName}
	if serverName == "" {
		names = names[:0]
		for _, status := range m.Statuses() {
			if status.Status == StatusReady {
				names = append(names, status.Name)
			}
		}
		sort.Strings(names)
	}
	var matches []Match
	for _, name := range names {
		listed, err := m.ListTools(name)
		if err != nil {
			if serverName == "" {
				continue // the server left the ready set between the status read and the listing
			}
			return nil, err
		}
		matches = append(matches, rankTools(name, listed, tokens)...)
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].rank != matches[j].rank {
			return matches[i].rank < matches[j].rank
		}
		if matches[i].Server != matches[j].Server {
			return matches[i].Server < matches[j].Server
		}
		return matches[i].Name < matches[j].Name
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return matches, nil
}

// Describe returns one tool's full entry. An unknown name is an error that
// suggests the nearest names, because calls need exact names and a near miss
// is the common failure.
func (m *Manager) Describe(serverName, toolName string) (Tool, error) {
	listed, err := m.ListTools(serverName)
	if err != nil {
		return Tool{}, err
	}
	for _, tool := range listed {
		if tool.Name == toolName {
			return tool, nil
		}
	}
	hint := ""
	if nearest := nearestNames(listed, toolName); len(nearest) > 0 {
		hint = "; nearest: " + strings.Join(nearest, ", ")
	}
	return Tool{}, fmt.Errorf("MCP tool %q not found on server %q%s", toolName, serverName, hint)
}

func searchTokens(query string) []string {
	return strings.Fields(strings.ToLower(query))
}

// rankTools keeps the tools every token matches and scores each by the
// fields the tokens hit: 0 for the name, 1 for the title, 2 for the
// description or a schema property name.
func rankTools(server string, listed []Tool, tokens []string) []Match {
	var matches []Match
	for _, tool := range listed {
		name, title := strings.ToLower(tool.Name), strings.ToLower(tool.Title)
		body := strings.ToLower(tool.Description + " " + strings.Join(schemaProperties(tool.InputSchema), " "))
		rank, matched := 0, true
		for _, token := range tokens {
			switch {
			case strings.Contains(name, token):
			case strings.Contains(title, token):
				rank++
			case strings.Contains(body, token):
				rank += 2
			default:
				matched = false
			}
			if !matched {
				break
			}
		}
		if matched {
			matches = append(matches, Match{Server: server, Name: tool.Name, Title: tool.Title, Summary: summarize(tool.Description), rank: rank})
		}
	}
	return matches
}

// schemaProperties returns a schema's top-level property names, which are
// often the best keywords a tool has (run_id, trace_id, project_id).
func schemaProperties(schema json.RawMessage) []string {
	var decoded struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if json.Unmarshal(schema, &decoded) != nil {
		return nil
	}
	return slices.Sorted(maps.Keys(decoded.Properties))
}

// summarize is the first line of a description, cut at summaryLimit runes,
// with a trailing ellipsis whenever anything was left out.
func summarize(description string) string {
	line := strings.TrimSpace(description)
	cut := false
	if index := strings.IndexByte(line, '\n'); index >= 0 {
		line, cut = strings.TrimSpace(line[:index]), true
	}
	if utf8.RuneCountInString(line) > summaryLimit {
		runes := []rune(line)
		line, cut = strings.TrimSpace(string(runes[:summaryLimit])), true
	}
	if cut {
		line += "…"
	}
	return line
}

// nearestNames ranks the catalog's names by how many parts of the requested
// name they contain, splitting on the separators tool names use; a name
// within two edits of the request (a typo) counts as sharing one part.
func nearestNames(listed []Tool, requested string) []string {
	lower := strings.ToLower(requested)
	parts := strings.FieldsFunc(lower, func(r rune) bool { return r == '_' || r == '-' || r == '.' || r == '/' || r == ' ' })
	type scored struct {
		name  string
		score int
	}
	var candidates []scored
	for _, tool := range listed {
		name := strings.ToLower(tool.Name)
		score := 0
		for _, part := range parts {
			if strings.Contains(name, part) {
				score++
			}
		}
		if score == 0 && editDistance(name, lower) <= 2 {
			score = 1
		}
		if score > 0 {
			candidates = append(candidates, scored{tool.Name, score})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].name < candidates[j].name
	})
	names := make([]string, 0, min(len(candidates), suggestionLimit))
	for _, candidate := range candidates[:min(len(candidates), suggestionLimit)] {
		names = append(names, candidate.name)
	}
	return names
}

// editDistance is the Levenshtein distance over runes.
func editDistance(a, b string) int {
	left, right := []rune(a), []rune(b)
	previous := make([]int, len(right)+1)
	current := make([]int, len(right)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(left); i++ {
		current[0] = i
		for j := 1; j <= len(right); j++ {
			cost := 1
			if left[i-1] == right[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
		}
		previous, current = current, previous
	}
	return previous[len(right)]
}
