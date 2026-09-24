// Package protocoltransport owns bounded WHIP wire framing, not RPC dispatch.
package protocoltransport

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/context-labs/whip/internal/protocol"
)

const MaxFrameSize = 1 << 20

var ErrFrameTooLarge = errors.New("protocol frame exceeds 1 MiB")

type Message struct {
	JSONRPC string             `json:"jsonrpc"`
	ID      json.RawMessage    `json:"id,omitempty"`
	Method  string             `json:"method,omitempty"`
	Params  json.RawMessage    `json:"params,omitempty"`
	Result  any                `json:"result,omitempty"`
	Error   *protocol.RPCError `json:"error,omitempty"`
}

func MarshalFrame(message Message) ([]byte, error) {
	message.JSONRPC = "2.0"
	if message.Error != nil {
		message.Result = nil
	} else if message.Method == "" && len(message.ID) != 0 && message.Result == nil {
		message.Result = json.RawMessage("null")
	}
	data, err := json.Marshal(message)
	if err != nil {
		return nil, err
	}
	if len(data)+1 > MaxFrameSize {
		return nil, ErrFrameTooLarge
	}
	return append(data, '\n'), nil
}

func DecodeFrame(data []byte) (Message, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return Message{}, errors.New("empty protocol frame")
	}
	var message Message
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&message); err != nil {
		return Message{}, err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return Message{}, errors.New("invalid trailing protocol data")
	}
	if message.JSONRPC != "2.0" {
		return Message{}, fmt.Errorf("unsupported jsonrpc version %q", message.JSONRPC)
	}
	return message, nil
}

func ReadFrame(reader *bufio.Reader) ([]byte, error) {
	frame, err := reader.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) || len(frame) > MaxFrameSize {
		return nil, ErrFrameTooLarge
	}
	if errors.Is(err, io.EOF) && len(frame) > 0 {
		return nil, io.ErrUnexpectedEOF
	}
	if err != nil {
		return nil, err
	}
	return frame, nil
}
