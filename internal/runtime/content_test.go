package runtime

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func TestProviderHydratesAuthorizedImagesWithoutChangingHistory(t *testing.T) {
	image := []byte("image bytes")
	var calls atomic.Int32
	r, owner := openHTTPTest(t, func(w http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		var body struct {
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		var parts []struct {
			Type     string `json:"type"`
			ImageURL struct {
				URL string `json:"url"`
			} `json:"image_url"`
		}
		if err := json.Unmarshal(body.Messages[len(body.Messages)-1].Content, &parts); err != nil {
			t.Error(err)
		}
		if len(parts) != 2 || parts[1].ImageURL.URL != "data:image/png;base64,"+base64.StdEncoding.EncodeToString(image) {
			t.Errorf("encoded content: %+v", parts)
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"image received"},"finish_reason":"stop"}]}`)
	})
	reference, err := r.PutContent(t.Context(), owner.ID, "image", "image/png", image)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := r.PutContent(t.Context(), owner.ID, "image", "image/png", image)
	if err != nil || reference != retry {
		t.Fatal("upload retry changed reference", err)
	}
	other := createTest(t, r)
	if _, _, err := r.ReadContent(t.Context(), other.ID, reference.ID, session.MaxContentBytes); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("foreign reference read", err)
	}
	parts := []session.Part{{Type: "text", Text: "describe"}, {Type: "content", ReferenceID: reference.ID}}
	if _, err := r.Admit(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "image"}, store.Submission{SessionID: owner.ID, Source: session.UserInput, Parts: parts}); err != nil {
		t.Fatal(err)
	}
	admission := waitTest(t, r, "image", terminal)
	if admission.Turn.State != session.Succeeded || calls.Load() != 1 {
		t.Fatalf("turn=%+v calls=%d", admission.Turn, calls.Load())
	}
	history, err := r.History(t.Context(), owner.ID, 0, 100)
	if err != nil || len(history) != 2 || history[0].Parts[1].ReferenceID != reference.ID || history[0].Parts[1].Text != "" {
		t.Fatalf("hydration rewrote history: %+v %v", history, err)
	}
}

func TestCorruptContentFailsBeforeModelDispatch(t *testing.T) {
	var calls atomic.Int32
	r, owner := openHTTPTest(t, func(http.ResponseWriter, *http.Request) { calls.Add(1) })
	reference, err := r.PutContent(t.Context(), owner.ID, "content", "text/plain", []byte("original"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(r.directory, "artifacts", "sha256", reference.Digest)
	if err := os.WriteFile(path, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Admit(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "corrupt"}, store.Submission{SessionID: owner.ID, Source: session.UserInput, Parts: []session.Part{{Type: "content", ReferenceID: reference.ID}}}); err != nil {
		t.Fatal(err)
	}
	a := waitTest(t, r, "corrupt", terminal)
	attempts, err := r.ModelAttempts(t.Context(), a.Turn.ID, "", 100)
	if err != nil || a.Turn.State != session.Failed || calls.Load() != 0 || len(attempts) != 0 {
		t.Fatalf("corrupt body dispatched: %+v %+v calls=%d err=%v", a.Turn, attempts, calls.Load(), err)
	}
}

func TestContentReopenCollectsOrphansAndPreservesSurvivingOwners(t *testing.T) {
	path := t.TempDir()
	r := openTest(t, path, model.Scripted{})
	a, b := createTest(t, r), createTest(t, r)
	data := bytes.Repeat([]byte("text"), 20000)
	reference, err := r.PutContent(t.Context(), a.ID, "first", "text/plain", data)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.PutContent(t.Context(), b.ID, "second", "text/plain", data); err != nil {
		t.Fatal(err)
	}
	orphan, err := r.content.Put([]byte("published before failed SQL"))
	if err != nil {
		t.Fatal(err)
	}
	unpublished := filepath.Join(path, "artifacts", "sha256", ".publish-killed")
	if err := os.WriteFile(unpublished, []byte("publisher killed before rename"), 0o600); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(path, "artifacts", "sha256", ".publish-link")
	if err := os.Symlink(reference.Digest, linked); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(path, "artifacts", "sha256", ".publish-directory")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteSubtree(t.Context(), a.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r = openTest(t, path, model.Scripted{})
	_, read, err := r.ReadContent(t.Context(), b.ID, "second", session.MaxContentBytes)
	if err != nil || !bytes.Equal(read, data) {
		t.Fatal("surviving content was collected or truncated", err)
	}
	if _, err := os.Stat(unpublished); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("killed publisher's temporary file not collected", err)
	}
	if info, err := os.Lstat(linked); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("temporary-file collection touched a symlink", err)
	}
	if info, err := os.Stat(nested); err != nil || !info.IsDir() {
		t.Fatal("temporary-file collection touched a directory", err)
	}
	if _, err := os.Stat(filepath.Join(path, "artifacts", "sha256", orphan.Digest)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("orphan publication not collected", err)
	}
	if err := r.DeleteSubtree(t.Context(), b.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r = openTest(t, path, model.Scripted{})
	if _, err := os.Stat(filepath.Join(path, "artifacts", "sha256", reference.Digest)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("last-owner deletion did not become collectible", err)
	}
	if _, err := r.PutContent(t.Context(), b.ID, "deleted", "text/plain", []byte(strings.Repeat("x", 10))); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("deleted session accepted upload", err)
	}
}
