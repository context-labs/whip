//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDesktopAskpassPrivateSocketRepliesAndCancellation(t *testing.T) {
	for _, kind := range []string{"answer", "cli-answer", "cancel", "missing", "unknown-field", "extra-json", "control", "large", "truncated", "cancel-context"} {
		t.Run(kind, func(t *testing.T) {
			directory, err := os.MkdirTemp("/tmp", "whip-prompt-") //nolint:usetesting // Unix sockets require a short absolute path on macOS.
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = os.RemoveAll(directory) }()
			socket := filepath.Join(directory, "prompt.sock")
			listener, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			_ = listener.(*net.UnixListener).SetDeadline(time.Now().Add(2 * time.Second))
			if err := os.Chmod(socket, 0o600); err != nil {
				t.Fatal(err)
			}
			token := strings.Repeat("x", 32)
			t.Setenv("WHIP_DESKTOP_PROMPT_TOKEN", token)
			t.Setenv("WHIP_DESKTOP_PROMPT_SOCKET", socket)
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				connection, err := listener.Accept()
				if err != nil {
					done <- err
					return
				}
				defer connection.Close()
				_ = connection.SetDeadline(time.Now().Add(2 * time.Second))
				var request struct {
					Token  string `json:"token"`
					Prompt string `json:"prompt"`
				}
				if err := json.NewDecoder(connection).Decode(&request); err != nil {
					done <- err
					return
				}
				if request.Token != token || request.Prompt != "Fixture password: " {
					done <- io.ErrUnexpectedEOF
					return
				}
				reply := `{"answer":"fixture-secret"}` + "\n"
				switch kind {
				case "cancel":
					reply = "{\"cancel\":true}\n"
				case "missing":
					reply = "{}\n"
				case "unknown-field":
					reply = "{\"answer\":\"unsafe\",\"extra\":true}\n"
				case "extra-json":
					reply = "{\"answer\":\"unsafe\"} {}\n"
				case "control", "large":
					answer := "unsafe\nanswer"
					if kind == "large" {
						answer = strings.Repeat("x", desktopAnswerLimit+1)
					}
					encoded, _ := json.Marshal(map[string]string{"answer": answer})
					reply = string(encoded) + "\n"
				case "truncated":
					reply = "{"
				case "cancel-context":
					cancel()
					done <- nil
					return
				}
				_, err = io.WriteString(connection, reply)
				done <- err
			}()
			var output bytes.Buffer
			if kind == "cli-answer" {
				file, fileErr := os.CreateTemp(directory, "stdout")
				if fileErr != nil {
					t.Fatal(fileErr)
				}
				defer file.Close()
				previous := os.Stdout
				os.Stdout = file
				defer func() { os.Stdout = previous }()
				if code := desktopAskpassCLI([]string{"Fixture password: "}); code != 0 {
					t.Fatalf("prompt exit code: %d", code)
				}
				if code := desktopAskpassCLI(nil); code != 1 {
					t.Fatalf("invalid prompt exit code: %d", code)
				}
				data, readErr := os.ReadFile(file.Name())
				if readErr != nil {
					t.Fatal(readErr)
				}
				output.Write(data)
			} else {
				err = desktopAskpass(ctx, []string{"Fixture password: "}, &output)
			}
			if kind == "answer" || kind == "cli-answer" {
				if err != nil || output.String() != "fixture-secret\n" {
					t.Fatal("valid private prompt reply was not returned")
				}
			} else if err == nil || output.Len() != 0 {
				t.Fatal("failed/cancelled prompt exposed an answer")
			}
			if serverErr := <-done; serverErr != nil {
				t.Fatal(serverErr)
			}
		})
	}
}

func TestDesktopAskpassRejectsMalformedInputWithoutOutput(t *testing.T) {
	for _, tt := range []struct {
		name  string
		args  []string
		token string
	}{
		{name: "no prompt", args: []string{}, token: strings.Repeat("x", 32)},
		{name: "extra prompt", args: []string{"one", "two"}, token: strings.Repeat("x", 32)},
		{name: "large prompt", args: []string{strings.Repeat("x", desktopPromptLimit+1)}, token: strings.Repeat("x", 32)},
		{name: "NUL prompt", args: []string{"private\x00prompt"}, token: strings.Repeat("x", 32)},
		{name: "invalid Unicode", args: []string{"private\xffprompt"}, token: strings.Repeat("x", 32)},
		{name: "missing token", args: []string{"private prompt"}},
		{name: "short token", args: []string{"private prompt"}, token: "short"},
		{name: "control token", args: []string{"private prompt"}, token: strings.Repeat("x", 32) + "\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("WHIP_DESKTOP_PROMPT_TOKEN", tt.token)
			var output bytes.Buffer
			if err := desktopAskpass(context.Background(), tt.args, &output); err == nil {
				t.Fatal("malformed prompt accepted")
			}
			if output.Len() != 0 {
				t.Fatal("failed prompt wrote output")
			}
		})
	}
}

func TestDesktopPromptSocketRejectsUnsafePaths(t *testing.T) {
	for _, path := range []string{"", "relative.sock", "/tmp/../prompt.sock", "/tmp/a\n.sock", "/" + strings.Repeat("a", 103)} {
		t.Run(path, func(t *testing.T) {
			if err := desktopPromptSocket(path); err == nil {
				t.Fatal("unsafe socket path accepted")
			}
		})
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "socket")
	if err := os.WriteFile(path, []byte("not a socket"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := desktopPromptSocket(path); err == nil {
		t.Fatal("ordinary file accepted as socket")
	}
	link := filepath.Join(directory, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := desktopPromptSocket(link); err == nil {
		t.Fatal("symlink accepted as socket")
	}
}

func TestDesktopPromptConfirmationKeepsAuthenticationSecret(t *testing.T) {
	authenticity := "The authenticity of host '[127.0.0.1]:2299 ([127.0.0.1]:2299)' can't be established.\n" +
		"ED25519 key fingerprint is: SHA256:fixture.\n" +
		"This key is not known by any other names.\n" +
		"Are you sure you want to continue connecting (yes/no/[fingerprint])? "
	for _, tt := range []struct {
		name   string
		prompt string
		hint   string
		want   bool
	}{
		{name: "OpenSSH unknown host without hint", prompt: authenticity, want: true},
		{name: "macOS fingerprint punctuation", prompt: strings.Replace(authenticity, "is: SHA256:", "is SHA256:", 1), want: true},
		{name: "different known key type", prompt: strings.Replace(authenticity, "established.\n", "established\nbut keys of different type are already known for this host.\n", 1), want: true},
		{name: "OpenSSH retry", prompt: "Please type 'yes', 'no' or the fingerprint: ", want: true},
		{name: "OpenSSH yes no retry", prompt: "Please type 'yes' or 'no': ", want: true},
		{name: "explicit permission hint", prompt: "Allow access?", hint: "confirm", want: true},
		{name: "notification hint", prompt: authenticity, hint: "none"},
		{name: "password", prompt: "fixture@host's password: "},
		{name: "encrypted private key", prompt: "Enter passphrase for key '/tmp/generated-key': "},
		{name: "keyboard interactive", prompt: "Enter verification code: "},
		{name: "generic yes no challenge", prompt: "Are you sure you want to continue connecting (yes/no/[fingerprint])? "},
		{name: "missing fingerprint", prompt: strings.Replace(authenticity, "ED25519 key fingerprint is: SHA256:fixture.\n", "", 1)},
		{name: "prefixed server challenge", prompt: "Server challenge: " + authenticity},
		{name: "appended password request", prompt: authenticity + "Password: "},
		{name: "changed host key warning", prompt: "WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := desktopPromptConfirmation(tt.prompt, tt.hint); got != tt.want {
				t.Fatalf("confirmation presentation = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDesktopPromptSocketRequiresPrivateExistingDirectory(t *testing.T) {
	directory, err := os.MkdirTemp("/tmp", "whip-private-") //nolint:usetesting // Unix sockets require a short absolute path on macOS.
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(directory)
	socket := filepath.Join(directory, "prompt.sock")
	if err := desktopPromptSocket(socket); err == nil {
		t.Fatal("missing socket accepted")
	}
	if err := desktopPromptSocket(filepath.Join(directory, "missing", "prompt.sock")); err == nil {
		t.Fatal("missing parent accepted")
	}
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := os.Chmod(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := desktopPromptSocket(socket); err == nil {
		t.Fatal("shared prompt directory accepted")
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := desktopPromptSocket(socket); err != nil {
		t.Fatal(err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	// Keep a stale socket inode, as after a crashed parent; dialing must fail
	// without emitting any prompt, token or answer.
	listener, err = (&net.ListenConfig{}).Listen(t.Context(), "unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	listener.(*net.UnixListener).SetUnlinkOnClose(false)
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WHIP_DESKTOP_PROMPT_SOCKET", socket)
	t.Setenv("WHIP_DESKTOP_PROMPT_TOKEN", strings.Repeat("x", 32))
	var output bytes.Buffer
	if err := desktopAskpass(t.Context(), []string{"private prompt"}, &output); err == nil || output.Len() != 0 {
		t.Fatal("stale prompt socket did not fail privately")
	}
}
