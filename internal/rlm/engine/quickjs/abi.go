package quickjs

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/rlm/engine"
	"github.com/tetratelabs/wazero/api"
)

func (v *runtime) call(ctx context.Context, name string, args ...uint64) uint64 {
	f := v.module.ExportedFunction(name)
	check(f != nil, "missing export: "+name)
	result, err := f.Call(ctx, args...)
	if err != nil {
		// An interrupted/trapped C stack is not a resumable quiescent boundary.
		_ = v.module.Close(context.WithoutCancel(ctx))
		v.factory.vms.Delete(v.module.Name())
		v.closed = true
		v.queue = nil
		v.pending = nil
		v.cells = nil
		must(err)
	}
	if len(result) == 0 {
		return 0
	}
	if f.Definition().ResultTypes()[0] == api.ValueTypeI32 {
		return uint64(uint32(result[0]))
	}
	return result[0]
}
func (v *runtime) valueCall(ctx context.Context, name string, args ...uint64) uint64 {
	h := v.call(ctx, name, args...)
	check(h != 0, "null JSValue box: "+name)
	v.boxes++
	return h
}
func (v *runtime) freeValue(ctx context.Context, h uint64) {
	if h == 0 {
		return
	}
	v.boxes--
	if !v.module.IsClosed() {
		v.call(ctx, "qjs_free_value", h)
	}
}
func (v *runtime) alloc(ctx context.Context, n int) uint64 {
	p := v.call(ctx, "wasm_malloc", uint64(n))
	check(p != 0, "malloc failed")
	v.allocations++
	return p
}
func (v *runtime) free(ctx context.Context, p uint64) {
	v.allocations--
	if !v.module.IsClosed() {
		v.call(ctx, "wasm_free", p)
	}
}
func (v *runtime) put(ctx context.Context, s string) uint64 {
	p := v.alloc(ctx, len(s)+1)
	check(v.module.Memory().Write(uint32(p), append([]byte(s), 0)), "write out of bounds")
	return p
}
func (v *runtime) newString(ctx context.Context, s string) uint64 {
	p := v.put(ctx, s)
	defer v.free(ctx, p)
	h := v.valueCall(ctx, "qjs_new_string", p, uint64(len(s)))
	if v.call(ctx, "qjs_is_exception", h) != 0 {
		defer v.freeValue(ctx, h)
		v.checkException(ctx, h)
	}
	return h
}
func (v *runtime) text(ctx context.Context, h uint64, max int) string {
	lp := v.alloc(ctx, 4)
	defer v.free(ctx, lp)
	p := v.call(ctx, "qjs_get_string_len", h, lp)
	check(p != 0, "string conversion failed")
	v.strings++
	defer func() {
		v.strings--
		if !v.module.IsClosed() {
			v.call(ctx, "qjs_free_cstring", p)
		}
	}()
	n, ok := v.module.Memory().ReadUint32Le(uint32(lp))
	check(ok, "string length read failed")
	check(uint64(n) <= uint64(max), "string byte limit")
	b, ok := v.module.Memory().Read(uint32(p), n)
	check(ok, "string read out of bounds")
	check(utf8.Valid(b), "invalid UTF-8 string")
	return string(b)
}
func (v *runtime) evalRaw(ctx context.Context, source string, flags uint64) uint64 {
	p := v.put(ctx, source)
	defer v.free(ctx, p)
	name := v.put(ctx, "<rlm-javascript>")
	defer v.free(ctx, name)
	return v.valueCall(ctx, "qjs_eval", p, uint64(len(source)), name, flags)
}
func (v *runtime) eval(ctx context.Context, source string, flags uint64) uint64 {
	h := v.evalRaw(ctx, source, flags)
	if v.call(ctx, "qjs_is_exception", h) != 0 {
		defer v.freeValue(ctx, h)
		v.checkException(ctx, h)
	}
	return h
}
func (v *runtime) checkException(ctx context.Context, h uint64) {
	if v.call(ctx, "qjs_is_exception", h) == 0 {
		return
	}
	e := v.valueCall(ctx, "qjs_get_exception")
	defer v.freeValue(ctx, e)
	panic(failure{fmt.Errorf("bridge: guest exception: %s", v.text(ctx, e, v.factory.options.Limits.MaxOutputBytes))})
}
func (v *runtime) property(ctx context.Context, obj uint64, name string) uint64 {
	p := v.put(ctx, name)
	defer v.free(ctx, p)
	h := v.valueCall(ctx, "qjs_get_prop_string", obj, p)
	if v.call(ctx, "qjs_is_exception", h) != 0 {
		defer v.freeValue(ctx, h)
		v.checkException(ctx, h)
	}
	return h
}
func (v *runtime) controlCall(ctx context.Context, name string, args ...uint64) uint64 {
	f := v.property(ctx, v.control, name)
	defer v.freeValue(ctx, f)
	var argv uint64
	if len(args) > 0 {
		argv = v.alloc(ctx, len(args)*4)
		defer v.free(ctx, argv)
		for i, arg := range args {
			check(v.module.Memory().WriteUint32Le(uint32(argv)+uint32(i*4), uint32(arg)), "argv write failed")
		}
	}
	h := v.valueCall(ctx, "qjs_call", f, v.control, uint64(len(args)), argv)
	if v.call(ctx, "qjs_is_exception", h) != 0 {
		defer v.freeValue(ctx, h)
		v.checkException(ctx, h)
	}
	return h
}
func (v *runtime) installHost(ctx context.Context) {
	p := v.put(ctx, callbackName)
	defer v.free(ctx, p)
	h := v.valueCall(ctx, "qjs_new_host_function", p, uint64(len(callbackName)), 3)
	defer v.freeValue(ctx, h)
	v.checkException(ctx, h)
	g := v.valueCall(ctx, "qjs_get_global")
	defer v.freeValue(ctx, g)
	name := v.put(ctx, "__whipSubmit")
	defer v.free(ctx, name)
	check(int32(v.call(ctx, "qjs_set_prop_string", g, name, h)) >= 0, "host registration failed")
}

// Host JSON decoding uses tokens to reject duplicate keys and excessive nesting.
// No arbitrary Go object graph is retained, and no guest serializer is trusted.
func validJSON(b []byte, max int) error {
	if len(b) == 0 || len(b) > max || !utf8.Valid(b) {
		return errors.New("bridge: JSON byte limit/encoding")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err := jsonValue(d, 0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("bridge: trailing JSON data")
	}
	return nil
}
func jsonValue(d *json.Decoder, depth int) error {
	if depth > 64 {
		return errors.New("bridge: JSON nesting limit")
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		if n, ok := token.(json.Number); ok && strings.ContainsAny(string(n), ".eE") {
			if _, err := strconv.ParseFloat(string(n), 64); err != nil {
				return errors.New("bridge: non-finite JSON number")
			}
		}
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]bool)
		for d.More() {
			k, err := d.Token()
			if err != nil {
				return err
			}
			key, ok := k.(string)
			if !ok || seen[key] {
				return errors.New("bridge: duplicate/invalid JSON key")
			}
			seen[key] = true
			if err = jsonValue(d, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err = jsonValue(d, depth+1); err != nil {
				return err
			}
		}
	default:
		return errors.New("bridge: invalid JSON delimiter")
	}
	_, err = d.Token()
	return err
}
func supportedImport(mod, name string) bool {
	if mod == "env" {
		switch name {
		case "host_call", "host_interrupt", "host_promise_rejection", "host_module_normalize", "host_module_load", "host_get_timezone_offset":
			return true
		}
	}
	if mod == "wasi_snapshot_preview1" {
		switch name {
		case "clock_time_get", "random_get", "fd_close", "fd_seek", "fd_fdstat_get", "fd_write":
			return true
		}
	}
	return false
}
func (f *factory) host(ctx context.Context, m api.Module, mod, name string, s []uint64) {
	if mod == "env" {
		value, ok := f.vms.Load(m.Name())
		check(ok, "callback runtime not registered")
		v := value.(*runtime)
		switch name {
		case "host_get_timezone_offset":
			s[0] = 0
		case "host_interrupt":
			s[0] = 0
			if ctx.Err() != nil {
				s[0] = 1
			}
		case "host_module_normalize", "host_module_load":
			panic(failure{errors.New("bridge: dynamic modules disabled")})
		case "host_promise_rejection":
			// The callback owns both boxes. Record promise identity in a guest Set,
			// using only captured native Set operations on this same guest owner.
			v.boxes += 2
			if v.control != 0 {
				method := "rejectionAdd"
				if uint32(s[2]) != 0 {
					method = "rejectionDelete"
				}
				result := v.controlCall(ctx, method, s[0])
				v.freeValue(ctx, result)
			}
			v.freeValue(ctx, s[0])
			v.freeValue(ctx, s[1])
		case "host_call":
			b, ok := m.Memory().Read(uint32(s[0]), uint32(s[1]))
			check(ok && string(b) == callbackName, "unbound callback")
			check(uint32(s[3]) == 3, "callback arity")
			args := make([]string, 3)
			for i := range args {
				p, ok := m.Memory().ReadUint32Le(uint32(s[4]) + uint32(i*4))
				check(ok, "callback argv read failed")
				check(v.call(ctx, "qjs_is_string", uint64(p)) != 0, "callback arguments must be strings")
				max := f.options.Limits.MaxRequestBytes
				if i == 0 {
					max = 1600
				}
				if i == 1 {
					max = 256
				}
				args[i] = v.text(ctx, uint64(p), max)
			}
			code := v.submit(args[0], args[1], []byte(args[2]))
			// C owns this newly allocated result box; argv/this are only borrowed.
			h := v.newString(ctx, code)
			v.boxes--
			s[0] = h
		}
		return
	}
	switch name {
	case "clock_time_get":
		if uint32(s[0]) > 1 {
			s[0] = 52
			return
		}
		check(m.Memory().WriteUint64Le(uint32(s[2]), uint64(time.Now().UnixNano())), "clock write failed")
		s[0] = 0
	case "random_get":
		b, ok := m.Memory().Read(uint32(s[0]), uint32(s[1]))
		check(ok, "random write failed")
		_, err := rand.Read(b)
		must(err)
		s[0] = 0
	case "fd_close", "fd_seek", "fd_fdstat_get", "fd_write":
		s[0] = 8
	default:
		panic(failure{errors.New("bridge: unsupported WASI call")})
	}
}
func (v *runtime) submit(id, tool string, args []byte) string {
	if v.cellID == "" || !strings.HasPrefix(id, v.prefix) {
		return "E_ID"
	}
	suffix := strings.TrimPrefix(id, v.prefix)
	n, err := strconv.ParseUint(suffix, 10, 53)
	if err != nil || n == 0 || n <= v.ordinal || strconv.FormatUint(n, 10) != suffix {
		return "E_ID"
	}
	v.ordinal = n
	if _, err := v.encodeOutcome(engine.CancellationOutcome(id)); err != nil {
		return "E_LIMIT"
	}
	if !v.factory.allowed[tool] {
		return "E_CAPABILITY"
	}
	if _, exists := v.pending[id]; exists {
		return "E_ID"
	}
	if err = validArgs(args, v.factory.options.Limits.MaxRequestBytes); err != nil {
		return "E_JSON"
	}
	l := v.factory.options.Limits
	if len(v.queue) >= l.MaxQueuedRequests || len(v.pending) >= l.MaxQueuedRequests {
		return "E_QUEUE"
	}
	request := engine.Request{ID: id, Tool: tool, Args: append(json.RawMessage(nil), args...)}
	payload, err := json.Marshal(request)
	if err != nil || len(payload) > l.MaxRequestBytes {
		return "E_LIMIT"
	}
	v.queue = append(v.queue, request)
	v.pending[id] = cloneRequest(request)
	return ""
}
