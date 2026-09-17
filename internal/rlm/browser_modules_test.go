package rlm

import (
	"context"
	"reflect"
	"testing"
)

func TestBrowserLifecycleAndExplicitSpawnSurfaceBothEngines(t *testing.T) {
	for _, tc := range []struct{ engine, code string }{
		{EngineStarlark, `tab = browser.open(url="https://example.test")
browser.attach(tab_id=tab["tab_id"])
browser.run(attachment_id=tab["attachment_id"], code="info()", expected_document="doc-1")
browser.allow_preview_port(attachment_id=tab["attachment_id"], port=3000)
agents.spawn(name="child", prompt="inspect", browser_attachments=[tab["attachment_id"]])
browser.detach(attachment_id=tab["attachment_id"])
browser.run(session="legacy", code="info()")`},
		{EngineQuickJS, `const tab = await browser.open({url:"https://example.test"});
await browser.attach({tab_id:tab.tab_id});
await browser.run({attachment_id:tab.attachment_id, code:"info()", expected_document:"doc-1"});
await browser.allow_preview_port({attachment_id:tab.attachment_id, port:3000});
await agents.spawn({name:"child", prompt:"inspect", browser_attachments:[tab.attachment_id]});
await browser.detach({attachment_id:tab.attachment_id});
await browser.run({session:"legacy", code:"info()"});`},
	} {
		t.Run(tc.engine, func(t *testing.T) {
			var calls []string
			host := HostFunc(func(_ context.Context, module, operation string, args map[string]any) (any, error) {
				calls = append(calls, module+"."+operation)
				if module == "agents" {
					if ids, ok := args["browser_attachments"].([]any); !ok || len(ids) != 1 || ids[0] != "attachment" {
						t.Errorf("spawn lost explicit attachment IDs: %#v", args)
					}
				}
				return map[string]any{"tab_id": "tab", "attachment_id": "attachment", "document_revision": "doc-1"}, nil
			})
			kernel := testModulesKernel(t, tc.engine, []string{"browser", "agents"}, host)
			if _, err := kernel.Exec(t.Context(), tc.code); err != nil {
				t.Fatal(err)
			}
			want := []string{"browser.open", "browser.attach", "browser.run", "browser.allow_preview_port", "agents.spawn", "browser.detach", "browser.run"}
			if !reflect.DeepEqual(calls, want) {
				t.Fatalf("calls=%v want=%v", calls, want)
			}
		})
	}
}
