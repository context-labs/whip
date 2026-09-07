package tools

import (
	"bytes"
	"fmt"
	"unicode/utf8"
)

// isBinary reports whether data looks like binary rather than text. It is the
// gate that keeps raw NUL/control bytes out of the conversation: when a tool
// reads a binary file (a .png, a .sqlite, an ELF, base64 blobs) or a command
// cats one through bash, the caller substitutes a compact placeholder instead
// of injecting screenfuls of junk into the message stream.
//
// Heuristic (stdlib only):
//   - if the output is not valid UTF-8, it is binary;
//   - any NUL byte is a strong binary signal (NUL is itself valid UTF-8, so
//     this is a separate check);
//   - a high density of other C0 control bytes (>10%, excluding the benign
//     whitespace ones like \t \n \r \f \v and ESC for ANSI-colored output) is
//     treated as binary too.
//
// IsBinary is the exported form for the TUI's `!` shell escape, which injects
// command output into the conversation the same way the bash tool does.
func IsBinary(data []byte) bool { return isBinary(data) }

func isBinary(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	// NUL anywhere in the output is the strongest binary signal — and cheap to
	// check over the whole buffer (bytes.IndexByte is SIMD-friendly), so a
	// text-then-binary stream (cat README.md image.png) is still caught.
	if bytes.IndexByte(data, 0x00) >= 0 {
		return true
	}

	// UTF-8 validity must hold for the whole buffer too: a text-then-binary
	// stream with invalid UTF-8 past the 1KB probe (a log file with corrupt
	// bytes partway through, `head -c 2000 README; cat latin1.log`) leaks
	// mojibake into the conversation if we only validate the prefix. The
	// check is a single pass — same cost class as the NUL scan — and tool
	// outputs are bounded (≤ ~50KB after read/bash paths), so validating the
	// whole buffer is cheap.
	if !utf8.Valid(data) {
		return true
	}

	// The control-byte density check runs over the whole buffer too: a
	// NUL-free control-junk tail (2000 'a' + 4KB of 0x01) passes the NUL/UTF-8
	// checks above, so the prefix-only scope would leak it. The check is a
	// single pass — same cost class as the NUL/UTF-8 passes — and tool outputs
	// are bounded (≤ ~50KB after read/bash paths).
	ctrl := 0
	for _, b := range data {
		switch {
		case b == 0x1b:
			// ESC starts ANSI escape sequences (ls --color, grep --color) —
			// colored text is not binary, so don't count it as a control byte.
		case b < 0x20 && b != '\t' && b != '\n' && b != '\r' && b != 0x0b && b != 0x0c:
			ctrl++
		}
	}
	return ctrl*10 > len(data)
}

// bytesHuman renders a byte count compactly for placeholders, e.g. "88 KB".
func bytesHuman(n int) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%d bytes", n)
	}
}

// binaryPlaceholder is the compact stand-in injected in place of binary tool
// output. name is the offending source (a file path for read, "" for bash);
// size is the size of the output that was suppressed.
//
// BinaryPlaceholder is the exported form for the TUI's `!` shell escape.
func BinaryPlaceholder(name string, size int) string { return binaryPlaceholder(name, size) }

func binaryPlaceholder(name string, size int) string {
	if name == "" {
		return fmt.Sprintf("[binary output: %s, not shown]", bytesHuman(size))
	}
	return fmt.Sprintf("[binary: %s, %s]", name, bytesHuman(size))
}
