package quickjs

import (
	"testing"

	"github.com/context-labs/whip/internal/rlm/engine"
)

func TestWASIClockNormalizesI32ArgumentWidth(t *testing.T) {
	factory, err := NewFactory(t.Context(), engine.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer factory.Close(t.Context())
	instance, err := factory.New(t.Context(), "width-regression")
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close(t.Context())
	vm := instance.(*runtime)
	ptr := vm.alloc(t.Context(), 8)
	defer vm.free(t.Context(), ptr)
	// Wazero's amd64 host-call ABI can leave nonzero bits above an i32 value.
	// Rejecting this valid realtime clock made QuickJS abort during JS_NewContext.
	stack := []uint64{0xffffffff00000000, 1, ptr}
	vm.factory.host(t.Context(), vm.module, "wasi_snapshot_preview1", "clock_time_get", stack)
	if stack[0] != 0 {
		t.Fatalf("valid clock rejected: errno %d", stack[0])
	}
	now, ok := vm.module.Memory().ReadUint64Le(uint32(ptr))
	if !ok || now == 0 {
		t.Fatal("clock timestamp was not written")
	}
}
