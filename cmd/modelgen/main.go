// Command modelgen updates the bundled Models.dev subset; ordinary builds are offline.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/config/modelsdev"
	"github.com/context-labs/whip/internal/openaiauth"
)

const maxInput = 32 << 20

var (
	envName       = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
	decimalNumber = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]{1,2})?$`)
)

type options struct {
	input, snapshot, environment string
	check                        bool
}

func main() {
	o := options{}
	flag.StringVar(&o.input, "input", "", "read upstream JSON from a local file instead of downloading")
	flag.StringVar(&o.snapshot, "out", "internal/config/modelsdev/catalog.json", "bundled snapshot path")
	flag.StringVar(&o.environment, "environment-out", "apps/desktop/src/provider-environment.ts", "generated desktop environment path")
	flag.BoolVar(&o.check, "check", false, "validate existing snapshot and generated artifacts without network access")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "modelgen does not accept positional arguments")
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, o, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, o options, log io.Writer) error {
	oldData, readErr := os.ReadFile(o.snapshot)
	var old modelsdev.Snapshot
	if readErr == nil {
		var err error
		old, err = modelsdev.Decode(oldData)
		if err != nil {
			return err
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	if o.check {
		if o.input != "" {
			return errors.New("-check cannot be combined with -input")
		}
		if readErr != nil {
			return readErr
		}
		if err := validate(old, log); err != nil {
			return err
		}
		normalized, err := encode(old)
		if err != nil {
			return err
		}
		if !bytes.Equal(oldData, normalized) {
			return errors.New("models.dev snapshot is not normalized; run task models:update")
		}
		actual, err := os.ReadFile(o.environment)
		if err != nil {
			return err
		}
		if !bytes.Equal(actual, environment(old)) {
			return errors.New("desktop provider environment names drifted; run task models:update")
		}
		fmt.Fprintln(log, "Models.dev snapshot and desktop environment names are current (offline check)")
		return nil
	}
	var input []byte
	if o.input != "" {
		f, err := os.Open(o.input)
		if err != nil {
			return err
		}
		input, err = readBounded(f)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	} else {
		var err error
		input, err = fetch(ctx, &http.Client{Timeout: 30 * time.Second}, modelsdev.SourceURL)
		if err != nil {
			return err
		}
	}
	snapshot, err := normalize(input, old, time.Now().UTC())
	if err != nil {
		return err
	}
	if err := validate(snapshot, log); err != nil {
		return err
	}
	data, err := encode(snapshot)
	if err != nil {
		return err
	}
	changes(log, old, snapshot)
	// Validate and generate every artifact before touching either destination.
	files := []artifact{{o.snapshot, data}, {o.environment, environment(snapshot)}}
	if err := replaceArtifacts(files); err != nil {
		return err
	}
	fmt.Fprintf(log, "Models.dev: %d providers, %d models; input %d bytes, SHA-256 %s\n", len(snapshot.Providers), modelCount(snapshot), len(input), snapshot.InputSHA256)
	return nil
}

func fetch(ctx context.Context, client *http.Client, endpoint string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download Models.dev: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download Models.dev: HTTP %d", response.StatusCode)
	}
	return readBounded(response.Body)
}

func readBounded(reader io.Reader) ([]byte, error) {
	input, err := io.ReadAll(io.LimitReader(reader, maxInput+1))
	if err != nil {
		return nil, err
	}
	if len(input) > maxInput {
		return nil, errors.New("models.dev input exceeds 32 MiB")
	}
	return input, nil
}

type upstreamProvider struct {
	ID     string                   `json:"id"`
	Name   string                   `json:"name"`
	API    string                   `json:"api"`
	Env    []string                 `json:"env"`
	Models map[string]upstreamModel `json:"models"`
}
type upstreamModel struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	ToolCall         *bool  `json:"tool_call"`
	Reasoning        *bool  `json:"reasoning"`
	ReasoningOptions []struct {
		Type   string   `json:"type"`
		Values []string `json:"values"`
	} `json:"reasoning_options"`
	Limit struct {
		Context *int `json:"context"`
		Output  *int `json:"output"`
	} `json:"limit"`
	Modalities struct {
		Input  []string `json:"input"`
		Output []string `json:"output"`
	} `json:"modalities"`
	Cost *struct {
		Input     json.Number `json:"input"`
		Output    json.Number `json:"output"`
		CacheRead json.Number `json:"cache_read"`
	} `json:"cost"`
}

func normalize(input []byte, old modelsdev.Snapshot, now time.Time) (modelsdev.Snapshot, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(input, &raw); err != nil {
		return modelsdev.Snapshot{}, fmt.Errorf("decode upstream catalog: %w", err)
	}
	digest := sha256.Sum256(input)
	snapshot := modelsdev.Snapshot{SchemaVersion: modelsdev.SchemaVersion, SourceURL: modelsdev.SourceURL, InputSHA256: hex.EncodeToString(digest[:]), RetrievedAt: now.Format(time.RFC3339), Providers: map[string]modelsdev.ProviderInfo{}}
	if old.InputSHA256 == snapshot.InputSHA256 {
		snapshot.RetrievedAt = old.RetrievedAt
	}
	for _, policy := range config.ProviderPresetPolicy() {
		if policy.ID == openaiauth.Provider {
			continue
		}
		id := config.ModelsDevProviderID(policy.ID)
		data, ok := raw[id]
		if !ok {
			return snapshot, fmt.Errorf("required Models.dev provider %q is missing", id)
		}
		var source upstreamProvider
		if err := json.Unmarshal(data, &source); err != nil {
			return snapshot, fmt.Errorf("provider %s: %w", id, err)
		}
		if source.ID != id {
			return snapshot, fmt.Errorf("provider %s has mismatched ID", id)
		}
		provider := modelsdev.ProviderInfo{ID: id, Name: source.Name, API: source.API, Env: source.Env, Models: map[string]modelsdev.Model{}}
		for modelID, source := range source.Models {
			if source.ID != modelID {
				return snapshot, fmt.Errorf("provider %s model %s has mismatched ID", id, modelID)
			}
			model := modelsdev.Model{ID: modelID, Name: source.Name, ContextLength: source.Limit.Context, MaxCompletionTokens: source.Limit.Output, SupportsTools: source.ToolCall, Reasoning: source.Reasoning, InputModalities: source.Modalities.Input, OutputModalities: source.Modalities.Output}
			if source.ReasoningOptions != nil {
				model.ReasoningEfforts = []string{}
			}
			for _, option := range source.ReasoningOptions {
				if option.Type == "effort" {
					model.ReasoningEfforts = append(model.ReasoningEfforts, option.Values...)
				}
			}
			if source.Cost != nil {
				var err error
				model.Pricing = &modelsdev.Pricing{}
				model.Pricing.Prompt, err = perToken(source.Cost.Input)
				if err != nil {
					return snapshot, fmt.Errorf("%s/%s input cost: %w", id, modelID, err)
				}
				model.Pricing.Completion, err = perToken(source.Cost.Output)
				if err != nil {
					return snapshot, fmt.Errorf("%s/%s output cost: %w", id, modelID, err)
				}
				model.Pricing.InputCacheRead, err = perToken(source.Cost.CacheRead)
				if err != nil {
					return snapshot, fmt.Errorf("%s/%s cache cost: %w", id, modelID, err)
				}
			}
			provider.Models[modelID] = model
		}
		snapshot.Providers[id] = provider
	}
	return snapshot, nil
}

func perToken(number json.Number) (string, error) {
	if number == "" {
		return "", nil
	}
	if len(number) > 64 || !decimalNumber.MatchString(string(number)) {
		return "", errors.New("cost must be a bounded nonnegative decimal")
	}
	value, ok := new(big.Rat).SetString(string(number))
	if !ok || value.Sign() < 0 {
		return "", errors.New("cost must be a nonnegative decimal")
	}
	value.Quo(value, big.NewRat(1000000, 1))
	digits, exact := value.FloatPrec()
	if !exact || digits > 64 {
		return "", errors.New("cost precision exceeds supported decimal range")
	}
	return value.FloatString(digits), nil
}

func validate(snapshot modelsdev.Snapshot, log io.Writer) error {
	if snapshot.SchemaVersion != modelsdev.SchemaVersion || snapshot.SourceURL != modelsdev.SourceURL {
		return errors.New("invalid Models.dev snapshot provenance")
	}
	if sum, err := hex.DecodeString(snapshot.InputSHA256); err != nil || len(sum) != sha256.Size {
		return errors.New("invalid Models.dev input SHA-256")
	}
	if _, err := time.Parse(time.RFC3339, snapshot.RetrievedAt); err != nil {
		return fmt.Errorf("invalid Models.dev retrieval date: %w", err)
	}
	expected := 0
	for _, policy := range config.ProviderPresetPolicy() {
		if policy.ID == openaiauth.Provider {
			continue
		}
		expected++
		id := config.ModelsDevProviderID(policy.ID)
		provider, ok := snapshot.Providers[id]
		if !ok || provider.ID != id || !validText(provider.Name) || len(provider.Env) == 0 || len(provider.Models) == 0 {
			return fmt.Errorf("invalid or missing Models.dev provider %s", id)
		}
		for _, name := range provider.Env {
			if !envName.MatchString(name) {
				return fmt.Errorf("provider %s contains invalid environment name", id)
			}
		}
		if provider.API != "" {
			parsed, err := url.Parse(provider.API)
			if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
				return fmt.Errorf("provider %s contains invalid API URL", id)
			}
		}
		if strings.TrimRight(provider.API, "/") != policy.Provider.BaseURL {
			fmt.Fprintf(log, "%s endpoint override retained: upstream %q; Whip %q\n", policy.ID, provider.API, policy.Provider.BaseURL)
		}
		for id, model := range provider.Models {
			if model.ID != id || !validText(id) || !validText(model.Name) {
				return fmt.Errorf("provider %s contains invalid model identity", provider.ID)
			}
			for _, limit := range []*int{model.ContextLength, model.MaxCompletionTokens} {
				if limit != nil && (*limit < 0 || *limit > 1000000000) {
					return fmt.Errorf("%s/%s has invalid model limits", provider.ID, id)
				}
			}
			for _, values := range [][]string{model.InputModalities, model.OutputModalities, model.ReasoningEfforts} {
				for _, value := range values {
					if !validText(value) {
						return fmt.Errorf("%s/%s has invalid capability value", provider.ID, id)
					}
				}
			}
			if model.Pricing != nil {
				for _, value := range []string{model.Pricing.Prompt, model.Pricing.Completion, model.Pricing.InputCacheRead} {
					if value == "" {
						continue
					}
					if len(value) > 96 || !decimalNumber.MatchString(value) {
						return fmt.Errorf("%s/%s has invalid pricing", provider.ID, id)
					}
					rate, ok := new(big.Rat).SetString(value)
					if !ok || rate.Sign() < 0 {
						return fmt.Errorf("%s/%s has invalid pricing", provider.ID, id)
					}
				}
			}
		}
		for _, modelID := range policy.SuggestedModels {
			if _, found := provider.Models[modelID]; found {
				continue
			}
			found := false
			for _, model := range config.RetainedPresetModels(policy.ID) {
				if model.ID == modelID {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("%s default %s missing upstream; add an explicit retained override or adjust policy", policy.ID, modelID)
			}
			fmt.Fprintf(log, "%s/%s: explicit retained model override (absent upstream)\n", policy.ID, modelID)
		}
	}
	if len(snapshot.Providers) != expected {
		return errors.New("models.dev snapshot contains providers outside Whip policy")
	}
	return nil
}

func validText(value string) bool {
	return strings.TrimSpace(value) != "" && len(value) <= 512 && !strings.ContainsFunc(value, func(r rune) bool { return r < 32 || r == 127 })
}

func environment(snapshot modelsdev.Snapshot) []byte {
	names := []string{}
	for _, policy := range config.ProviderPresetPolicy() {
		if policy.ID == openaiauth.Provider {
			continue
		}
		names = append(names, policy.EnvironmentVariables...)
		names = append(names, snapshot.Providers[config.ModelsDevProviderID(policy.ID)].Env...)
	}
	slices.Sort(names)
	names = slices.Compact(names)
	var out strings.Builder
	out.WriteString("// Code generated by go run ./cmd/modelgen; DO NOT EDIT.\nexport const providerEnvironmentNames = [\n")
	for _, name := range names {
		fmt.Fprintf(&out, "  %q,\n", name)
	}
	out.WriteString("] as const;\n")
	return []byte(out.String())
}

func encode(snapshot modelsdev.Snapshot) ([]byte, error) {
	data, err := json.MarshalIndent(snapshot, "", "  ")
	return append(data, '\n'), err
}

func modelCount(snapshot modelsdev.Snapshot) int {
	count := 0
	for _, p := range snapshot.Providers {
		count += len(p.Models)
	}
	return count
}

func changes(log io.Writer, before, after modelsdev.Snapshot) {
	ids := make([]string, 0, len(after.Providers))
	for id := range after.Providers {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		current := after.Providers[id]
		old, existed := before.Providers[id]
		if !existed {
			fmt.Fprintf(log, "%s: added provider (%d models)\n", id, len(current.Models))
			continue
		}
		if old.API != current.API {
			fmt.Fprintf(log, "%s upstream endpoint changed: %q -> %q (Whip endpoint preserved)\n", id, old.API, current.API)
		}
		if old.Name != current.Name || !reflect.DeepEqual(old.Env, current.Env) {
			fmt.Fprintf(log, "%s: provider metadata changed\n", id)
		}
		added, removed, changed := []string{}, []string{}, []string{}
		for name, model := range current.Models {
			previous, ok := old.Models[name]
			if !ok {
				added = append(added, name)
			} else if !reflect.DeepEqual(previous, model) {
				changed = append(changed, name)
			}
		}
		for name := range old.Models {
			if _, ok := current.Models[name]; !ok {
				removed = append(removed, name)
			}
		}
		for _, row := range []struct {
			kind string
			ids  []string
		}{{"added", added}, {"removed", removed}, {"changed", changed}} {
			slices.Sort(row.ids)
			for _, name := range row.ids {
				fmt.Fprintf(log, "%s/%s: %s\n", id, name, row.kind)
			}
		}
	}
	for id := range before.Providers {
		if _, ok := after.Providers[id]; !ok {
			fmt.Fprintf(log, "%s: removed provider\n", id)
		}
	}
}

type artifact struct {
	path string
	data []byte
}

func replaceArtifacts(files []artifact) error {
	staged := make([]string, len(files))
	defer func() {
		for _, path := range staged {
			if path != "" {
				_ = os.Remove(path)
			}
		}
	}()
	for i, file := range files {
		// #nosec G301 -- Generated source artifacts are public and need readable parent directories.
		if err := os.MkdirAll(filepath.Dir(file.path), 0o755); err != nil {
			return err
		}
		temp, err := os.CreateTemp(filepath.Dir(file.path), ".modelgen-*")
		if err != nil {
			return err
		}
		staged[i] = temp.Name()
		if _, err := temp.Write(file.data); err != nil {
			_ = temp.Close()
			return err
		}
		if err := temp.Chmod(0o644); err != nil {
			_ = temp.Close()
			return err
		}
		if err := temp.Close(); err != nil {
			return err
		}
	}
	// Desktop names first: a failed snapshot replacement retains the old catalog.
	for i, file := range slices.Backward(files) {
		if err := os.Rename(staged[i], file.path); err != nil {
			return err
		}
	}
	return nil
}
