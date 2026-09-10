package rlm

import (
	"fmt"
	"slices"
	"strings"
)

// ModuleNames returns every host module in the order the runtime guide
// describes them.
func ModuleNames() []string {
	names := make([]string, 0, len(moduleRegistry))
	for _, line := range guideCatalog {
		for _, module := range line.modules {
			if !slices.Contains(names, module) {
				names = append(names, module)
			}
		}
		for _, part := range line.parts {
			if !slices.Contains(names, part.module) {
				names = append(names, part.module)
			}
		}
	}
	return names
}

// SystemPrompt is a definition's standalone system prompt for one engine: the
// persona followed by the runtime guide. ComposePrompt adds identity, rules,
// environment, and discovered instructions around it.
func SystemPrompt(engine, persona string, modules []string, workingDirectory string, history *ContextHandle) (string, error) {
	guide, err := RuntimeGuide(engine, modules, workingDirectory, history)
	if err != nil || persona == "" {
		return guide, err
	}
	return persona + " " + guide, nil
}

// RuntimeGuide renders the execution engine's guidance for the selected host
// modules: the rlm_exec introduction, the module catalog, the rules, the
// messaging section, then the working directory and any context handle. It is
// the runtime-owned part of a system prompt and never names an agent.
func RuntimeGuide(engine string, modules []string, workingDirectory string, history *ContextHandle) (string, error) {
	// Engine descriptors hash this guide, so resolve the id directly.
	if engine == "" {
		engine = EngineStarlark
	}
	if engine != EngineStarlark && engine != EngineQuickJS {
		return "", fmt.Errorf("unsupported execution engine %q", engine)
	}
	for _, module := range modules {
		if _, ok := moduleRegistry[module]; !ok {
			return "", fmt.Errorf("unknown RLM module %q", module)
		}
	}
	javascript := engine == EngineQuickJS
	var b strings.Builder
	b.WriteString(guideIntro.text(javascript))
	b.WriteString("\n\n")
	b.WriteString(guideCatalogHeader.text(javascript))
	writeGuideLines(&b, guideCatalog, modules, javascript)
	b.WriteString("\n\nRules:")
	writeGuideLines(&b, guideRules, modules, javascript)
	if slices.ContainsFunc(guideMessaging, func(line guideLine) bool { return line.selected(modules) }) {
		b.WriteString("\n\nMessaging and delegation (runtime behavior):")
		writeGuideLines(&b, guideMessaging, modules, javascript)
	}
	if workingDirectory != "" {
		b.WriteString("\n\nWorking directory: " + workingDirectory)
	}
	if history != nil && history.ReferenceID != "" {
		fmt.Fprintf(&b, "\nAvailable context: handle=%s size=%d source=%s", history.ReferenceID, history.Size, history.Source)
	}
	guide := b.String()
	if javascript {
		guide = strings.ReplaceAll(javascriptExamples(guide), "include_grants=True", "include_grants: true")
	}
	return guide, nil
}

func writeGuideLines(b *strings.Builder, lines []guideLine, modules []string, javascript bool) {
	for _, line := range lines {
		if !line.selected(modules) {
			continue
		}
		b.WriteString("\n")
		if line.parts != nil {
			b.WriteString(line.joined(modules))
			continue
		}
		b.WriteString(line.text(javascript))
	}
}

func (line guideLine) text(javascript bool) string {
	if javascript && line.javascript != "" {
		return line.javascript
	}
	return line.starlark
}

// selected reports whether a line applies: core lines always do, and a line
// with modules or parts applies when any of them was selected.
func (line guideLine) selected(modules []string) bool {
	if len(line.modules) == 0 && len(line.parts) == 0 {
		return true
	}
	for _, module := range line.modules {
		if slices.Contains(modules, module) {
			return true
		}
	}
	for _, part := range line.parts {
		if slices.Contains(modules, part.module) {
			return true
		}
	}
	return false
}

func (line guideLine) joined(modules []string) string {
	texts := make([]string, 0, len(line.parts))
	for _, part := range line.parts {
		if slices.Contains(modules, part.module) {
			texts = append(texts, part.text)
		}
	}
	return "- " + strings.Join(texts, ", ")
}
