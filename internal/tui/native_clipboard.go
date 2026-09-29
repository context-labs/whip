package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/capability"
)

// Clipboard programs are client-owned helpers, never host/session commands.
// One bounded action owns and joins every helper process, including descendants.
func nativeClipboardImage(ctx context.Context, directory string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for _, name := range []string{"wl-paste", "xclip", "xsel", "pngpaste", "powershell.exe"} {
		program, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		var args []string
		switch name {
		case "wl-paste", "xclip":
			args = []string{"--list-types"}
			if name == "xclip" {
				args = []string{"-selection", "clipboard", "-o", "-t", "TARGETS"}
			}
			types, err := nativeClipboardCommand(ctx, directory, program, args, 4<<10)
			if err != nil {
				return nil, err
			}
			media := ""
			for line := range strings.SplitSeq(string(types), "\n") {
				line = strings.TrimSpace(line)
				switch line {
				case "image/png", "image/jpeg", "image/gif", "image/webp", "image/bmp":
					media = line
				}
				if media != "" {
					break
				}
			}
			if media == "" {
				continue
			}
			args = []string{"--type", media}
			if name == "xclip" {
				args = []string{"-selection", "clipboard", "-o", "-t", media}
			}
		case "xsel":
			args = []string{"--clipboard", "--output", "--target", "image/png"}
		case "pngpaste":
			// pngpaste has supported bounded stdout capture since 0.2.1:
			// https://github.com/jcsalterego/pngpaste/blob/main/CHANGELOG.md
			args = []string{"-"}
		case "powershell.exe":
			args = []string{"-NoProfile", "-Command", `Add-Type -AssemblyName System.Windows.Forms; $img = [Windows.Forms.Clipboard]::GetImage(); if ($img -eq $null) { exit 1 }; $img.Save([Console]::OpenStandardOutput(), [System.Drawing.Imaging.ImageFormat]::Png)`}
		}
		return nativeClipboardCommand(ctx, directory, program, args, nativeImageReadLimit)
	}
	return nil, errors.New("no supported clipboard image helper is available; use /attach with a client-local file")
}

type nativeClipboardBuffer struct {
	buffer bytes.Buffer
	limit  int
	stop   context.CancelFunc
	full   bool
}

func (b *nativeClipboardBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.buffer.Len() {
		b.full = true
		b.stop()
		return 0, errors.New("clipboard output exceeds its byte limit")
	}
	return b.buffer.Write(p)
}

func nativeClipboardCommand(ctx context.Context, directory, program string, args []string, limit int) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	manager := capability.NewProcessManager()
	defer func() { _ = manager.Close() }()
	stdout := nativeClipboardBuffer{limit: limit, stop: cancel}
	stderr := nativeClipboardBuffer{limit: 16 << 10, stop: cancel}
	env := map[string]string{}
	for _, name := range []string{"DISPLAY", "WAYLAND_DISPLAY", "XDG_RUNTIME_DIR", "XAUTHORITY"} {
		if value, ok := os.LookupEnv(name); ok {
			env[name] = value
		}
	}
	process, err := manager.Start(ctx, "terminal-clipboard", program, args, capability.ProcessOptions{Cwd: directory, Env: env, Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr})
	if err != nil {
		return nil, err
	}
	err = process.Wait()
	process.Stop()
	if stdout.full || stderr.full {
		return nil, errors.New("clipboard helper exceeded its output limit")
	}
	if err = errors.Join(err, ctx.Err()); err != nil {
		return nil, fmt.Errorf("clipboard helper failed: %w", err)
	}
	return stdout.buffer.Bytes(), nil
}
