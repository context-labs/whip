package daemon

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
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
	request := httptest.NewRequest(http.MethodPost, "http://localhost/api/v2/content/upload?root_id="+root, bytes.NewReader(data))
	request.Header.Set("Content-Type", "text/html")
	request.Header.Set("X-Content-SHA256", hex.EncodeToString(digest[:]))
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
		request := httptest.NewRequest(http.MethodGet, "http://localhost/api/v2/content/"+handle.ReferenceID+"?root_id="+test.root+"&agent_id="+test.agent, nil)
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
	request = httptest.NewRequest(http.MethodPost, "http://localhost/api/v2/content/upload?root_id="+root, bytes.NewReader(data[:10]))
	request.ContentLength = int64(len(data))
	request.Header.Set("X-Content-SHA256", hex.EncodeToString(digest[:]))
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
