package protocol

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestEveryLiteralEventProducerIsRegistered(t *testing.T) {
	registry := EventPayloads()
	for _, directory := range []string{"../daemon", "../session"} {
		files, err := os.ReadDir(directory)
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			if !strings.HasSuffix(file.Name(), ".go") || strings.HasSuffix(file.Name(), "_test.go") {
				continue
			}
			path := filepath.Join(directory, file.Name())
			tree, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(tree, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				name := ""
				switch fn := call.Fun.(type) {
				case *ast.Ident:
					name = fn.Name
				case *ast.SelectorExpr:
					name = fn.Sel.Name
				}
				switch name {
				case "emit", "AppendRootEvent", "insertActorEventTx", "enqueueInboxTx", "emitQuestionEvent", "emitSessionUpdate":
				default:
					return true
				}
				for _, arg := range call.Args {
					literal, ok := arg.(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING {
						continue
					}
					kind, err := strconv.Unquote(literal.Value)
					if err != nil || !strings.Contains(kind, ".") {
						continue
					}
					if _, ok := registry[kind]; !ok {
						t.Errorf("%s emits unregistered event %s", path, kind)
					}
				}
				return true
			})
		}
	}
}
