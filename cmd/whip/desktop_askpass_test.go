//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
