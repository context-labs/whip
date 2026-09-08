package daemon

import (
	"bytes"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

func TestWebsocketTransportFragmentBounds(t *testing.T) {
	for _, size := range []int{MaxFrameSize, MaxFrameSize + 1} {
		t.Run(string(rune(size-MaxFrameSize+'0')), func(t *testing.T) {
			var wire bytes.Buffer
			first := bytes.Repeat([]byte{'a'}, size/2)
			second := bytes.Repeat([]byte{'b'}, size-len(first))
			for _, frame := range []ws.Frame{
				ws.NewFrame(ws.OpText, false, first), ws.NewFrame(ws.OpContinuation, true, second),
			} {
				if err := ws.WriteFrame(&wire, ws.MaskFrameInPlace(frame)); err != nil {
					t.Fatal(err)
				}
			}
			conn, peer := net.Pipe()
			defer func() { _ = conn.Close(); _ = peer.Close() }()
			transport := newWebsocketMessageTransport(conn, &wire)
			got, err := transport.ReadMessage()
			if size > MaxFrameSize {
				if !errors.Is(err, ErrFrameTooLarge) {
					t.Fatalf("want size error, got %v", err)
				}
			} else if err != nil || len(got) != size {
				t.Fatalf("read %d bytes: %v", len(got), err)
			}
		})
	}
}

func TestWebsocketTransportRejectsInvalidFrames(t *testing.T) {
	for name, frame := range map[string]ws.Frame{
		"unmasked":                   ws.NewTextFrame([]byte("{}")),
		"binary":                     ws.MaskFrameInPlace(ws.NewBinaryFrame([]byte("{}"))),
		"invalid_utf8":               ws.MaskFrameInPlace(ws.NewTextFrame([]byte{0xff})),
		"continuation_without_start": ws.MaskFrameInPlace(ws.NewFrame(ws.OpContinuation, true, []byte("{}"))),
	} {
		t.Run(name, func(t *testing.T) {
			var wire bytes.Buffer
			if err := ws.WriteFrame(&wire, frame); err != nil {
				t.Fatal(err)
			}
			transport := newWebsocketMessageTransport(nil, &wire)
			if _, err := transport.ReadMessage(); err == nil {
				t.Fatal("accepted invalid frame")
			}
		})
	}
}

func TestWebsocketTransportSerializesControlAndData(t *testing.T) {
	conn, peer := net.Pipe()
	defer func() { _ = conn.Close(); _ = peer.Close() }()
	deadline := time.Now().Add(5 * time.Second)
	_ = conn.SetDeadline(deadline)
	_ = peer.SetDeadline(deadline)
	transport := newWebsocketMessageTransport(conn, conn)
	done := make(chan error, 2)
	go func() {
		body, err := transport.ReadMessage()
		if err == nil && string(body) != "hello" {
			err = errors.New("fragmented payload mismatch")
		}
		done <- err
	}()
	go func() {
		for range 32 {
			if err := transport.WriteMessage([]byte(`{"event":"changed"}`)); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	var wg sync.WaitGroup
	wg.Go(func() {
		for _, frame := range []ws.Frame{
			ws.NewFrame(ws.OpText, false, []byte("hel")),
			ws.NewPingFrame([]byte("probe")),
			ws.NewFrame(ws.OpContinuation, true, []byte("lo")),
		} {
			if err := ws.WriteFrame(peer, ws.MaskFrameInPlace(frame)); err != nil {
				return
			}
		}
	})
	reader := wsutil.NewClientSideReader(peer)
	pongs := 0
	for range 33 {
		header, err := reader.NextFrame()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		switch header.OpCode {
		case ws.OpText:
			if string(body) != `{"event":"changed"}` {
				t.Fatalf("corrupted data: %q", body)
			}
		case ws.OpPong:
			pongs++
			if string(body) != "probe" {
				t.Fatalf("corrupted pong: %q", body)
			}
		default:
			t.Fatalf("unexpected opcode %v", header.OpCode)
		}
	}
	if pongs != 1 {
		t.Fatalf("got %d pong frames", pongs)
	}
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
}

func TestWebsocketTransportDoesNotAcceptCloseAsMessageCompletion(t *testing.T) {
	conn, peer := net.Pipe()
	defer func() { _ = conn.Close(); _ = peer.Close() }()
	_ = peer.SetDeadline(time.Now().Add(5 * time.Second))
	var wire bytes.Buffer
	for _, frame := range []ws.Frame{
		ws.NewFrame(ws.OpText, false, []byte(`{"incomplete":`)),
		ws.NewCloseFrame(nil),
	} {
		if err := ws.WriteFrame(&wire, ws.MaskFrameInPlace(frame)); err != nil {
			t.Fatal(err)
		}
	}
	done := make(chan error, 1)
	go func() {
		_, err := newWebsocketMessageTransport(conn, &wire).ReadMessage()
		done <- err
	}()
	reader := wsutil.NewClientSideReader(peer)
	if _, err := reader.NextFrame(); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(reader); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("close accepted an incomplete JSON envelope")
	}
}
