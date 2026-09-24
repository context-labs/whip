package quickjs

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/rlm/engine"
)

func rewriteSnapshotMetadata(t *testing.T, image []byte, change func(*imageMetadata)) []byte {
	t.Helper()
	size := int(binary.LittleEndian.Uint32(image[8:12]))
	var metadata imageMetadata
	if err := json.Unmarshal(image[16:16+size], &metadata); err != nil {
		t.Fatal(err)
	}
	change(&metadata)
	encoded, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	var modified bytes.Buffer
	modified.Write(image[:16])
	modified.Write(encoded)
	modified.Write(image[16+size : len(image)-sha256.Size])
	data := modified.Bytes()
	binary.LittleEndian.PutUint32(data[8:12], uint32(len(encoded)))
	digest := sha256.Sum256(data)
	return append(data, digest[:]...)
}

func TestSnapshotRejectsCorruptionAndIncompatibleState(t *testing.T) {
	factory, err := NewFactory(t.Context(), engine.Options{AllowedTools: []string{"files.read"}})
	if err != nil {
		t.Fatal(err)
	}
	defer factory.Close(t.Context())
	vm, err := factory.New(t.Context(), "snapshot")
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close(t.Context())
	if err := vm.RunCell(t.Context(), "cell", "let saved = 42; saved"); err != nil {
		t.Fatal(err)
	}
	if _, err := vm.Drain(t.Context(), 100); err != nil {
		t.Fatal(err)
	}
	if err := vm.Finish(t.Context()); err != nil {
		t.Fatal(err)
	}
	image, err := vm.Checkpoint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name   string
		change func([]byte) []byte
		want   string
	}{
		{name: "truncated", change: func(b []byte) []byte { return b[:20] }, want: "snapshot size"},
		{name: "wrong magic", change: func(b []byte) []byte { b[0] ^= 1; return b }, want: "schema/magic"},
		{name: "invalid lengths", change: func(b []byte) []byte { binary.LittleEndian.PutUint32(b[12:16], 1); return b }, want: "lengths/limits"},
		{name: "damaged heap", change: func(b []byte) []byte { b[len(b)-sha256.Size-1] ^= 1; return b }, want: "integrity"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if restored, err := factory.Restore(t.Context(), "snapshot", tt.change(bytes.Clone(image))); err == nil || !strings.Contains(err.Error(), tt.want) || restored != nil {
				t.Fatalf("corrupt image produced runtime=%v, error=%v", restored, err)
			}
		})
	}
	for _, tt := range []struct {
		name   string
		change func(*imageMetadata)
		want   string
	}{
		{name: "different session", change: func(m *imageMetadata) { m.Session = "other" }, want: "namespace/WASM/schema/policy mismatch"},
		{name: "misaligned pointer", change: func(m *imageMetadata) { m.Runtime++ }, want: "pointer bounds/alignment"},
		{name: "unsafe ordinal", change: func(m *imageMetadata) { m.Ordinal = 1 << 53 }, want: "metadata limits"},
		{name: "wrong cell namespace", change: func(m *imageMetadata) { m.Prefix = "other" }, want: "cell identity"},
		{name: "invalid source hash", change: func(m *imageMetadata) { m.Cells[m.CellID] = "not-a-hash" }, want: "cell ledger"},
		{name: "invalid pending identity", change: func(m *imageMetadata) {
			m.Pending["other"] = engine.Request{ID: "other", Tool: "files.read", Args: []byte(`{}`)}
		}, want: "request identity/capability"},
		{name: "non-object pending arguments", change: func(m *imageMetadata) {
			m.Ordinal = 1
			id := m.Prefix + "1"
			m.Pending[id] = engine.Request{ID: id, Tool: "files.read", Args: []byte(`[]`)}
		}, want: "JSON object"},
		{name: "unsettled continuation", change: func(m *imageMetadata) {
			m.Ordinal = 1
			id := m.Prefix + "1"
			m.Pending[id] = engine.Request{ID: id, Tool: "files.read", Args: []byte(`{}`)}
		}, want: "unsettled checkpoint"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			modified := rewriteSnapshotMetadata(t, image, tt.change)
			if restored, err := factory.Restore(t.Context(), "snapshot", modified); err == nil || !strings.Contains(err.Error(), tt.want) || restored != nil {
				t.Fatalf("incompatible image produced runtime=%v, error=%v", restored, err)
			}
		})
	}
	if restored, err := factory.Restore(t.Context(), "", image); err == nil || restored != nil {
		t.Fatalf("invalid session restored: %v, %v", restored, err)
	}
	// Failed restores must not damage the factory or the original image.
	restored, err := factory.Restore(t.Context(), "snapshot", image)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close(t.Context())
	clear(image)
	if err := restored.RunCell(t.Context(), "next", "saved + 1"); err != nil {
		t.Fatal(err)
	}
	if _, err := restored.Drain(t.Context(), 100); err != nil {
		t.Fatal(err)
	}
	view, err := restored.Inspect(t.Context())
	if err != nil || string(view.Value) != "43" {
		t.Fatalf("restored heap aliased caller-owned image: %+v, %v", view, err)
	}
	if err := factory.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := factory.Restore(t.Context(), "snapshot", image); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed factory restored image: %v", err)
	}
}
