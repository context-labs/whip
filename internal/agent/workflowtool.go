package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/tools"
	"github.com/context-labs/whip/internal/workflow"
)

// workflowTool lets the model run a dynamic workflow: a deterministic
// JavaScript orchestration script that fans work out to subagents. This is
// the whip port of the pi better-workflows extension's tool (tool.ts), which
// is itself faithful to Claude Code's Workflow tool.
//
// Runs are BACKGROUND by default: the tool returns a run id immediately and
// the finished result is steered back into the parent conversation — the same
// close-to-broadcast + Steer fan-in the background subagent registry uses
// (background.go), no new plumbing.
func workflowTool(parent *Agent) tools.Tool {
	return tools.Tool{
		Def: llm.NewTool("workflow", workflowDescription, workflowSchema),
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var a struct {
				Script          string          `json:"script"`
				ScriptPath      string          `json:"scriptPath"`
				Args            json.RawMessage `json:"args"`
				ResumeFromRunID string          `json:"resumeFromRunId"`
			}
			if err := json.Unmarshal(args, &a); err != nil {
				return "", err
			}
			script, err := resolveWorkflowScript(a.Script, a.ScriptPath)
			if err != nil {
				//nolint:nilerr // tool contract: failures are tool output the model reads, never loop aborts
				return "Error: " + err.Error(), nil
			}
			var scriptArgs any
			if len(a.Args) > 0 {
				if err := json.Unmarshal(a.Args, &scriptArgs); err != nil {
					return "Error: args must be a JSON value: " + err.Error(), nil
				}
			}

			mgr := parent.Workflows()
			run, err := mgr.Start(script, scriptArgs, a.ResumeFromRunID)
			if err != nil {
				return "Error: " + err.Error(), nil
			}
			return fmt.Sprintf(`Workflow started in the background. Run ID: %s
Script persisted at: %s
It keeps running on its own; when it finishes the result is delivered back here and the conversation continues automatically — the user does not need to do anything.
To iterate, edit the script file and re-invoke with { scriptPath: %q, resumeFromRunId: %q } — unchanged agent() calls replay from the journal.
If the completion message is truncated, read the full result with: jq '.result' ~/.whip/workflows/runs/%s.json`,
				run.ID, run.ScriptPath, run.ScriptPath, run.ID, run.ID), nil
		},
	}
}

// resolveWorkflowScript implements tool.ts resolveScript: inline script
// (Markdown fences stripped) or a persisted scriptPath.
func resolveWorkflowScript(script, scriptPath string) (string, error) {
	if s := strings.TrimSpace(script); s != "" {
		// Strip a single surrounding ``` fence if the model added one.
		if strings.HasPrefix(s, "```") {
			if i := strings.IndexByte(s, '\n'); i >= 0 {
				s = s[i+1:]
			}
			s = strings.TrimSuffix(strings.TrimSpace(s), "```")
			return strings.TrimSpace(s), nil
		}
		return s, nil
	}
	if p := strings.TrimSpace(scriptPath); p != "" {
		data, err := os.ReadFile(p)
		if err != nil {
			return "", fmt.Errorf("could not read scriptPath %q: %w", p, err)
		}
		return string(data), nil
	}
	return "", errors.New("workflow requires one of: `script` or `scriptPath`")
}

// Workflows returns the agent's workflow run manager, creating it lazily.
// The runner bridges workflow agent() calls into fresh subagents on this
// agent's routes (SubModel precedence: per-call override → TaskDefault → the
// parent's own model — newSub).
func (a *Agent) Workflows() *workflow.Manager {
	a.wfMu.Lock()
	defer a.wfMu.Unlock()
	if a.wf == nil {
		a.wf = workflow.NewManager(a.runWorkflowAgent, "")
		a.wf.OnSettle = a.onWorkflowSettle
	}
	return a.wf
}

// runWorkflowAgent is the workflow.Runner: one agent() call = one fresh
// subagent Turn. Usage rolls into the parent's session totals.
func (a *Agent) runWorkflowAgent(ctx context.Context, req workflow.AgentRequest) (any, workflow.Usage, error) {
	o, err := a.resolveSub(req.Model, "")
	if err != nil {
		return nil, workflow.Usage{}, fmt.Errorf("model override: %w", err)
	}
	o.Effort = req.Effort
	sub := a.newSub(o)

	// Structured output: with a schema the subagent must end by calling a
	// structured_output tool whose args ARE the return value (agent.ts).
	if len(req.Options.Schema) > 0 {
		capture := &structuredCapture{}
		sub.Tools = append(sub.Tools, structuredOutputTool(req.Options.Schema, capture))
		prompt := req.Prompt + "\n\nFinal output contract:\n- Your final action MUST be a single structured_output tool call.\n- Its arguments are the return value of this subagent.\n- Inspect files / run commands first if needed, then call structured_output exactly once."
		_, terr := sub.Turn(ctx, prompt, Events{OnUsage: a.AddUsage})
		if capture.called {
			return capture.value, usageOf(sub), nil
		}
		// One repair pass (agent.ts resolveStructured).
		if terr == nil {
			_, terr = sub.Turn(ctx, "You did not call the structured_output tool. Call structured_output now as your only action, with every required field filled in. Do not write a prose answer.", Events{OnUsage: a.AddUsage})
			if capture.called {
				return capture.value, usageOf(sub), nil
			}
		}
		if terr != nil {
			return nil, usageOf(sub), terr
		}
		return nil, usageOf(sub), fmt.Errorf("agent %q did not produce valid structured_output", req.Options.Label)
	}

	report, terr := sub.Turn(ctx, req.Prompt, Events{OnUsage: a.AddUsage})
	if terr != nil {
		return nil, usageOf(sub), terr
	}
	return report, usageOf(sub), nil
}

// usageOf reads the subagent's accumulated usage for the budget counter.
func usageOf(sub *Agent) workflow.Usage {
	u := sub.Usage()
	return workflow.Usage{Total: u.PromptTokens + u.CompletionTokens}
}

// onWorkflowSettle fans a finished run back into the parent as a steered
// message (settle → Steer, the same shape as background subagents).
func (a *Agent) onWorkflowSettle(run *workflow.ManagedRun) {
	snap, _ := a.Workflows().Snapshot(run.ID)
	var b strings.Builder
	fmt.Fprintf(&b, "[workflow %s %s] %s", run.ID, run.Status, run.Name)
	if snap.Error != "" {
		fmt.Fprintf(&b, "\nError: %s", snap.Error)
	}
	if run.Status == "complete" && snap.Result != nil {
		out, _ := json.MarshalIndent(snap.Result, "", "  ")
		text := string(out)
		if len(text) > subagentReportCap {
			text = text[:subagentReportCap] + fmt.Sprintf("\n\n…(truncated — full result via the run journal: jq '.result' ~/.whip/workflows/runs/%s.json)", run.ID)
		}
		fmt.Fprintf(&b, "\n\n%s", text)
	}
	a.Steer(b.String())
}

// structuredCapture holds the one structured_output call's parsed args.
type structuredCapture struct {
	mu     sync.Mutex
	called bool
	value  any
}

// structuredOutputTool builds the schema-bound capture tool a schema-agent's
// final action must call (structured-output.ts createStructuredOutputTool).
func structuredOutputTool(schema json.RawMessage, capture *structuredCapture) tools.Tool {
	return tools.Tool{
		Def: llm.NewTool("structured_output",
			"Emit the final structured result. Your arguments must validate against the provided JSON schema; they ARE the return value of this subagent.",
			string(schema)),
		Run: func(_ context.Context, args json.RawMessage) (string, error) {
			var v any
			if err := json.Unmarshal(args, &v); err != nil {
				return "", err
			}
			capture.mu.Lock()
			capture.called, capture.value = true, v
			capture.mu.Unlock()
			return "ok", nil
		},
	}
}

// workflowSchema is the workflow tool's JSON schema. Built via concatenation
// because the descriptions contain backticks (Go raw strings can't).
var workflowSchema = `{"type":"object","properties":{` +
	`"script":{"type":"string","description":"Raw JavaScript workflow script (no Markdown fences). First statement must be export const meta = { name, description, phases? }. Body uses agent()/pipeline()/parallel()/phase()/log()/workflow(), and the globals args + budget. Must call agent() at least once. Provide this OR scriptPath."},` +
	`"scriptPath":{"type":"string","description":"Path to a workflow script file to run instead of script. Every invocation persists its script and returns the path; pass it back here (with resumeFromRunId, to resume) after editing."},` +
	`"args":{"description":"JSON value exposed to the script as the global args, verbatim. Pass arrays/objects as actual JSON values, NOT a JSON-encoded string."},` +
	`"resumeFromRunId":{"type":"string","description":"Run ID of a prior invocation to resume from. Completed agent() calls with unchanged (prompt, opts) replay from the journal instantly; the first edited/new call and everything after it run live."}` +
	`}}`

// workflowDescription is the LLM-facing contract, ported from
// better-workflows' description.ts (itself extracted from Claude Code's
// Workflow tool) and trimmed to what the whip port implements (no agentType,
// no isolation, no saved-name registry, no budget.total).
const workflowDescription = `Execute a workflow script that orchestrates multiple subagents deterministically. Workflows run in the background — this tool returns immediately with a run ID, and the result arrives as a message when the workflow completes.

A workflow structures work across many agents — to be comprehensive (decompose and cover in parallel), to be confident (independent perspectives and adversarial checks), or to take on scale one context can't hold (migrations, audits, broad sweeps). The script is where you encode that structure: what fans out, what verifies, what synthesizes.

ONLY call this tool when the user has explicitly opted into multi-agent orchestration in their own words ("use a workflow", "fan out agents", "orchestrate this with subagents"). Workflows can spawn dozens of agents and consume a large amount of tokens; a task that would merely benefit from parallelism does NOT qualify — use a single subagent instead.

Pass the script inline via ` + "`script`" + ` (raw JavaScript, no Markdown fences). Every invocation persists its script to a file and returns the path; to iterate, edit that file and re-invoke with { scriptPath, resumeFromRunId }.

Every script must begin with ` + "`export const meta = {...}`" + ` — a PURE LITERAL (no variables, function calls, spreads, or template interpolation). Required fields: name, description. Optional: whenToUse, phases (each { title, detail?, model?, effort? }), model, effort.

Script body hooks:
- agent(prompt, opts?): spawn a subagent. opts: { label?, phase?, schema?, model?, effort?, timeoutMs? }. Without schema, returns its final text as a string. With schema (a JSON Schema), the subagent is forced to call a structured_output tool and agent() returns the validated object. Returns null if the subagent dies on a terminal error (filter with .filter(Boolean)).
- pipeline(items, stage1, stage2, ...): run each item through all stages independently, NO barrier between stages. Stage callbacks receive (prevResult, originalItem, index). A stage that throws drops that item to null and skips its remaining stages. DEFAULT to pipeline() for multi-stage work.
- parallel(thunks): run tasks concurrently — a BARRIER. Thunks, not promises: parallel(items.map(x => () => agent(...))). A thunk that throws resolves to null. Use ONLY when you genuinely need all results together.
- log(message): emit progress. phase(title): start a new phase; subsequent agent() calls group under it.
- args: the value passed as the tool's ` + "`args`" + ` input, verbatim. budget: { total: null, spent(), remaining() }.
- workflow({ scriptPath }, args?): run one nested workflow as a sub-step (one level only; shares this run's caps).

Scripts are plain JavaScript, NOT TypeScript. The body runs in an async context — use await directly. Standard JS built-ins are available EXCEPT Date.now()/Math.random()/argless new Date(), which throw (they break resume); pass timestamps via args and vary randomness by index. No filesystem or Node.js API access.

Concurrent agent() calls are capped at min(16, cpu cores - 2) per workflow — excess calls queue. Total agents per run are capped at 1000. A single parallel()/pipeline() call accepts at most 4096 items.

## Resume
The tool result includes a runId. To resume after a kill or a script edit, relaunch with { scriptPath, resumeFromRunId } — the longest unchanged prefix of agent() calls returns cached results instantly; the first edited/new call and everything after it run live. Same script + same args → 100% cache hit.

## Truncated results
When a completed-workflow message arrives truncated, DO NOT respond from the truncated text alone — read the full result from the persisted journal first: jq '.result' ~/.whip/workflows/runs/<runId>.json`
