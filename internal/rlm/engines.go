package rlm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/context-labs/whip/internal/rlm/engine/quickjs"
)

const (
	EngineStarlark     = "starlark"
	EngineQuickJS      = "quickjs"
	MaxCheckpointBytes = 40 << 20
)

// EngineDescriptor identifies a trusted bundled execution engine. Build, ABI and
// profile must all match before an opaque checkpoint can be restored.
type EngineDescriptor struct {
	BridgeSHA256 string            `json:"bridge_sha256,omitempty"`
	GuideSHA256  string            `json:"guide_sha256"`
	Features     []string          `json:"features"`
	Limits       map[string]uint64 `json:"limits"`
	ID           string            `json:"id"`
	Language     string            `json:"language"`
	Label        string            `json:"label"`
	Build        string            `json:"build"`
	ABI          string            `json:"abi"`
	Profile      string            `json:"profile"`
	Fidelity     string            `json:"fidelity"`
}

// Bundled engine guides and their digests are static for this process.
var engineDescriptors = sync.OnceValue(func() []EngineDescriptor {
	descriptors := []EngineDescriptor{
		{ID: EngineStarlark, Language: "starlark", Label: "Starlark", Build: "starlark-6dd8f160a37f", ABI: "whip-host-v2", Profile: "settled-v1", Fidelity: "tagged-partial"},
		{ID: EngineQuickJS, Language: "javascript", Label: "JavaScript (QuickJS)", Build: quickjs.WasmSHA256, ABI: "whip-quickjs-v1-host-v2", Profile: "settled-v1", Fidelity: "whole-image"},
	}
	for i := range descriptors {
		d := &descriptors[i]
		d.Features = []string{"host_modules", "settled_checkpoints", "worker_isolation"}
		d.Limits = map[string]uint64{"process_memory_bytes": defaultMemoryBytes, "output_bytes": defaultOutputBytes, "frame_bytes": defaultFrameBytes, "host_requests_per_cell": defaultHostRequests, "default_compute_nanoseconds": uint64(30 * time.Second), "checkpoint_bytes": MaxCheckpointBytes}
		if d.ID == EngineQuickJS {
			d.BridgeSHA256 = quickjs.BridgeSHA256()
			d.Features = append(d.Features, "top_level_await", "concurrent_host_calls", "lossless_host_numbers", "whole_heap_restore")
			d.Limits["guest_memory_bytes"], d.Limits["wasm_memory_pages"], d.Limits["outstanding_host_calls"], d.Limits["jobs_per_cell"], d.Limits["cell_watchdog_nanoseconds"] = 32<<20, 1024, maxOutstandingCalls, maxQuickJSJobs, uint64(10*time.Minute)
		} else {
			d.Limits["starlark_steps_per_cell"] = defaultSteps
		}
		guide, _ := RuntimeGuide(d.ID, ModuleNames(), nil, nil, "", nil)
		digest := sha256.Sum256([]byte(guide))
		d.GuideSHA256 = hex.EncodeToString(digest[:])
	}
	return descriptors
})

func Engines() []EngineDescriptor {
	descriptors := slices.Clone(engineDescriptors())
	for i := range descriptors {
		descriptors[i].Features = slices.Clone(descriptors[i].Features)
		descriptors[i].Limits = maps.Clone(descriptors[i].Limits)
	}
	return descriptors
}

func ResolveEngine(id string) (EngineDescriptor, error) {
	if id == "" {
		id = EngineStarlark
	}
	for _, descriptor := range Engines() {
		if descriptor.ID == id {
			return descriptor, nil
		}
	}
	return EngineDescriptor{}, fmt.Errorf("unsupported execution engine %q", id)
}

func (kernel *Kernel) Describe() EngineDescriptor {
	descriptor := kernel.engine
	descriptor.Limits = maps.Clone(descriptor.Limits)
	descriptor.Features = slices.Clone(descriptor.Features)
	descriptor.Limits["process_memory_bytes"] = kernel.limits.MemoryBytes
	descriptor.Limits["host_requests_per_cell"] = uint64(kernel.limits.HostRequests) //nolint:gosec // NewKernel requires HostRequests >= 1.
	descriptor.Limits["default_compute_nanoseconds"] = uint64(kernel.limits.Wall)    //nolint:gosec // NewKernel requires Wall >= time.Millisecond.
	if descriptor.ID == EngineQuickJS {
		descriptor.Limits["concurrent_host_calls"] = uint64(kernel.limits.MaxConcurrentHostCalls) //nolint:gosec // NewKernel requires MaxConcurrentHostCalls >= 1.
	} else {
		descriptor.Limits["concurrent_host_calls"] = 1
	}
	return descriptor
}

// CheckpointEnvelope contains integrity and compatibility metadata; storage
// adapters bind the image to its root and agent before atomic publication.
type CheckpointEnvelope struct {
	BridgeSHA256  string           `json:"bridge_sha256,omitempty"`
	FormatVersion int              `json:"format_version"`
	RootID        string           `json:"root_id,omitempty"`
	AgentID       string           `json:"agent_id,omitempty"`
	Engine        string           `json:"engine"`
	Build         string           `json:"build"`
	ABI           string           `json:"abi"`
	Profile       string           `json:"profile"`
	Boundary      string           `json:"boundary"`
	Fidelity      string           `json:"fidelity"`
	Sequence      uint64           `json:"sequence"`
	Bytes         int              `json:"bytes"`
	SHA256        string           `json:"sha256"`
	Manifest      SnapshotManifest `json:"manifest"`
}
type Checkpoint struct {
	Envelope CheckpointEnvelope `json:"envelope"`
	Data     []byte             `json:"-"`
}
type CheckpointStore interface {
	Load(context.Context) (*Checkpoint, error)
	Save(context.Context, Checkpoint) error
}

func (checkpoint Checkpoint) Validate(descriptor EngineDescriptor) error {
	e := checkpoint.Envelope
	if e.BridgeSHA256 != descriptor.BridgeSHA256 || e.FormatVersion != 1 || e.Engine != descriptor.ID || e.Build != descriptor.Build || e.ABI != descriptor.ABI || e.Profile != descriptor.Profile || e.Fidelity != descriptor.Fidelity || e.Boundary != "settled-cell" {
		return errors.New("checkpoint unavailable: incompatible engine build, ABI, or profile; retained image requires explicit recovery")
	}
	if e.Bytes != len(checkpoint.Data) || e.Bytes < 1 || e.Bytes > MaxCheckpointBytes {
		return errors.New("invalid checkpoint size")
	}
	digest := sha256.Sum256(checkpoint.Data)
	if e.SHA256 != hex.EncodeToString(digest[:]) {
		return errors.New("checkpoint integrity mismatch")
	}
	return nil
}

func newCheckpoint(descriptor EngineDescriptor, sequence uint64, data []byte, manifest SnapshotManifest) Checkpoint {
	digest := sha256.Sum256(data)
	return Checkpoint{Envelope: CheckpointEnvelope{BridgeSHA256: descriptor.BridgeSHA256, FormatVersion: 1, Engine: descriptor.ID, Build: descriptor.Build, ABI: descriptor.ABI, Profile: descriptor.Profile, Boundary: "settled-cell", Fidelity: descriptor.Fidelity, Sequence: sequence, Bytes: len(data), SHA256: hex.EncodeToString(digest[:]), Manifest: manifest}, Data: data}
}
