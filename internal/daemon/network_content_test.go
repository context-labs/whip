package daemon

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestContentHTTPTransferGrantsAndCleanup(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	root := createRoot(t, store)
	other := createRoot(t, store)
	dir := t.TempDir()
	manager := newUploadManager(store, dir)
	handler := newContentHTTPHandler(manager)
	data := bytes.Repeat([]byte("<script>unsafe()</script>"), 30000)
	digest := sha256.Sum256(data)
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://localhost/api/v3/content/upload?root_id="+root, bytes.NewReader(data))
	request.Header.Set("Content-Type", "text/html")
	request.Header.Set("X-Content-Sha256", hex.EncodeToString(digest[:]))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", response.Code, response.Body.String())
	}
	var handle ContentHandle
	if err := json.Unmarshal(response.Body.Bytes(), &handle); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		root, agent string
		status      int
	}{
		{root: root, status: http.StatusOK},
		{root: other, status: http.StatusForbidden},
		{root: root, agent: "unknown", status: http.StatusForbidden},
	} {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost/api/v3/content/"+handle.ReferenceID+"?root_id="+test.root+"&agent_id="+test.agent, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.status {
			t.Fatalf("download status %d", response.Code)
		}
		if test.status == http.StatusOK {
			if !bytes.Equal(response.Body.Bytes(), data) {
				t.Fatal("content body differs")
			}
			if response.Header().Get("Content-Disposition") != "attachment" {
				t.Fatal("active content is not forced to download")
			}
		}
	}
	request = httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://localhost/api/v3/content/upload?root_id="+root, bytes.NewReader(data[:10]))
	request.ContentLength = int64(len(data))
	request.Header.Set("X-Content-Sha256", hex.EncodeToString(digest[:]))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("incomplete upload status %d", response.Code)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary uploads remain: %v %v", entries, err)
	}
	if len(manager.live) != 0 {
		t.Fatal("upload state remains")
	}
}

type interruptedUploadReader struct{}

func (interruptedUploadReader) Read([]byte) (int, error) {
	return 0, errors.New("client disconnected")
}

func TestContentHTTPRejectsInvalidUploadsAndReleasesTemporaryState(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	root := createRoot(t, store)
	directory := t.TempDir()
	manager := newUploadManager(store, directory)
	handler := newContentHTTPHandler(manager)
	digest := sha256.Sum256([]byte("body"))
	for _, test := range []struct {
		name   string
		length int64
		digest string
		body   io.Reader
		status int
	}{
		{"unknown size", -1, hex.EncodeToString(digest[:]), bytes.NewBufferString("body"), http.StatusRequestEntityTooLarge},
		{"oversized body", MaxUploadSize + 1, hex.EncodeToString(digest[:]), bytes.NewBufferString("body"), http.StatusRequestEntityTooLarge},
		{"invalid digest", 4, "not-a-digest", bytes.NewBufferString("body"), http.StatusBadRequest},
		{"body exceeds declared size", 2, hex.EncodeToString(digest[:]), bytes.NewBufferString("body"), http.StatusBadRequest},
		{"interrupted body", 8, hex.EncodeToString(digest[:]), io.MultiReader(bytes.NewBufferString("body"), interruptedUploadReader{}), http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://localhost/api/v3/content/upload?root_id="+root, test.body)
			request.ContentLength = test.length
			request.Header.Set("X-Content-Sha256", test.digest)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("invalid transfer returned %d: %s", response.Code, response.Body.String())
			}
			files, err := os.ReadDir(directory)
			if err != nil || len(files) != 0 || len(manager.live) != 0 {
				t.Fatalf("failed upload retained temporary resources: %v %v", files, err)
			}
		})
	}
}

type revokingContentWriter struct {
	*httptest.ResponseRecorder
	revoke func()
	writes int
}

func (w *revokingContentWriter) Write(data []byte) (int, error) {
	w.writes++
	n, err := w.ResponseRecorder.Write(data)
	if w.writes == 1 {
		w.revoke()
	}
	return n, err
}

func TestContentHTTPStopsDownloadAfterGrantRevocation(t *testing.T) {
	t.Parallel()
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	root := createRoot(t, store)
	data := bytes.Repeat([]byte("x"), 2*MaxContentChunk)
	value, err := store.StoreContent(t.Context(), session.ContentGrant{RootID: root, Scope: session.ContentGrantRoot}, session.RuntimePayload{Data: data})
	if err != nil {
		t.Fatal(err)
	}
	response := &revokingContentWriter{ResponseRecorder: httptest.NewRecorder(), revoke: func() {
		if err := store.RevokeContentGrant(t.Context(), value.ReferenceID, root, ""); err != nil {
			t.Fatal(err)
		}
	}}
	handler := newContentHTTPHandler(newUploadManager(store, t.TempDir()))
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost/api/v3/content/"+value.ReferenceID+"?root_id="+root, nil)
	handler.ServeHTTP(response, request)
	if response.writes != 1 || response.Body.Len() != min(MaxContentChunk, session.MaxContentRead) {
		t.Fatalf("download continued after revocation: %d writes, %d bytes", response.writes, response.Body.Len())
	}
	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, request)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("revoked download can restart: %d", denied.Code)
	}
}
