package rlm

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func quickJSWorkerInput(t *testing.T, requests ...frame) *bytes.Buffer {
	t.Helper()
	var input bytes.Buffer
	for _, request := range requests {
		if err := writeFrame(&input, defaultFrameBytes, request); err != nil {
			t.Fatal(err)
		}
	}
	return &input
}

func readQuickJSWorkerFrame(t *testing.T, output *bufio.Reader, kind string, id uint64) frame {
	t.Helper()
	response, err := readFrame(output, defaultFrameBytes, true)
	if err != nil || response.Type != kind || response.ID != id {
		t.Fatalf("want %s frame %d, got %+v: %v", kind, id, response, err)
	}
	return response
}

func TestQuickJSWorkerProtocolAndCheckpointRestore(t *testing.T) {
	descriptor, err := ResolveEngine(EngineQuickJS)
	if err != nil {
		t.Fatal(err)
	}
	hello := frame{Type: "hello", ID: 1, Engine: descriptor.ID, Build: descriptor.Build, ABI: descriptor.ABI, Profile: descriptor.Profile}
	input := quickJSWorkerInput(t, hello,
		frame{Type: "eval", ID: 2, Code: `let count = 40; const next = () => ++count; print("ready"); next()`},
		frame{Type: "eval", ID: 3, Code: `const content = await files.read({path:"note.txt"}); [content.text, content.large === 9007199254740993n]`},
		frame{Type: "host_response", ID: 1, CellID: 3, Value: map[string]any{"text": "from host", "large": json.Number("9007199254740993")}},
		frame{Type: "checkpoint", ID: 4},
	)
	var output bytes.Buffer
	if err := runQuickJSWorker(input, &output, DefaultLimits(), nil, nil); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(&output)
	response := readQuickJSWorkerFrame(t, reader, "result", 1)
	if response.Engine != descriptor.ID || response.Build != descriptor.Build || response.ABI != descriptor.ABI || response.Profile != descriptor.Profile {
		t.Fatalf("handshake lost engine identity: %+v", response)
	}
	if update := readQuickJSWorkerFrame(t, reader, "output", 2); update.Output != "ready\n" {
		t.Fatalf("streamed output: %+v", update)
	}
	response = readQuickJSWorkerFrame(t, reader, "result", 2)
	if response.Error != "" || !response.HasValue || response.Value != json.Number("41") || response.Output != "ready\n" || response.ComputeNanos == 0 {
		t.Fatalf("cell result: %+v", response)
	}
	request := readQuickJSWorkerFrame(t, reader, "host_request", 1)
	if request.CellID != 3 || request.Module != "files" || request.Operation != "read" || request.Arguments["path"] != "note.txt" {
		t.Fatalf("host request lost identity or arguments: %+v", request)
	}
	response = readQuickJSWorkerFrame(t, reader, "result", 3)
	value, ok := response.Value.([]any)
	if response.Error != "" || !ok || len(value) != 2 || value[0] != "from host" || value[1] != true || response.HostWaitNanos == 0 {
		t.Fatalf("host result lost precision or identity: %+v", response)
	}
	begin := readQuickJSWorkerFrame(t, reader, "checkpoint_begin", 4)
	image, err := readBlob(reader, defaultFrameBytes, begin)
	if err != nil {
		t.Fatal(err)
	}
	response = readQuickJSWorkerFrame(t, reader, "result", 4)
	var manifest SnapshotManifest
	if err := decodeFrameValue(response.Value, &manifest); err != nil || response.Error != "" || manifest.Bytes != len(image) || len(manifest.Saved) != 1 {
		t.Fatalf("checkpoint manifest: %+v, %v", response, err)
	}
	if _, err := readFrame(reader, defaultFrameBytes); !errors.Is(err, io.EOF) {
		t.Fatalf("unexpected trailing response: %v", err)
	}

	digest := sha256.Sum256(image)
	input = quickJSWorkerInput(t, hello, frame{Type: "restore_checkpoint", ID: 5, Bytes: len(image), SHA256: hex.EncodeToString(digest[:])})
	if err := writeBlobChunks(input, defaultFrameBytes, 5, image); err != nil {
		t.Fatal(err)
	}
	if err := writeFrame(input, defaultFrameBytes, frame{Type: "eval", ID: 6, Code: `next()`}); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := runQuickJSWorker(input, &output, DefaultLimits(), nil, nil); err != nil {
		t.Fatal(err)
	}
	reader = bufio.NewReader(&output)
	readQuickJSWorkerFrame(t, reader, "result", 1)
	response = readQuickJSWorkerFrame(t, reader, "result", 5)
	var restored RestoreReport
	if err := decodeFrameValue(response.Value, &restored); err != nil || response.Error != "" || len(restored.Restored) != 1 {
		t.Fatalf("restore report: %+v, %v", response, err)
	}
	response = readQuickJSWorkerFrame(t, reader, "result", 6)
	if response.Error != "" || !response.HasValue || response.Value != json.Number("42") {
		t.Fatalf("restored closure: %+v", response)
	}
}

func TestQuickJSWorkerRejectsInvalidProtocol(t *testing.T) {
	for _, tt := range []struct {
		name     string
		requests []frame
		want     string
	}{
		{name: "missing request ID", requests: []frame{{Type: "eval", Code: "1"}}, want: "missing RLM request ID"},
		{name: "wrong engine identity", requests: []frame{{Type: "hello", ID: 1, Engine: EngineStarlark}}, want: "engine handshake mismatch"},
		{name: "unexpected frame", requests: []frame{{Type: "host_response", ID: 1}}, want: "unexpected RLM worker frame"},
		{name: "wrong host cell", requests: []frame{{Type: "eval", ID: 1, Code: `await files.read({path:"a"})`}, {Type: "host_response", ID: 1, CellID: 2}}, want: "mismatched QuickJS host response"},
		{name: "unknown host request", requests: []frame{{Type: "eval", ID: 1, Code: `await files.read({path:"a"})`}, {Type: "host_response", ID: 2, CellID: 1}}, want: "mismatched QuickJS host response"},
		{name: "invalid checkpoint declaration", requests: []frame{{Type: "restore_checkpoint", ID: 1}}, want: "invalid checkpoint declaration"},
		{name: "unresolved promise", requests: []frame{{Type: "eval", ID: 1, Code: `await new Promise(() => {})`}}, want: "stalled"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			err := runQuickJSWorker(quickJSWorkerInput(t, tt.requests...), &output, DefaultLimits(), nil, nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want %q", err, tt.want)
			}
			if tt.requests[0].Type == "eval" && tt.requests[0].ID != 0 {
				reader := bufio.NewReader(&output)
				if len(tt.requests) > 1 {
					readQuickJSWorkerFrame(t, reader, "host_request", 1)
				}
				result := readQuickJSWorkerFrame(t, reader, "result", 1)
				if !strings.Contains(result.Error, tt.want) || result.Termination == "" {
					t.Fatalf("protocol failure missing terminal result: %+v", result)
				}
			}
		})
	}
}

func TestQuickJSWorkerGuestErrorsDoNotTerminateProtocol(t *testing.T) {
	input := quickJSWorkerInput(t,
		frame{Type: "eval", ID: 1, Code: `throw new Error("guest failure")`},
		frame{Type: "eval", ID: 2, Code: `var message = ""; try { await files.read({path:"missing"}) } catch (error) { message = error.message }; message`},
		frame{Type: "host_response", ID: 1, CellID: 2, Error: "file unavailable"},
		frame{Type: "eval", ID: 3, Code: "6 * 7"},
	)
	var output bytes.Buffer
	if err := runQuickJSWorker(input, &output, DefaultLimits(), nil, nil); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(&output)
	if result := readQuickJSWorkerFrame(t, reader, "result", 1); !strings.Contains(result.Error, "guest failure") || result.Termination != "" {
		t.Fatalf("guest error must leave worker usable: %+v", result)
	}
	readQuickJSWorkerFrame(t, reader, "host_request", 1)
	if result := readQuickJSWorkerFrame(t, reader, "result", 2); result.Error != "" || result.Value != "file unavailable" {
		t.Fatalf("host rejection must reach guest catch: %+v", result)
	}
	if result := readQuickJSWorkerFrame(t, reader, "result", 3); result.Error != "" || result.Value != json.Number("42") {
		t.Fatalf("worker did not recover after guest error: %+v", result)
	}
}

func TestQuickJSWorkerHostLimitsReachGuest(t *testing.T) {
	for _, tt := range []struct {
		name   string
		limits Limits
		code   string
		value  any
		want   string
	}{
		{
			name: "request quota", limits: Limits{HostRequests: 1},
			code:  `const settled = await Promise.allSettled([files.read({path:"a"}), files.read({path:"b"})]); settled.map(item => item.status).join(",")`,
			value: "first result", want: "fulfilled,rejected",
		},
		{
			name: "result payload quota", limits: Limits{FrameBytes: 512},
			code:  `var message = ""; try { await files.read({path:"a"}) } catch (error) { message = error.message }; message`,
			value: strings.Repeat("a", 300), want: "host result exceeds guest payload limit",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			input := quickJSWorkerInput(t,
				frame{Type: "eval", ID: 1, Code: tt.code},
				frame{Type: "host_response", ID: 1, CellID: 1, Value: tt.value},
				frame{Type: "eval", ID: 2, Code: "6 * 7"},
			)
			var output bytes.Buffer
			if err := runQuickJSWorker(input, &output, tt.limits.normalized(), nil, nil); err != nil {
				t.Fatal(err)
			}
			reader := bufio.NewReader(&output)
			request := readQuickJSWorkerFrame(t, reader, "host_request", 1)
			if request.Arguments["path"] != "a" {
				t.Fatalf("quota admitted the wrong host request: %+v", request)
			}
			result := readQuickJSWorkerFrame(t, reader, "result", 1)
			if result.Error != "" || result.Value != tt.want {
				t.Fatalf("quota rejection did not reach guest: %+v", result)
			}
			if result := readQuickJSWorkerFrame(t, reader, "result", 2); result.Error != "" || result.Value != json.Number("42") {
				t.Fatalf("quota rejection corrupted next cell: %+v", result)
			}
		})
	}
}

func TestQuickJSWorkerRejectsMalformedIO(t *testing.T) {
	for _, tt := range []struct {
		name, input, want string
		failOutput        bool
	}{
		{name: "malformed frame", input: "not-json\n", want: "decode RLM route"},
		{name: "unsupported version", input: "{\"version\":99,\"type\":\"eval\",\"id\":1}\n", want: "unsupported RLM protocol version"},
		{name: "write failed", input: "{\"version\":2,\"type\":\"eval\",\"id\":1,\"code\":\"1\"}\n", failOutput: true, want: "write failed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			output := io.Discard
			if tt.failOutput {
				output = errorWriter{}
			}
			err := runQuickJSWorker(strings.NewReader(tt.input), output, DefaultLimits(), nil, nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want %q", err, tt.want)
			}
		})
	}
}
