package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"sync"
)

// stdioFrames validates each bounded protocol frame before the SDK decoder can
// allocate a JSON value. MCP stdio uses one complete JSON message per line.
type stdioFrames struct {
	io.ReadCloser
	reader  *bufio.Reader
	pending []byte
	charge  frameCharge
}

func boundedStdio(reader io.ReadCloser, budget *Budget) *stdioFrames {
	return &stdioFrames{ReadCloser: reader, reader: bufio.NewReaderSize(reader, 4096), charge: frameCharge{budget: budget}}
}

func (r *stdioFrames) Read(out []byte) (int, error) {
	if len(out) == 0 {
		return 0, nil
	}
	if len(r.pending) == 0 {
		var frame []byte
		for {
			chunk, err := r.reader.ReadSlice('\n')
			if len(chunk) > maxWireBytes-len(frame) || !r.charge.add(len(chunk)) {
				r.charge.release()
				return 0, errors.New("MCP stdio frame exceeds size limit")
			}
			frame = append(frame, chunk...)
			if errors.Is(err, bufio.ErrBufferFull) {
				continue
			}
			if err != nil && err != io.EOF {
				r.charge.release()
				return 0, err
			}
			if len(frame) == 0 {
				return 0, io.EOF
			}
			if !json.Valid(bytes.TrimSpace(frame)) {
				r.charge.release()
				return 0, errors.New("MCP stdio frame is not a complete JSON message")
			}
			if frame[len(frame)-1] != '\n' {
				frame = append(frame, '\n')
			}
			r.pending = frame
			break
		}
	}
	n := copy(out, r.pending)
	r.pending = r.pending[n:]
	if len(r.pending) == 0 {
		r.pending = nil
		r.charge.release()
	}
	return n, nil
}

func (r *stdioFrames) Close() error { err := r.ReadCloser.Close(); r.charge.close(); return err }

// HTTP JSON bodies and individual SSE events have independent byte limits.
// Repeated bounded events do not consume a connection-lifetime byte quota.
type wireBody struct {
	io.ReadCloser
	stream      bool
	bytes, line int
	failed      bool
	charge      frameCharge
}

func boundHTTPBody(response *http.Response, budget *Budget) {
	media, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	response.Body = &wireBody{ReadCloser: response.Body, stream: media == "text/event-stream", charge: frameCharge{budget: budget}}
}

func (b *wireBody) Read(out []byte) (int, error) {
	if b.failed {
		return 0, errors.New("MCP response frame exceeds size limit")
	}
	n, err := b.ReadCloser.Read(out)
	if !b.charge.add(n) {
		b.failed = true
		_ = b.Close()
		return 0, errors.New("MCP aggregate wire byte limit reached")
	}
	lastBoundary := -1
	for index, value := range out[:n] {
		b.bytes++
		if b.bytes > maxWireBytes {
			b.failed = true
			_ = b.Close()
			return index, errors.New("MCP response frame exceeds size limit")
		}
		if b.stream {
			if value == '\n' {
				if b.line == 0 {
					b.bytes = 0
					lastBoundary = index
					b.charge.release()
				}
				b.line = 0
			} else if value != '\r' {
				b.line++
			}
		}
	}
	if lastBoundary >= 0 && !b.charge.add(n-lastBoundary-1) {
		b.failed = true
		_ = b.Close()
		return lastBoundary + 1, errors.New("MCP aggregate wire byte limit reached")
	}
	if err != nil {
		b.charge.release()
	}
	return n, err
}

func (b *wireBody) Close() error { err := b.ReadCloser.Close(); b.charge.close(); return err }

type frameCharge struct {
	mu     sync.Mutex
	budget *Budget
	bytes  int
	closed bool
}

func (c *frameCharge) add(n int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	if c.budget != nil && !c.budget.reserveWire(n) {
		return false
	}
	c.bytes += n
	return true
}

func (c *frameCharge) release() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.budget != nil {
		c.budget.releaseWire(c.bytes)
	}
	c.bytes = 0
}

func (c *frameCharge) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.budget != nil {
		c.budget.releaseWire(c.bytes)
	}
	c.bytes = 0
	c.closed = true
}
