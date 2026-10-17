package commandpresentation

import (
	"testing"
)

func TestDecodeCommandPresentation(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, operation, body, status, output, failure string
	}{
		{name: "submit", operation: "submit", body: `{"text":"answer"}`, output: "answer"},
		{name: "steer", operation: "steer", body: `{"text":"answer"}`, output: "answer"},
		{name: "submit parts", operation: "submit.parts", body: `{"text":"answer"}`, output: "answer"},
		{name: "steer parts", operation: "steer.parts", body: `{"text":"answer"}`, output: "answer"},
		{name: "shell", operation: "shell.run", body: `{"text":"answer"}`, output: "answer"},
		{name: "tool", operation: "tool.call", body: `{"text":"answer"}`, output: "answer"},
		{name: "create", operation: "session.create", body: `{"root_id":"root"}`, output: "root"},
		{name: "open", operation: "session.open", body: `{"root_id":"root"}`, output: "root"},
		{name: "fork", operation: "session.fork", body: `{"root_id":"root"}`, output: "root"},
		{name: "delete", operation: "session.delete", body: `{"root_id":"root"}`, output: "root"},
		{name: "inspect", operation: "workspace.inspect", body: `{"path":"/workspace"}`, output: "/workspace"},
		{name: "set workspace", operation: "workspace.set", body: `{"path":"/workspace"}`, output: "/workspace"},
		{name: "rename", operation: "session.rename", body: `{"title":"title"}`, output: "title"},
		{name: "set goal", operation: "goal.set", body: `{"goal":"goal"}`, output: "goal"},
		{name: "run goal", operation: "goal.run", body: `{"goal":"goal"}`, output: "goal"},
		{name: "derive goal", operation: "goal.from-context", body: `{"goal":"goal"}`, output: "goal"},
		{name: "set effort", operation: "session.effort", body: `{"effort":"high"}`, output: "high"},
		{name: "get effort", operation: "session.effort.get", body: `{"effort":"high"}`, output: "high"},
		{name: "set model", operation: "session.model", body: `{"model":"m","provider":"p"}`, output: "m @ p"},
		{name: "get model", operation: "session.model.get", body: `{"model":"m","provider":"p"}`, output: "m @ p"},
		{name: "reload model", operation: "session.reload", body: `{"model":"m","provider":"p"}`, output: "m @ p"},
		{name: "empty success", operation: "submit"},
		{name: "empty failure", operation: "submit", status: "failed"},
		{name: "failed", operation: "submit", body: `{"message":"failure"}`, status: "failed", failure: "failure"},
		{name: "cancelled", operation: "submit", body: `{"message":"failure"}`, status: "cancelled", failure: "failure"},
		{name: "interrupted unknown operation", operation: "unknown", body: `{"message":"failure"}`, status: "interrupted", failure: "failure"},
		{name: "empty failure object", operation: "submit", body: `{}`, status: "failed"},
		{name: "null failure", operation: "submit", body: `null`, status: "failed"},
		{name: "null failure message", operation: "submit", body: `{"message":null}`, status: "failed"},
		{name: "malformed failure", operation: "submit", body: `{`, status: "failed", failure: "invalid structured command failure"},
		{name: "numeric failure message", operation: "submit", body: `{"message":1}`, status: "failed", failure: "invalid structured command failure"},
		{name: "invalid failure code", operation: "submit", body: `{"message":"failure","code":"bad"}`, status: "failed", failure: "invalid structured command failure"},
		{name: "invalid failure data", operation: "submit", body: `{"message":"failure","data":false}`, status: "failed", failure: "invalid structured command failure"},
		{name: "failure field case", operation: "submit", body: `{"MESSAGE":"failure"}`, status: "failed", failure: "failure"},
		{name: "status is case sensitive", operation: "submit", body: `{"text":"answer"}`, status: "FAILED", output: "answer"},
		{name: "ignored field and retained whitespace", operation: "submit", body: `{"text":" answer ","ignored":true}`, output: " answer "},
		{name: "null result field", operation: "submit", body: `{"text":null}`},
		{name: "missing result field", operation: "submit", body: `{}`, failure: "invalid command result field"},
		{name: "null result object", operation: "submit", body: `null`, failure: "invalid command result field"},
		{name: "result field case", operation: "submit", body: `{"Text":"answer"}`, failure: "invalid command result field"},
		{name: "numeric result field", operation: "submit", body: `{"text":1}`, failure: "invalid command result field"},
		{name: "object result field", operation: "submit", body: `{"text":{}}`, failure: "invalid command result field"},
		{name: "array result field", operation: "submit", body: `{"text":[]}`, failure: "invalid command result field"},
		{name: "malformed result", operation: "submit", body: `{`, failure: "invalid command result"},
		{name: "trailing result", operation: "submit", body: `{"text":"a"} {}`, failure: "invalid command result"},
		{name: "scalar result", operation: "submit", body: `"answer"`, failure: "invalid command result"},
		{name: "array result", operation: "submit", body: `[]`, failure: "invalid command result"},
		{name: "duplicate result field", operation: "submit", body: `{"text":"first","text":"last"}`, output: "last"},
		{name: "empty model", operation: "session.model", body: `{}`, output: "@"},
		{name: "null model", operation: "session.model", body: `null`, output: "@"},
		{name: "model only", operation: "session.model", body: `{"model":"m"}`, output: "m @"},
		{name: "provider only", operation: "session.model", body: `{"provider":"p"}`, output: "@ p"},
		{name: "model whitespace", operation: "session.model", body: `{"model":" m ","provider":" p "}`, output: "m  @  p"},
		{name: "model field case", operation: "session.model", body: `{"MODEL":"m","PROVIDER":"p"}`, output: "m @ p"},
		{name: "malformed model", operation: "session.model", body: `{`, failure: "invalid model result"},
		{name: "numeric model", operation: "session.model", body: `{"model":1}`, failure: "invalid model result"},
		{name: "numeric provider", operation: "session.model", body: `{"provider":1}`, failure: "invalid model result"},
		{name: "invalid ignored model field", operation: "session.model", body: `{"reload_pending":"yes"}`, failure: "invalid model result"},
		{name: "unknown plain text", operation: "unknown", body: "answer", output: "answer"},
		{name: "unknown malformed json", operation: "unknown", body: `{`, output: `{`},
		{name: "unknown whitespace", operation: "unknown", body: " \n\t ", output: " \n\t "},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			output, failure := Decode(test.operation, []byte(test.body), test.status)
			if output != test.output || failure != test.failure {
				t.Fatalf("presentation = (%q, %q), want (%q, %q)", output, failure, test.output, test.failure)
			}
		})
	}
	if output, failure := Decode("submit", nil, "failed"); output != "" || failure != "" {
		t.Fatalf("nil failure body = (%q, %q)", output, failure)
	}
}
