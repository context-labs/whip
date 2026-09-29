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

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/trace"
	"golang.org/x/sys/unix"
)

// otlpPushBatchBytes keeps each pushed request under the 4 MiB body cap that
// HALO desktop and inference.net enforce, with headroom for the envelope.
const otlpPushBatchBytes = 4<<20 - 64<<10

// `whipcode sessions export <root> [-trace id] [-o file|-] [-push URL] [-token T]`
// renders a session's spans as one OTLP/JSON ExportTraceServiceRequest. The
// native host builds the document; the CLI fetches its bounded content reference
// and either writes it or posts it to an OTLP/HTTP endpoint in gzip batches.
func sessionsExportCLI(args []string) error {
	fs := flag.NewFlagSet("sessions export", flag.ContinueOnError)
	traceID := fs.String("trace", "", "export one trace instead of the whole session")
	out := fs.String("o", "", "write the OTLP/JSON here; '-' for stdout (default: <root>.otlp.json)")
	push := fs.String("push", "", "POST the export to an OTLP/HTTP endpoint instead of writing a file, e.g. http://127.0.0.1:8799/v1/traces")
	token := fs.String("token", os.Getenv("INFERENCE_API_KEY"), "bearer token for -push (default $INFERENCE_API_KEY)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: whipcode sessions export <root> [-trace id] [-o file|-] [-push URL] [-token T]")
		fs.PrintDefaults()
	}
	// The standard parser stops at the first positional argument; accept flags
	// on either side of the root id by re-parsing after each positional.
	root := ""
	for {
		if err := fs.Parse(args); err != nil {
			return err
		}
		if fs.NArg() == 0 {
			break
		}
		if root != "" {
			fs.Usage()
			return fmt.Errorf("unexpected argument %q", fs.Arg(0))
		}
		root, args = fs.Arg(0), fs.Args()[1:]
	}
	if root == "" {
		fs.Usage()
		return errors.New("a session (root) id is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	connection, err := connectNativeRuntime(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = connection.Close() }()
	var result protocol.TraceExportResult
	if err := connection.Call(ctx, "trace.export", protocol.TraceExportParams{RootID: protocol.ID(root), TraceID: *traceID}, &result); err != nil {
		return err
	}
	owner, err := connection.Session(protocol.ID(root))
	if err != nil {
		return err
	}
	reference, data, err := owner.ReadContent(ctx, result.Reference.ID)
	if err != nil {
		return err
	}
	if reference != result.Reference {
		return errors.New("trace export content identity changed")
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
	// Exports carry prompts and tool output; keep them private to the user.
	if err := writeTraceExport(path, data); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "exported %d spans across %d traces to %s\n", result.Spans, result.Traces, path)
	return nil
}

func writeTraceExport(path string, data []byte) error {
	fd, err := unix.Open(path, unix.O_WRONLY|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), path)
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || !runRecordOwned(info) {
		return errors.New("trace export requires an owned regular file")
	}
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	if err := file.Truncate(0); err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		return err
	}
	return file.Sync()
}

// pushOTLP posts the export as gzip OTLP/JSON, split so every request stays
// under the endpoint's body cap.
func pushOTLP(ctx context.Context, endpoint, token string, data []byte, result protocol.TraceExportResult) error {
	return pushOTLPWithBatchSize(ctx, endpoint, token, data, result, otlpPushBatchBytes)
}

func pushOTLPWithBatchSize(ctx context.Context, endpoint, token string, data []byte, result protocol.TraceExportResult, batchBytes int) error {
	batches, err := trace.SplitOTLP(data, batchBytes)
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
