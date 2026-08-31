// taskmodel.go: subagent model routing. Subagents default to the same cheap
// fast route compaction uses (config.DefaultTaskModel); the user pins it with
// config taskModel/taskProvider; the main model overrides per task via the
// task tool's model/provider params. Exported helpers so `whip run` wires the
// same routing headlessly.
package tui

import (
	"fmt"
	"strings"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
)

// SubModelFor resolves a model name (config entry or catalog id) into a
// subagent route: client, API model id, context window, output cap.
func SubModelFor(cfg *config.Config, model, provider string) (agent.SubModel, error) {
	prov, mdl, apiID, err := cfg.Resolve(model, provider)
	if err != nil {
		return agent.SubModel{}, err
	}
	key, err := prov.ResolveKey()
	if err != nil {
		return agent.SubModel{}, err
	}
	if key == "" {
		return agent.SubModel{}, fmt.Errorf("no API key for the provider serving %q", model)
	}
	cli := llm.New(prov.BaseURL, key)
	cli.MaxRetries = cfg.MaxRetries
	return agent.SubModel{Client: cli, Model: apiID, ContextLimit: mdl.ContextWindow(), MaxTokens: mdl.MaxOut}, nil
}

// TaskDefaultFor resolves the default subagent route: cfg.taskModel when set
// (its failure is an error worth surfacing), else config.DefaultTaskModel,
// else a catalog id ending in "/<default>" (gateway catalogs like openrouter
// prefix ids with the vendor). A missing default is not an error — the zero
// SubModel falls back to the conversation's own model.
func TaskDefaultFor(cfg *config.Config) (agent.SubModel, error) {
	tm, explicit := cfg.TaskModel, cfg.TaskModel != ""
	if !explicit {
		tm = config.DefaultTaskModel
	}
	o, err := SubModelFor(cfg, tm, cfg.TaskProvider)
	if err == nil {
		return o, nil
	}
	if explicit {
		return agent.SubModel{}, err
	}
	if id := catalogSuffixMatch(tm); id != "" {
		if o, err2 := SubModelFor(cfg, id, ""); err2 == nil {
			return o, nil
		}
	}
	return agent.SubModel{}, nil
}

// catalogSuffixMatch scans the cached provider catalogs for a model id ending
// in "/<name>". First hit wins ("" when none).
func catalogSuffixMatch(name string) string {
	for _, cat := range config.LoadCatalogs() {
		for _, mi := range cat.Models {
			if strings.HasSuffix(mi.ID, "/"+name) {
				return mi.ID
			}
		}
	}
	return ""
}

// applyTaskModel points subagents at the configured task model and installs
// the per-task model resolver. The resolver runs on tool worker goroutines,
// so it closes over a config snapshot, never live m.cfg (which /auth and
// /model mutate on the UI goroutine); every agent swap re-applies with a
// fresh snapshot.
func (m *model) applyTaskModel() {
	snap := m.cfg.Snapshot()
	m.agent.ResolveModel = func(model, provider string) (agent.SubModel, error) {
		return SubModelFor(snap, model, provider)
	}
	o, err := TaskDefaultFor(snap)
	if err != nil {
		m.agent.TaskDefault = agent.SubModel{}
		m.append(errStyle.Render("task model: " + err.Error() + " — subagents use the current model"))
		return
	}
	m.agent.TaskDefault = o
}

// taskCommand handles "/subagent [-m model[@provider]] <prompt>": spawn a
// background subagent by hand. Works while a turn is running — its report
// steers into the conversation like any model-spawned task.
func (m *model) taskCommand(rest string) {
	model, prov := "", ""
	if strings.HasPrefix(rest, "-m ") {
		rest = strings.TrimSpace(rest[3:])
		spec, tail, found := strings.Cut(rest, " ")
		if !found {
			rest = "" // "-m model" with no prompt: fall through to usage
		} else {
			rest = strings.TrimSpace(tail)
			if at, prov2, ok := strings.Cut(spec, "@"); ok {
				model, prov = at, prov2
			} else {
				model = spec
			}
		}
	}
	if rest == "" {
		m.append(dimStyle.Render("usage: /subagent [-m model[@provider]] <prompt> — spawn a background subagent (ctrl+t to watch, /subagents to list)"))
		return
	}
	var o agent.SubModel
	if model != "" {
		if m.agent.ResolveModel == nil {
			m.append(errStyle.Render("task model: overrides unavailable"))
			return
		}
		var err error
		if o, err = m.agent.ResolveModel(model, prov); err != nil {
			m.append(errStyle.Render("task model: " + err.Error()))
			return
		}
	}
	t := m.agent.StartBackground(taskDesc(rest), rest, o)
	m.append(dimStyle.Render(fmt.Sprintf("⚙ %s started — %s  (ctrl+t to watch · /subagents %s to open)", t.ID, taskDesc(rest), t.ID)))
}

// subagentModelCommand picks the model subagents run on. The pick persists to
// the global config (taskModel/taskProvider); "off" restores the built-in
// default. Driven from the ctrl+p "Subagent model" picker. The model may be a
// config entry or a catalog-advertised id (the catalog fallback in Resolve
// routes it); anything else resolves fuzzy before giving up.
func (m *model) subagentModelCommand(args []string) {
	if args[0] == "off" {
		m.cfg.TaskModel, m.cfg.TaskProvider = "", ""
		m.applyTaskModel()
		if err := m.cfg.Save(); err != nil {
			m.append(errStyle.Render("config save failed: " + err.Error()))
		}
		m.append(dimStyle.Render("◎ subagent model: default (" + config.DefaultTaskModel + ")"))
		return
	}
	model, prov := args[0], ""
	if at, p, ok := strings.Cut(model, "@"); ok {
		model, prov = at, p
	}
	if _, ok := m.cfg.Models[model]; !ok && !catalogAdvertises(m.cfg, model) {
		resolved, ok2, cands := resolveModelFuzzy(m.cfg, model)
		if !ok2 {
			if len(cands) > 0 {
				m.append(errStyle.Render("ambiguous model " + model + " — could be " + strings.Join(cands, ", ")))
			} else {
				m.append(errStyle.Render("unknown model " + model))
			}
			return
		}
		model = resolved
	}
	if len(args) > 1 {
		prov = args[1]
	}
	if _, err := SubModelFor(m.cfg, model, prov); err != nil {
		m.append(errStyle.Render("task model: " + err.Error()))
		return
	}
	m.cfg.TaskModel, m.cfg.TaskProvider = model, prov
	m.applyTaskModel() // the explicit pick resolves (checked above) — can't fail
	if err := m.cfg.Save(); err != nil {
		m.append(errStyle.Render("config save failed: " + err.Error()))
	}
	note := "◎ subagent model: " + model
	if p := resolvedProvider(m.cfg, model, prov); p != "" {
		note += " @ " + p
	}
	m.append(dimStyle.Render(note))
}

// taskDesc derives a short dock description from a /task prompt.
func taskDesc(prompt string) string {
	f := strings.Fields(prompt)
	if len(f) > 8 {
		return strings.Join(f[:8], " ") + "…"
	}
	return strings.Join(f, " ")
}
