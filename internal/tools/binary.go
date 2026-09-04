package tools

import (
	"fmt"
	"unicode/utf8"
)

// binaryProbeSize bounds the bytes we inspect when sniffing for binary output.
// A short prefix is enough to catch NUL-heavy files and text that has gone
// binary early on; scanning an entire huge file buys nothing.
const binaryProbeSize = 1024

// isBinary reports whether data looks like binary rather than text. It is the
// gate that keeps raw NUL/control bytes out of the conversation: when a tool
// reads a binary file (a .png, a .sqlite, an ELF, base64 blobs) or a command
// cats one through bash, the caller substitutes a compact placeholder instead
// of injecting screenfuls of junk into the message stream.
//
// Heuristic (stdlib only):
//   - if the first binaryProbeSize bytes are not valid UTF-8, it is binary;
//   - any NUL byte is a strong binary signal (NUL is itself valid UTF-8, so
//     this is a separate check);
//   - a high density of other C0 control bytes (>10%, excluding the benign
//     whitespace ones like \t \n \r \f \v) is treated as binary too.
func isBinary(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	n := len(data)
	if n > binaryProbeSize {
		n = binaryProbeSize
	}
	sample := data[:n]

	if !utf8.Valid(sample) {
		return true
	}

	var nul, ctrl int
	for _, b := range sample {
		switch {
		case b == 0x00:
			nul++
		case b < 0x20 && b != '\t' && b != '\n' && b != '\r' && b != 0x0b && b != 0x0c:
			ctrl++
		}
	}
	if nul > 0 {
		return true
	}
	return ctrl*10 > len(sample)
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
func binaryPlaceholder(name string, size int) string {
	if name == "" {
		return fmt.Sprintf("[binary output: %s, not shown]", bytesHuman(size))
	}
	return fmt.Sprintf("[binary: %s, %s]", name, bytesHuman(size))
}
