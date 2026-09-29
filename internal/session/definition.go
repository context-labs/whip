package session

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/hostmodule"
)

// ModelSelection captures logical names and generation preferences. Endpoints,
// credentials and client instances belong to host execution resources. Nil sampling
// values retain provider defaults; explicit zero is meaningful.
type ModelSelection struct {
	Provider    string   `json:"provider"`
	Name        string   `json:"name"`
	Effort      string   `json:"effort"`
	Temperature *float64 `json:"temperature"`
	TopP        *float64 `json:"top_p"`
}

type Instructions struct {
	ProjectRoot          *string  `json:"project_root"`
	StandingInstructions bool     `json:"standing_instructions"`
	SkillRoots           []string `json:"skill_roots"`
	Text                 string   `json:"text"`
	ProjectFiles         []string `json:"project_files"`
	DiscoverSkills       bool     `json:"discover_skills"`
}

// ToolDeclaration advertises a contract, never authority or a live handler.
type ToolDeclaration struct {
	// Zero selects the retained five-minute default; the maximum is fifteen minutes.
	TimeoutMillis int64           `json:"timeout_millis"`
	Description   string          `json:"description"`
	InputSchema   json.RawMessage `json:"input_schema"`
	OutputSchema  json.RawMessage `json:"output_schema"`
}

type HookDeclaration struct {
	// Zero timeout selects the retained thirty-second default; the maximum is one minute.
	Operations    []string `json:"operations"`
	Optional      bool     `json:"optional"`
	TimeoutMillis int64    `json:"timeout_millis"`
}

// OutputPolicy is a patch value. A present policy with a nil schema explicitly
// clears an inherited output contract and survives a JSON round trip.
type OutputPolicy struct {
	Schema json.RawMessage `json:"schema"`
}

// ReportMode controls automatic child completion mail. Message mode leaves
// successful reporting to the child; unsuccessful outcomes still notify its parent.
type ReportMode string

const (
	ReportNotice  ReportMode = "notice"
	ReportInline  ReportMode = "inline"
	ReportMessage ReportMode = "message"
)

func (m ReportMode) Validate() error {
	switch m {
	case ReportNotice, ReportInline, ReportMessage:
		return nil
	default:
		return fmt.Errorf("%w: unknown report mode %q", ErrInvalid, m)
	}
}

type DefinitionRef struct {
	ID       string `json:"id"`
	Revision string `json:"revision"`
}

func (r DefinitionRef) Validate() error {
	if err := ValidateID(r.ID); err != nil {
		return err
	}
	digest, err := hex.DecodeString(r.Revision)
	if err != nil || len(digest) != sha256.Size || strings.ToLower(r.Revision) != r.Revision {
		return fmt.Errorf("%w: definition revision must be a SHA-256 digest", ErrInvalid)
	}
	return nil
}

type Configuration struct {
	Modules         []string                   `json:"modules"`
	ToolsDefinition *DefinitionRef             `json:"tools_definition"`
	HooksDefinition *DefinitionRef             `json:"hooks_definition"`
	AutomaticTitle  bool                       `json:"automatic_title"`
	GoalsEnabled    bool                       `json:"goals_enabled"`
	ReportMode      ReportMode                 `json:"report_mode"`
	Model           ModelSelection             `json:"model"`
	Compaction      CompactionPolicy           `json:"compaction"`
	Instructions    Instructions               `json:"instructions"`
	Tools           map[string]ToolDeclaration `json:"tools"`
	Children        map[string]DefinitionRef   `json:"children"`
	Hooks           map[string]HookDeclaration `json:"hooks"`
	OutputSchema    json.RawMessage            `json:"output_schema"`
}

// ConfigPatch replaces whole fields. Nil inherits; an explicit empty collection
// clears. A present Output policy with no schema clears structured output.
// This avoids recursive merge rules and implicit inheritance of credentials.
type ConfigPatch struct {
	Modules        []string                   `json:"modules"`
	AutomaticTitle *bool                      `json:"automatic_title"`
	GoalsEnabled   *bool                      `json:"goals_enabled"`
	ReportMode     *ReportMode                `json:"report_mode"`
	Model          *ModelSelection            `json:"model"`
	Compaction     *CompactionPolicy          `json:"compaction"`
	Instructions   *Instructions              `json:"instructions"`
	Tools          map[string]ToolDeclaration `json:"tools"`
	Children       map[string]DefinitionRef   `json:"children"`
	Hooks          map[string]HookDeclaration `json:"hooks"`
	Output         *OutputPolicy              `json:"output"`
}

type DefinitionDocument struct {
	ID       string      `json:"id"`
	Name     string      `json:"name"`
	Defaults ConfigPatch `json:"defaults"`
}

type DefinitionRevision struct {
	Ref       DefinitionRef
	Document  DefinitionDocument
	CreatedAt time.Time
}

// Builtins uses the same registration and revision path as user documents.
// Returned documents do not share mutable collections across calls.
func Builtins() []DefinitionDocument {
	return []DefinitionDocument{{
		ID:   "assistant",
		Name: "Assistant",
		Defaults: ConfigPatch{Modules: hostmodule.Names(), AutomaticTitle: new(true), GoalsEnabled: new(true), Instructions: &Instructions{
			Text:           "Help the user complete their task. Use only the operations made available to you.",
			ProjectFiles:   []string{"AGENTS.md"},
			DiscoverSkills: true,
		}},
	}}
}

func (c Configuration) Clone() Configuration {
	c.Modules = slices.Clone(c.Modules)
	if c.ToolsDefinition != nil {
		c.ToolsDefinition = new(*c.ToolsDefinition)
	}
	if c.HooksDefinition != nil {
		c.HooksDefinition = new(*c.HooksDefinition)
	}
	c.Model = c.Model.Clone()
	if c.Compaction.Model != nil {
		c.Compaction.Model = new(c.Compaction.Model.Clone())
	}
	if c.Instructions.ProjectRoot != nil {
		c.Instructions.ProjectRoot = new(*c.Instructions.ProjectRoot)
	}
	c.Instructions.ProjectFiles = slices.Clone(c.Instructions.ProjectFiles)
	c.Instructions.SkillRoots = slices.Clone(c.Instructions.SkillRoots)
	c.Tools = maps.Clone(c.Tools)
	for name, tool := range c.Tools {
		tool.InputSchema = slices.Clone(tool.InputSchema)
		tool.OutputSchema = slices.Clone(tool.OutputSchema)
		c.Tools[name] = tool
	}
	c.Children = maps.Clone(c.Children)
	c.Hooks = maps.Clone(c.Hooks)
	for name, hook := range c.Hooks {
		hook.Operations = slices.Clone(hook.Operations)
		c.Hooks[name] = hook
	}
	c.OutputSchema = slices.Clone(c.OutputSchema)
	return c
}

// Resolve applies host/parent defaults, the pinned definition, then explicit
// overrides. Every returned map, slice and schema belongs to the caller.
func Resolve(base Configuration, definition DefinitionDocument, overrides ConfigPatch) (Configuration, error) {
	resolved := base
	var origin *DefinitionRef
	if definition.ID != "" && (definition.Defaults.Tools != nil || definition.Defaults.Hooks != nil) {
		_, _, ref, err := CanonicalDefinition(definition)
		if err != nil {
			return Configuration{}, err
		}
		origin = &ref
	}
	for index, patch := range []ConfigPatch{definition.Defaults, overrides} {
		if err := patch.Validate(); err != nil {
			return Configuration{}, err
		}
		if patch.Modules != nil {
			resolved.Modules = patch.Modules
		}
		if patch.GoalsEnabled != nil {
			resolved.GoalsEnabled = *patch.GoalsEnabled
		}
		if patch.AutomaticTitle != nil {
			resolved.AutomaticTitle = *patch.AutomaticTitle
		}
		if patch.Model != nil {
			resolved.Model = *patch.Model
		}
		if patch.Compaction != nil {
			resolved.Compaction = *patch.Compaction
		}
		if patch.ReportMode != nil {
			resolved.ReportMode = *patch.ReportMode
		}
		if patch.Instructions != nil {
			resolved.Instructions = *patch.Instructions
		}
		if patch.Tools != nil {
			if index == 0 {
				resolved.ToolsDefinition = origin
			} else if definition.ID != "" && !contractSubset(resolved.Tools, patch.Tools) {
				resolved.ToolsDefinition = nil
			}
			resolved.Tools = patch.Tools
		}
		if patch.Children != nil {
			resolved.Children = patch.Children
		}
		if patch.Hooks != nil {
			if index == 0 {
				resolved.HooksDefinition = origin
			} else if definition.ID != "" && !contractSubset(resolved.Hooks, patch.Hooks) {
				resolved.HooksDefinition = nil
			}
			resolved.Hooks = patch.Hooks
		}
		if patch.Output != nil {
			resolved.OutputSchema = patch.Output.Schema
		}
	}
	if resolved.Modules == nil {
		resolved.Modules = hostmodule.Names()
	}
	if resolved.ReportMode == "" {
		resolved.ReportMode = ReportNotice
	}
	if resolved.Compaction.ThresholdPercent == 0 {
		resolved.Compaction.ThresholdPercent = 50
	}
	if err := resolved.Validate(); err != nil {
		return Configuration{}, err
	}
	return resolved.Clone(), nil
}

// Clone gives the caller independent ownership of optional sampling values.
func (m ModelSelection) Clone() ModelSelection {
	if m.Temperature != nil {
		m.Temperature = new(*m.Temperature)
	}
	if m.TopP != nil {
		m.TopP = new(*m.TopP)
	}
	return m
}

// Equal compares captured values, including absent versus explicit zero.
func (m ModelSelection) Equal(other ModelSelection) bool {
	equal := func(a, b *float64) bool {
		return a == nil && b == nil || a != nil && b != nil && *a == *b
	}
	return m.Provider == other.Provider && m.Name == other.Name && m.Effort == other.Effort &&
		equal(m.Temperature, other.Temperature) && equal(m.TopP, other.TopP)
}

func (m ModelSelection) Validate() error {
	if err := ValidateID(m.Provider); err != nil {
		return err
	}
	if err := ValidateText(m.Name, 256); err != nil {
		return err
	}
	if len(m.Effort) > 64 || strings.ContainsRune(m.Effort, 0) {
		return fmt.Errorf("%w: invalid model effort", ErrInvalid)
	}
	for _, field := range []struct {
		name    string
		value   *float64
		maximum float64
	}{{"temperature", m.Temperature, 2}, {"top_p", m.TopP, 1}} {
		if field.value != nil && (math.IsNaN(*field.value) || math.IsInf(*field.value, 0) || *field.value < 0 || *field.value > field.maximum) {
			return fmt.Errorf("%w: %s must be a finite number from 0 to %g", ErrInvalid, field.name, field.maximum)
		}
	}
	return nil
}

func (c Configuration) Validate() error {
	for _, ref := range []*DefinitionRef{c.ToolsDefinition, c.HooksDefinition} {
		if ref != nil {
			if err := ref.Validate(); err != nil {
				return err
			}
		}
	}
	// Host defaults may omit the policy. Resolve always records an explicit mode.
	if c.ReportMode != "" {
		if err := c.ReportMode.Validate(); err != nil {
			return err
		}
	}
	if err := c.Model.Validate(); err != nil {
		return err
	}
	return (ConfigPatch{
		Modules: c.Modules, Compaction: &c.Compaction, Instructions: &c.Instructions, Tools: c.Tools, Children: c.Children,
		Hooks: c.Hooks, Output: &OutputPolicy{Schema: c.OutputSchema},
	}).Validate()
}

var toolIdentifier = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func (p ConfigPatch) Validate() error {
	if len(p.Modules) > len(hostmodule.Names()) {
		return fmt.Errorf("%w: too many host modules", ErrInvalid)
	}
	for index, name := range p.Modules {
		if !hostmodule.Has(name) || slices.Contains(p.Modules[:index], name) {
			return fmt.Errorf("%w: invalid host module", ErrInvalid)
		}
	}

	if p.Compaction != nil {
		if err := p.Compaction.Validate(); err != nil {
			return err
		}
	}
	if p.ReportMode != nil {
		if err := p.ReportMode.Validate(); err != nil {
			return err
		}
	}
	if p.Model != nil {
		if err := p.Model.Validate(); err != nil {
			return err
		}
	}
	if p.Instructions != nil {
		if err := p.Instructions.Validate(); err != nil {
			return err
		}
	}
	if len(p.Tools) > 128 || len(p.Children) > 128 || len(p.Hooks) > 3 {
		return fmt.Errorf("%w: too many declarations", ErrInvalid)
	}
	for name, tool := range p.Tools {
		if !toolIdentifier.MatchString(name) || name == "constructor" || name == "prototype" || name == "__proto__" {
			return fmt.Errorf("%w: invalid custom tool identifier", ErrInvalid)
		}
		if tool.TimeoutMillis < 0 || tool.TimeoutMillis > int64(15*time.Minute/time.Millisecond) {
			return fmt.Errorf("%w: invalid custom tool timeout", ErrInvalid)
		}
		if len(tool.Description) > 16384 {
			return fmt.Errorf("%w: tool description exceeds bounds", ErrInvalid)
		}
		if err := validateSchema(tool.InputSchema, false); err != nil {
			return err
		}
		if err := validateSchema(tool.OutputSchema, true); err != nil {
			return err
		}
	}
	for name, child := range p.Children {
		if err := ValidateID(name); err != nil {
			return err
		}
		if err := child.Validate(); err != nil {
			return err
		}
	}
	for name, hook := range p.Hooks {
		if name != "before_tool" && name != "before_spawn" && name != "turn_start" {
			return fmt.Errorf("%w: unknown hook %q", ErrInvalid, name)
		}
		if hook.TimeoutMillis < 0 || hook.TimeoutMillis > 60000 || len(hook.Operations) > 128 ||
			(name != "before_tool" && len(hook.Operations) != 0) {
			return fmt.Errorf("%w: invalid hook policy", ErrInvalid)
		}
		for index, operation := range hook.Operations {
			module, name, found := strings.Cut(operation, ".")
			known := hostmodule.Contains(module, name) || module == "tools" && toolIdentifier.MatchString(name)
			if !found || !known || slices.Contains(hook.Operations[:index], operation) {
				return fmt.Errorf("%w: invalid hook operation", ErrInvalid)
			}
		}
	}
	if p.Output != nil {
		if err := validateSchema(p.Output.Schema, true); err != nil {
			return err
		}
	}
	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("%w: malformed configuration JSON", ErrInvalid)
	}
	if len(data) > MaxDocumentBytes {
		return fmt.Errorf("%w: configuration exceeds size limit", ErrInvalid)
	}
	return nil
}

// CanonicalDefinition normalizes JSON object ordering and whitespace before
// hashing. Revision identity covers the whole immutable document.
func CanonicalDefinition(document DefinitionDocument) (DefinitionDocument, []byte, DefinitionRef, error) {
	if err := ValidateID(document.ID); err != nil {
		return DefinitionDocument{}, nil, DefinitionRef{}, err
	}
	if err := ValidateText(document.Name, 256); err != nil {
		return DefinitionDocument{}, nil, DefinitionRef{}, err
	}
	if err := document.Defaults.Validate(); err != nil {
		return DefinitionDocument{}, nil, DefinitionRef{}, err
	}
	raw, err := json.Marshal(document)
	if err != nil {
		return DefinitionDocument{}, nil, DefinitionRef{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return DefinitionDocument{}, nil, DefinitionRef{}, err
	}
	raw, err = json.Marshal(value)
	if err != nil {
		return DefinitionDocument{}, nil, DefinitionRef{}, err
	}
	var canonical DefinitionDocument
	if err := json.Unmarshal(raw, &canonical); err != nil {
		return DefinitionDocument{}, nil, DefinitionRef{}, err
	}
	sum := sha256.Sum256(raw)
	return canonical, raw, DefinitionRef{ID: document.ID, Revision: hex.EncodeToString(sum[:])}, nil
}
