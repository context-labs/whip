package webgateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/context-labs/whip/internal/protocol"
)

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func contentServer(client *fakeClient) *Server {
	s := bareServer()
	s.options.Open = func(context.Context) (Client, error) { return client, nil }
	return s
}

func assertClosed(t *testing.T, client *fakeClient) {
	t.Helper()
	select {
	case <-client.Done():
	default:
		t.Fatal("transfer leaked its client")
	}
}

func TestChunkedUploadPreservesMetadataAndBounds(t *testing.T) {
	t.Parallel()
	data := bytes.Repeat([]byte("html"), maxContentChunk)
	client := newFakeClient()
	var begin protocol.UploadBeginParams
	var uploaded []byte
	var chunks int
	client.call = func(ctx context.Context, method string, params, result any) error {
		switch method {
		case "upload.begin":
			begin = params.(protocol.UploadBeginParams)
		case "upload.chunk":
			chunk := params.(protocol.UploadChunkParams)
			if len(chunk.Data) > maxContentChunk || chunk.Offset != int64(len(uploaded)) || chunk.UploadID != begin.UploadID {
				t.Fatal("invalid chunk")
			}
			uploaded = append(uploaded, chunk.Data...)
			chunks++
		case "upload.finish":
			if params.(protocol.UploadFinishParams).UploadID != begin.UploadID {
				t.Fatal("upload id changed")
			}
			*result.(*protocol.ContentHandle) = protocol.ContentHandle{ReferenceID: "ref", Size: int64(len(data)), Digest: digest(data)}
		default:
			t.Fatalf("unexpected %s", method)
		}
		return nil
	}
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://localhost:8080/api/v3/content/upload?root_id=root&agent_id=agent", bytes.NewReader(data))
	request.Header.Set("X-Content-Sha256", digest(data))
	request.Header.Set("Content-Type", "text/html")
	response := httptest.NewRecorder()
	contentServer(client).handler().ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("%d: %s", response.Code, response.Body.String())
	}
	if !bytes.Equal(uploaded, data) || chunks < 2 || begin.RootID != "root" || begin.AgentID != "agent" || begin.MediaType != "text/html" || begin.Source != "http-upload" {
		t.Fatal("transfer metadata/body changed")
	}
	assertClosed(t, client)
}

type interruptedReader struct{}

func (interruptedReader) Read([]byte) (int, error) { return 0, errors.New("browser disconnected") }

func TestInvalidUploadsNeverCommitAndCloseClient(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		length int64
		hash   string
		body   io.Reader
		status int
	}{
		{"unknown size", -1, digest([]byte("body")), strings.NewReader("body"), 413},
		{"oversized", maxUploadSize + 1, digest([]byte("body")), strings.NewReader("body"), 413},
		{"invalid digest", 4, "not-a-digest", strings.NewReader("body"), 400},
		{"body longer than declared", 2, digest([]byte("body")), strings.NewReader("body"), 400},
		{"short body", 8, digest([]byte("body")), strings.NewReader("body"), 400},
		{"wrong digest", 4, digest([]byte("oops")), strings.NewReader("body"), 400},
		{"interrupted body", 8, digest([]byte("body")), io.MultiReader(strings.NewReader("body"), interruptedReader{}), 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client := newFakeClient()
			calls := 0
			client.call = func(ctx context.Context, method string, params, result any) error {
				calls++
				if method == "upload.finish" {
					t.Fatal("invalid upload committed")
				}
				return nil
			}
			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://localhost:8080/api/v3/content/upload?root_id=root", tc.body)
			request.ContentLength = tc.length
			request.Header.Set("X-Content-Sha256", tc.hash)
			response := httptest.NewRecorder()
			contentServer(client).handler().ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("%d: %s", response.Code, response.Body.String())
			}
			if calls > 0 {
				assertClosed(t, client)
			}
		})
	}
}

func TestDownloadsRecheckGrantsAndValidateDigest(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"success", "revoked", "corrupt", "changed-metadata", "oversized-chunk", "empty-chunk"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			data := bytes.Repeat([]byte("x"), 2*maxContentChunk)
			handle := protocol.ContentHandle{ReferenceID: "ref", Size: int64(len(data)), Digest: digest(data), MediaType: "text/html"}
			client := newFakeClient()
			calls := 0
			client.call = func(ctx context.Context, method string, params, result any) error {
				calls++
				p := params.(protocol.ContentReadParams)
				if method != "content.read" || p.RootID != "root" || p.AgentID != "agent" || p.ReferenceID != "ref" || p.Limit != maxContentChunk {
					t.Fatal("grant or bounded read lost")
				}
				if mode == "revoked" && calls > 1 {
					return &protocol.RPCError{Code: -32003, Message: "denied"}
				}
				read := result.(*protocol.ContentReadResult)
				read.Content = handle
				read.Data = bytes.Clone(data[p.Offset:min(int64(len(data)), p.Offset+int64(p.Limit))])
				switch mode {
				case "corrupt":
					read.Data[0] = '!'
				case "changed-metadata":
					if calls > 1 {
						read.Content.Digest = digest([]byte("changed"))
					}
				case "oversized-chunk":
					read.Data = append(read.Data, '!')
				case "empty-chunk":
					read.Data = nil
				}
				return nil
			}
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost:8080/api/v3/content/ref?root_id=root&agent_id=agent", nil)
			response := httptest.NewRecorder()
			aborted := false
			func() {
				defer func() {
					if recovered := recover(); recovered != nil {
						err, ok := recovered.(error)
						if !ok || !errors.Is(err, http.ErrAbortHandler) {
							panic(recovered)
						}
						aborted = true
					}
				}()
				contentServer(client).handler().ServeHTTP(response, request)
			}()
			assertClosed(t, client)
			if mode == "success" {
				if aborted || !bytes.Equal(data, response.Body.Bytes()) || calls != 2 {
					t.Fatal("download differs")
				}
				for key, want := range map[string]string{"Content-Type": "application/octet-stream", "Content-Disposition": "attachment", "X-Content-Type-Options": "nosniff", "Cache-Control": "no-store"} {
					if response.Header().Get(key) != want {
						t.Errorf("unsafe %s", key)
					}
				}
			} else if response.Body.Len() >= len(data) {
				t.Fatal("invalid transfer appeared complete")
			}
			if (mode == "revoked" || mode == "corrupt" || mode == "changed-metadata") && (!aborted || calls != 2) {
				t.Fatalf("invalid read was not aborted: calls=%d abort=%v", calls, aborted)
			}
		})
	}
}

func TestContentClientsPinnedToRuntimeAndRestriction(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"generation", "runtime", "capability"} {
		client := newFakeClient()
		switch mode {
		case "generation":
			client.init.Generation++
		case "runtime":
			client.init.RuntimeID = "replacement"
		case "capability":
			client.init.NegotiatedCapabilities = nil
		}
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost:8080/api/v3/content/ref?root_id=root", nil)
		response := httptest.NewRecorder()
		contentServer(client).handler().ServeHTTP(response, request)
		if response.Code != 502 {
			t.Fatalf("%s returned %d", mode, response.Code)
		}
		assertClosed(t, client)
	}
}

func TestRequestCancellationClosesTransfer(t *testing.T) {
	t.Parallel()
	client := newFakeClient()
	called := make(chan struct{})
	client.call = func(ctx context.Context, method string, params, result any) error {
		close(called)
		<-client.Done()
		return ctx.Err()
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	request := httptest.NewRequestWithContext(ctx, http.MethodGet, "http://localhost:8080/api/v3/content/ref?root_id=root", nil)
	done := make(chan struct{})
	go func() { defer close(done); contentServer(client).handler().ServeHTTP(httptest.NewRecorder(), request) }()
	<-called
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("canceled transfer kept client")
	}
	assertClosed(t, client)
}

type failingDownloadWriter struct{ *httptest.ResponseRecorder }

func (w failingDownloadWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestDownloadSizeNotLimitedByUploadCeiling(t *testing.T) {
	t.Parallel()
	client := newFakeClient()
	size := int64(maxUploadSize + 1)
	client.call = func(ctx context.Context, method string, params, result any) error {
		if method != "content.read" {
			t.Fatalf("unexpected method %s", method)
		}
		*result.(*protocol.ContentReadResult) = protocol.ContentReadResult{
			Content: protocol.ContentHandle{ReferenceID: "ref", Size: size, Digest: digest(nil)}, Data: []byte("first chunk"),
		}
		return nil
	}
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost:8080/api/v3/content/ref?root_id=root", nil)
	response := failingDownloadWriter{httptest.NewRecorder()}
	contentServer(client).handler().ServeHTTP(response, request)
	if response.Header().Get("Content-Length") != strconv.FormatInt(size, 10) {
		t.Fatal("large stored content incorrectly rejected by the upload ceiling")
	}
	assertClosed(t, client)
}

func TestServerDeadlineClosesStalledLiveTransfers(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"upload.begin", "content.read"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				client := newFakeClient()
				client.call = func(ctx context.Context, called string, params, result any) error {
					if called != method {
						t.Fatalf("unexpected method %s", called)
					}
					// The connection remains healthy; only Close can unblock this RPC.
					<-client.Done()
					if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
						t.Errorf("RPC ended without the server deadline: %v", ctx.Err())
					}
					return ctx.Err()
				}
				request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost:8080/api/v3/content/ref?root_id=root", nil)
				if method == "upload.begin" {
					request = httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://localhost:8080/api/v3/content/upload?root_id=root", strings.NewReader("body"))
					request.Header.Set("X-Content-Sha256", digest([]byte("body")))
				}
				s := contentServer(client)
				started := time.Now()
				s.handler().ServeHTTP(httptest.NewRecorder(), request)
				if elapsed := time.Since(started); elapsed != requestTimeout {
					t.Fatalf("server request bound: %v, want %v", elapsed, requestTimeout)
				}
				if request.Context().Err() != nil {
					t.Fatal("caller canceled instead of server enforcing its bound")
				}
				assertClosed(t, client)
				if len(s.transfers) != 0 {
					t.Fatal("timed-out transfer retained admission slot")
				}
				s.workers.Wait()
			})
		})
	}
}
