package mcp

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestStdioFramesRejectBeforeJSONDecodeAndRetainFraming(t *testing.T) {
	for _, input := range []string{`{"a":"` + strings.Repeat("x", maxWireBytes) + `"}`, "{\n\"a\":1\n}", strings.Repeat(" ", maxWireBytes+1)} {
		reader := boundedStdio(io.NopCloser(strings.NewReader(input)), nil)
		if _, err := io.ReadAll(reader); err == nil {
			t.Fatal("invalid or oversized stdio message accepted")
		}
	}
	input := "{\"a\":1}\n{\"b\":2}\r\n"
	reader := boundedStdio(io.NopCloser(strings.NewReader(input)), nil)
	data, err := io.ReadAll(reader)
	if err != nil || string(data) != input {
		t.Fatalf("bounded frames: %q %v", data, err)
	}
}

func TestHTTPResponsesBoundEachJSONBodyAndSSEEvent(t *testing.T) {
	for _, kind := range []string{"application/json", "text/event-stream"} {
		response := &http.Response{Header: http.Header{"Content-Type": []string{kind}}, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", maxWireBytes+1)))}
		boundHTTPBody(response, nil)
		count, err := io.Copy(io.Discard, response.Body)
		if err == nil || count > maxWireBytes {
			t.Fatalf("%s: bytes=%d error=%v", kind, count, err)
		}
		if _, err = response.Body.Read(make([]byte, 1)); err == nil || errors.Is(err, io.EOF) {
			t.Fatalf("overflow was silently reset: %v", err)
		}
	}
	event := "data: " + strings.Repeat("x", maxWireBytes/2) + "\r\n\r\n"
	response := &http.Response{Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(strings.Repeat(event, 3)))}
	boundHTTPBody(response, nil)
	if count, err := io.Copy(io.Discard, response.Body); err != nil || count != int64(3*len(event)) {
		t.Fatalf("bounded SSE events should survive across lifetime: %d %v", count, err)
	}
}

func TestAggregateWireBudgetRejectsAndReleasesBufferedFrames(t *testing.T) {
	budget := NewBudget()
	var readers []*stdioFrames
	t.Cleanup(func() {
		for _, reader := range readers {
			_ = reader.Close()
		}
	})
	frame := `{"data":"` + strings.Repeat("x", maxWireBytes-16) + `"}` + "\n"
	for index := range 5 {
		reader := boundedStdio(io.NopCloser(strings.NewReader(frame)), budget)
		readers = append(readers, reader)
		_, err := reader.Read(make([]byte, 1))
		if (index == 4) != (err != nil) {
			t.Fatalf("frame %d admission: %v", index, err)
		}
	}
	for _, reader := range readers {
		_ = reader.Close()
	}
	if budget.wireBytes != 0 {
		t.Fatalf("closed frames retained %d bytes", budget.wireBytes)
	}
}
