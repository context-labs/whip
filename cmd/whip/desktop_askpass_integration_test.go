//go:build integration && (darwin || linux)

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDesktopAskpassPrivateSocket(t *testing.T) {
	for _, tt := range []struct {
		name    string
		reply   string
		confirm bool
		want    string
	}{
		{name: "password", reply: `{"answer":"private password ✓"}` + "\n", want: "private password ✓\n"},
		{name: "empty password", reply: `{"answer":""}` + "\n", want: "\n"},
		{name: "confirmation", reply: `{"answer":"yes"}` + "\n", confirm: true, want: "yes\n"},
		{name: "cancelled", reply: `{"cancel":true}` + "\n"},
		{name: "cancel overrides answer", reply: `{"cancel":true,"answer":"private password"}` + "\n"},
		{name: "missing answer", reply: `{}` + "\n"},
		{name: "unknown field", reply: `{"answer":"private password","secret":true}` + "\n"},
		{name: "two objects", reply: `{"answer":"private password"}{}` + "\n"},
		{name: "malformed JSON", reply: `{"answer": private password}` + "\n"},
		{name: "missing newline", reply: `{"answer":"private password"}`},
		{name: "answer newline", reply: `{"answer":"private\npassword"}` + "\n"},
		{name: "answer carriage return", reply: `{"answer":"private\rpassword"}` + "\n"},
		{name: "answer tab", reply: `{"answer":"private\tpassword"}` + "\n"},
		{name: "answer NUL", reply: `{"answer":"private\u0000password"}` + "\n"},
		{name: "answer DEL", reply: `{"answer":"private\u007fpassword"}` + "\n"},
		{name: "answer line separator", reply: `{"answer":"private\u2028password"}` + "\n"},
		{name: "answer too large", reply: `{"answer":"` + strings.Repeat("p", desktopAnswerLimit+1) + `"}` + "\n"},
		{name: "frame too large", reply: strings.Repeat(" ", 32*1024) + `{"answer":"private password"}` + "\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			listener := desktopPromptFixture(t)
			if tt.confirm {
				t.Setenv("SSH_ASKPASS_PROMPT", "confirm")
			}
			result := make(chan error, 1)
			go func() {
				connection, err := listener.Accept()
				if err != nil {
					result <- err
					return
				}
				defer connection.Close()
				_ = connection.SetDeadline(time.Now().Add(3 * time.Second))
				line, err := bufio.NewReader(connection).ReadBytes('\n')
				if err != nil {
					result <- err
					return
				}
				var request struct {
					Token   string `json:"token"`
					Prompt  string `json:"prompt"`
					Confirm bool   `json:"confirm"`
				}
				if err := json.Unmarshal(line, &request); err != nil {
					result <- err
					return
				}
				valid := request.Token == strings.Repeat("t", 64) && request.Prompt == "Host key\nContinue?"
				if !valid || request.Confirm != tt.confirm {
					result <- errors.New("prompt metadata changed")
					return
				}
				_, _ = io.WriteString(connection, tt.reply)
				result <- nil
			}()
			var output bytes.Buffer
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			err := desktopAskpass(ctx, []string{"Host key\nContinue?"}, &output)
			if (err == nil) != (tt.want != "") {
				t.Fatalf("prompt success = %v, want %v", err == nil, tt.want != "")
			}
			if output.String() != tt.want {
				t.Fatal("prompt did not return exactly the expected answer, or emitted output on failure")
			}
			if err := <-result; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDesktopAskpassCancellationClosesSocket(t *testing.T) {
	listener := desktopPromptFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverDone := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer connection.Close()
		_ = connection.SetDeadline(time.Now().Add(3 * time.Second))
		reader := bufio.NewReader(connection)
		if _, err := reader.ReadBytes('\n'); err != nil {
			serverDone <- err
			return
		}
		cancel()
		_, err = reader.ReadByte()
		serverDone <- err
	}()
	var output bytes.Buffer
	started := time.Now()
	if err := desktopAskpass(ctx, []string{"Password:"}, &output); err == nil {
		t.Fatal("cancelled prompt succeeded")
	}
	if output.Len() != 0 || time.Since(started) > 2*time.Second {
		t.Fatal("cancelled prompt leaked output or did not stop promptly")
	}
	if err := <-serverDone; !errors.Is(err, io.EOF) {
		t.Fatalf("private socket was not closed: %v", err)
	}
}

func TestDesktopPromptSocketRequiresPrivateDirectory(t *testing.T) {
	listener := desktopPromptFixture(t)
	path := listener.Addr().String()
	if err := desktopPromptSocket(path); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Dir(path)
	if err := os.Chmod(directory, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := desktopPromptSocket(path); err == nil {
		t.Fatal("group-readable prompt directory accepted")
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := desktopPromptSocket(link); err == nil {
		t.Fatal("symlink to socket accepted")
	}
}

func desktopPromptFixture(t *testing.T) *net.UnixListener {
	t.Helper()
	// macOS sun_path is 104 bytes; testing.TempDir's descriptive path can exceed it.
	directory, err := os.MkdirTemp("/tmp", "whip-prompt-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	path := filepath.Join(directory, "p")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	if err := listener.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WHIP_DESKTOP_PROMPT_SOCKET", path)
	t.Setenv("WHIP_DESKTOP_PROMPT_TOKEN", strings.Repeat("t", 64))
	t.Setenv("SSH_ASKPASS_PROMPT", "")
	return listener
}
