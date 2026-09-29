package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/browser"
	"github.com/context-labs/whip/internal/computer"
	"github.com/context-labs/whip/internal/engine/process"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tool"
)

func TestExecutionInstructionsMatchSelectedEngineAndModules(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			tree := session.Tree{Engine: engine}
			for _, module := range []string{"browser", "computer", "mcp", "user"} {
				current := session.Session{ID: "root", Config: session.Configuration{Modules: []string{module}}}
				guide := executionInstructions(current, tree)
				for _, other := range []string{"browser.list_tabs", "computer.run", "mcp.list_servers", "user.ask"} {
					if strings.Contains(guide, other) != strings.HasPrefix(other, module+".") {
						t.Errorf("module %s describes unavailable operation %s", module, other)
					}
				}
				if !strings.Contains(guide, "json.encode") || !strings.Contains(guide, "checkpoint") {
					t.Fatal("local libraries or checkpoint contract missing")
				}
				if engine == session.QuickJS && (!strings.Contains(guide, "not Node.js") || !strings.Contains(guide, "await each result") || strings.Contains(guide, "time.parse_time")) {
					t.Fatal("JavaScript guide describes the wrong execution environment")
				}
				if engine == session.Starlark && (!strings.Contains(guide, "not Python") || strings.Contains(guide, "await ")) {
					t.Fatal("Starlark guide describes the wrong execution environment")
				}
			}
			parent := session.SessionID("parent")
			child := session.Session{ID: "child", ParentID: &parent, Config: session.Configuration{Modules: []string{"user", "agents"}}}
			guide := executionInstructions(child, tree)
			if !strings.Contains(guide, "user.ask is root-only") || strings.Contains(guide, "Question example:") {
				t.Fatal("child guide invites a forbidden human question")
			}
			if !strings.Contains(guide, "live permission-mode changes") || strings.Contains(guide, "does not restore prior child authority") {
				t.Fatal("child guide does not describe live policy inheritance")
			}
		})
	}
}

func TestExecutionInstructionsChildNamesAndCapturedTemplateAliases(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			current := session.Session{ID: "root", Config: session.Configuration{
				Modules: []string{"agents"},
				Children: map[string]session.DefinitionRef{
					"z-review": {ID: "reviewer"}, "a:research": {ID: "researcher"},
				},
			}}
			captured := current
			captured.Config = current.Config.Clone()
			current.Config.Children["later"] = session.DefinitionRef{ID: "later"}
			tree := session.Tree{Engine: engine}
			guide := executionInstructions(captured, tree)
			if !strings.Contains(guide, `Available child template aliases: ["a:research","z-review"].`) || strings.Contains(guide, `"later"`) {
				t.Fatal("guide did not preserve the sorted captured alias list")
			}
			for _, contract := range []string{
				"immutable display label", "Duplicate names are allowed", "always use session_id",
				"mutually exclusive with definition", "cannot widen the parent's modules",
			} {
				if !strings.Contains(guide, contract) {
					t.Errorf("child guidance omitted %q", contract)
				}
			}
			example := `agents.spawn(prompt="work", name="Reviewer")`
			if engine == session.QuickJS {
				example = `await agents.spawn({prompt:"work", name:"Reviewer"})`
			}
			if !strings.Contains(guide, example) {
				t.Fatal("guide omitted the engine's named spawn example")
			}
			captured.Config.Modules = []string{}
			withoutAgents := executionInstructions(captured, tree)
			if strings.Contains(withoutAgents, "child template aliases") || strings.Contains(withoutAgents, "a:research") || strings.Contains(withoutAgents, "agents.spawn") {
				t.Fatal("disabled agents module exposed child instructions or aliases")
			}
			captured.Config.Modules, captured.Config.Children = []string{"agents"}, nil
			if !strings.Contains(executionInstructions(captured, tree), "Available child template aliases: [].") {
				t.Fatal("empty alias list was not represented explicitly")
			}
		})
	}
}

// Execute the exact fenced examples shipped in the prompt. Host examples are
// validated at their real argument/parser boundaries without acquiring a
// browser, asking a human, connecting a server or performing an external effect.
func TestExecutionInstructionsExamplesBothEngines(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			modules := []string{"browser", "computer", "mcp", "user"}
			current := session.Session{ID: "root", TreeID: "tree", ConfigRevision: 1, Config: session.Configuration{Modules: modules, MCPServers: &session.MCPSelection{All: true}}}
			guide := executionInstructions(current, session.Tree{Engine: engine})
			examples := regexp.MustCompile("(?s)```(?:starlark|javascript)\n(.*?)\n```").FindAllStringSubmatch(guide, -1)
			if len(examples) != 5 {
				t.Fatalf("expected all five executable examples, got %d", len(examples))
			}
			calls := []string{}
			kernel, err := process.NewKernel(process.KernelOptions{
				Command: engineOptions(t).EngineCommand,
				Engine:  string(engine), Modules: modules,
				Host: process.HostFunc(func(ctx context.Context, module, operation string, args map[string]any) (any, error) {
					calls = append(calls, module+"."+operation)
					switch module {
					case "browser":
						if operation == "list_tabs" && len(args) == 0 {
							return map[string]any{"tabs": []any{}}, nil
						}
						var parsed browserArguments
						if err := decodeArguments(args, &parsed); err != nil {
							return nil, err
						}
						if operation != "run" || parsed.AttachmentID != "attachment-id" || parsed.ExpectedDocument != "document-revision" {
							return nil, fmt.Errorf("invalid documented browser target: %+v", parsed)
						}
						_, err := browser.CompileProgram(parsed.Code)
						return map[string]any{}, err
					case "computer":
						code, ok := args["code"].(string)
						if !ok || len(args) != 1 || operation != "run" {
							return nil, fmt.Errorf("invalid documented computer arguments: %+v", args)
						}
						_, err := computer.CompileBatch(code)
						return map[string]any{}, err
					case "mcp":
						// Preparing catalog metadata is pure and runs the native request validator.
						r := &Runtime{mcp: &mcpOwners{ctx: ctx}}
						_, err := r.prepareMCP(ctx, current, tool.Invocation{Module: module, Name: operation, Arguments: args})
						return []any{}, err
					case "user":
						raw, err := json.Marshal(args)
						if err != nil {
							return nil, err
						}
						question, err := session.ParseQuestionRequest(raw)
						if err != nil {
							return nil, err
						}
						return question.AnswerValue([]session.QuestionAnswer{{Answer: []string{"Markdown"}}})
					}
					return nil, fmt.Errorf("unexpected example call %s.%s", module, operation)
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(kernel.Close)
			for index, example := range examples {
				result, err := kernel.Exec(t.Context(), process.Cell{Code: example[1]})
				if err != nil {
					t.Fatalf("example %d: %v\n%s", index, err, example[1])
				}
				if index == 0 && (!strings.Contains(result.Output, "9007199254740993") || !strings.Contains(result.Output, "9") || !strings.Contains(result.Output, "2026")) {
					t.Fatalf("local library example lost data: %q", result.Output)
				}
			}
			want := []string{"browser.list_tabs", "browser.run", "computer.run", "mcp.list_servers", "mcp.search", "user.ask"}
			if !reflect.DeepEqual(calls, want) {
				t.Fatalf("examples called %v; want %v", calls, want)
			}
		})
	}
}
