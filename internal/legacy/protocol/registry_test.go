package protocol

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strconv"
	"testing"
)

func TestSessionTitleChangedContract(t *testing.T) {
	if got := Events()["sessions.title.changed"]; got != reflect.TypeFor[SessionTitleChangedParams]() {
		t.Fatalf("title notification type = %v", got)
	}
	payload, err := json.Marshal(SessionTitleChangedParams{RootID: "root"})
	if err != nil || string(payload) != `{"root_id":"root"}` {
		t.Fatalf("title notification payload = %s, %v", payload, err)
	}
}

func TestAutomaticTitlesNeedNoRuntimeOptIn(t *testing.T) {
	if _, ok := LookupRuntime("session.autotitle"); ok {
		t.Fatal("automatic titles must not expose a per-session opt-in command")
	}
}

func TestEveryRuntimeHandlerHasAContract(t *testing.T) {
	source, err := parser.ParseFile(token.NewFileSet(), "../../daemon/client_control.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, declaration := range source.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "isClientOperation" {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			branch, ok := node.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, expression := range branch.List {
				literal, ok := expression.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					continue
				}
				name, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatal(err)
				}
				count++
				if _, ok := LookupRuntime(name); !ok {
					t.Errorf("runtime handler %s has no typed contract", name)
				}
			}
			return true
		})
	}
	if count == 0 {
		t.Fatal("no runtime operations inspected")
	}
}
