package runtime

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/lsp"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func TestLanguageServerFixture(t *testing.T) {
	if os.Getenv("WHIP_LSP_FIXTURE") != "1" {
		return
	}
	reader := bufio.NewReader(os.Stdin)
	for {
		header, err := reader.ReadString('\n')
		if err != nil {
			os.Exit(0)
		}
		length, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(header, "Content-Length:")))
		if err != nil || length < 0 || length > 1<<20 {
			os.Exit(1)
		}
		if _, err := reader.ReadString('\n'); err != nil {
			os.Exit(1)
		}
		body := make([]byte, length)
		if _, err := io.ReadFull(reader, body); err != nil {
			os.Exit(0)
		}
		var message struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				TextDocument struct {
					URI     string `json:"uri"`
					Version int    `json:"version"`
				} `json:"textDocument"`
			} `json:"params"`
		}
		if json.Unmarshal(body, &message) != nil {
			os.Exit(1)
		}
		var response map[string]any
		if message.Method == "textDocument/didOpen" || message.Method == "textDocument/didChange" {
			response = map[string]any{"jsonrpc": "2.0", "method": "textDocument/publishDiagnostics", "params": map[string]any{"uri": message.Params.TextDocument.URI, "version": message.Params.TextDocument.Version, "diagnostics": []any{map[string]any{"range": map[string]any{"start": map[string]int{"line": 1, "character": 2}}, "severity": 1, "message": "fixture diagnostic"}}}}
		} else if len(message.ID) > 0 {
			response = map[string]any{"jsonrpc": "2.0", "id": message.ID, "result": map[string]any{}}
		} else {
			continue
		}
		encoded, _ := json.Marshal(response)
		if _, err := fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(encoded), encoded); err != nil {
			os.Exit(0)
		}
	}
}

func TestBothEnginesLanguageServerAuthorityAndLifetime(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			write := "print(files.write(path=\"main.go\",content=\"package main\"))"
			explicit := "print(files.diagnostics(path=\"main.go\"))"
			if engine == session.QuickJS {
				write = "print(await files.write({path:'main.go',content:'package main'}))"
				explicit = "print(await files.diagnostics({path:'main.go'}))"
			}
			r := openEngineTest(t, t.TempDir(), cellProvider(map[string]string{"write": write, "explicit": explicit, "standing": write, "child": write, "revoked": write, "missing": write}))
			snapshot, err := r.HostConfiguration().Snapshot(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			_, err = r.HostConfiguration().Update(t.Context(), snapshot.Revision, func(host *config.Host) error {
				host.LSP = map[string]lsp.Config{"fixture": {Command: []string{os.Args[0], "-test.run=^TestLanguageServerFixture$"}, Extensions: []string{".go"}, RootMarkers: []string{"go.mod"}, Env: map[string]string{"WHIP_LSP_FIXTURE": "1"}}, "gopls": {Enabled: new(false)}}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			root := createEngineSession(t, r, engine)
			finish := func(key string, owner session.SessionID, want int) []session.Operation {
				t.Helper()
				admitted := waitTestWithin(t, r, key, terminal, 30*time.Second)
				if admitted.Turn.State != session.Succeeded {
					t.Fatal(admitted.Turn)
				}
				ops, err := r.Operations(t.Context(), admitted.Turn.ID, "", 10)
				if err != nil || len(ops) != want {
					t.Fatal(key, ops, err)
				}
				for _, op := range ops {
					if op.SessionID != owner {
						t.Fatal("cross-owner operation", op)
					}
				}
				return ops
			}
			checkStatus := func(owner session.SessionID, want string) {
				t.Helper()
				values, err := r.LSPStatus(t.Context(), owner)
				if err != nil || len(values) != 1 || values[0].State != want {
					t.Fatal(values, err)
				}
			}
			submitTest(t, r, root.ID, "write")
			pending := awaitRuntimeFilePermission(t, r, root.ID, "write", "files.write")
			if _, err := r.ResolvePermission(t.Context(), pending.ID, true); err != nil {
				t.Fatal(err)
			}
			finish("write", root.ID, 1)
			checkStatus(root.ID, "not started")
			submitTest(t, r, root.ID, "explicit")
			pending = awaitRuntimeFilePermission(t, r, root.ID, "explicit", "lsp.diagnostics")
			checkStatus(root.ID, "not started")
			if _, err := r.ResolvePermission(t.Context(), pending.ID, true); err != nil {
				t.Fatal(err)
			}
			ops := finish("explicit", root.ID, 1)
			if ops[0].State != session.OperationSucceeded || !strings.Contains(string(ops[0].Result.Value), "fixture diagnostic") {
				t.Fatal(ops)
			}
			checkStatus(root.ID, "not started")
			for _, capability := range []string{"files.write", "lsp.diagnostics"} {
				if _, err := r.CreateGrant(t.Context(), session.Grant{ID: session.GrantID(capability), SessionID: root.ID, Capability: capability, Resource: root.WorkingDirectory}); err != nil {
					t.Fatal(err)
				}
			}
			submitTest(t, r, root.ID, "standing")
			finish("standing", root.ID, 2)
			checkStatus(root.ID, "connected")
			child, err := r.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "child"}, store.ChildRequest{ParentID: root.ID, Parts: []session.Part{{Type: "text", Text: "child"}}})
			if err != nil {
				t.Fatal(err)
			}
			finish("child", child.Session.ID, 2)
			checkStatus(child.Session.ID, "connected")
			if _, err := r.RevokeGrant(t.Context(), "lsp.diagnostics"); err != nil {
				t.Fatal(err)
			}
			checkStatus(root.ID, "not started")
			checkStatus(child.Session.ID, "not started")
			submitTest(t, r, child.Session.ID, "revoked")
			finish("revoked", child.Session.ID, 1)
			if _, err := os.Stat(filepath.Join(root.WorkingDirectory, "main.go")); err != nil {
				t.Fatal(err)
			}
		})
	}
}
