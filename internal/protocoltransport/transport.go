package protocoltransport

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

// Transport owns framing; application code sees one JSON envelope per read.
type Transport interface {
	ReadMessage() ([]byte, error)
	WriteMessage([]byte) error
	SetReadDeadline(time.Time) error
	SetWriteDeadline(time.Time) error
	Close() error
}

type Unix struct {
	net.Conn
	reader  *bufio.Reader
	writeMu sync.Mutex
}

func NewUnix(conn net.Conn) *Unix {
	return &Unix{Conn: conn, reader: bufio.NewReaderSize(conn, MaxFrameSize)}
}

func (t *Unix) ReadMessage() ([]byte, error) { return ReadFrame(t.reader) }

func (t *Unix) WriteMessage(message []byte) error {
	message = bytes.TrimSuffix(message, []byte{'\n'})
	if len(message)+1 > MaxFrameSize {
		return ErrFrameTooLarge
	}
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	_, err := io.Copy(t.Conn, io.MultiReader(bytes.NewReader(message), bytes.NewReader([]byte{'\n'})))
	return err
}

type WebSocket struct {
	clientSide bool
	net.Conn
	reader  *wsutil.Reader
	writeMu sync.Mutex
}

func NewWebSocket(conn net.Conn, source io.Reader) *WebSocket {
	t := &WebSocket{Conn: conn}
	t.reader = &wsutil.Reader{
		Source: source, State: ws.StateServerSide, CheckUTF8: true,
		MaxFrameSize:   MaxFrameSize,
		OnIntermediate: t.control,
	}
	return t
}

func (t *WebSocket) ReadMessage() ([]byte, error) {
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

func (t *WebSocket) WriteMessage(message []byte) error {
	message = bytes.TrimSuffix(message, []byte{'\n'})
	if len(message) > MaxFrameSize {
		return ErrFrameTooLarge
	}
	return t.writeFrame(ws.OpText, message)
}

func (t *WebSocket) writeFrame(op ws.OpCode, data []byte) error {
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

func (t *WebSocket) control(header ws.Header, reader io.Reader) error {
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

// NewWebSocketClient frames the client side of a WebSocket connection.
func NewWebSocketClient(conn net.Conn, source io.Reader) *WebSocket {
	t := NewWebSocket(conn, source)
	t.clientSide = true
	t.reader.State = ws.StateClientSide
	return t
}
