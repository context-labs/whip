package daemon

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func gatewayHTTPURL(f v2Fixture) string {
	return "http" + strings.TrimSuffix(strings.TrimPrefix(f.endpoint, "ws"), "/api/v3/ws")
}

func gatewayHTTPRequest(t *testing.T, request *http.Request) (*http.Response, []byte) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	t.Cleanup(client.CloseIdleConnections)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response, body
}

func waitGatewayUploads(t *testing.T, manager *uploadManager, want int) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		manager.mu.Lock()
		count := len(manager.live)
		manager.mu.Unlock()
		if count == want {
			if want != 0 {
				return
			}
			// The manager drops its index before unlinking temporary files.
			entries, err := filepath.Glob(filepath.Join(manager.dir, ".whip-upload-*"))
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) == 0 {
				return
			}
		}
		select {
		case <-deadline.C:
			t.Fatalf("active uploads=%d, want %d", count, want)
		case <-tick.C:
		}
	}
}

func TestContentHTTPTransferGrantsAndCleanup(t *testing.T) {
	f := newV2Fixture(t, &fakeRunner{})
	other := createRoot(t, f.store)
	data := bytes.Repeat([]byte("<script>unsafe()</script>"), 30000)
	digest := sha256.Sum256(data)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, gatewayHTTPURL(f)+"/api/v3/content/upload?root_id="+f.rootID, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "text/html")
	request.Header.Set("X-Content-Sha256", hex.EncodeToString(digest[:]))
	response, body := gatewayHTTPRequest(t, request)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("upload: %d %s", response.StatusCode, body)
	}
	var handle ContentHandle
	if err := json.Unmarshal(body, &handle); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		root, agent string
		status      int
	}{
		{root: f.rootID, status: http.StatusOK},
		{root: other, status: http.StatusForbidden},
		{root: f.rootID, agent: "unknown", status: http.StatusForbidden},
	} {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, gatewayHTTPURL(f)+"/api/v3/content/"+handle.ReferenceID+"?root_id="+test.root+"&agent_id="+test.agent, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, body := gatewayHTTPRequest(t, request)
		if response.StatusCode != test.status {
			t.Fatalf("download status %d: %s", response.StatusCode, body)
		}
		if test.status == http.StatusOK {
			if !bytes.Equal(body, data) {
				t.Fatal("content body differs")
			}
			if response.Header.Get("Content-Disposition") != "attachment" || response.Header.Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("active content is not forced to a safe download")
			}
		}
	}
	if err := f.store.RevokeContentGrant(t.Context(), handle.ReferenceID, f.rootID, ""); err != nil {
		t.Fatal(err)
	}
	request, err = http.NewRequestWithContext(t.Context(), http.MethodGet, gatewayHTTPURL(f)+"/api/v3/content/"+handle.ReferenceID+"?root_id="+f.rootID, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, body = gatewayHTTPRequest(t, request)
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("revoked grant served: %d %s", response.StatusCode, body)
	}
	waitGatewayUploads(t, f.server.uploads, 0)
}

func TestContentHTTPInterruptedUploadReleasesDaemonState(t *testing.T) {
	f := newV2Fixture(t, &fakeRunner{})
	endpoint, err := url.Parse(gatewayHTTPURL(f))
	if err != nil {
		t.Fatal(err)
	}
	connection, err := net.DialTimeout("tcp", endpoint.Host, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
	digest := sha256.Sum256([]byte("bodybody"))
	_, err = fmt.Fprintf(connection, "POST /api/v3/content/upload?root_id=%s HTTP/1.1\r\nHost: %s\r\nContent-Length: 8\r\nX-Content-Sha256: %x\r\n\r\nbody", f.rootID, endpoint.Host, digest)
	if err != nil {
		t.Fatal(err)
	}
	waitGatewayUploads(t, f.server.uploads, 1)
	_ = connection.Close()
	waitGatewayUploads(t, f.server.uploads, 0)
	entries, err := filepath.Glob(filepath.Join(f.server.uploads.dir, ".whip-upload-*"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary uploads remain: %v %v", entries, err)
	}
}
