package rlm

import (
	"bufio"
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestProtocolRejectsOversizedFramesBeforeDecode(t *testing.T) {
	var output bytes.Buffer
	if err := writeFrame(&output, 16, frame{Type: "eval", Code: strings.Repeat("x", 32)}); !errors.Is(err, ErrFrameLimit) {
		t.Fatalf("writeFrame error = %v", err)
	}
	_, err := readFrame(bufio.NewReader(strings.NewReader(strings.Repeat("x", 32)+"\n")), 16)
	if !errors.Is(err, ErrFrameLimit) {
		t.Fatalf("readFrame error = %v", err)
	}
}
