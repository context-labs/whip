// Package gateway relays browser traffic to a pinned private v4 runtime. Exact
// Host/Origin checks are not authentication; remote access requires a trusted
// network or an authenticated reverse proxy.
package gateway

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/context-labs/whip/internal/protocol"
)

const maxFrameBytes = protocol.MaxFrameBytes

var ErrFrameTooLarge = errors.New("gateway frame exceeds 8 MiB")

func ReadFrame(reader *bufio.Reader) ([]byte, error) {
	var frame []byte
	for {
		chunk, err := reader.ReadSlice('\n')
		if len(frame)+len(chunk) > maxFrameBytes {
			return nil, ErrFrameTooLarge
		}
		frame = append(frame, chunk...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) && len(frame) > 0 {
			return nil, io.ErrUnexpectedEOF
		}
		return frame, err
	}
}

// compactEnvelope rejects duplicate keys before canonicalizing envelopes. Relaying raw pretty-printed
// JSON onto a newline-framed socket would permit request smuggling.
func compactEnvelope(frame []byte) ([]byte, map[string]json.RawMessage, error) {
	if len(frame) > maxFrameBytes {
		return nil, nil, ErrFrameTooLarge
	}
	decoder := json.NewDecoder(bytes.NewReader(frame))
	decoder.UseNumber()
	if err := uniqueJSON(decoder, 0); err != nil {
		return nil, nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, nil, errors.New("trailing gateway frame data")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(frame, &fields); err != nil || fields == nil {
		return nil, nil, errors.New("gateway envelope must be an object")
	}
	var version string
	if json.Unmarshal(fields["jsonrpc"], &version) != nil || version != "2.0" {
		return nil, nil, errors.New("invalid JSON-RPC version")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, frame); err != nil {
		return nil, nil, err
	}
	if compact.Len()+1 > maxFrameBytes {
		return nil, nil, ErrFrameTooLarge
	}
	return compact.Bytes(), fields, nil
}

func uniqueJSON(decoder *json.Decoder, depth int) error {
	if depth > 128 {
		return errors.New("gateway JSON nesting exceeds limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	if delimiter != '{' && delimiter != '[' {
		return errors.New("invalid gateway JSON")
	}
	keys := map[string]bool{}
	for decoder.More() {
		if delimiter == '{' {
			token, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := token.(string)
			if !ok || keys[key] {
				return errors.New("duplicate gateway JSON key")
			}
			keys[key] = true
		}
		if err := uniqueJSON(decoder, depth+1); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}
