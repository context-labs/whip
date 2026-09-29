package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/context-labs/whip/internal/protocol"
)

func TestPushOTLPPostsGzipBatchesWithTheBearerToken(t *testing.T) {
	var requests atomic.Int32
	var spans atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Content-Encoding") != "gzip" || r.Header.Get("Authorization") != "Bearer secret" {
			http.Error(w, "bad headers", http.StatusBadRequest)
			return
		}
		reader, err := gzip.NewReader(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var body struct {
			ResourceSpans []struct {
				ScopeSpans []struct {
					Spans []json.RawMessage `json:"spans"`
				} `json:"scopeSpans"`
			} `json:"resourceSpans"`
		}
		if err := json.NewDecoder(reader).Decode(&body); err != nil || len(body.ResourceSpans) != 1 || len(body.ResourceSpans[0].ScopeSpans) != 1 {
			http.Error(w, "not one resource and scope", http.StatusBadRequest)
			return
		}
		spans.Add(int32(len(body.ResourceSpans[0].ScopeSpans[0].Spans)))
		_, _ = io.WriteString(w, "{}")
	}))
	defer server.Close()
	// Six spans with large attributes force more than one batch under a small cap.
	var encoded []string
	for i := range 6 {
		encoded = append(encoded, `{"traceId":"`+strings.Repeat("a", 32)+`","spanId":"`+strings.Repeat("b", 15)+string(rune('0'+i))+`","name":"`+strings.Repeat("n", 1500)+`","kind":1,"startTimeUnixNano":"1","endTimeUnixNano":"2","attributes":[],"status":{"code":1}}`)
	}
	document := `{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"whipcode"}}]},"scopeSpans":[{"scope":{"name":"whip"},"spans":[` + strings.Join(encoded, ",") + `]}]}]}`
	if err := pushOTLPWithBatchSize(context.Background(), server.URL, "secret", []byte(document), protocol.TraceExportResult{Spans: 6, Traces: 1}, 4096); err != nil {
		t.Fatal(err)
	}
	if requests.Load() < 2 || spans.Load() != 6 {
		t.Fatalf("expected the spans split across several requests, got %d requests carrying %d spans", requests.Load(), spans.Load())
	}
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "nope", http.StatusUnauthorized) }))
	defer failing.Close()
	if err := pushOTLPWithBatchSize(context.Background(), failing.URL, "", []byte(document), protocol.TraceExportResult{}, 4096); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("a rejected batch must fail the push with its status, got %v", err)
	}
}

func TestSessionsExportCLIWritesAndPushesTheSessionTrace(t *testing.T) {
	runFixture(t, "done", nil)
	if _, err := runCapture(t, "", "trace me"); err != nil {
		t.Fatal(err)
	}
	id := string(nativeSession(t).ID)
	dir := t.TempDir()

	if err := sessionsExportCLI(nil); err == nil {
		t.Fatal("export without a root must fail")
	}
	out := captureStdout(t, func() {
		if err := sessionsExportCLI([]string{id, "-o", "-"}); err != nil {
			t.Fatal(err)
		}
	})
	var export struct {
		ResourceSpans []struct {
			ScopeSpans []struct {
				Spans []struct {
					Name       string `json:"name"`
					Attributes []struct {
						Key   string `json:"key"`
						Value struct {
							String string `json:"stringValue"`
						} `json:"value"`
					} `json:"attributes"`
				} `json:"spans"`
			} `json:"scopeSpans"`
		} `json:"resourceSpans"`
	}
	if err := json.Unmarshal([]byte(out), &export); err != nil || len(export.ResourceSpans) != 1 || len(export.ResourceSpans[0].ScopeSpans[0].Spans) < 2 {
		t.Fatalf("stdout export: %v\n%s", err, out)
	}
	produced := ""
	for _, span := range export.ResourceSpans[0].ScopeSpans[0].Spans {
		for _, attr := range span.Attributes {
			if attr.Key == "output.value" && strings.Contains(attr.Value.String, `"text":"done"`) {
				produced = attr.Value.String
			}
		}
	}
	if !strings.Contains(produced, `"text":"done"`) {
		t.Fatalf("the model call's produced message was not exported: %s", out)
	}
	path := filepath.Join(dir, "trace.otlp.json")
	if err := sessionsExportCLI([]string{id, "-o", path}); err != nil {
		t.Fatal(err)
	}
	if written, err := os.ReadFile(path); err != nil || string(written) != out {
		t.Fatalf("file export differs from stdout export: %v", err)
	}
	var pushed atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer k" && r.Header.Get("Content-Encoding") == "gzip" {
			pushed.Add(1)
		}
		_, _ = io.WriteString(w, "{}")
	}))
	defer server.Close()
	if err := sessionsExportCLI([]string{id, "-push", server.URL, "-token", "k"}); err != nil || pushed.Load() != 1 {
		t.Fatalf("push: requests=%d err=%v", pushed.Load(), err)
	}
}

func TestSessionsExportCLIReportsArgumentAndDeliveryFailures(t *testing.T) {
	runFixture(t, "done", nil)
	if _, err := runCapture(t, "", "trace me"); err != nil {
		t.Fatal(err)
	}
	id := string(nativeSession(t).ID)
	dir := t.TempDir()

	for name, args := range map[string][]string{
		"unknown flag":      {id, "-bogus"},
		"second positional": {id, "extra"},
		"unwritable output": {id, "-o", filepath.Join(dir, "missing", "dir", "trace.json")},
		"unreachable push":  {id, "-push", "http://127.0.0.1:1", "-token", "k"},
	} {
		if err := sessionsExportCLI(args); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
}

func TestSessionsExportFilesStayPrivateAndRejectSymlinks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "export.json")
	if err := os.WriteFile(path, []byte("longer previous contents"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeTraceExport(path, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 || info.Size() != 2 {
		t.Fatal(info, err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := writeTraceExport(link, []byte("overwrite")); err == nil {
		t.Fatal("followed export symlink")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "{}" {
		t.Fatal(string(data), err)
	}
}
