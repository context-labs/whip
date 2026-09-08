package daemon

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

// messageTransport owns framing; application code sees one JSON envelope per read.
type messageTransport interface {
	ReadMessage() ([]byte, error)
	WriteMessage([]byte) error
	SetReadDeadline(time.Time) error
	SetWriteDeadline(time.Time) error
	Close() error
}

type unixMessageTransport struct {
	net.Conn
	reader  *bufio.Reader
	writeMu sync.Mutex
}

func newUnixMessageTransport(conn net.Conn) *unixMessageTransport {
	return &unixMessageTransport{Conn: conn, reader: bufio.NewReaderSize(conn, MaxFrameSize)}
}

func (t *unixMessageTransport) ReadMessage() ([]byte, error) { return readProtocolFrame(t.reader) }

func (t *unixMessageTransport) WriteMessage(message []byte) error {
	message = bytes.TrimSuffix(message, []byte{'\n'})
	if len(message)+1 > MaxFrameSize {
		return ErrFrameTooLarge
	}
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	_, err := io.Copy(t.Conn, io.MultiReader(bytes.NewReader(message), bytes.NewReader([]byte{'\n'})))
	return err
}

type websocketMessageTransport struct {
	clientSide bool
	net.Conn
	reader  *wsutil.Reader
	writeMu sync.Mutex
}

func newWebsocketMessageTransport(conn net.Conn, source io.Reader) *websocketMessageTransport {
	t := &websocketMessageTransport{Conn: conn}
	t.reader = &wsutil.Reader{
		Source: source, State: ws.StateServerSide, CheckUTF8: true,
		MaxFrameSize:   MaxFrameSize,
		OnIntermediate: t.control,
	}
	return t
}

func (t *websocketMessageTransport) ReadMessage() ([]byte, error) {
	for {
		header, err := t.reader.NextFrame()
		if err != nil {
			return nil, err
		}
		if header.OpCode.IsControl() {
			if err := t.control(header, t.reader); err != nil {
				return nil, err
			}
			continue
		}
		if header.OpCode != ws.OpText {
			return nil, errors.New("protocol requires WebSocket text messages")
		}
		// The frame limit alone does not bound a fragmented message.
		message, err := io.ReadAll(io.LimitReader(t.reader, MaxFrameSize+1))
		if err != nil {
			return nil, err
		}
		if len(message) > MaxFrameSize {
			return nil, ErrFrameTooLarge
		}
		return message, nil
	}
}

func (t *websocketMessageTransport) WriteMessage(message []byte) error {
	message = bytes.TrimSuffix(message, []byte{'\n'})
	if len(message) > MaxFrameSize {
		return ErrFrameTooLarge
	}
	return t.writeFrame(ws.OpText, message)
}

func (t *websocketMessageTransport) writeFrame(op ws.OpCode, data []byte) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	if err := t.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	if t.clientSide {
		return wsutil.WriteClientMessage(t.Conn, op, data)
	}
	return wsutil.WriteServerMessage(t.Conn, op, data)
}

func (t *websocketMessageTransport) control(header ws.Header, reader io.Reader) error {
	data, err := io.ReadAll(io.LimitReader(reader, 126))
	if err != nil {
		return err
	}
	if len(data) > 125 {
		return errors.New("oversized WebSocket control frame")
	}
	switch header.OpCode {
	case ws.OpPing:
		return t.writeFrame(ws.OpPong, data)
	case ws.OpPong:
		return nil
	case ws.OpClose:
		if len(data) == 1 {
			return errors.New("invalid WebSocket close payload")
		}
		if len(data) >= 2 {
			code, reason := ws.ParseCloseFrameData(data)
			if err := ws.CheckCloseFrameData(code, reason); err != nil {
				return err
			}
		}
		if err := t.writeFrame(ws.OpClose, data); err != nil {
			return err
		}
		return net.ErrClosed
	default:
		return errors.New("unsupported WebSocket control frame")
	}
}
