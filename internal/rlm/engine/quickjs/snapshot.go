package quickjs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/context-labs/whip/internal/rlm/engine"
	"github.com/tetratelabs/wazero/api"
)

const (
	imageMagic    = "QJSWIMG0"
	imageSchema   = 1
	bridgeVersion = 1
)

type imageMetadata struct {
	Schema  int                       `json:"schema"`
	Bridge  int                       `json:"bridge"`
	Wasm    string                    `json:"wasm"`
	Policy  string                    `json:"policy"`
	Session string                    `json:"session"`
	Stack   uint32                    `json:"stack"`
	Runtime uint32                    `json:"runtime"`
	Context uint32                    `json:"context"`
	Control uint32                    `json:"control"`
	CellID  string                    `json:"cellId"`
	Prefix  string                    `json:"prefix"`
	Ordinal uint64                    `json:"ordinal"`
	Cells   map[string]string         `json:"cells"`
	Pending map[string]engine.Request `json:"pending"`
}

func (v *runtime) validateGuest(ctx context.Context) {
	text := func() string {
		h := v.controlCall(ctx, "identity")
		defer v.freeValue(ctx, h)
		return v.text(ctx, h, v.viewLimit())
	}()
	var guest struct {
		Bridge  int
		Prefix  string
		Ordinal uint64
		CellID  string
		Pending []string
	}
	must(json.Unmarshal([]byte(text), &guest))
	check(guest.Bridge == bridgeVersion && guest.Prefix == v.prefix && guest.CellID == v.cellID && guest.Ordinal == v.ordinal, "guest namespace/bridge identity mismatch")
	want := make([]string, 0, len(v.pending))
	for id := range v.pending {
		want = append(want, id)
	}
	sort.Strings(want)
	sort.Strings(guest.Pending)
	check(len(want) == len(guest.Pending), "guest/host pending count mismatch")
	for i, id := range want {
		check(id == guest.Pending[i], "guest/host pending identity mismatch")
	}
}

// Checkpoint returns an owned immutable-by-convention image only after all
// owned host requests and jobs settle. No pending continuation is durable.
func (v *runtime) Checkpoint(ctx context.Context) (image []byte, err error) {
	if err = v.enter(ctx); err != nil {
		return nil, err
	}
	defer v.mu.Unlock()
	defer recoverError(&err)
	ctx, cancel := bounded(ctx)
	defer cancel()
	if len(v.queue) != 0 || len(v.pending) != 0 || v.call(ctx, "qjs_is_job_pending") != 0 || v.cellStatus == "running" {
		return nil, ErrQueuedRequests
	}
	v.validateGuest(ctx)
	if len(v.queue) != 0 || len(v.pending) != 0 || v.call(ctx, "qjs_is_job_pending") != 0 || v.cellStatus == "running" {
		return nil, ErrQueuedRequests
	}
	check(v.boxes == 1 && v.allocations == 0 && v.strings == 0, "unbalanced host handles at checkpoint")
	metadata := imageMetadata{Schema: imageSchema, Bridge: bridgeVersion, Wasm: WasmSHA256, Policy: v.factory.identity, Session: v.session, Stack: api.DecodeU32(v.module.ExportedGlobal("__stack_pointer").Get()), Runtime: api.DecodeU32(v.call(ctx, "qjs_get_runtime_ptr")), Context: api.DecodeU32(v.call(ctx, "qjs_get_context_ptr")), Control: api.DecodeU32(v.control), CellID: v.cellID, Prefix: v.prefix, Ordinal: v.ordinal, Cells: v.cells, Pending: v.pending}
	data, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	size := v.module.Memory().Size()
	total := uint64(16+sha256.Size) + uint64(len(data)) + uint64(size)
	if len(data) > 16<<20 || total > v.factory.options.Limits.MaxSnapshotBytes || total > math.MaxInt {
		return nil, errors.New("bridge: snapshot byte limit")
	}
	image = make([]byte, int(total))
	copy(image, imageMagic)
	image[7] = imageSchema
	binary.LittleEndian.PutUint32(image[8:12], uint32(len(data))) //nolint:gosec // Metadata is capped at 16 MiB above.
	binary.LittleEndian.PutUint32(image[12:16], size)
	copy(image[16:], data)
	memory, ok := v.module.Memory().Read(0, size)
	check(ok, "snapshot memory read failed")
	copy(image[16+len(data):], memory)
	digest := sha256.Sum256(image[:len(image)-sha256.Size])
	copy(image[len(image)-sha256.Size:], digest[:])
	return image, nil
}

func (f *factory) decode(session string, image []byte) (imageMetadata, []byte, error) {
	var meta imageMetadata
	if uint64(len(image)) > f.options.Limits.MaxSnapshotBytes || len(image) < 16+sha256.Size {
		return meta, nil, errors.New("bridge: snapshot size")
	}
	if string(image[:7]) != imageMagic[:7] || image[7] != imageSchema {
		return meta, nil, errors.New("bridge: snapshot schema/magic")
	}
	metadataLen := uint64(binary.LittleEndian.Uint32(image[8:12]))
	memoryLen := uint64(binary.LittleEndian.Uint32(image[12:16]))
	if metadataLen == 0 || metadataLen > 16<<20 || memoryLen == 0 || memoryLen%65536 != 0 || memoryLen > uint64(f.options.Limits.MemoryPages)*65536 || 16+metadataLen+memoryLen+sha256.Size != uint64(len(image)) {
		return meta, nil, errors.New("bridge: snapshot lengths/limits")
	}
	digest := sha256.Sum256(image[:len(image)-sha256.Size])
	if !bytes.Equal(digest[:], image[len(image)-sha256.Size:]) {
		return meta, nil, errors.New("bridge: snapshot integrity")
	}
	if err := validJSON(image[16:16+metadataLen], 16<<20); err != nil {
		return meta, nil, err
	}
	d := json.NewDecoder(bytes.NewReader(image[16 : 16+metadataLen]))
	d.DisallowUnknownFields()
	if err := d.Decode(&meta); err != nil {
		return meta, nil, err
	}
	if meta.Schema != imageSchema || meta.Bridge != bridgeVersion || meta.Wasm != WasmSHA256 || meta.Policy != f.identity || meta.Session != session {
		return meta, nil, errors.New("bridge: snapshot namespace/WASM/schema/policy mismatch")
	}
	for _, p := range []uint32{meta.Stack, meta.Runtime, meta.Context, meta.Control} {
		if p == 0 || uint64(p)+16 > memoryLen || p%4 != 0 {
			return meta, nil, errors.New("bridge: snapshot pointer bounds/alignment")
		}
	}
	if meta.Ordinal >= 1<<53 || meta.Cells == nil || meta.Pending == nil || len(meta.Cells) > 1 || len(meta.Pending) > f.options.Limits.MaxQueuedRequests {
		return meta, nil, errors.New("bridge: snapshot metadata limits")
	}
	if meta.CellID != "" {
		if !validID(meta.CellID) || meta.Prefix != scope(session, meta.CellID) || meta.Cells[meta.CellID] == "" {
			return meta, nil, errors.New("bridge: snapshot cell identity")
		}
	} else if len(meta.Pending) != 0 || meta.Prefix != "" || meta.Ordinal != 0 {
		return meta, nil, errors.New("bridge: snapshot idle metadata")
	}
	for id, sourceHash := range meta.Cells {
		_, hashErr := hex.DecodeString(sourceHash)
		if !validID(id) || len(sourceHash) != 64 || hashErr != nil || sourceHash != strings.ToLower(sourceHash) {
			return meta, nil, errors.New("bridge: snapshot cell ledger")
		}
	}
	for id, r := range meta.Pending {
		suffix := strings.TrimPrefix(id, meta.Prefix)
		n, idErr := strconv.ParseUint(suffix, 10, 53)
		if idErr != nil || n == 0 || n > meta.Ordinal || strconv.FormatUint(n, 10) != suffix || id != r.ID || !f.allowed[r.Tool] {
			return meta, nil, errors.New("bridge: snapshot request identity/capability")
		}
		if err := validArgs(r.Args, f.options.Limits.MaxRequestBytes); err != nil {
			return meta, nil, err
		}
		data, _ := json.Marshal(r)
		if len(data) > f.options.Limits.MaxRequestBytes {
			return meta, nil, errors.New("bridge: snapshot request bytes")
		}
	}
	// Restore never aliases caller-owned bytes; this copy is also used before
	// module instantiation. Images remain trusted local artifacts, not auth tokens.
	memory := append([]byte(nil), image[16+metadataLen:len(image)-sha256.Size]...)
	return meta, memory, nil
}

func (f *factory) Restore(ctx context.Context, session string, image []byte) (_ engine.Runtime, err error) {
	if !validID(session) {
		return nil, errors.New("bridge: invalid session ID")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	defer recoverError(&err)
	if f.closed {
		return nil, ErrClosed
	}
	meta, memory, err := f.decode(session, image)
	if err == nil && len(meta.Pending) != 0 {
		return nil, errors.New("quickjs: unsettled checkpoint is unsupported")
	}
	if err != nil {
		return nil, err
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	v := f.instantiate(ctx, session)
	defer func() {
		if p := recover(); p != nil {
			_ = v.module.Close(context.WithoutCancel(ctx))
			f.vms.Delete(v.module.Name())
			panic(p)
		}
	}()
	// decode bounds memory to the configured maximum of 4,096 WASM pages.
	pages := uint32(len(memory) / 65536) //nolint:gosec // The validated image fits in the wasm32 page count.
	current := v.module.Memory().Size() / 65536
	check(pages >= current, "snapshot smaller than initial memory")
	if pages > current {
		_, ok := v.module.Memory().Grow(pages - current)
		check(ok, "restore memory grow failed")
	}
	check(v.module.Memory().Write(0, memory), "restore memory write failed")
	stack, ok := v.module.ExportedGlobal("__stack_pointer").(api.MutableGlobal)
	check(ok, "mutable stack export missing")
	stack.Set(uint64(meta.Stack))
	v.call(ctx, "qjs_set_runtime_and_context", uint64(meta.Runtime), uint64(meta.Context))
	v.initialized = true
	v.control = uint64(meta.Control)
	v.boxes = 1
	v.cellID = meta.CellID
	v.prefix = meta.Prefix
	v.ordinal = meta.Ordinal
	v.cells = meta.Cells
	v.pending = meta.Pending
	// The imported host dispatcher binds this fresh module to the new runtime;
	// the C callback's name and guest-owned resolver closures survive in memory.
	v.configure(ctx)
	v.validateGuest(ctx)
	v.refreshAdmission(ctx)
	check(len(v.queue) == 0, "restored identity unexpectedly submitted requests")
	return v, nil
}
