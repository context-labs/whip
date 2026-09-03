package rlm

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"math"
	"runtime/debug"
	"strings"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/syntax"
)

const (
	defaultSteps        = 1_000_000
	defaultHostRequests = 1_024
	defaultMemoryBytes  = 256 << 20
	defaultOutputBytes  = 64 << 10
	defaultFrameBytes   = 1 << 20
)

// WorkerMain runs the authority-free side of the RLM protocol. It is public
// so the hidden whip entrypoint and subprocess tests use the identical path.
func WorkerMain(args []string, input io.Reader, output io.Writer) error {
	if raceEnabled {
		return workerMain(args, input, output, applySoftMemoryLimit)
	}
	return workerMain(args, input, output, applyMemoryLimit)
}

func applySoftMemoryLimit(bytes uint64) error {
	if bytes > math.MaxInt64 {
		return errors.New("RLM memory limit is too large")
	}
	debug.SetMemoryLimit(int64(bytes))
	return nil
}

func workerMain(args []string, input io.Reader, output io.Writer, limitMemory func(uint64) error) error {
	fs := flag.NewFlagSet("_kernel", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	steps := fs.Uint64("steps", defaultSteps, "maximum Starlark steps per cell")
	hostRequests := fs.Int("host-requests", defaultHostRequests, "maximum host requests per cell")
	memoryBytes := fs.Uint64("memory-bytes", defaultMemoryBytes, "worker address-space limit")
	outputBytes := fs.Int("output-bytes", defaultOutputBytes, "maximum captured cell output")
	frameBytes := fs.Int("frame-bytes", defaultFrameBytes, "maximum protocol frame")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || *steps == 0 || *hostRequests < 1 || *memoryBytes == 0 || *memoryBytes > math.MaxInt64/4 || *outputBytes < 1 || *frameBytes < 1 {
		return errors.New("invalid RLM worker limits")
	}
	if err := limitMemory(*memoryBytes); err != nil {
		return fmt.Errorf("apply RLM memory limit: %w", err)
	}
	worker := worker{
		input: bufio.NewReaderSize(input, min(*frameBytes, 64<<10)), output: output,
		steps: *steps, hostRequests: *hostRequests, outputBytes: *outputBytes, frameBytes: *frameBytes,
		globals: make(starlark.StringDict), modules: make(starlark.StringDict),
	}
	worker.installModules()
	return worker.run()
}

type worker struct {
	input        *bufio.Reader
	output       io.Writer
	steps        uint64
	hostRequests int
	outputBytes  int
	frameBytes   int
	globals      starlark.StringDict
	modules      starlark.StringDict
	requests     int
	nextRequest  uint64
	cellOutput   strings.Builder
	sources      map[string]scratchSource // top-level defs and lambdas, for snapshots
	nextSource   int
	restoring    bool
}

// cellFileOptions is the Starlark dialect for cells and scratch programs.
var cellFileOptions = &syntax.FileOptions{While: true, TopLevelControl: true, GlobalReassign: true, Recursion: true}

func (w *worker) installModules() {
	for module, operations := range moduleRegistry {
		members := make(starlark.StringDict, len(operations))
		for _, operation := range operations {
			members[operation] = starlark.NewBuiltin(module+"."+operation, func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
				if len(args) != 0 {
					return nil, errors.New("RLM module operations accept keyword arguments only")
				}
				arguments := make(map[string]any, len(kwargs))
				for _, item := range kwargs {
					key, ok := item[0].(starlark.String)
					if !ok {
						return nil, errors.New("RLM keyword name is not a string")
					}
					value, err := starlarkToGo(item[1])
					if err != nil {
						return nil, fmt.Errorf("%s: %w", key, err)
					}
					arguments[string(key)] = value
				}
				return w.hostCall(module, operation, arguments)
			})
		}
		value := starlarkstruct.FromStringDict(starlark.String(module), members)
		w.modules[module] = value
		w.globals[module] = value
	}
}

func (w *worker) run() error {
	for {
		request, err := readFrame(w.input, w.frameBytes)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if request.ID == 0 {
			return fmt.Errorf("unexpected RLM frame %q", request.Type)
		}
		var result frame
		switch request.Type {
		case "eval":
			result = w.evaluate(request.Code)
		case "snapshot":
			result = w.snapshot()
		case "restore":
			result = w.restore(request.Code)
		default:
			return fmt.Errorf("unexpected RLM frame %q", request.Type)
		}
		result.Type = "result"
		result.ID = request.ID
		if err := writeFrame(w.output, w.frameBytes, result); err != nil {
			return err
		}
	}
}

func (w *worker) evaluate(code string) frame {
	maps.Copy(w.globals, w.modules)
	w.requests = 0
	w.cellOutput.Reset()
	thread := &starlark.Thread{Name: "rlm-cell"}
	thread.SetMaxExecutionSteps(w.steps)
	thread.Print = func(thread *starlark.Thread, message string) {
		if w.cellOutput.Len()+len(message)+1 > w.outputBytes {
			thread.Cancel("cell output limit exceeded")
			return
		}
		w.cellOutput.WriteString(message)
		w.cellOutput.WriteByte('\n')
	}

	file, err := cellFileOptions.Parse("<rlm-cell>", code, 0)
	if err != nil {
		return frame{Output: w.cellOutput.String(), Error: err.Error()}
	}
	statements := file.Stmts
	var value starlark.Value = starlark.None
	if count := len(file.Stmts); count > 0 {
		if expression, ok := file.Stmts[count-1].(*syntax.ExprStmt); ok {
			file.Stmts = file.Stmts[:count-1]
			if len(file.Stmts) > 0 {
				err = starlark.ExecREPLChunk(file, thread, w.globals)
			}
			if err == nil {
				value, err = starlark.EvalExprOptions(file.Options, thread, expression.X, w.globals)
			}
		} else {
			err = starlark.ExecREPLChunk(file, thread, w.globals)
		}
	}
	result := frame{Output: w.cellOutput.String(), Steps: thread.ExecutionSteps()}
	if err != nil {
		result.Error = err.Error()
		return result
	}
	w.recordSources(code, statements)
	result.Value, err = starlarkToGo(value)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	encoded, err := json.Marshal(result.Value)
	if err != nil {
		result.Error = err.Error()
	} else if len(encoded)+len(result.Output) > w.outputBytes {
		result.Value = nil
		result.Error = "cell output limit exceeded"
	}
	return result
}

func (w *worker) hostCall(module, operation string, arguments map[string]any) (starlark.Value, error) {
	if w.restoring {
		return nil, errors.New("host calls are unavailable while scratch is restored")
	}
	if err := validateModuleOperation(module, operation); err != nil {
		return nil, err
	}
	w.requests++
	if w.requests > w.hostRequests {
		return nil, errors.New("host request limit exceeded")
	}
	w.nextRequest++
	id := w.nextRequest
	if err := writeFrame(w.output, w.frameBytes, frame{Type: "host_request", ID: id, Module: module, Operation: operation, Arguments: arguments}); err != nil {
		return nil, err
	}
	response, err := readFrame(w.input, w.frameBytes)
	if err != nil {
		return nil, err
	}
	if response.Type != "host_response" || response.ID != id {
		return nil, errors.New("mismatched RLM host response")
	}
	if response.Error != "" {
		return nil, errors.New(response.Error)
	}
	return goToStarlark(response.Value)
}

func starlarkToGo(value starlark.Value) (any, error) {
	switch value := value.(type) {
	case starlark.NoneType:
		return nil, nil //nolint:nilnil // nil is the valid Go representation of Starlark None
	case starlark.Bool:
		return bool(value), nil
	case starlark.String:
		return string(value), nil
	case starlark.Bytes:
		return string(value), nil
	case starlark.Int:
		var result int64
		if err := starlark.AsInt(value, &result); err != nil {
			return value.String(), nil
		}
		return result, nil
	case starlark.Float:
		return float64(value), nil
	case *starlark.List:
		result := make([]any, 0, value.Len())
		iterator := value.Iterate()
		defer iterator.Done()
		var item starlark.Value
		for iterator.Next(&item) {
			converted, err := starlarkToGo(item)
			if err != nil {
				return nil, err
			}
			result = append(result, converted)
		}
		return result, nil
	case starlark.Tuple:
		result := make([]any, len(value))
		for index, item := range value {
			converted, err := starlarkToGo(item)
			if err != nil {
				return nil, err
			}
			result[index] = converted
		}
		return result, nil
	case *starlark.Dict:
		result := make(map[string]any, value.Len())
		for _, item := range value.Items() {
			key, ok := item[0].(starlark.String)
			if !ok {
				return nil, errors.New("RLM dictionaries require string keys")
			}
			converted, err := starlarkToGo(item[1])
			if err != nil {
				return nil, err
			}
			result[string(key)] = converted
		}
		return result, nil
	default:
		return nil, fmt.Errorf("unsupported Starlark value %s", value.Type())
	}
}

func goToStarlark(value any) (starlark.Value, error) {
	switch value := value.(type) {
	case nil:
		return starlark.None, nil
	case bool:
		return starlark.Bool(value), nil
	case string:
		return starlark.String(value), nil
	case float64:
		if value == math.Trunc(value) && value >= math.MinInt64 && value <= math.MaxInt64 {
			return starlark.MakeInt64(int64(value)), nil
		}
		return starlark.Float(value), nil
	case []any:
		result := make([]starlark.Value, len(value))
		for index, item := range value {
			converted, err := goToStarlark(item)
			if err != nil {
				return nil, err
			}
			result[index] = converted
		}
		return starlark.NewList(result), nil
	case map[string]any:
		result := starlark.NewDict(len(value))
		for key, item := range value {
			converted, err := goToStarlark(item)
			if err != nil {
				return nil, err
			}
			if err := result.SetKey(starlark.String(key), converted); err != nil {
				return nil, err
			}
		}
		return result, nil
	default:
		data, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		var generic any
		if err := json.Unmarshal(data, &generic); err != nil {
			return nil, err
		}
		return goToStarlark(generic)
	}
}
