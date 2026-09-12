package quickjs

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/rlm/engine"
)

func TestAllocationRejectsOutOfRangeSizes(t *testing.T) {
	cases := []struct {
		name string
		size int
	}{
		{name: "negative", size: -1},
		{name: "zero", size: 0},
	}
	if strconv.IntSize == 64 {
		tooLarge := uint64(math.MaxUint32) + 1
		cases = append(cases, struct {
			name string
			size int
		}{name: "exceeds wasm32", size: int(tooLarge)})
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			vm := &runtime{}
			err := func() (err error) {
				defer recoverError(&err)
				vm.alloc(t.Context(), tt.size)
				return nil
			}()
			if err == nil || !strings.Contains(err.Error(), "allocation size exceeds wasm32 address space") {
				t.Fatalf("allocation size %d: got %v, want size rejection before guest entry", tt.size, err)
			}
		})
	}
}

func TestCallbackArgumentBounds(t *testing.T) {
	factory, err := NewFactory(t.Context(), engine.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer factory.Close(t.Context())
	instance, err := factory.New(t.Context(), "callback-bounds")
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close(t.Context())
	vm := instance.(*runtime)
	name := vm.put(t.Context(), callbackName)
	defer vm.free(t.Context(), name)
	for _, tt := range []struct {
		name  string
		stack []uint64
		want  string
	}{
		{name: "incomplete stack", stack: []uint64{0, 0, 0}, want: "incomplete callback arguments"},
		{name: "truncated argv", stack: []uint64{name, uint64(len(callbackName)), 0, 3, uint64(vm.module.Memory().Size() - 4)}, want: "callback argv read failed"},
		{name: "wrapping argv", stack: []uint64{name, uint64(len(callbackName)), 0, 3, math.MaxUint32 - 3}, want: "callback argv read failed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := func() (err error) {
				defer recoverError(&err)
				vm.factory.host(t.Context(), vm.module, "env", "host_call", tt.stack)
				return nil
			}()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want %q", err, tt.want)
			}
		})
	}
}

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
