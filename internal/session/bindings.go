package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
)

// SameContract compares canonical JSON values without treating schema property
// ordering or whitespace as a different executable contract.
func SameContract(a, b any) bool {
	canonical := func(value any) []byte {
		raw, err := json.Marshal(value)
		if err != nil {
			return nil
		}
		decoded, err := decodeExactJSON(raw)
		if err != nil {
			return nil
		}
		raw, err = json.Marshal(decoded)
		if err != nil {
			return nil
		}
		return raw
	}
	first, second := canonical(a), canonical(b)
	return first != nil && second != nil && bytes.Equal(first, second)
}

// NarrowBindings restricts callable syntax without changing canonical contracts.
// Definition provenance can differ for an explicitly selected child template;
// its reference is validated independently against registered declarations.
func NarrowBindings(ceiling, candidate Configuration) error {
	for _, name := range candidate.Modules {
		if !slices.Contains(ceiling.Modules, name) {
			return fmt.Errorf("%w: host module exceeds initial binding ceiling", ErrInvalid)
		}
	}
	for name, contract := range candidate.Tools {
		original, ok := ceiling.Tools[name]
		if !ok || !SameContract(original, contract) {
			return fmt.Errorf("%w: custom tool exceeds initial binding contract", ErrInvalid)
		}
	}
	for name, hook := range candidate.Hooks {
		original, exists := ceiling.Hooks[name]
		if !exists || !SameContract(original, hook) {
			return fmt.Errorf("%w: hook exceeds initial binding contract", ErrInvalid)
		}
	}
	for name, hook := range ceiling.Hooks {
		candidateHook, exists := candidate.Hooks[name]
		if !hook.Optional && (!exists || !SameContract(hook, candidateHook)) {
			return fmt.Errorf("%w: required hook cannot be removed or replaced", ErrInvalid)
		}
	}
	return nil
}

// UpdateBindings validates changes against immutable configuration revision 1.
// Guest globals and saved aliases retain these initial bindings; only the
// enabled subset changes, and every host dispatch checks its captured turn.
func UpdateBindings(initial, candidate Configuration) error {
	if err := NarrowBindings(initial, candidate); err != nil {
		return err
	}
	if !SameContract(initial.ToolsDefinition, candidate.ToolsDefinition) || !SameContract(initial.HooksDefinition, candidate.HooksDefinition) {
		return fmt.Errorf("%w: executor definition is immutable", ErrInvalid)
	}
	return nil
}

// ValidateInput checks the captured custom-tool contract before executor lookup.
// Exact numeric lexemes are preserved by the same bounded schema machinery as
// structured output; schemas never fetch external resources.
func (t ToolDeclaration) ValidateInput(arguments map[string]any) error {
	if arguments == nil {
		arguments = map[string]any{}
	}
	raw, err := json.Marshal(arguments)
	if err != nil {
		return fmt.Errorf("%w: invalid tool arguments", ErrInvalid)
	}
	value, err := decodeExactJSON(raw)
	if err != nil {
		return err
	}
	schema, err := compileSchema(t.InputSchema, false)
	if err != nil {
		return err
	}
	if err := schema.Validate(value); err != nil {
		return fmt.Errorf("%w: tool arguments do not match schema", ErrInvalid)
	}
	return nil
}

// Fresh template overrides that change contracts cannot claim the template's
// executor. Empty subsets retain their owner for later re-enabling within the
// initial ceiling. Configuration edits are checked against revision 1 by store.
func contractSubset[T any](declared, enabled map[string]T) bool {
	for name, contract := range enabled {
		original, exists := declared[name]
		if !exists || !SameContract(original, contract) {
			return false
		}
	}
	return true
}
