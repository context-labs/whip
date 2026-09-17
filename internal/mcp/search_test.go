package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func searchFixture() []Tool {
	return []Tool{
		{Name: "halo_run_list", Description: "List Halo runs for a project.\nSecond line the summary must drop.", InputSchema: json.RawMessage(`{"type":"object","properties":{"project_id":{"type":"string"}}}`)},
		{Name: "list_traces", Title: "Traces", Description: "Page through traces; filter by run.", InputSchema: json.RawMessage(`{"type":"object","properties":{"run_id":{"type":"string"}}}`)},
		{Name: "whoami", Description: "Current team and project.", InputSchema: json.RawMessage(`{}`)},
	}
}

func names(matches []Match) []string {
	out := make([]string, len(matches))
	for i, match := range matches {
		out[i] = match.Name
	}
	return out
}

// Every token must hit; a name hit outranks a title hit outranks a
// description or schema property hit; ties fall back to the name.
func TestRankToolsRequiresEveryTokenAndPrefersNames(t *testing.T) {
	tools := searchFixture()
	if got := names(rankTools("s", tools, searchTokens("Halo RUN"))); strings.Join(got, ",") != "halo_run_list" {
		t.Fatalf("two-token query = %v", got)
	}
	if got := names(rankTools("s", tools, searchTokens("run"))); strings.Join(got, ",") != "halo_run_list,list_traces" {
		t.Fatalf("name hit must lead the description hit: %v", got)
	}
	if got := names(rankTools("s", tools, searchTokens("project"))); strings.Join(got, ",") != "halo_run_list,whoami" {
		t.Fatalf("property and description hits tie and sort by name: %v", got)
	}
	if got := names(rankTools("s", tools, searchTokens("traces"))); strings.Join(got, ",") != "list_traces" {
		t.Fatalf("title/name hit = %v", got)
	}
	if got := rankTools("s", tools, searchTokens("nothing-here")); len(got) != 0 {
		t.Fatalf("unmatched query returned %v", got)
	}
	if len(searchTokens("   ")) != 0 {
		t.Fatal("blank query must have no tokens")
	}
	match := rankTools("s", tools, searchTokens("halo"))[0]
	if match.Server != "s" || match.Summary != "List Halo runs for a project.…" {
		t.Fatalf("match=%+v", match)
	}
}

func TestSummarizeMarksEverythingItLeavesOut(t *testing.T) {
	if got := summarize("  short and whole  "); got != "short and whole" {
		t.Fatalf("short=%q", got)
	}
	if got := summarize("first line\nsecond line"); got != "first line…" {
		t.Fatalf("multi-line=%q", got)
	}
	long := strings.Repeat("é", summaryLimit+5)
	if got := summarize(long); got != strings.Repeat("é", summaryLimit)+"…" {
		t.Fatalf("long line cut on runes: %d chars", len(got))
	}
}

func TestNearestNamesSuggestSharedPartsThenPrefixes(t *testing.T) {
	tools := []Tool{{Name: "echo"}, {Name: "image"}, {Name: "large"}, {Name: "mutate"}, {Name: "mutate.other"}}
	if got := nearestNames(tools, "mutate_other"); strings.Join(got, ",") != "mutate.other,mutate" {
		t.Fatalf("shared parts = %v", got)
	}
	if got := nearestNames(tools, "eco"); strings.Join(got, ",") != "echo" {
		t.Fatalf("typo fallback = %v", got)
	}
	if editDistance("kitten", "sitting") != 3 || editDistance("", "ab") != 2 || editDistance("same", "same") != 0 {
		t.Fatal("edit distance is off")
	}
	if got := nearestNames(tools, "zzz"); len(got) != 0 {
		t.Fatalf("no candidate must mean no suggestion: %v", got)
	}
}

// Without servers, search is empty rather than an error, a blank query and an
// unknown server are errors, and describe reports the missing server.
func TestSearchAndDescribeWithoutReadyServers(t *testing.T) {
	m := NewManager(nil)
	defer m.Close()
	if matches, err := m.Search("", "anything", 0); err != nil || len(matches) != 0 {
		t.Fatalf("search with no servers = %v, %v", matches, err)
	}
	if _, err := m.Search("", "  ", 0); err == nil {
		t.Fatal("blank query accepted")
	}
	if _, err := m.Search("nope", "anything", 0); err == nil {
		t.Fatal("unknown server accepted")
	}
	if _, err := m.Describe("nope", "tool"); err == nil {
		t.Fatal("describe on unknown server succeeded")
	}
}
