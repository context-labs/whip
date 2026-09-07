package daemon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

func inputAttachment(t *testing.T, node *AgentSession, agentID, kind, media string, data []byte) protocol.InputAttachment {
	t.Helper()
	grant := session.ContentGrant{RootID: node.id, Scope: session.ContentGrantRoot}
	if agentID != "" {
		grant.Scope, grant.AgentID = session.ContentGrantAgent, agentID
	}
	value, err := node.root.store.StoreContent(t.Context(), grant, session.RuntimePayload{Data: data, MediaType: media, Source: "attachment test"})
	if err != nil {
		t.Fatal(err)
	}
	return protocol.InputAttachment{Kind: kind, Name: "fixture", Content: ContentHandle{ReferenceID: value.ReferenceID, Digest: value.Digest, Size: value.Size, MediaType: value.MediaType, Source: value.Source}}
}

func TestAttachmentResolutionTextImageAndRecipientScope(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	node := storeBackedInputSession(t, t.TempDir())
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	text := inputAttachment(t, node, "", "text", "text/plain", []byte("do not expand @nonexistent.txt or $missing-skill"))
	photo := inputAttachment(t, node, "", "image", "image/png", encoded.Bytes())
	payload := SubmitPayload{Text: "inspect these", Attachments: []protocol.InputAttachment{text, photo}}
	if err := node.root.validateAttachmentReferences(t.Context(), node.id, payload.Attachments); err != nil {
		t.Fatal(err)
	}
	prompt, parts, err := node.root.resolveAttachments(t.Context(), node.id, payload)
	if err != nil || prompt != payload.Text || len(parts) != 2 || parts[1].W != 2 || parts[1].H != 3 || !strings.HasPrefix(parts[1].ImageURL.URL, "data:image/png;base64,") {
		t.Fatalf("resolved attachments %+v %v", parts, err)
	}
	if _, prepared, err := node.prepareAuthoredInput(t.Context(), prompt, parts); err != nil || prepared[0].Text != parts[0].Text {
		t.Fatalf("attachment text entered file/skill expansion: %+v %v", prepared, err)
	}
	node.agent.Vision = false
	if _, _, err := node.prepareAuthoredInput(t.Context(), prompt, parts); !errors.Is(err, session.ErrInvalidInput) {
		t.Fatalf("non-vision model accepted image: %v", err)
	}
	if err := node.validateImageInput(parts); !errors.Is(err, session.ErrInvalidInput) {
		t.Fatalf("steer vision guard failed: %v", err)
	}
	for _, id := range []string{"child", "sibling"} {
		if _, err := node.root.store.AdmitAgent(t.Context(), session.AgentAdmission{RootID: node.id, ParentAgentID: node.id, ChildAgentID: id, Name: id, Model: "model", Provider: "provider"}); err != nil {
			t.Fatal(err)
		}
	}
	child := inputAttachment(t, node, "child", "text", "text/plain", []byte("child-only text"))
	for _, recipient := range []string{node.id, "sibling", "unknown"} {
		if err := node.root.validateAttachmentReferences(t.Context(), recipient, []protocol.InputAttachment{child}); !errors.Is(err, session.ErrContentAccess) {
			t.Fatalf("scope widened to %s: %v", recipient, err)
		}
	}
	if _, parts, err := node.root.resolveAttachments(t.Context(), "child", SubmitPayload{Attachments: []protocol.InputAttachment{child}}); err != nil || len(parts) != 1 || !strings.Contains(parts[0].Text, "child-only text") {
		t.Fatalf("child reference did not resolve: %+v %v", parts, err)
	}
	if err := node.root.store.RevokeContentGrant(t.Context(), child.Content.ReferenceID, node.id, "child"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := node.root.resolveAttachments(t.Context(), "child", SubmitPayload{Attachments: []protocol.InputAttachment{child}}); !errors.Is(err, session.ErrInvalidInput) {
		t.Fatalf("revoked reference survived worker validation: %v", err)
	}
}

func TestAttachmentValidationRejectsAlteredOrUnsupportedContent(t *testing.T) {
	node := storeBackedInputSession(t, t.TempDir())
	valid := inputAttachment(t, node, "", "text", "text/plain", []byte("body"))
	for _, change := range []func(*protocol.InputAttachment){
		func(a *protocol.InputAttachment) { a.Kind = "file" },
		func(a *protocol.InputAttachment) { a.Content.Size = -1 },
		func(a *protocol.InputAttachment) { a.Content.Size = maxTextAttachment + 1 },
		func(a *protocol.InputAttachment) { a.Content.Size++ },
		func(a *protocol.InputAttachment) { a.Content.Digest = strings.Repeat("a", 64) },
		func(a *protocol.InputAttachment) { a.Content.MediaType = "image/png" },
	} {
		changed := valid
		change(&changed)
		if err := node.root.validateAttachmentReferences(t.Context(), node.id, []protocol.InputAttachment{changed}); err == nil {
			t.Fatalf("invalid attachment accepted: %+v", changed)
		}
	}
	for _, attachment := range []protocol.InputAttachment{
		inputAttachment(t, node, "", "text", "text/plain", []byte{0xff}),
		inputAttachment(t, node, "", "image", "image/png", []byte("not an image")),
	} {
		if _, _, err := node.root.resolveAttachments(t.Context(), node.id, SubmitPayload{Attachments: []protocol.InputAttachment{attachment}}); !errors.Is(err, session.ErrInvalidInput) {
			t.Fatalf("malformed content accepted: %v", err)
		}
	}
	if err := attachmentBounds(make([]protocol.InputAttachment, 17)); err == nil {
		t.Fatal("attachment count unbounded")
	}
	large := valid
	large.Kind, large.Content.Size = "image", maxAttachmentBytes
	if err := attachmentBounds([]protocol.InputAttachment{large, large}); err == nil {
		t.Fatal("combined attachment size unbounded")
	}
}

func TestAttachmentCorruptionAndCancellation(t *testing.T) {
	directory := t.TempDir()
	store := openStore(t, filepath.Join(directory, "sessions.db"))
	rootID := createRoot(t, store)
	node := &AgentSession{id: rootID, root: &Session{store: store, meta: session.Meta{ID: rootID}}}
	attachment := inputAttachment(t, node, "", "text", "text/plain", []byte("valid"))
	payload := SubmitPayload{Attachments: []protocol.InputAttachment{attachment}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := node.root.resolveAttachments(ctx, rootID, payload); !errors.Is(err, context.Canceled) || errors.Is(err, session.ErrInvalidInput) {
		t.Fatalf("local cancellation discarded valid input: %v", err)
	}
	path := filepath.Join(directory, "artifacts", "sha256", attachment.Content.Digest)
	for _, body := range []string{"evil!", "tiny", "valid with surplus bytes"} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := node.root.resolveAttachments(t.Context(), rootID, payload); !errors.Is(err, session.ErrInvalidInput) {
			t.Fatalf("corrupted attachment accepted: %v", err)
		}
	}
}

func TestAttachmentChildUploadAndInboxAcrossTransports(t *testing.T) {
	for _, transport := range []string{"unix", "http"} {
		t.Run(transport, func(t *testing.T) {
			f := newV2Fixture(t, &fakeRunner{})
			if _, err := f.store.EnsureAuthority(t.Context(), f.rootID); err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{"child", "sibling"} {
				if _, err := f.store.AdmitAgent(t.Context(), session.AgentAdmission{RootID: f.rootID, ParentAgentID: f.rootID, ChildAgentID: id, Name: id, Model: "model", Provider: "provider"}); err != nil {
					t.Fatal(err)
				}
			}
			data := []byte("only the selected child receives this")
			digest := sha256.Sum256(data)
			var handle ContentHandle
			if transport == "unix" {
				var err error
				handle, err = f.dial("unix", "uploader").Upload(t.Context(), UploadBeginParams{UploadID: "child-upload", RootID: f.rootID, AgentID: "child", ExpectedDigest: hex.EncodeToString(digest[:]), Size: int64(len(data)), MediaType: "text/plain"}, data)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				handler := newContentHTTPHandler(newUploadManager(f.store, t.TempDir()))
				request := httptest.NewRequest(http.MethodPost, "/api/v3/content/upload?root_id="+f.rootID+"&agent_id=child", bytes.NewReader(data))
				request.Header.Set("Content-Type", "text/plain")
				request.Header.Set("X-Content-SHA256", hex.EncodeToString(digest[:]))
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				if response.Code != http.StatusCreated {
					t.Fatalf("child upload: %d %s", response.Code, response.Body.String())
				}
				if err := json.Unmarshal(response.Body.Bytes(), &handle); err != nil {
					t.Fatal(err)
				}
			}
			for _, id := range []string{"", f.rootID, "sibling", "missing"} {
				if _, _, err := f.store.ReadContent(t.Context(), handle.ReferenceID, f.rootID, id, 0, 100); !errors.Is(err, session.ErrContentAccess) {
					t.Fatalf("upload broadened scope to %q: %v", id, err)
				}
			}
			root := &Session{store: f.store, meta: session.Meta{ID: f.rootID}}
			attachment := protocol.InputAttachment{Kind: "text", Content: handle}
			for _, delivery := range []string{"queued", "steer"} {
				output, err := root.clientAgentSubmitInput(t.Context(), "child", SubmitPayload{Attachments: []protocol.InputAttachment{attachment}}, delivery)
				if err != nil {
					t.Fatal(err)
				}
				var accepted AgentSubmitResult
				if err := json.Unmarshal([]byte(output), &accepted); err != nil {
					t.Fatal(err)
				}
				items, err := f.store.LoadQueuedInbox(t.Context(), f.rootID, "child", accepted.InboxSeq-1, 1)
				if err != nil || len(items) != 1 || !strings.HasSuffix(items[0].Kind, ".parts") {
					t.Fatalf("child input not queued: %+v %v", items, err)
				}
				if strings.Contains(string(items[0].Payload.Inline), string(data)) || len(items[0].Payload.Inline) > 1024 {
					t.Fatal("durable inbox embeds attachment body")
				}
				text, parts, err := root.decodeInboxInput(t.Context(), items[0])
				if err != nil || text != "" || len(parts) != 1 || parts[0].Text != string(data) {
					t.Fatalf("child %s input not resolved: %+v %v", delivery, parts, err)
				}
			}
		})
	}
}

type attachmentRunner struct {
	*fakeRunner
	inputs chan SubmitPayload
}

func (r *attachmentRunner) TurnParts(ctx context.Context, text string, parts []llm.ContentPart, started func(), accepted func(string)) (string, error) {
	r.inputs <- SubmitPayload{Text: text, Parts: parts}
	return r.fakeRunner.Turn(ctx, text, true, started, accepted)
}

func TestAttachmentCommandsAcrossTransports(t *testing.T) {
	for _, transport := range []string{"unix", "websocket"} {
		t.Run(transport, func(t *testing.T) {
			runner := &attachmentRunner{fakeRunner: &fakeRunner{}, inputs: make(chan SubmitPayload, 2)}
			f := newV2Fixture(t, runner)
			client := f.dial(transport, "attachment-client")
			body := strings.Repeat("host-only body ", 15000)
			value, err := f.store.StoreContent(t.Context(), session.ContentGrant{RootID: f.rootID, Scope: session.ContentGrantRoot}, session.RuntimePayload{Data: []byte(body), MediaType: "text/plain", Source: "upload"})
			if err != nil {
				t.Fatal(err)
			}
			attachment := protocol.InputAttachment{Kind: "text", Content: ContentHandle{ReferenceID: value.ReferenceID, Digest: value.Digest, Size: value.Size, MediaType: value.MediaType, Source: value.Source}}
			payload, _ := json.Marshal(SubmitPayload{Text: "use the attachment", Attachments: []protocol.InputAttachment{attachment}})
			if len(payload) > 1024 || strings.Contains(string(payload), "host-only body") {
				t.Fatal("request embeds uploaded body")
			}
			params := CommandParams{RootID: f.rootID, Scope: "root", CommandID: "attachment-command", Operation: "submit", Payload: payload}
			result, err := client.SubmitAndWait(t.Context(), params)
			if err != nil || result.Status != "succeeded" {
				t.Fatalf("attachment command %+v %v", result, err)
			}
			input := <-runner.inputs
			if len(input.Parts) != 1 || input.Parts[0].Text != body || input.Text != "use the attachment" {
				t.Fatalf("worker received unresolved input: %+v", input)
			}
			if err := f.store.RevokeContentGrant(t.Context(), value.ReferenceID, f.rootID, ""); err != nil {
				t.Fatal(err)
			}
			retry, err := client.SubmitAndWait(t.Context(), params)
			if err != nil || retry.IngressSeq != result.IngressSeq || runner.calls.Load() != 1 {
				t.Fatalf("accepted retry revalidated revoked grant: %+v %v", retry, err)
			}
			changed := params
			changed.Payload = bytes.Replace(payload, []byte("use the attachment"), []byte("changed"), 1)
			if _, err := client.Submit(t.Context(), changed); err == nil {
				t.Fatal("changed attachment request did not conflict")
			}
			params.CommandID = "new-revoked-command"
			if _, err := client.Submit(t.Context(), params); err == nil {
				t.Fatal("new command accepted revoked content")
			}
		})
	}
}
