// Package agentdef describes an agent as data: what it is told, which host
// modules and capabilities it receives, its model and compaction defaults, and
// which children it may name. The runtime executes a definition; it never
// hardcodes one. Host facts such as provider credentials, kernel limits,
// browser policy, permission mode, and the execution engine stay outside.
package agentdef

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/rlm"
)

// Capabilities lists every capability name a root may hold. A child's list
// only narrows its parent's.
var Capabilities = []string{"read", "write", "shell", "browser", "computer", "mcp"}

// Definition is one agent. Roots and children use the same shape; a child is
// its parent's definition narrowed by Child.
//
// The struct is also the wire document: registered definitions arrive as this
// JSON, validated and stored by the daemon. Absent collections are null in the
// canonical encoding; see Normalize.
type Definition struct {
	ID           string       `json:"id"`
	Instructions Instructions `json:"instructions"`
	// Modules names the host modules the model is told about and may call.
	Modules []string `json:"modules"`
	// Capabilities names the authority a root receives at bind.
	Capabilities []string `json:"capabilities"`
	// Model supplies defaults; empty fields fall back to host configuration and
	// the session's own selection remains authoritative.
	Model      ModelDefaults      `json:"model"`
	Compaction CompactionDefaults `json:"compaction"`
	// MCP names the configured servers this agent may use.
	MCP MCPSelection `json:"mcp"`
	// Tools are custom operations served by an executor. A non-empty list
	// implies the reserved tools module; the cell calls tools.<name>.
	Tools []Tool `json:"tools"`
	// Output is the JSON Schema a turn's final assistant message must match.
	// Nil means free text. The guide states the contract; the daemon validates
	// the message, returns one mismatch for correction, and fails the turn on
	// a second.
	Output json.RawMessage `json:"output"`
	// Children are named child definitions a spawn may select. Unnamed spawns
	// inherit the parent definition.
	Children map[string]Child `json:"children"`
	Surface  Surface          `json:"surface"`
	// Hooks are the decision points the agent's executor serves. Nil declares
	// none. Children inherit their parent's hooks unchanged.
	Hooks *Hooks `json:"hooks"`
}

// Hooks names the hooks a definition declares. Each declared hook must be
// covered by the executor that binds the definition.
type Hooks struct {
	// BeforeTool runs before every host operation a cell calls and may deny or
	// rewrite the arguments.
	BeforeTool *Hook `json:"before_tool"`
	// BeforeSpawn runs after a spawn request is parsed and resolved and may
	// deny or rewrite the request.
	BeforeSpawn *Hook `json:"before_spawn"`
	// TurnStart contributes ephemeral context to each turn. It never gates.
	TurnStart *Hook `json:"turn_start"`
}

// Hook configures one hook.
type Hook struct {
	// Operations narrows before_tool to these module.operation names; nil
	// means every host operation. Only before_tool accepts it.
	Operations []string `json:"operations"`
	// Optional hooks proceed with a notice when unanswered or failing; a
	// required hook (the default) denies instead.
	Optional bool `json:"optional"`
	// TimeoutMillis bounds one invocation; zero uses DefaultHookTimeout. A
	// gate holds the cell's kernel slot, so MaxHookTimeout caps it.
	TimeoutMillis int64 `json:"timeout_millis"`
}

// Hook names, in the canonical order HookNames reports them.
const (
	HookBeforeTool  = "before_tool"
	HookBeforeSpawn = "before_spawn"
	HookTurnStart   = "turn_start"
)

const (
	DefaultHookTimeout = 30 * time.Second
	MaxHookTimeout     = 60 * time.Second
)

// Timeout is the effective invocation deadline.
func (h Hook) Timeout() time.Duration {
	if h.TimeoutMillis <= 0 {
		return DefaultHookTimeout
	}
	return time.Duration(h.TimeoutMillis) * time.Millisecond
}

// Hook returns the declared hook by name, or nil.
func (d Definition) Hook(name string) *Hook {
	if d.Hooks == nil {
		return nil
	}
	switch name {
	case HookBeforeTool:
		return d.Hooks.BeforeTool
	case HookBeforeSpawn:
		return d.Hooks.BeforeSpawn
	case HookTurnStart:
		return d.Hooks.TurnStart
	}
	return nil
}

// HookNames lists the declared hooks in canonical order.
func (d Definition) HookNames() []string {
	var names []string
	for _, name := range []string{HookBeforeTool, HookBeforeSpawn, HookTurnStart} {
		if d.Hook(name) != nil {
			names = append(names, name)
		}
	}
	return names
}

// Instructions is the agent-owned part of the system prompt plus the discovery
// the agent wants. Runtime guidance for the execution engine is not here.
type Instructions struct {
	Persona string `json:"persona"`
	Rules   string `json:"rules"`
	// ProjectFiles are read along the authorized project chain, broad to
	// specific. Empty disables project instruction discovery.
	ProjectFiles         []string `json:"project_files"`
	SkillDiscovery       bool     `json:"skill_discovery"`
	StandingInstructions bool     `json:"standing_instructions"`
}

type ModelDefaults struct {
	Model    string `json:"model"`
	Provider string `json:"provider"`
	Effort   string `json:"effort"`
}

// CompactionDefaults selects the summary route and trigger. Zero values use the
// host defaults.
type CompactionDefaults struct {
	Model    string `json:"model"`
	Provider string `json:"provider"`
	// Threshold is the fraction of the context window that triggers compaction.
	Threshold float64 `json:"threshold"`
}

// MCPSelection names configured MCP servers. A nil list means every server the
// host configures; an explicit list narrows to those names. Any use requires
// the mcp capability.
type MCPSelection struct {
	Servers []string `json:"servers"`
}

// Tool is a custom operation implemented outside the daemon and served by an
// executor bound to the definition. The cell calls it as tools.<name> with
// keyword arguments validated against InputSchema.
type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// InputSchema is a JSON Schema object for the keyword arguments.
	InputSchema json.RawMessage `json:"input_schema"`
	// OutputSchema, when set, is the JSON Schema the tool's result must match.
	// The guide shows the model what the tool returns; the daemon rejects a
	// result from any executor that does not match.
	OutputSchema json.RawMessage `json:"output_schema"`
	// TimeoutMillis bounds one invocation; zero uses DefaultToolTimeout. A
	// running handler holds the cell's kernel slot, so MaxToolTimeout caps it.
	TimeoutMillis int64 `json:"timeout_millis"`
}

const (
	DefaultToolTimeout = 5 * time.Minute
	MaxToolTimeout     = 15 * time.Minute
)

// Timeout is the effective invocation deadline.
func (t Tool) Timeout() time.Duration {
	if t.TimeoutMillis <= 0 {
		return DefaultToolTimeout
	}
	return time.Duration(t.TimeoutMillis) * time.Millisecond
}

// Child is a named child definition. Nil or empty fields inherit the parent's.
type Child struct {
	Instructions *Instructions    `json:"instructions"`
	Modules      []string         `json:"modules"`
	Capabilities []string         `json:"capabilities"`
	Tools        []string         `json:"tools"`
	Model        ModelDefaults    `json:"model"`
	Budgets      map[string]int64 `json:"budgets"`
	Report       string           `json:"report"`
	// Output replaces the parent's output contract when set; null inherits.
	Output json.RawMessage `json:"output"`
}

// schemaPresent reports a JSON Schema that is set: non-empty and not null.
func schemaPresent(schema json.RawMessage) bool {
	trimmed := bytes.TrimSpace(schema)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

// validateSchemaObject checks that a present schema is a JSON object.
func validateSchemaObject(schema json.RawMessage, subject string) error {
	if !schemaPresent(schema) {
		return nil
	}
	var object map[string]any
	if err := json.Unmarshal(schema, &object); err != nil || object == nil {
		return fmt.Errorf("%s must be a JSON Schema object", subject)
	}
	return nil
}

// Surface toggles root-only behaviors the daemon runs around turns.
type Surface struct {
	AutoTitle bool `json:"auto_title"`
	GoalLoop  bool `json:"goal_loop"`
}

// ChildOverrides are the spawn-time narrowings a parent applies. Nil lists
// inherit; explicit lists narrow.
type ChildOverrides struct {
	Modules      []string
	Capabilities []string
	Tools        []string
	Model        ModelDefaults
}

var (
	definitionID = regexp.MustCompile(`^[a-z][a-z0-9-]{1,63}$`)
	toolName     = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
)

// Validate checks the definition against the runtime's module registry and the
// known capability names.
func (d Definition) Validate() error {
	if d.ID == "" {
		return errors.New("agent definition requires an id")
	}
	if len(d.Modules) == 0 {
		return fmt.Errorf("agent definition %q selects no host modules", d.ID)
	}
	known := rlm.Modules()
	for i, module := range d.Modules {
		if _, ok := known[module]; !ok {
			return fmt.Errorf("agent definition %q selects unknown host module %q", d.ID, module)
		}
		if slices.Contains(d.Modules[:i], module) {
			return fmt.Errorf("agent definition %q repeats host module %q", d.ID, module)
		}
	}
	for i, capability := range d.Capabilities {
		if !slices.Contains(Capabilities, capability) {
			return fmt.Errorf("agent definition %q names unknown capability %q", d.ID, capability)
		}
		if slices.Contains(d.Capabilities[:i], capability) {
			return fmt.Errorf("agent definition %q repeats capability %q", d.ID, capability)
		}
	}
	if t := d.Compaction.Threshold; t < 0 || t >= 1 {
		return fmt.Errorf("agent definition %q compaction threshold %v must be 0 or in (0, 1)", d.ID, t)
	}
	if d.MCP.Servers != nil && !slices.Contains(d.Capabilities, "mcp") {
		return fmt.Errorf("agent definition %q names MCP servers without the mcp capability", d.ID)
	}
	for i, tool := range d.Tools {
		if !toolName.MatchString(tool.Name) {
			return fmt.Errorf("agent definition %q tool name %q must match %s", d.ID, tool.Name, toolName)
		}
		if slices.ContainsFunc(d.Tools[:i], func(other Tool) bool { return other.Name == tool.Name }) {
			return fmt.Errorf("agent definition %q repeats tool %q", d.ID, tool.Name)
		}
		var schema map[string]any
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil || schema == nil {
			return fmt.Errorf("agent definition %q tool %q input schema must be a JSON object", d.ID, tool.Name)
		}
		if err := validateSchemaObject(tool.OutputSchema, fmt.Sprintf("agent definition %q tool %q output schema", d.ID, tool.Name)); err != nil {
			return err
		}
		if tool.TimeoutMillis < 0 || time.Duration(tool.TimeoutMillis)*time.Millisecond > MaxToolTimeout {
			return fmt.Errorf("agent definition %q tool %q timeout must be between 0 and %s", d.ID, tool.Name, MaxToolTimeout)
		}
	}
	if err := d.validateHooks(known); err != nil {
		return err
	}
	if err := validateSchemaObject(d.Output, fmt.Sprintf("agent definition %q output", d.ID)); err != nil {
		return err
	}
	for name, child := range d.Children {
		if name == "" {
			return fmt.Errorf("agent definition %q has a child without a name", d.ID)
		}
		if err := validateSchemaObject(child.Output, fmt.Sprintf("agent definition %q child %q output", d.ID, name)); err != nil {
			return err
		}
		if _, err := d.Child(name, ChildOverrides{}); err != nil {
			return err
		}
		switch child.Report {
		case "", "notice", "inline", "message":
		default:
			return fmt.Errorf("agent definition %q child %q has unknown report mode %q", d.ID, name, child.Report)
		}
		for kind, limit := range child.Budgets {
			if limit < 0 {
				return fmt.Errorf("agent definition %q child %q budget %q is negative", d.ID, name, kind)
			}
		}
	}
	return nil
}

// validateHooks bounds timeouts and checks before_tool's operation filter
// against the module registry and the declared tools.
func (d Definition) validateHooks(known map[string][]string) error {
	for _, name := range d.HookNames() {
		hook := d.Hook(name)
		if hook.TimeoutMillis < 0 || time.Duration(hook.TimeoutMillis)*time.Millisecond > MaxHookTimeout {
			return fmt.Errorf("agent definition %q hook %s timeout must be between 0 and %s", d.ID, name, MaxHookTimeout)
		}
		if name != HookBeforeTool && hook.Operations != nil {
			return fmt.Errorf("agent definition %q hook %s does not accept operations", d.ID, name)
		}
		for i, operation := range hook.Operations {
			module, op, ok := strings.Cut(operation, ".")
			operations, knownModule := known[module]
			if module == "tools" {
				operations, knownModule = d.ToolNames(), true
			}
			if !ok || !knownModule || !slices.Contains(operations, op) {
				return fmt.Errorf("agent definition %q hook %s names unknown operation %q", d.ID, name, operation)
			}
			if slices.Contains(hook.Operations[:i], operation) {
				return fmt.Errorf("agent definition %q hook %s repeats operation %q", d.ID, name, operation)
			}
		}
	}
	return nil
}

// ToolNames lists the definition's custom tools in declaration order.
func (d Definition) ToolNames() []string {
	names := make([]string, 0, len(d.Tools))
	for _, tool := range d.Tools {
		names = append(names, tool.Name)
	}
	return names
}

// Child resolves the effective definition of a child: the named child when name
// is non-empty, then the spawn overrides. Modules, capabilities, and tools only
// narrow; a request for anything the parent lacks fails.
func (d Definition) Child(name string, overrides ChildOverrides) (Definition, error) {
	child := d
	child.Children = nil
	child.Modules = slices.Clone(d.Modules)
	child.Capabilities = slices.Clone(d.Capabilities)
	child.Tools = slices.Clone(d.Tools)
	if name != "" {
		named, ok := d.Children[name]
		if !ok {
			return Definition{}, fmt.Errorf("agent definition %q has no child named %q", d.ID, name)
		}
		child.ID = d.ID + "/" + name
		if named.Instructions != nil {
			child.Instructions = *named.Instructions
		}
		var err error
		if child.Modules, err = narrow("module", d.Modules, named.Modules); err != nil {
			return Definition{}, err
		}
		if child.Capabilities, err = narrow("capability", d.Capabilities, named.Capabilities); err != nil {
			return Definition{}, err
		}
		if child.Tools, err = narrowTools(child.Tools, named.Tools); err != nil {
			return Definition{}, err
		}
		child.Model = merge(child.Model, named.Model)
		if schemaPresent(named.Output) {
			child.Output = slices.Clone(named.Output)
		}
	}
	var err error
	if child.Modules, err = narrow("module", child.Modules, overrides.Modules); err != nil {
		return Definition{}, err
	}
	if child.Capabilities, err = narrow("capability", child.Capabilities, overrides.Capabilities); err != nil {
		return Definition{}, err
	}
	if child.Tools, err = narrowTools(child.Tools, overrides.Tools); err != nil {
		return Definition{}, err
	}
	child.Model = merge(child.Model, overrides.Model)
	return child, nil
}

// narrowTools keeps the requested tools in declaration order; nil inherits.
func narrowTools(inherited []Tool, requested []string) ([]Tool, error) {
	if requested == nil {
		return slices.Clone(inherited), nil
	}
	result := make([]Tool, 0, len(requested))
	for _, tool := range inherited {
		if slices.Contains(requested, tool.Name) {
			result = append(result, tool)
		}
	}
	for _, name := range requested {
		if !slices.ContainsFunc(inherited, func(tool Tool) bool { return tool.Name == name }) {
			return nil, fmt.Errorf("tool %q is not available to the parent", name)
		}
	}
	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

// narrow returns the requested subset of inherited, or a copy of inherited
// when requested is nil. Order follows the request; duplicates collapse.
func narrow(kind string, inherited, requested []string) ([]string, error) {
	if requested == nil {
		return slices.Clone(inherited), nil
	}
	result := make([]string, 0, len(requested))
	for _, name := range requested {
		if !slices.Contains(inherited, name) {
			return nil, fmt.Errorf("%s %q is not available to the parent", kind, name)
		}
		if !slices.Contains(result, name) {
			result = append(result, name)
		}
	}
	return result, nil
}

func merge(base, override ModelDefaults) ModelDefaults {
	if override.Model != "" {
		base.Model = override.Model
	}
	if override.Provider != "" {
		base.Provider = override.Provider
	}
	if override.Effort != "" {
		base.Effort = override.Effort
	}
	return base
}
