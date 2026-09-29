package terminal

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestReadWaitObservesOutputWithoutOwningTheShell(t *testing.T) {
	m := startManager(t, 1, RingBytes)
	options := testOptions(t)
	options.Args = []string{"-c", "stty -echo; printf ready; read line; printf '%s' \"$line\"; read line"}
	term, err := m.Open(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close(term.ID) })
	var cursor int64
	text := ""
	for text != "ready" {
		page, err := term.ReadWait(t.Context(), cursor, ChunkBytes, MaxReadWait)
		if err != nil {
			t.Fatal(err)
		}
		text += string(page.Data)
		cursor = page.Next
		if len(text) > len("ready") || page.Status.Exited {
			t.Fatalf("unexpected startup: %q %+v", text, page.Status)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := term.ReadWait(ctx, cursor, ChunkBytes, MaxReadWait); done <- err }()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read = %v", err)
	}
	if term.Status().Closing || term.Status().Exited {
		t.Fatal("cancelling observation closed the shell")
	}
	// Both independent observers must wake for the same output, without stealing
	// bytes or keeping a PTY attachment alive after the read completes.
	var observers sync.WaitGroup
	for range 2 {
		observers.Go(func() {
			page, err := term.ReadWait(t.Context(), cursor, ChunkBytes, MaxReadWait)
			if err != nil || string(page.Data) != "wake" || page.From != cursor || page.Next != cursor+4 {
				t.Errorf("output read = %+v, %v", page, err)
			}
		})
	}
	write(t, term, "wake\n")
	joined := make(chan struct{})
	go func() { observers.Wait(); close(joined) }()
	select {
	case <-joined:
	case <-time.After(time.Second):
		t.Fatal("output did not wake both waiting readers")
	}
}

func TestReadWaitBoundsAndLifecycle(t *testing.T) {
	for _, action := range []string{"timeout", "exit", "close", "shutdown"} {
		t.Run(action, func(t *testing.T) {
			m := startManager(t, 1, RingBytes)
			options := testOptions(t)
			options.Args = []string{"-c", "stty -echo; printf ready; read line"}
			term, err := m.Open(options)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = m.Close(term.ID) })
			var cursor int64
			for cursor < 5 {
				page, err := term.ReadWait(t.Context(), cursor, ChunkBytes, MaxReadWait)
				if err != nil {
					t.Fatal(err)
				}
				cursor = page.Next
			}
			for _, wait := range []time.Duration{-1, MaxReadWait + 1} {
				if _, err := term.ReadWait(t.Context(), cursor, ChunkBytes, wait); !errors.Is(err, ErrReadWait) {
					t.Fatalf("invalid wait %v = %v", wait, err)
				}
			}
			if action == "timeout" {
				page, err := term.ReadWait(t.Context(), cursor, ChunkBytes, time.Millisecond)
				if err != nil || page.Next != cursor || len(page.Data) != 0 || page.Status.Exited {
					t.Fatalf("timeout = %+v, %v", page, err)
				}
				return
			}
			done := make(chan error, 1)
			go func() {
				page, err := term.ReadWait(t.Context(), cursor, ChunkBytes, MaxReadWait)
				if action == "exit" && err == nil && !page.Status.Exited {
					err = errors.New("exit status was not observed")
				}
				done <- err
			}()
			switch action {
			case "exit":
				write(t, term, "\n")
			case "close":
				if err := m.Close(term.ID); err != nil {
					t.Fatal(err)
				}
			case "shutdown":
				m.Shutdown()
			}
			select {
			case err := <-done:
				if action == "exit" && err != nil || action != "exit" && !errors.Is(err, ErrNotFound) {
					t.Fatalf("%s read = %v", action, err)
				}
			case <-time.After(time.Second):
				t.Fatalf("%s did not wake its reader", action)
			}
		})
	}
}
