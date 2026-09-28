package providerhost

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

const maxModels = 1024

// Model preserves absent/zero rates and absent/empty capabilities. Prices use
// the same exact nano-USD per million token units as execution accounting.
type Model struct {
	ID                      string              `json:"id"`
	Name                    string              `json:"name"`
	Prices                  session.ModelPrices `json:"prices"`
	ContextWindowTokens     *int64              `json:"context_window_tokens"`
	AdvertisedContextTokens *int64              `json:"advertised_context_tokens"`
	EffectiveContextPercent *int64              `json:"effective_context_percent"`
	MaxOutputTokens         *int64              `json:"max_output_tokens"`
	ReasoningEfforts        []string            `json:"reasoning_efforts"`
	InputModalities         []string            `json:"input_modalities"`
	OutputModalities        []string            `json:"output_modalities"`
	SupportsTools           *bool               `json:"supports_tools"`
	MetadataSource          string              `json:"metadata_source"`
}

type advertisedModel struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	DisplayName      string          `json:"display_name"`
	Type             string          `json:"type"`
	Context          *int64          `json:"context_length"`
	ContextWindow    *int64          `json:"context_window"`
	Output           *int64          `json:"max_completion_tokens"`
	Efforts          []string        `json:"reasoning_efforts"`
	Input            []string        `json:"input_modalities"`
	OutputModalities []string        `json:"output_modalities"`
	Tools            *bool           `json:"supports_tools"`
	Parameters       []string        `json:"supported_parameters"`
	Pricing          json.RawMessage `json:"pricing"`
	Architecture     struct {
		Input  []string `json:"input_modalities"`
		Output []string `json:"output_modalities"`
	} `json:"architecture"`
	TopProvider struct {
		Context *int64 `json:"context_length"`
		Output  *int64 `json:"max_completion_tokens"`
	} `json:"top_provider"`
	Metadata struct {
		Context *int64   `json:"context_length"`
		Tags    []string `json:"tags"`
	} `json:"metadata"`
}

func decodeModels(raw []byte, id string, p config.Provider) ([]Model, error) {
	if p.Kind == "openai-codex" {
		return decodeSubscription(raw)
	}
	data := bytes.TrimSpace(raw)
	if !bytes.HasPrefix(data, []byte("[")) {
		var envelope struct {
			Data json.RawMessage `json:"data"`
		}
		if json.Unmarshal(data, &envelope) != nil {
			return nil, ErrDiscovery
		}
		data = bytes.TrimSpace(envelope.Data)
	}
	if !bytes.HasPrefix(data, []byte("[")) {
		return nil, ErrDiscovery
	}
	rows, err := decodeRows[advertisedModel](data, maxModels)
	if err != nil {
		return nil, ErrDiscovery
	}
	reviewed, err := bundled()
	if err != nil {
		return nil, err
	}
	canonical := canonical(id, p)
	result := make([]Model, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		if !safeText(row.ID, 256, false) || seen[row.ID] {
			return nil, ErrDiscovery
		}
		seen[row.ID] = true
		if canonical && row.Type != "" && row.Type != "chat" {
			continue
		}
		value := Model{ID: row.ID, Name: row.Name, ContextWindowTokens: row.Context, MaxOutputTokens: row.Output, ReasoningEfforts: row.Efforts, InputModalities: row.Input, OutputModalities: row.OutputModalities, SupportsTools: row.Tools, MetadataSource: "advertised"}
		if value.Name == "" {
			value.Name = row.DisplayName
		}
		if value.ContextWindowTokens == nil {
			value.ContextWindowTokens = row.ContextWindow
		}
		if value.ContextWindowTokens == nil {
			value.ContextWindowTokens = row.TopProvider.Context
		}
		if value.ContextWindowTokens == nil {
			value.ContextWindowTokens = row.Metadata.Context
		}
		if value.MaxOutputTokens == nil {
			value.MaxOutputTokens = row.TopProvider.Output
		}
		if value.InputModalities == nil {
			value.InputModalities = row.Architecture.Input
		}
		if value.OutputModalities == nil {
			value.OutputModalities = row.Architecture.Output
		}
		if value.SupportsTools == nil && row.Parameters != nil {
			value.SupportsTools = new(slices.Contains(row.Parameters, "tools"))
		}
		if canonical {
			nonChat := false
			for _, tag := range []string{"embed", "image-gen", "video-gen", "tts", "stt"} {
				nonChat = nonChat || slices.Contains(row.Metadata.Tags, tag)
			}
			if nonChat || value.OutputModalities != nil && !slices.Contains(value.OutputModalities, "text") || value.SupportsTools != nil && !*value.SupportsTools || !transportSupported(id, value.ID, p.Kind) {
				continue
			}
		}
		var rates map[string]json.RawMessage
		if len(row.Pricing) > 0 && string(row.Pricing) != "null" && json.Unmarshal(row.Pricing, &rates) != nil {
			return nil, ErrDiscovery
		}
		value.Prices, err = parsePrices(rates, canonical && id == "togetherai")
		if err != nil {
			return nil, err
		}
		if canonical {
			if base, ok := reviewed[id][value.ID]; ok {
				value = enrich(value, base)
			}
			if value.OutputModalities != nil && !slices.Contains(value.OutputModalities, "text") || value.SupportsTools != nil && !*value.SupportsTools {
				continue
			}
			if id == "deepseek" {
				value.ReasoningEfforts = []string{}
			}
		}
		if !validModel(value) {
			return nil, ErrDiscovery
		}
		result = append(result, value)
	}
	slices.SortFunc(result, func(a, b Model) int { return strings.Compare(a.ID, b.ID) })
	return result, nil
}

// Bound element allocation before decoding. A short JSON array containing many
// nulls must not allocate a large slice of provider metadata structs.
func decodeRows[T any](raw []byte, limit int) ([]T, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('[') {
		return nil, ErrDiscovery
	}
	result := make([]T, 0)
	for decoder.More() {
		if len(result) == limit {
			return nil, ErrDiscovery
		}
		var row T
		if decoder.Decode(&row) != nil {
			return nil, ErrDiscovery
		}
		result = append(result, row)
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim(']') {
		return nil, ErrDiscovery
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, ErrDiscovery
	}
	return result, nil
}

var decimal = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)

func parseRate(raw json.RawMessage, multiplier int64) (*int64, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil //nolint:nilnil // An absent rate is unknown, distinct from a known zero price.
	}
	value := string(raw)
	if bytes.HasPrefix(raw, []byte(`"`)) {
		if json.Unmarshal(raw, &value) != nil {
			return nil, ErrDiscovery
		}
	}
	if len(value) > 64 || !decimal.MatchString(value) {
		return nil, ErrDiscovery
	}
	if _, exponent, ok := strings.Cut(strings.ToLower(value), "e"); ok {
		n, err := strconv.Atoi(exponent)
		if err != nil || n < -24 || n > 24 {
			return nil, ErrDiscovery
		}
	}
	rate, ok := new(big.Rat).SetString(value)
	if !ok {
		return nil, ErrDiscovery
	}
	rate.Mul(rate, new(big.Rat).SetInt64(multiplier))
	if !rate.IsInt() || !rate.Num().IsInt64() {
		return nil, ErrDiscovery
	}
	return new(rate.Num().Int64()), nil
}

func parsePrices(raw map[string]json.RawMessage, together bool) (session.ModelPrices, error) {
	keys := []string{"prompt", "completion", "input_cache_read"}
	scale := int64(1_000_000_000_000_000)
	if together {
		keys = []string{"input", "output", "cached_input"}
		scale = 1_000_000_000
	}
	values := make([]*int64, 3)
	for i, key := range keys {
		value, err := parseRate(raw[key], scale)
		if err != nil {
			return session.ModelPrices{}, err
		}
		values[i] = value
	}
	return session.ModelPrices{Input: values[0], Output: values[1], CachedInput: values[2]}, nil
}

func enrich(value, base Model) Model {
	if value.Name == "" {
		value.Name = base.Name
	}
	if value.ContextWindowTokens == nil {
		value.ContextWindowTokens = copyPointer(base.ContextWindowTokens)
	}
	if value.MaxOutputTokens == nil {
		value.MaxOutputTokens = copyPointer(base.MaxOutputTokens)
	}
	if value.ReasoningEfforts == nil {
		value.ReasoningEfforts = slices.Clone(base.ReasoningEfforts)
	}
	if value.InputModalities == nil {
		value.InputModalities = slices.Clone(base.InputModalities)
	}
	if value.OutputModalities == nil {
		value.OutputModalities = slices.Clone(base.OutputModalities)
	}
	if value.SupportsTools == nil {
		value.SupportsTools = copyPointer(base.SupportsTools)
	}
	if value.Prices.Input == nil {
		value.Prices.Input = copyPointer(base.Prices.Input)
	}
	if value.Prices.Output == nil {
		value.Prices.Output = copyPointer(base.Prices.Output)
	}
	if value.Prices.CachedInput == nil {
		value.Prices.CachedInput = copyPointer(base.Prices.CachedInput)
	}
	value.MetadataSource = "advertised+bundled"
	return value
}

func validModel(value Model) bool {
	if !safeText(value.ID, 256, false) || !safeText(value.Name, 512, true) {
		return false
	}
	for _, limit := range []*int64{value.ContextWindowTokens, value.MaxOutputTokens} {
		if limit != nil && (*limit < 0 || *limit > 1_000_000_000) {
			return false
		}
	}
	for _, list := range [][]string{value.ReasoningEfforts, value.InputModalities, value.OutputModalities} {
		if len(list) > 32 {
			return false
		}
		for _, entry := range list {
			if !safeText(entry, 64, false) {
				return false
			}
		}
	}
	return value.Prices.Validate() == nil
}

func safeText(value string, limit int, empty bool) bool {
	return (empty || value != "") && len(value) <= limit && utf8.ValidString(value) && !strings.ContainsFunc(value, unicode.IsControl)
}

func copyPointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	return new(*value)
}

func cloneModel(value Model) Model {
	value.Prices = value.Prices.Clone()
	value.ContextWindowTokens = copyPointer(value.ContextWindowTokens)
	value.MaxOutputTokens = copyPointer(value.MaxOutputTokens)
	value.AdvertisedContextTokens = copyPointer(value.AdvertisedContextTokens)
	value.EffectiveContextPercent = copyPointer(value.EffectiveContextPercent)
	value.SupportsTools = copyPointer(value.SupportsTools)
	value.ReasoningEfforts = slices.Clone(value.ReasoningEfforts)
	value.InputModalities = slices.Clone(value.InputModalities)
	value.OutputModalities = slices.Clone(value.OutputModalities)
	return value
}

func transportSupported(provider, name, kind string) bool {
	if provider == "deepseek" {
		return name != "deepseek-reasoner"
	}
	if provider != "openai" {
		return true
	}
	for _, prefix := range []string{"text-", "dall-e-", "tts-", "whisper-", "sora-", "omni-moderation", "babbage-", "davinci-", "computer-use-"} {
		if strings.HasPrefix(name, prefix) {
			return false
		}
	}
	for _, part := range []string{"-embedding", "-image", "-audio", "-realtime", "-transcribe", "-search", "-deep-research", "-instruct"} {
		if strings.Contains(name, part) {
			return false
		}
	}
	if kind == "openai-chat" {
		for _, prefix := range []string{"gpt-6-astra", "gpt-5-pro", "gpt-5.2-pro", "o1-pro", "o3-pro", "o1-mini", "o1-preview", "codex-"} {
			if name == prefix || strings.HasPrefix(name, prefix+"-") {
				return false
			}
		}
		if strings.Contains(name, "-codex") {
			return false
		}
	}
	return true
}

func decodeSubscription(raw []byte) ([]Model, error) {
	var data struct {
		Models json.RawMessage `json:"models"`
	}
	if json.Unmarshal(raw, &data) != nil || !bytes.HasPrefix(bytes.TrimSpace(data.Models), []byte("[")) {
		return nil, ErrDiscovery
	}
	type subscriptionRow struct {
		Slug       string   `json:"slug"`
		Name       string   `json:"display_name"`
		Visibility string   `json:"visibility"`
		Context    int64    `json:"context_window"`
		MaxContext int64    `json:"max_context_window"`
		Percent    int64    `json:"effective_context_window_percent"`
		Input      []string `json:"input_modalities"`
		Efforts    []struct {
			Effort string `json:"effort"`
		} `json:"supported_reasoning_levels"`
	}
	rows, err := decodeRows[subscriptionRow](data.Models, 512)
	if err != nil {
		return nil, ErrDiscovery
	}
	result := []Model{}
	seen := map[string]bool{}
	for _, row := range rows {
		ceiling := model.SubscriptionOutputLimit(row.Slug)
		if row.Visibility != "list" || ceiling == 0 {
			continue
		}
		if seen[row.Slug] {
			return nil, ErrDiscovery
		}
		seen[row.Slug] = true
		context := row.Context
		if context <= 0 {
			context = row.MaxContext
		}
		if context <= 0 || context > 16<<20 {
			continue
		}
		percent := row.Percent
		if percent <= 0 || percent > 100 {
			percent = 95
		}
		value := Model{ID: row.Slug, Name: row.Name, ContextWindowTokens: new(context * percent / 100), AdvertisedContextTokens: new(context), EffectiveContextPercent: new(percent), MaxOutputTokens: new(ceiling), InputModalities: row.Input, MetadataSource: "advertised"}
		if row.Efforts != nil {
			value.ReasoningEfforts = []string{}
		}
		for _, effort := range row.Efforts {
			value.ReasoningEfforts = append(value.ReasoningEfforts, effort.Effort)
		}
		if !validModel(value) {
			return nil, ErrDiscovery
		}
		result = append(result, value)
	}
	return result, nil
}
