package runtime

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func mailEvidenceProvider(codes *sync.Map, inspected chan<- model.Request) providerFunc {
	return func(_ context.Context, request model.Request) (model.Response, error) {
		var key string
		for _, message := range request.Messages {
			if message.Role == session.User {
				if _, exists := codes.Load(message.Parts[0].Text); exists {
					key = message.Parts[0].Text
				}
			}
		}
		last := request.Messages[len(request.Messages)-1]
		if last.Role == session.Tool {
			result := last.Parts[0].Result
			if result.IsError != (key == "foreign-evidence") {
				return model.Response{}, fmt.Errorf("unexpected evidence cell outcome: %s", result.Output)
			}
			return model.Response{Parts: []session.Part{{Type: "text", Text: "done"}}}, nil
		}
		if key == "read-evidence" {
			inspected <- request
		}
		code, ok := codes.Load(key)
		if !ok {
			return model.Response{Parts: []session.Part{{Type: "text", Text: "ready"}}}, nil
		}
		return model.Response{Parts: []session.Part{mailCode("evidence", code.(string))}}, nil
	}
}

func mailEvidenceTurn(t *testing.T, r *Runtime, owner session.SessionID, key string) session.Turn {
	t.Helper()
	submitTest(t, r, owner, key)
	finished := waitTestWithin(t, r, key, terminal, 30*time.Second)
	if finished.Turn.State != session.Succeeded {
		history, _ := r.History(t.Context(), owner, 0, 100)
		evidence, _ := json.Marshal(struct {
			Turn    *session.Turn
			History []session.Message
		}{finished.Turn, history})
		t.Fatalf("mail evidence execution failed: %s", evidence)
	}
	return *finished.Turn
}

func mailEvidenceReadCode(engine session.Engine, id session.MailID, first, second string) string {
	code := fmt.Sprintf(`listed=mail.list()
if len(listed["items"]) != 1 or listed["items"][0]["id"] != %q: fail("wrong evidence mail")
message=mail.read(id=listed["items"][0]["id"])
if message["body"] != "" or message["evidence_ref"] != listed["items"][0]["evidence_ref"]: fail("wrong evidence metadata")
first=artifacts.read(id=message["evidence_ref"],offset="0",length=65534)
second=artifacts.read(id=message["evidence_ref"],offset=first["next_offset"],length=65534)
if first["data"] != %q or second["data"] != %q or second["next_offset"] != None: fail("changed evidence bytes")
print("mail-evidence-ok")`, id, first, second)
	if engine == session.QuickJS {
		code = fmt.Sprintf(`var listed=await mail.list({});
if(listed.items.length!==1||listed.items[0].id!==%q) throw Error("wrong evidence mail");
var message=await mail.read({id:listed.items[0].id});
if(message.body!==""||message.evidence_ref!==listed.items[0].evidence_ref) throw Error("wrong evidence metadata");
var first=await artifacts.read({id:message.evidence_ref,offset:"0",length:65534});
var second=await artifacts.read({id:message.evidence_ref,offset:first.next_offset,length:65534});
if(first.data!==%q||second.data!==%q||second.next_offset!==null) throw Error("changed evidence bytes");
print("mail-evidence-ok");`, id, first, second)
	}
	return code
}

func TestBothEnginesSiblingMailEvidenceSurvivesSenderDeletionAndRestart(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			var codes sync.Map
			inspected := make(chan model.Request, 1)
			provider := mailEvidenceProvider(&codes, inspected)
			directory := t.TempDir()
			r := openMailRuntime(t, directory, provider)
			root := createEngineSession(t, r, engine)
			for _, capability := range []string{"mail.send", "mail.list", "mail.read", "artifacts.read"} {
				if _, err := r.CreateGrant(t.Context(), session.Grant{ID: session.GrantID(strings.ReplaceAll(capability, ".", "_")), SessionID: root.ID, Capability: capability, Resource: string(root.TreeID)}); err != nil {
					t.Fatal(err)
				}
			}
			children := make([]session.Session, 0, 3)
			for _, key := range []string{"sender", "recipient", "outsider"} {
				child, err := r.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: key}, store.ChildRequest{
					ParentID: root.ID, Parts: []session.Part{{Type: "text", Text: key}},
				})
				if err != nil {
					t.Fatal(err)
				}
				children = append(children, *child.Session)
			}
			sender, recipient, outsider := children[0], children[1], children[2]
			body := []byte(strings.Repeat("界🙂\n", 9000))
			// Byte pages deliberately split a multibyte character. The guest must
			// receive exact base64 bytes, not separately decoded UTF-8 strings.
			if utf8.Valid(body[:65534]) {
				t.Fatal("fixture did not split a UTF-8 sequence")
			}
			source, err := r.PutContent(t.Context(), sender.ID, "sender_evidence", "text/plain", body)
			if err != nil {
				t.Fatal(err)
			}
			if err := r.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"sender", "recipient", "outsider"} {
				if initial := waitTestWithin(t, r, key, terminal, 30*time.Second); initial.Turn.State != session.Succeeded {
					t.Fatalf("seed child failed: %+v", initial.Turn)
				}
			}
			send := fmt.Sprintf("mail.send(recipient_id=%q,evidence_ref=%q,delivery=\"next_turn\")\nprint(\"sent\")", recipient.ID, source.ID)
			if engine == session.QuickJS {
				send = fmt.Sprintf("await mail.send({recipient_id:%q,evidence_ref:%q,delivery:'next_turn'}); print('sent');", recipient.ID, source.ID)
			}
			codes.Store("send-evidence", send)
			sent := mailEvidenceTurn(t, r, sender.ID, "send-evidence")
			operations, err := r.Operations(t.Context(), sent.ID, "", 100)
			if err != nil || len(operations) != 1 || operations[0].Capability != "mail.send" || operations[0].State != session.OperationSucceeded || operations[0].GrantID == nil {
				t.Fatalf("send bypassed scoped operation: %+v %v", operations, err)
			}
			items, err := r.ListMail(t.Context(), recipient.ID, session.MailPending, "", 100)
			if err != nil || len(items) != 1 || items[0].EvidenceRef == nil {
				t.Fatalf("missing recipient evidence metadata: %+v %v", items, err)
			}
			mail, err := r.ReadMail(t.Context(), recipient.ID, items[0].ID)
			if err != nil || mail.Body != "" || mail.BodyBytes != 0 || mail.EvidenceRef == nil || *mail.EvidenceRef != *items[0].EvidenceRef || mail.State != session.MailPending {
				t.Fatalf("inspection changed evidence-only mail: %+v %v", mail, err)
			}
			alias := *mail.EvidenceRef
			if _, _, err := r.ReadContentRange(t.Context(), recipient.ID, source.ID, 0, 1); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("sender reference granted recipient access: %v", err)
			}
			if err := r.DeleteSubtree(t.Context(), sender.ID); err != nil {
				t.Fatal(err)
			}
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			r = openMailRuntime(t, directory, provider)
			if err := r.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			assertMailState(t, r, recipient.ID, string(mail.ID), session.MailPending)
			reference, actual, err := r.ReadContent(t.Context(), recipient.ID, alias, session.MaxContentBytes)
			if err != nil || reference.Digest != source.Digest || !bytes.Equal(actual, body) {
				t.Fatalf("sender deletion/startup collection lost recipient evidence: %+v %v", reference, err)
			}
			first := base64.StdEncoding.EncodeToString(body[:65534])
			second := base64.StdEncoding.EncodeToString(body[65534:])
			codes.Store("read-evidence", mailEvidenceReadCode(engine, mail.ID, first, second))
			read := mailEvidenceTurn(t, r, recipient.ID, "read-evidence")
			request := awaitMailSignal(t, inspected)
			if len(request.Contents) != 0 {
				t.Fatal("mail evidence was eagerly hydrated into the model request")
			}
			found := false
			for _, message := range request.Messages {
				if message.Role != session.User {
					continue
				}
				raw, err := json.Marshal(message.Parts)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Contains(raw, []byte(alias)) {
					continue
				}
				found = true
				if len(raw) > 4096 || bytes.Contains(raw, body[:256]) {
					t.Fatalf("mail digest did not preserve bounded recipient reference: %s %v", raw, err)
				}
			}
			if !found {
				t.Fatal("initial request omitted canonical mail digest")
			}
			history, err := r.History(t.Context(), recipient.ID, 0, 100)
			if err != nil {
				t.Fatal(err)
			}
			found = false
			for _, message := range history {
				if message.TurnID == read.ID && message.Mail != nil && message.Mail.ID == mail.ID {
					found = true
					raw, err := json.Marshal(message.Parts)
					if err != nil || message.Mail.Revision != mail.Revision || message.InputID != nil || len(raw) > 4096 || !bytes.Contains(raw, []byte(alias)) {
						t.Fatalf("history lost exact mail revision/reference: %+v %v", message, err)
					}
				}
			}
			if !found {
				t.Fatal("history omitted source-backed evidence mail")
			}
			assertMailState(t, r, recipient.ID, string(mail.ID), session.MailDelivered)
			operations, err = r.Operations(t.Context(), read.ID, "", 100)
			if err != nil || len(operations) != 4 {
				t.Fatalf("read bypassed operation ledger: %+v %v", operations, err)
			}
			for _, operation := range operations {
				if operation.State != session.OperationSucceeded || operation.GrantID == nil || operation.SessionID != recipient.ID || operation.Resource != string(root.TreeID) {
					t.Fatalf("read bypassed recipient authority: %+v", operation)
				}
			}
			foreign := fmt.Sprintf("artifacts.read(id=%q,offset=\"0\",length=1)", alias)
			if engine == session.QuickJS {
				foreign = fmt.Sprintf("await artifacts.read({id:%q,offset:'0',length:1});", alias)
			}
			codes.Store("foreign-evidence", foreign)
			denied := mailEvidenceTurn(t, r, outsider.ID, "foreign-evidence")
			operations, err = r.Operations(t.Context(), denied.ID, "", 100)
			if err != nil || len(operations) != 1 || operations[0].Capability != "artifacts.read" || operations[0].State != session.OperationFailed || operations[0].GrantID == nil || operations[0].Result == nil || operations[0].Result.Value != nil {
				t.Fatalf("known alias escaped its recipient: %+v %v", operations, err)
			}
		})
	}
}
