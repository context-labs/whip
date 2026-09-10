package rlm

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const protocolVersion = 2

var ErrFrameLimit = errors.New("RLM protocol frame exceeds limit")

type frame struct {
	Version       int            `json:"version"`
	Termination   string         `json:"termination,omitempty"`
	ComputeNanos  uint64         `json:"compute_nanos,omitempty"`
	HostWaitNanos uint64         `json:"host_wait_nanos,omitempty"`
	CellID        uint64         `json:"cell_id,omitempty"`
	Engine        string         `json:"engine,omitempty"`
	Build         string         `json:"build,omitempty"`
	ABI           string         `json:"abi,omitempty"`
	Profile       string         `json:"profile,omitempty"`
	HasValue      bool           `json:"has_value,omitempty"`
	Jobs          uint64         `json:"jobs,omitempty"`
	Bytes         int            `json:"bytes,omitempty"`
	SHA256        string         `json:"sha256,omitempty"`
	Data          []byte         `json:"data,omitempty"`
	Offset        int            `json:"offset,omitempty"`
	Type          string         `json:"type"`
	ID            uint64         `json:"id,omitempty"`
	Code          string         `json:"code,omitempty"`
	Module        string         `json:"module,omitempty"`
	Operation     string         `json:"operation,omitempty"`
	Arguments     map[string]any `json:"arguments,omitempty"`
	Value         any            `json:"value,omitempty"`
	Output        string         `json:"output,omitempty"`
	Error         string         `json:"error,omitempty"`
	Steps         uint64         `json:"steps,omitempty"`
}

func writeFrame(w io.Writer, limit int, value frame) error {
	value.Version = protocolVersion
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(data)+1 > limit {
		return ErrFrameLimit
	}
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}

func readFrame(r *bufio.Reader, limit int, exactValue ...bool) (frame, error) {
	var data []byte
	for {
		fragment, err := r.ReadSlice('\n')
		data = append(data, fragment...)
		if len(data) > limit {
			return frame{}, ErrFrameLimit
		}
		if err == nil {
			break
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return frame{}, err
		}
	}
	data = bytes.TrimSuffix(data, []byte{'\n'})

	// Inspect routing metadata without first decoding numbers into float64.
	var route struct {
		Module    string `json:"module"`
		Operation string `json:"operation"`
	}
	if err := json.Unmarshal(data, &route); err != nil {
		return frame{}, fmt.Errorf("decode RLM route: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if route.Module == "state" || route.Module == "messages" || route.Module == "mcp" && route.Operation == "call" || len(exactValue) > 0 && exactValue[0] {
		decoder.UseNumber()
	}
	var value frame
	if err := decoder.Decode(&value); err != nil {
		return frame{}, fmt.Errorf("decode RLM frame: %w", err)
	}
	if value.Version != protocolVersion {
		return frame{}, fmt.Errorf("unsupported RLM protocol version %d", value.Version)
	}
	return value, nil
}
