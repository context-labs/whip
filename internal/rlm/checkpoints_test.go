package rlm

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestCheckpointTransferBoundsAndIntegrity(t *testing.T) {
	const frameLimit = 512
	image := bytes.Repeat([]byte("checkpoint payload"), 100)
	var wire bytes.Buffer
	if err := writeBlob(&wire, frameLimit, 7, image); err != nil {
		t.Fatal(err)
	}
	for line := range bytes.SplitSeq(bytes.TrimSpace(wire.Bytes()), []byte{'\n'}) {
		if len(line)+1 > frameLimit {
			t.Fatalf("checkpoint chunk exceeds frame limit: %d", len(line)+1)
		}
	}
	reader := bufio.NewReader(bytes.NewReader(wire.Bytes()))
	begin, err := readFrame(reader, frameLimit)
	if err != nil || begin.Type != "checkpoint_begin" || begin.ID != 7 || begin.Bytes != len(image) {
		t.Fatalf("checkpoint declaration: %+v, %v", begin, err)
	}
	received, err := readBlob(reader, frameLimit, begin)
	if err != nil || !bytes.Equal(received, image) {
		t.Fatalf("bounded transfer changed image: bytes=%d, error=%v", len(received), err)
	}
	if _, err := readFrame(reader, frameLimit); !errors.Is(err, io.EOF) {
		t.Fatalf("unexpected trailing frame: %v", err)
	}

	for _, tt := range []struct {
		name   string
		change func(*frame, []frame) []frame
	}{
		{name: "empty declaration", change: func(b *frame, chunks []frame) []frame { b.Bytes = 0; return chunks }},
		{name: "oversized declaration", change: func(b *frame, chunks []frame) []frame { b.Bytes = MaxCheckpointBytes + 1; return chunks }},
		{name: "missing digest", change: func(b *frame, chunks []frame) []frame { b.SHA256 = ""; return chunks }},
		{name: "wrong request", change: func(_ *frame, chunks []frame) []frame { chunks[0].ID++; return chunks }},
		{name: "out of order", change: func(_ *frame, chunks []frame) []frame { chunks[0], chunks[1] = chunks[1], chunks[0]; return chunks }},
		{name: "empty chunk", change: func(_ *frame, chunks []frame) []frame { chunks[0].Data = nil; return chunks }},
		{name: "excess bytes", change: func(b *frame, chunks []frame) []frame { chunks[0].Data = make([]byte, b.Bytes+1); return chunks }},
		{name: "truncated transfer", change: func(_ *frame, chunks []frame) []frame { return chunks[:1] }},
		{name: "corrupted bytes", change: func(_ *frame, chunks []frame) []frame { chunks[0].Data[0] ^= 1; return chunks }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			reader := bufio.NewReader(bytes.NewReader(wire.Bytes()))
			begin, err := readFrame(reader, frameLimit)
			if err != nil {
				t.Fatal(err)
			}
			var chunks []frame
			for {
				chunk, err := readFrame(reader, frameLimit)
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				chunks = append(chunks, chunk)
			}
			chunks = tt.change(&begin, chunks)
			received, err := receiveBlob(begin, func() (frame, error) {
				if len(chunks) == 0 {
					return frame{}, io.EOF
				}
				chunk := chunks[0]
				chunks = chunks[1:]
				return chunk, nil
			})
			if err == nil || received != nil {
				t.Fatalf("invalid transfer published image: bytes=%d, error=%v", len(received), err)
			}
		})
	}
	if err := writeBlob(io.Discard, frameLimit, 7, nil); err == nil {
		t.Fatal("empty image accepted")
	}
	if err := writeBlob(io.Discard, 256, 7, image); !errors.Is(err, ErrFrameLimit) {
		t.Fatalf("insufficient chunk envelope space: %v", err)
	}
}
