// Package agentdef describes an agent as data: what it is told, which host
// modules and capabilities it receives, its model and compaction defaults, and
// which children it may name. The runtime executes a definition; it never
// hardcodes one. Host facts such as provider credentials, kernel limits,
// browser policy, permission mode, and the execution engine stay outside.
package agentdef

import (
	"errors"
	"fmt"
	"slices"

	"github.com/context-labs/whip/internal/rlm"
)

// Capabilities lists every capability name a root may hold. A child's list
// only narrows its parent's.
var Capabilities = []string{"read", "write", "shell", "browser", "computer", "mcp"}

// Definition is one agent. Roots and children use the same shape; a child is
// its parent's definition narrowed by Child.
type Definition struct {
	ID           string
	Instructions Instructions
	// Modules names the host modules the model is told about and may call.
	Modules []string
	// Capabilities names the authority a root receives at bind.
	Capabilities []string
	// Model supplies defaults; empty fields fall back to host configuration and
	// the session's own selection remains authoritative.
	Model      ModelDefaults
	Compaction CompactionDefaults
	// Children are named child definitions a spawn may select. Unnamed spawns
	// inherit the parent definition.
	Children map[string]Child
	Surface  Surface
}

// Instructions is the agent-owned part of the system prompt plus the discovery
// the agent wants. Runtime guidance for the execution engine is not here.
type Instructions struct {
	Persona string
	Rules   string
	// ProjectFiles are read along the authorized project chain, broad to
	// specific. Empty disables project instruction discovery.
	ProjectFiles         []string
	SkillDiscovery       bool
	StandingInstructions bool
}

type ModelDefaults struct{ Model, Provider, Effort string }

// CompactionDefaults selects the summary route and trigger. Zero values use the
// host defaults.
type CompactionDefaults struct {
	Model, Provider string
	// Threshold is the fraction of the context window that triggers compaction.
	Threshold float64
}

// Child is a named child definition. Nil or empty fields inherit the parent's.
type Child struct {
	Instructions *Instructions
	Modules      []string
	Capabilities []string
	Model        ModelDefaults
	Budgets      map[string]int64
	Report       string
}

// Surface toggles root-only behaviors the daemon runs around turns.
type Surface struct{ AutoTitle, GoalLoop bool }

// ChildOverrides are the spawn-time narrowings a parent applies. Nil lists
// inherit; explicit lists narrow.
type ChildOverrides struct {
	Modules      []string
	Capabilities []string
	Model        ModelDefaults
}

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
	for name, child := range d.Children {
		if name == "" {
			return fmt.Errorf("agent definition %q has a child without a name", d.ID)
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

// Child resolves the effective definition of a child: the named child when name
// is non-empty, then the spawn overrides. Modules and capabilities only narrow;
// a request for anything the parent lacks fails.
func (d Definition) Child(name string, overrides ChildOverrides) (Definition, error) {
	child := d
	child.Children = nil
	child.Modules = slices.Clone(d.Modules)
	child.Capabilities = slices.Clone(d.Capabilities)
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
		child.Model = merge(child.Model, named.Model)
	}
	var err error
	if child.Modules, err = narrow("module", child.Modules, overrides.Modules); err != nil {
		return Definition{}, err
	}
	if child.Capabilities, err = narrow("capability", child.Capabilities, overrides.Capabilities); err != nil {
		return Definition{}, err
	}
	child.Model = merge(child.Model, overrides.Model)
	return child, nil
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
