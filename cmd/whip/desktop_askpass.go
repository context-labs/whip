//go:build darwin || linux

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	desktopPromptLimit    = 32 * 1024
	desktopAnswerLimit    = 4 * 1024
	desktopPromptDeadline = 10 * time.Minute
)

func desktopAskpassCLI(args []string) int {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
		syscall.SIGHUP,
		syscall.SIGQUIT,
	)
	defer stop()
	if err := desktopAskpass(ctx, args, os.Stdout); err != nil {
		// OpenSSH treats nonzero as cancellation. Do not print errors, prompts,
		// tokens, or answers: stderr may be retained by the connection UI.
		return 1
	}
	return 0
}

func desktopAskpass(ctx context.Context, args []string, output io.Writer) error {
	if len(args) != 1 {
		return errors.New("invalid SSH prompt")
	}
	prompt := args[0]
	validPrompt := len(prompt) <= desktopPromptLimit && utf8.ValidString(prompt)
	if !validPrompt || strings.IndexByte(prompt, 0) >= 0 {
		return errors.New("invalid SSH prompt")
	}
	token := os.Getenv("WHIP_DESKTOP_PROMPT_TOKEN")
	validToken := len(token) >= 32 && len(token) <= 256 && utf8.ValidString(token)
	if !validToken || strings.IndexFunc(token, unicode.IsControl) >= 0 {
		return errors.New("invalid SSH prompt token")
	}
	path := os.Getenv("WHIP_DESKTOP_PROMPT_SOCKET")
	if err := desktopPromptSocket(path); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, desktopPromptDeadline)
	defer cancel()
	dialer := net.Dialer{Timeout: 5 * time.Second}
	connection, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return errors.New("SSH prompt connection failed")
	}
	defer connection.Close()
	closed := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		_ = connection.Close()
		close(closed)
	})
	defer func() {
		if !stop() {
			<-closed
		}
	}()
	deadline, _ := ctx.Deadline()
	if err := connection.SetDeadline(deadline); err != nil {
		return errors.New("SSH prompt connection failed")
	}
	request := struct {
		Token   string `json:"token"`
		Prompt  string `json:"prompt"`
		Confirm bool   `json:"confirm"`
	}{Token: token, Prompt: prompt, Confirm: desktopPromptConfirmation(prompt, os.Getenv("SSH_ASKPASS_PROMPT"))}
	// JSON escaping can expand the bounded prompt by up to six times. Limit the
	// complete frame too, before sending anything to the private socket.
	encoded, err := json.Marshal(request)
	if err != nil || len(encoded) >= 64*1024 {
		return errors.New("SSH prompt is too large")
	}
	if _, err := connection.Write(append(encoded, '\n')); err != nil {
		return errors.New("SSH prompt connection failed")
	}
	line, err := bufio.NewReaderSize(connection, 32*1024).ReadSlice('\n')
	if err != nil {
		return errors.New("invalid SSH prompt reply")
	}
	var reply struct {
		Answer *string `json:"answer"`
		Cancel bool    `json:"cancel"`
	}
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&reply); err != nil {
		return errors.New("invalid SSH prompt reply")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("invalid SSH prompt reply")
	}
	if reply.Cancel || reply.Answer == nil {
		return errors.New("SSH prompt cancelled")
	}
	answer := *reply.Answer
	if len(answer) > desktopAnswerLimit || strings.IndexFunc(answer, desktopAnswerControl) >= 0 {
		return errors.New("invalid SSH prompt answer")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, answer)
	return err
}

func desktopAnswerControl(r rune) bool {
	return unicode.IsControl(r) || r == '\u2028' || r == '\u2029'
}

// desktopPromptConfirmation is a presentation hint, never proof of a prompt's
// origin or permission to answer it. A keyboard-interactive server can imitate
// client text. The UI must require an explicit answer for every request; only
// OpenSSH verifies keys and writes known_hosts.
//
// OpenSSH 10.2's sshconnect.c confirm() uses RP_ECHO, whereas readpass.c sets
// SSH_ASKPASS_PROMPT=confirm only for RP_ASK_PERMISSION. Recognize the complete
// client authenticity form so an unknown host gets a Trust/Cancel presentation.
// https://github.com/openssh/openssh-portable/blob/V_10_2_P1/sshconnect.c
// https://github.com/openssh/openssh-portable/blob/V_10_2_P1/readpass.c
func desktopPromptConfirmation(prompt, hint string) bool {
	if hint != "" {
		return hint == "confirm"
	}
	switch prompt {
	case "Please type 'yes' or 'no': ", "Please type 'yes', 'no' or the fingerprint: ":
		return true
	}
	header, rest, ok := strings.Cut(prompt, "\n")
	if !ok || !strings.HasPrefix(header, "The authenticity of host '") {
		return false
	}
	headerComplete := strings.HasSuffix(header, "' can't be established.") ||
		strings.HasSuffix(header, "' can't be established")
	if !headerComplete {
		return false
	}
	if !strings.HasSuffix(rest, "\nAre you sure you want to continue connecting (yes/no/[fingerprint])? ") {
		return false
	}
	for line := range strings.SplitSeq(rest, "\n") {
		kind, fingerprint, found := strings.Cut(line, " key fingerprint is")
		if !found {
			continue
		}
		switch kind {
		case "RSA", "ECDSA", "ED25519", "ECDSA-SK", "ED25519-SK":
		default:
			return false
		}
		// Current OpenSSH uses "is: "; macOS also ships versions using "is ".
		fingerprint = strings.TrimPrefix(fingerprint, ":")
		return strings.HasPrefix(fingerprint, " SHA256:") || strings.HasPrefix(fingerprint, " MD5:")
	}
	return false
}

func desktopPromptSocket(path string) error {
	validPath := filepath.IsAbs(path) && filepath.Clean(path) == path && len(path) <= 103
	if !validPath || strings.IndexFunc(path, unicode.IsControl) >= 0 {
		return errors.New("invalid SSH prompt socket")
	}
	directory, err := os.Lstat(filepath.Dir(path))
	if err != nil {
		return errors.New("invalid SSH prompt directory")
	}
	directoryStat, ok := directory.Sys().(*syscall.Stat_t)
	privateDirectory := directory.IsDir() && directory.Mode().Perm()&0o077 == 0
	if !ok || !privateDirectory || directoryStat.Uid != uint32(os.Geteuid()) {
		return errors.New("invalid SSH prompt directory")
	}
	socket, err := os.Lstat(path)
	if err != nil {
		return errors.New("invalid SSH prompt socket")
	}
	socketStat, ok := socket.Sys().(*syscall.Stat_t)
	if !ok || socket.Mode()&os.ModeSocket == 0 || socketStat.Uid != uint32(os.Geteuid()) {
		return errors.New("invalid SSH prompt socket")
	}
	return nil
}
