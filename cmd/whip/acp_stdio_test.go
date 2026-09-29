package main

import (
	"bytes"
	"errors"
	"os"
	"testing"
	"time"
)

func TestACPStalledEditorReleasesBothDirections(t *testing.T) {
	input, inputWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	defer inputWriter.Close()
	outputReader, output, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer outputReader.Close()
	defer output.Close()
	stream, err := newProtocolStdio(input, output, 50*time.Millisecond, 10<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	read := make(chan error, 1)
	go func() { _, err := stream.input.Read(make([]byte, 1)); read <- err }()
	_, err = stream.Write(bytes.Repeat([]byte("x"), 2<<20))
	if !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("stalled write=%v", err)
	}
	select {
	case err := <-read:
		if !errors.Is(err, os.ErrClosed) {
			t.Fatalf("reader=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stalled editor left an owned reader blocked")
	}
}
