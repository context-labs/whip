package tui

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClientCLIParametersUseNamedFields(t *testing.T) {
	for _, test := range []struct{ name, input, method, want string }{
		{"schedule", "@every 10m inspect CI", "schedule.create", `"schedule":"@every 10m"`},
		{"mcp", "alpha disable", "mcp.disable", `"name":"alpha"`},
		{"mcp", "import claude off", "mcp.import.configure", `"enabled":false`},
		{"browser", "driver chromedp", "browser.set_driver", `"driver":"chromedp"`},
		{"computer", "allow Visual Studio Code", "computer.allow", `"app":"Visual Studio Code"`},
		{"budget.cap", "child tokens 9007199254740993", "budget.cap", `"limit":"9007199254740993"`},
		{"history.rewind", "12", "history.rewind", `"cut":12`},
	} {
		t.Run(test.method, func(t *testing.T) {
			method, payload, err := clientCLIParameters(test.name, test.input)
			if err != nil {
				t.Fatal(err)
			}
			body, err := json.Marshal(payload)
			if err != nil || method != test.method || !strings.Contains(string(body), test.want) || strings.Contains(string(body), `"args"`) {
				t.Fatalf("method=%s payload=%s error=%v", method, body, err)
			}
		})
	}
}

func TestClientCLIParametersSplitQueriesFromMutations(t *testing.T) {
	for _, name := range []string{"schedule", "mcp", "lsp", "browser", "computer"} {
		t.Run(name, func(t *testing.T) {
			method, payload, err := clientCLIParameters(name, "")
			if err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(payload)
			if method == name || string(body) != "{}" {
				t.Fatalf("query %s payload=%s", method, body)
			}
		})
	}
}
