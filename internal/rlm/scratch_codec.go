package rlm

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"math/big"
	"sort"
	"strconv"

	"go.starlark.net/resolve"
	"go.starlark.net/starlark"
)

// Values refer to a typed graph, so nested mutable aliases retain identity.
// Cycles are deliberately excluded. Traversal limits bound work before encoding.
type scratchSnapshot struct {
	Version  int              `json:"version"`
	Bindings []scratchBinding `json:"bindings"`
	Nodes    []scratchNode    `json:"nodes"`
	Helpers  []scratchHelper  `json:"helpers,omitempty"`
}
type scratchBinding struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}
type scratchNode struct {
	Kind  string `json:"kind"`
	Text  string `json:"text,omitempty"`
	Items []int  `json:"items,omitempty"`
}
type scratchHelper struct {
	Name   string   `json:"name"`
	Source string   `json:"source"`
	Deps   []string `json:"deps,omitempty"`
}
type scratchEncoder struct {
	nodes      []scratchNode
	identities map[starlark.Value]int
	path       map[starlark.Value]bool
	bytes      int
	steps      int
}

func newScratchEncoder() *scratchEncoder {
	return &scratchEncoder{identities: map[starlark.Value]int{}, path: map[starlark.Value]bool{}}
}

func (e *scratchEncoder) encode(value starlark.Value, depth int) (int, error) {
	e.steps++
	if depth > 128 || e.steps > 65536 {
		return 0, errors.New("scratch traversal limit exceeded")
	}
	mutable := false
	switch value.(type) {
	case *starlark.List, *starlark.Dict:
		mutable = true
	}
	if mutable {
		if e.path[value] {
			return 0, errors.New("self-referential value")
		}
		if id, ok := e.identities[value]; ok {
			return id, nil
		}
		e.path[value] = true
		defer delete(e.path, value)
	}
	node := scratchNode{}
	var children []starlark.Value
	switch value := value.(type) {
	case starlark.NoneType:
		node.Kind = "none"
	case starlark.Bool:
		node.Kind = "bool"
		node.Text = strconv.FormatBool(bool(value))
	case starlark.Int:
		node.Kind = "int"
		node.Text = value.String()
	case starlark.Float:
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return 0, errors.New("non-finite float")
		}
		node.Kind = "float"
		node.Text = strconv.FormatFloat(float64(value), 'g', -1, 64)
	case starlark.String:
		if len(value) > snapshotVariableBytes {
			return 0, errors.New("exceeds per-variable cap")
		}
		node.Kind = "string"
		node.Text = base64.StdEncoding.EncodeToString([]byte(value))
	case starlark.Bytes:
		if len(value) > snapshotVariableBytes {
			return 0, errors.New("exceeds per-variable cap")
		}
		node.Kind = "bytes"
		node.Text = base64.StdEncoding.EncodeToString([]byte(value))
	case starlark.Tuple:
		node.Kind = "tuple"
		children = value
	case *starlark.List:
		node.Kind = "list"
		if value.Len() > 65536 {
			return 0, errors.New("scratch traversal limit exceeded")
		}
		for i := range value.Len() {
			children = append(children, value.Index(i))
		}
	case *starlark.Dict:
		node.Kind = "dict"
		if value.Len() > 32768 {
			return 0, errors.New("scratch traversal limit exceeded")
		}
		for _, pair := range value.Items() {
			children = append(children, pair...)
		}
	default:
		return 0, fmt.Errorf("unsupported value type %s", value.Type())
	}
	e.bytes += len(node.Text) + 16
	if e.bytes > snapshotVariableBytes {
		return 0, errors.New("exceeds per-variable cap")
	}
	id := len(e.nodes)
	e.nodes = append(e.nodes, node)
	if mutable {
		e.identities[value] = id
	}
	for _, child := range children {
		ref, err := e.encode(child, depth+1)
		if err != nil {
			return 0, err
		}
		node.Items = append(node.Items, ref)
	}
	e.nodes[id] = node
	return id, nil
}

func sameScratchBinding(a, b starlark.Value) bool { return sameScratchBindingDepth(a, b, 0) }
func sameScratchBindingDepth(a, b starlark.Value, depth int) bool {
	if depth > 128 {
		return false
	}
	if a == nil || b == nil || a.Type() != b.Type() {
		return false
	}
	if f, ok := a.(starlark.Float); ok {
		return math.Float64bits(float64(f)) == math.Float64bits(float64(b.(starlark.Float)))
	}
	switch a := a.(type) {
	case *starlark.Function:
		return a == b
	case *starlark.List:
		return a == b
	case *starlark.Dict:
		return a == b
	case starlark.Tuple:
		other, ok := b.(starlark.Tuple)
		if !ok || len(a) != len(other) {
			return false
		}
		for i := range a {
			if !sameScratchBindingDepth(a[i], other[i], depth+1) {
				return false
			}
		}
		return true
	}
	equal, err := starlark.Equal(a, b)
	return err == nil && equal
}

func propagateHelperFailures(helpers map[string]scratchHelper, bad map[string]string, data func(string) bool) {
	names := make([]string, 0, len(helpers))
	for name := range helpers {
		names = append(names, name)
	}
	sort.Strings(names)
	for changed := true; changed; {
		changed = false
		for _, name := range names {
			helper := helpers[name]
			if bad[name] != "" {
				continue
			}
			for _, dep := range helper.Deps {
				_, fn := helpers[dep]
				if bad[dep] != "" || (!fn && !data(dep)) {
					bad[name] = "unsupported or missing dependency: " + dep
					changed = true
					break
				}
			}
		}
	}
}

func (w *worker) buildSnapshot() (string, SnapshotManifest) {
	snapshot := scratchSnapshot{Version: 1}
	manifest := SnapshotManifest{Saved: []string{}}
	names := make([]string, 0, len(w.globals))
	for name := range w.globals {
		if _, module := w.modules[name]; !module {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	encoder := newScratchEncoder()
	saved := map[string]bool{}
	helpers := map[string]scratchHelper{}
	bad := map[string]string{}
	skip := func(name, reason string) {
		manifest.Skipped = append(manifest.Skipped, SkippedName{Name: name, Reason: reason})
	}
	for _, name := range names {
		value := w.globals[name]
		if fn, ok := value.(*starlark.Function); ok {
			source, known := w.sources[fn]
			switch {
			case fn.NumFreeVars() > 0:
				bad[name] = "closure"
			case !known || source.name != name:
				bad[name] = "function source unavailable"
			case source.reason != "":
				bad[name] = source.reason
			case len(source.text) > snapshotVariableBytes:
				bad[name] = "exceeds per-variable cap"
			default:
				for i := range fn.NumParams() {
					if value := fn.ParamDefault(i); value != nil {
						if _, err := newScratchEncoder().encode(value, 0); err != nil {
							bad[name] = "unsupported default: " + err.Error()
						}
					}
				}
				helpers[name] = scratchHelper{Name: name, Source: source.text, Deps: source.deps}
				globals := fn.Globals()
				for _, dep := range source.deps {
					var captured starlark.Value
					switch source.scopes[dep] {
					case resolve.Global:
						captured = globals[dep]
					case resolve.Predeclared:
						captured = fn.Module().Predeclared()[dep]
					case resolve.Universal:
						captured = starlark.Universe[dep]
					}

					current := w.globals[dep]
					if current == nil {
						current = starlark.Universe[dep]
					}
					if !sameScratchBinding(captured, current) {
						bad[name] = "changed or missing global binding: " + dep
						break
					}
				}
			}
			continue
		}
		// Check each binding independently, including already-shared objects.
		probe := newScratchEncoder()
		_, err := probe.encode(value, 0)
		if err == nil {
			encoded, _ := json.Marshal(probe.nodes)
			if len(encoded) > snapshotVariableBytes {
				err = errors.New("exceeds per-variable cap")
			}
		}
		if err != nil {
			skip(name, err.Error())
			continue
		}
		count := len(encoder.nodes)
		oldIDs := make(map[starlark.Value]int, len(encoder.identities))
		maps.Copy(oldIDs, encoder.identities)
		encoder.bytes = 0
		encoder.steps = 0
		id, err := encoder.encode(value, 0)
		if err != nil {
			skip(name, err.Error())
			encoder.nodes = encoder.nodes[:count]
			encoder.identities = oldIDs
			continue
		}
		snapshot.Nodes = encoder.nodes
		snapshot.Bindings = append(snapshot.Bindings, scratchBinding{Name: name, Value: id})
		encoded, _ := json.Marshal(snapshot)
		if len(encoded) > snapshotTotalBytes {
			snapshot.Bindings = snapshot.Bindings[:len(snapshot.Bindings)-1]
			encoder.nodes = encoder.nodes[:count]
			encoder.identities = oldIDs
			snapshot.Nodes = encoder.nodes
			skip(name, "exceeds aggregate cap")
			continue
		}
		saved[name] = true
		manifest.Saved = append(manifest.Saved, name)
	}
	availableData := func(name string) bool {
		if saved[name] {
			return true
		}
		current, present := w.globals[name]
		if module, ok := w.modules[name]; ok {
			return present && sameScratchBinding(current, module)
		}
		_, builtin := starlark.Universe[name]
		return !present && builtin
	}
	propagateHelperFailures(helpers, bad, availableData)
	for _, name := range names {
		helper, ok := helpers[name]
		if !ok || bad[name] != "" {
			continue
		}
		snapshot.Helpers = append(snapshot.Helpers, helper)
		encoded, _ := json.Marshal(snapshot)
		if len(encoded) > snapshotTotalBytes {
			snapshot.Helpers = snapshot.Helpers[:len(snapshot.Helpers)-1]
			bad[name] = "exceeds aggregate cap"
		}
	}
	propagateHelperFailures(helpers, bad, availableData)
	kept := snapshot.Helpers[:0]
	for _, helper := range snapshot.Helpers {
		if bad[helper.Name] == "" {
			kept = append(kept, helper)
			manifest.Saved = append(manifest.Saved, helper.Name)
		}
	}
	snapshot.Helpers = kept
	for _, name := range names {
		if reason := bad[name]; reason != "" {
			skip(name, reason)
		}
	}
	encoded, _ := json.Marshal(snapshot)
	manifest.Bytes = len(encoded)
	return string(encoded), manifest
}

func decodeScratch(snapshot scratchSnapshot) (starlark.StringDict, error) {
	if len(snapshot.Nodes) > 65536 {
		return nil, errors.New("too many scratch nodes")
	}
	values := make([]starlark.Value, len(snapshot.Nodes))
	visiting := make([]bool, len(values))
	steps := 0
	var decode func(int, int) (starlark.Value, error)
	decode = func(id, depth int) (starlark.Value, error) {
		steps++
		if depth > 128 || steps > 131072 {
			return nil, errors.New("scratch traversal limit exceeded")
		}
		if id < 0 || id >= len(values) {
			return nil, errors.New("invalid scratch reference")
		}
		if visiting[id] {
			return nil, errors.New("cyclic scratch snapshot")
		}
		if values[id] != nil {
			return values[id], nil
		}
		visiting[id] = true
		defer func() { visiting[id] = false }()
		node := snapshot.Nodes[id]
		var value starlark.Value
		var children []starlark.Value
		for _, ref := range node.Items {
			child, err := decode(ref, depth+1)
			if err != nil {
				return nil, err
			}
			children = append(children, child)
		}
		if len(node.Items) > 0 && node.Kind != "list" && node.Kind != "tuple" && node.Kind != "dict" {
			return nil, errors.New("unexpected scratch children")
		}
		switch node.Kind {
		case "none":
			value = starlark.None
		case "bool":
			b, err := strconv.ParseBool(node.Text)
			if err != nil {
				return nil, err
			}
			value = starlark.Bool(b)
		case "int":
			i, ok := new(big.Int).SetString(node.Text, 10)
			if !ok {
				return nil, errors.New("invalid scratch integer")
			}
			value = starlark.MakeBigInt(i)
		case "float":
			f, err := strconv.ParseFloat(node.Text, 64)
			if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
				return nil, errors.New("invalid scratch float")
			}
			value = starlark.Float(f)
		case "string":
			b, err := base64.StdEncoding.DecodeString(node.Text)
			if err != nil {
				return nil, err
			}
			value = starlark.String(b)
		case "bytes":
			b, err := base64.StdEncoding.DecodeString(node.Text)
			if err != nil {
				return nil, err
			}
			value = starlark.Bytes(b)
		case "list":
			value = starlark.NewList(children)
		case "tuple":
			value = starlark.Tuple(children)
		case "dict":
			if len(children)%2 != 0 {
				return nil, errors.New("invalid scratch dictionary")
			}
			dict := starlark.NewDict(len(children) / 2)
			for i := 0; i < len(children); i += 2 {
				if err := dict.SetKey(children[i], children[i+1]); err != nil {
					return nil, err
				}
			}
			if dict.Len() != len(children)/2 {
				return nil, errors.New("duplicate scratch dictionary key")
			}
			value = dict
		default:
			return nil, fmt.Errorf("invalid scratch type %q", node.Kind)
		}
		values[id] = value
		return value, nil
	}
	result := starlark.StringDict{}
	for _, binding := range snapshot.Bindings {
		if !validScratchName(binding.Name) {
			return nil, errors.New("invalid scratch binding name")
		}
		if _, exists := result[binding.Name]; exists {
			return nil, errors.New("duplicate scratch binding")
		}
		value, err := decode(binding.Value, 0)
		if err != nil {
			return nil, err
		}
		result[binding.Name] = value
	}
	for _, value := range values {
		if value == nil {
			return nil, errors.New("unreferenced scratch node")
		}
	}
	return result, nil
}
