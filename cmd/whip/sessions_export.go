package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/buildinfo"
	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

// otlpPushBatchBytes keeps each pushed request under the 4 MiB body cap that
// HALO desktop and inference.net enforce, with headroom for the envelope.
const otlpPushBatchBytes = 4<<20 - 64<<10

// `whip sessions export <root> [-trace id] [-o file|-] [-push URL] [-token T]`
// renders a session's spans as one OTLP/JSON ExportTraceServiceRequest. The
// daemon builds the document; the CLI fetches it through bounded content reads
// and either writes it or posts it to an OTLP/HTTP endpoint in gzip batches.
func sessionsExportCLI(args []string) error {
	fs := flag.NewFlagSet("sessions export", flag.ContinueOnError)
	trace := fs.String("trace", "", "export one trace (one root turn) instead of the whole session")
	out := fs.String("o", "", "write the OTLP/JSON here; '-' for stdout (default: <root>.otlp.json)")
	push := fs.String("push", "", "POST the export to an OTLP/HTTP endpoint instead of writing a file, e.g. http://127.0.0.1:8799/v1/traces")
	token := fs.String("token", os.Getenv("INFERENCE_API_KEY"), "bearer token for -push (default $INFERENCE_API_KEY)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, buildinfo.Text("usage: whip sessions export <root> [-trace id] [-o file|-] [-push URL] [-token T]"))
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	root := fs.Arg(0)
	if root == "" {
		fs.Usage()
		return errors.New("a session (root) id is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	clientID := daemonClientID("sessions-export")
	connection, err := connectDaemon(ctx, "automation", clientID, nil)
	if err != nil {
		return err
	}
	defer func() { _ = connection.Close() }()
	caller, ok := connection.(interface {
		Call(context.Context, string, any, any) error
	})
	if !ok {
		return errors.New("this daemon connection cannot issue trace queries")
	}
	var result protocol.TraceExportResult
	if err := caller.Call(ctx, "trace.export", protocol.TraceExportParams{RootID: root, TraceID: *trace}, &result); err != nil {
		return err
	}
	data := []byte(result.Inline)
	if len(data) == 0 {
		data, err = readExportContent(ctx, caller, root, result.Content)
		if err != nil {
			return err
		}
	}
	if *push != "" {
		return pushOTLP(ctx, *push, *token, data, result)
	}
	path := *out
	if path == "" {
		path = root + ".otlp.json"
	}
	if path == "-" {
		_, err := os.Stdout.Write(data)
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "exported %d spans across %d traces to %s\n", result.Spans, result.Traces, path)
	return nil
}

func readExportContent(ctx context.Context, caller interface {
	Call(context.Context, string, any, any) error
}, root string, handle daemon.ContentHandle) ([]byte, error) {
	if handle.ReferenceID == "" {
		return nil, errors.New("the daemon returned an empty export")
	}
	data := make([]byte, 0, handle.Size)
	for offset := int64(0); offset < handle.Size; {
		var page protocol.ContentReadResult
		if err := caller.Call(ctx, "content.read", protocol.ContentReadParams{RootID: root, ReferenceID: handle.ReferenceID, Offset: offset, Limit: daemon.MaxContentChunk}, &page); err != nil {
			return nil, err
		}
		if len(page.Data) == 0 {
			return nil, errors.New("export content read returned no data before its end")
		}
		data = append(data, page.Data...)
		offset += int64(len(page.Data))
	}
	return data, nil
}

// pushOTLP posts the export as gzip OTLP/JSON, split so every request stays
// under the endpoint's body cap.
func pushOTLP(ctx context.Context, endpoint, token string, data []byte, result protocol.TraceExportResult) error {
	batches, err := session.SplitOTLP(data, otlpPushBatchBytes)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 2 * time.Minute}
	for index, batch := range batches {
		var body bytes.Buffer
		writer := gzip.NewWriter(&body)
		if _, err := writer.Write(batch); err != nil {
			return err
		}
		if err := writer.Close(); err != nil {
			return err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
		if err != nil {
			return err
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Content-Encoding", "gzip")
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		response, err := client.Do(request)
		if err != nil {
			return fmt.Errorf("push batch %d/%d: %w", index+1, len(batches), err)
		}
		reply, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		_ = response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return fmt.Errorf("push batch %d/%d: %s: %s", index+1, len(batches), response.Status, strings.TrimSpace(string(reply)))
		}
	}
	fmt.Fprintf(os.Stderr, "pushed %d spans across %d traces to %s in %d request(s)\n", result.Spans, result.Traces, endpoint, len(batches))
	return nil
}
