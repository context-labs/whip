package rlm

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const protocolVersion = 1

var ErrFrameLimit = errors.New("RLM protocol frame exceeds limit")

type frame struct {
	Version   int            `json:"version"`
	Type      string         `json:"type"`
	ID        uint64         `json:"id,omitempty"`
	Code      string         `json:"code,omitempty"`
	Module    string         `json:"module,omitempty"`
	Operation string         `json:"operation,omitempty"`
	Arguments map[string]any `json:"arguments,omitempty"`
	Value     any            `json:"value,omitempty"`
	Output    string         `json:"output,omitempty"`
	Error     string         `json:"error,omitempty"`
	Steps     uint64         `json:"steps,omitempty"`
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

func readFrame(r *bufio.Reader, limit int) (frame, error) {
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
	var value frame
	if err := json.Unmarshal(data, &value); err != nil {
		return frame{}, fmt.Errorf("decode RLM frame: %w", err)
	}
	if value.Version != protocolVersion {
		return frame{}, fmt.Errorf("unsupported RLM protocol version %d", value.Version)
	}
	return value, nil
}
