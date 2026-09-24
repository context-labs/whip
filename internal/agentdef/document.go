package agentdef

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Decode parses a definition document and normalizes it. Unknown fields are an
// error so a typo in an authored document cannot silently disable a setting.
func Decode(document []byte) (Definition, error) {
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.DisallowUnknownFields()
	var definition Definition
	if err := decoder.Decode(&definition); err != nil {
		return Definition{}, fmt.Errorf("agent definition document: %w", err)
	}
	if decoder.More() {
		return Definition{}, errors.New("agent definition document: trailing data")
	}
	return definition.Normalize(), nil
}

// Encode renders the canonical document: Go field order, sorted map keys,
// compact schemas, and null for absent collections.
func Encode(definition Definition) ([]byte, error) {
	return json.Marshal(definition.Normalize())
}

// Revision identifies a document by content: the hex SHA-256 of its canonical
// encoding, stable across key order and whitespace in the source.
func Revision(definition Definition) (string, error) {
	encoded, err := Encode(definition)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// Normalize returns a copy in canonical form. Empty lists become nil and encode
// as null, except MCP.Servers where nil and empty differ; empty maps become
// empty maps and encode as {}, matching the generated schema, which treats maps
// as non-nullable objects. Tool schemas are compacted.
func (d Definition) Normalize() Definition {
	d.Modules = nilIfEmpty(d.Modules)
	d.Capabilities = nilIfEmpty(d.Capabilities)
	d.Instructions.ProjectFiles = nilIfEmpty(d.Instructions.ProjectFiles)
	if d.MCP.Servers != nil {
		d.MCP.Servers = slices.Clone(d.MCP.Servers)
	}
	if len(d.Tools) == 0 {
		d.Tools = nil
	} else {
		tools := make([]Tool, len(d.Tools))
		for i, tool := range d.Tools {
			tools[i] = tool
			tools[i].InputSchema = compactSchema(tool.InputSchema)
			tools[i].OutputSchema = compactSchema(tool.OutputSchema)
		}
		d.Tools = tools
	}
	d.Output = compactSchema(d.Output)
	children := make(map[string]Child, len(d.Children))
	for name, child := range d.Children {
		child.Modules = nilIfEmpty(child.Modules)
		child.Capabilities = nilIfEmpty(child.Capabilities)
		child.Tools = nilIfEmpty(child.Tools)
		budgets := make(map[string]int64, len(child.Budgets))
		maps.Copy(budgets, child.Budgets)
		child.Budgets = budgets
		child.Output = compactSchema(child.Output)
		{
			if child.Instructions != nil {
				instructions := *child.Instructions
				instructions.ProjectFiles = nilIfEmpty(instructions.ProjectFiles)
				child.Instructions = &instructions
			}
			children[name] = child
		}
	}
	d.Children = children
	d.Hooks = normalizeHooks(d.Hooks)
	return d
}

// normalizeHooks copies declared hooks with empty filters as nil and drops an
// empty hooks object entirely.
func normalizeHooks(hooks *Hooks) *Hooks {
	if hooks == nil || (hooks.BeforeTool == nil && hooks.BeforeSpawn == nil && hooks.TurnStart == nil) {
		return nil
	}
	copyHook := func(hook *Hook) *Hook {
		if hook == nil {
			return nil
		}
		value := *hook
		value.Operations = nilIfEmpty(hook.Operations)
		return &value
	}
	return &Hooks{BeforeTool: copyHook(hooks.BeforeTool), BeforeSpawn: copyHook(hooks.BeforeSpawn), TurnStart: copyHook(hooks.TurnStart)}
}

// compactSchema compacts a present schema and turns an absent or null one into
// nil, which encodes as null.
func compactSchema(schema json.RawMessage) json.RawMessage {
	if !schemaPresent(schema) {
		return nil
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, schema); err != nil {
		return slices.Clone(schema)
	}
	return json.RawMessage(compact.Bytes())
}

func nilIfEmpty(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return slices.Clone(values)
}

// ValidateRegistration applies the rules for definitions that arrive over the
// wire in addition to Validate: a well-formed id that does not shadow a
// built-in, and children that name only the definition's own tools.
func (d Definition) ValidateRegistration() error {
	if err := d.Validate(); err != nil {
		return err
	}
	if !definitionID.MatchString(d.ID) {
		return fmt.Errorf("agent definition id %q must match %s", d.ID, definitionID)
	}
	if _, builtIn := Lookup(d.ID); builtIn {
		return fmt.Errorf("agent definition id %q is reserved for a built-in definition (built-ins: %s)", d.ID, strings.Join(IDs(), ", "))
	}
	for name, child := range d.Children {
		for _, tool := range child.Tools {
			if !slices.Contains(d.ToolNames(), tool) {
				return fmt.Errorf("agent definition %q child %q names unknown tool %q", d.ID, name, tool)
			}
		}
	}
	return nil
}
