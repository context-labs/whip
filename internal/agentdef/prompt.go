package agentdef

import (
	"slices"

	"github.com/context-labs/whip/internal/rlm"
)

// SystemPrompt renders the standalone system prompt for one engine: the persona
// followed by the runtime guide for the selected modules. The composer adds
// identity, rules, environment, and discovered instructions around it.
func (d Definition) SystemPrompt(engine, workingDirectory string, history *rlm.ContextHandle) (string, error) {
	return rlm.SystemPrompt(engine, d.Instructions.Persona, d.Modules, d.RuntimeTools(), d.Output, workingDirectory, history)
}

// RuntimeTools converts the definition's tools to the runtime guide's shape.
func (d Definition) RuntimeTools() []rlm.CustomTool {
	if len(d.Tools) == 0 {
		return nil
	}
	tools := make([]rlm.CustomTool, len(d.Tools))
	for i, tool := range d.Tools {
		tools[i] = rlm.CustomTool{Name: tool.Name, Description: tool.Description, InputSchema: tool.InputSchema, OutputSchema: tool.OutputSchema}
	}
	return tools
}

// PromptOptions applies the definition's instructions and module selection to
// composer options that already carry the environment.
func (d Definition) PromptOptions(options rlm.PromptOptions) rlm.PromptOptions {
	options.Persona, options.Rules = d.Instructions.Persona, d.Instructions.Rules
	options.Modules = slices.Clone(d.Modules)
	options.Tools = d.RuntimeTools()
	options.Output = d.Output
	options.ProjectFiles = slices.Clone(d.Instructions.ProjectFiles)
	options.SkillDiscovery, options.StandingInstructions = d.Instructions.SkillDiscovery, d.Instructions.StandingInstructions
	return options
}
