package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config/modelsdev"
)

func TestUpdateRejectsUnsafeMetadataWithoutReplacingArtifacts(t *testing.T) {
	for _, test := range []struct {
		name, want string
		change     func(map[string]any)
	}{
		{"provider identity", "mismatched ID", func(p map[string]any) { p["id"] = "another-provider" }},
		{"provider name", "invalid or missing", func(p map[string]any) { p["name"] = "unsafe\nname" }},
		{"environment injection", "invalid environment name", func(p map[string]any) { p["env"] = []string{"KEY=secret"} }},
		{"insecure endpoint", "invalid API URL", func(p map[string]any) { p["api"] = "http://example.com/v1" }},
		{"endpoint credentials", "invalid API URL", func(p map[string]any) { p["api"] = "https://user:secret@example.com/v1" }},
		{"endpoint query", "invalid API URL", func(p map[string]any) { p["api"] = "https://example.com/v1?key=secret" }},
		{"model identity", "mismatched ID", func(p map[string]any) { upstreamTestModel(p)["id"] = "another-model" }},
		{"model name", "invalid model identity", func(p map[string]any) { upstreamTestModel(p)["name"] = "\x1b[2J" }},
		{"negative limit", "invalid model limits", func(p map[string]any) { upstreamTestModel(p)["limit"] = map[string]int{"context": -1} }},
		{"oversized limit", "invalid model limits", func(p map[string]any) { upstreamTestModel(p)["limit"] = map[string]int{"output": 1000000001} }},
		{"capability control character", "invalid capability value", func(p map[string]any) {
			upstreamTestModel(p)["modalities"] = map[string][]string{"input": {"text\nimage"}}
		}},
		{"negative input price", "input cost", func(p map[string]any) { upstreamTestModel(p)["cost"] = map[string]int{"input": -1} }},
		{"negative output price", "output cost", func(p map[string]any) { upstreamTestModel(p)["cost"] = map[string]int{"output": -1} }},
		{"negative cache price", "cache cost", func(p map[string]any) { upstreamTestModel(p)["cost"] = map[string]int{"cache_read": -1} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			o := options{input: filepath.Join(dir, "upstream.json"), snapshot: filepath.Join(dir, "catalog.json"), environment: filepath.Join(dir, "environment.ts")}
			input := fixture(t)
			writeModelgenFile(t, o.input, input)
			if err := run(t.Context(), o, io.Discard); err != nil {
				t.Fatal(err)
			}
			beforeCatalog, err := os.ReadFile(o.snapshot)
			if err != nil {
				t.Fatal(err)
			}
			beforeEnvironment, err := os.ReadFile(o.environment)
			if err != nil {
				t.Fatal(err)
			}
			var upstream map[string]map[string]any
			if err := json.Unmarshal(input, &upstream); err != nil {
				t.Fatal(err)
			}
			test.change(upstream["cerebras"])
			input, err = json.Marshal(upstream)
			if err != nil {
				t.Fatal(err)
			}
			writeModelgenFile(t, o.input, input)
			if err := run(t.Context(), o, io.Discard); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q error, got %v", test.want, err)
			}
			for path, before := range map[string][]byte{o.snapshot: beforeCatalog, o.environment: beforeEnvironment} {
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatalf("rejected update changed %s: %v", path, err)
				}
			}
		})
	}
}

func upstreamTestModel(provider map[string]any) map[string]any {
	return provider["models"].(map[string]any)["test-model"].(map[string]any)
}

func writeModelgenFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestOfflineCheckRejectsTamperedProvenanceAndPricing(t *testing.T) {
	for _, test := range []struct {
		name, want string
		change     func(*modelsdev.Snapshot)
	}{
		{"source", "provenance", func(s *modelsdev.Snapshot) { s.SourceURL = "https://example.com/api.json" }},
		{"digest", "SHA-256", func(s *modelsdev.Snapshot) { s.InputSHA256 = "not-a-digest" }},
		{"retrieval date", "retrieval date", func(s *modelsdev.Snapshot) { s.RetrievedAt = "yesterday" }},
		{"unexpected provider", "outside Whip policy", func(s *modelsdev.Snapshot) { s.Providers["unexpected"] = modelsdev.ProviderInfo{} }},
		{"pricing", "invalid pricing", func(s *modelsdev.Snapshot) {
			p := s.Providers["cerebras"]
			m := p.Models["test-model"]
			m.Pricing = &modelsdev.Pricing{Prompt: "-0.1"}
			p.Models["test-model"] = m
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot, err := normalize(fixture(t), modelsdev.Snapshot{}, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			test.change(&snapshot)
			data, err := encode(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			o := options{snapshot: filepath.Join(dir, "catalog.json"), environment: filepath.Join(dir, "environment.ts"), check: true}
			writeModelgenFile(t, o.snapshot, data)
			writeModelgenFile(t, o.environment, environment(snapshot))
			if err := run(t.Context(), o, io.Discard); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q error, got %v", test.want, err)
			}
		})
	}
}

func TestModelgenMainOfflineRoundTrip(t *testing.T) {
	dir := t.TempDir()
	input, catalog, env := filepath.Join(dir, "upstream.json"), filepath.Join(dir, "catalog.json"), filepath.Join(dir, "environment.ts")
	writeModelgenFile(t, input, fixture(t))
	previousArgs, previousFlags := os.Args, flag.CommandLine
	t.Cleanup(func() { os.Args, flag.CommandLine = previousArgs, previousFlags })
	for _, operation := range [][]string{{"-input", input}, {"-check"}} {
		os.Args = append([]string{"modelgen", "-out", catalog, "-environment-out", env}, operation...)
		flag.CommandLine = flag.NewFlagSet("modelgen", flag.ContinueOnError)
		main()
	}
	data, err := os.ReadFile(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := modelsdev.Decode(data); err != nil {
		t.Fatalf("generated catalog is not consumable: %v", err)
	}
}

func TestArtifactStagingFailureRetainsCatalogAndCleansTemps(t *testing.T) {
	for _, destination := range []string{"blocked-parent", "existing-directory"} {
		t.Run(destination, func(t *testing.T) {
			dir := t.TempDir()
			catalog := filepath.Join(dir, "catalog.json")
			writeModelgenFile(t, catalog, []byte("original"))
			var env string
			if destination == "blocked-parent" {
				blocked := filepath.Join(dir, "blocked")
				writeModelgenFile(t, blocked, []byte("file"))
				env = filepath.Join(blocked, "environment.ts")
			} else {
				env = filepath.Join(dir, "environment.ts")
				if err := os.Mkdir(env, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if err := replaceArtifacts([]artifact{{catalog, []byte("new")}, {env, []byte("new")}}); err == nil {
				t.Fatal("invalid destination accepted")
			}
			data, err := os.ReadFile(catalog)
			if err != nil || string(data) != "original" {
				t.Fatalf("failed publication replaced catalog: %q, %v", data, err)
			}
			temps, err := filepath.Glob(filepath.Join(dir, ".modelgen-*"))
			if err != nil || len(temps) != 0 {
				t.Fatalf("temporary artifacts leaked: %v, %v", temps, err)
			}
		})
	}
}

func TestUpdateReportsMetadataChangesAndRetainsExplicitModelOverrides(t *testing.T) {
	dir := t.TempDir()
	o := options{input: filepath.Join(dir, "upstream.json"), snapshot: filepath.Join(dir, "catalog.json"), environment: filepath.Join(dir, "environment.ts")}
	input := fixture(t)
	writeModelgenFile(t, o.input, input)
	if err := run(t.Context(), o, io.Discard); err != nil {
		t.Fatal(err)
	}
	var upstream map[string]map[string]any
	if err := json.Unmarshal(input, &upstream); err != nil {
		t.Fatal(err)
	}
	provider := upstream["cerebras"]
	provider["api"], provider["name"] = "https://cerebras.example/v2", "Updated Cerebras"
	model := upstreamTestModel(provider)
	model["name"] = "Changed model"
	model["reasoning_options"] = []any{map[string]any{"type": "effort", "values": []string{"low", "high"}}}
	models := provider["models"].(map[string]any)
	models["new-model"] = map[string]any{"id": "new-model", "name": "New model"}
	// Whip explicitly retains these routes because the upstream catalog lags.
	delete(upstream["inference"]["models"].(map[string]any), "kimi-k3-fast")
	delete(upstream["inference"]["models"].(map[string]any), "kimi-k3")
	input, err := json.Marshal(upstream)
	if err != nil {
		t.Fatal(err)
	}
	writeModelgenFile(t, o.input, input)
	var log bytes.Buffer
	if err := run(t.Context(), o, &log); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"cerebras upstream endpoint changed", "Whip endpoint preserved", "cerebras: provider metadata changed", "cerebras/new-model: added", "cerebras/test-model: changed", "explicit retained model override", "kimi-k3-fast: removed"} {
		if !strings.Contains(log.String(), want) {
			t.Errorf("update report missing %q: %s", want, log.String())
		}
	}
	data, err := os.ReadFile(o.snapshot)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := modelsdev.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := snapshot.Providers["cerebras"].Models["test-model"].ReasoningEfforts; strings.Join(got, ",") != "low,high" {
		t.Fatalf("explicit upstream efforts lost: %v", got)
	}
}

func TestOfflineCheckDetectsMissingAndUnnormalizedArtifacts(t *testing.T) {
	for _, problem := range []string{"missing catalog", "missing environment", "unnormalized catalog", "input conflicts with check"} {
		t.Run(problem, func(t *testing.T) {
			dir := t.TempDir()
			o := options{input: filepath.Join(dir, "upstream.json"), snapshot: filepath.Join(dir, "catalog.json"), environment: filepath.Join(dir, "environment.ts")}
			writeModelgenFile(t, o.input, fixture(t))
			if err := run(t.Context(), o, io.Discard); err != nil {
				t.Fatal(err)
			}
			o.input, o.check = "", true
			var want string
			switch problem {
			case "missing catalog", "missing environment":
				path := o.snapshot
				if problem == "missing environment" {
					path = o.environment
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				want = "no such file"
			case "unnormalized catalog":
				data, err := os.ReadFile(o.snapshot)
				if err != nil {
					t.Fatal(err)
				}
				var compact bytes.Buffer
				if err := json.Compact(&compact, data); err != nil {
					t.Fatal(err)
				}
				writeModelgenFile(t, o.snapshot, compact.Bytes())
				want = "not normalized"
			case "input conflicts with check":
				o.input = filepath.Join(dir, "upstream.json")
				want = "cannot be combined"
			}
			if err := run(t.Context(), o, io.Discard); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("expected %q error, got %v", want, err)
			}
		})
	}
}
