//go:build integration && unix

package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/rlm"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
)

// scriptedCell finds the cell a fixture prompt asks the model to run: a fenced
// block tagged `cell`. The TypeScript agents acceptance test drives the whole
// runtime through these prompts, so every host call it asserts on is real.
var scriptedCell = regexp.MustCompile("(?s)```cell\n(.*?)\n```")

// scriptedFinal finds the final message a fixture prompt asks for: a fenced
// block tagged `final`, streamed verbatim once any cell has run, so an
// acceptance can exercise a definition's output contract.
var scriptedFinal = regexp.MustCompile("(?s)```final\n(.*?)\n```")

// sdkAgentsFactory backs the SDK fixture with the real recursive runtime and a
// scripted model: a prompt carrying a ```cell block becomes one rlm_exec call,
// and the tool result comes back as the turn's text prefixed with "done: ".
// Sessions resolve their registered definitions from the store, so executors,
// custom tools, named children, and hooks run exactly as they do in production.
func sdkAgentsFactory(store *session.Store) (Factory, func()) {
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var input llm.Request
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var last, prompt llm.Message
		for index := len(input.Messages) - 1; index >= 0; index-- {
			if input.Messages[index].Role != "system" && last.Role == "" {
				last = input.Messages[index]
			}
			if input.Messages[index].Role == "user" {
				prompt = input.Messages[index]
				break
			}
		}
		final := ""
		if match := scriptedFinal.FindStringSubmatch(prompt.Content); match != nil {
			final = match[1]
		}
		switch {
		case last.Role == "tool":
			if final != "" {
				streamText(w, final)
				return
			}
			streamText(w, "done: "+utf8PrefixRuntime(last.Content, 12<<10))
		case last.Role == "user":
			if match := scriptedCell.FindStringSubmatch(last.Content); match != nil {
				streamToolCall(w, "cell", match[1])
				return
			}
			if final != "" {
				streamText(w, final)
				return
			}
			streamText(w, "ack: "+utf8PrefixRuntime(last.Content, 512))
		default:
			streamText(w, "done")
		}
	}))
	factory := func(_ context.Context, meta session.Meta, history []llm.Message) (Components, error) {
		value := agent.NewRuntime(llm.New(model.URL, "fixture-key"), meta.Model, 1024, "", tools.NewServices())
		value.ModelName, value.Provider, value.WorkingDir = meta.Model, meta.Provider, meta.CWD
		value.ContextLimit = 65536
		limits := rlm.DefaultLimits()
		definition, _, err := DefinitionFor(context.Background(), store, meta)
		if err != nil {
			return Components{}, err
		}
		runtime, err := NewRecursiveRuntime(RecursiveRuntimeOptions{
			Engine: meta.ExecutionEngine, Definition: definition, Agent: value, History: history, Limits: limits,
			Kernels: rlm.NewManager(limits.MaxWorkers), KernelCommand: recursiveKernelCommand,
		})
		if err != nil {
			return Components{}, err
		}
		return Components{Runner: runtime.RootSession(), Runtime: runtime, Bind: runtime.Bind, Definition: definition}, nil
	}
	return factory, model.Close
}
