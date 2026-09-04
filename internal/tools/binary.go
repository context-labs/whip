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
//     whitespace ones like \t \n \r \f \v and ESC for ANSI-colored output) is
//     treated as binary too.
func isBinary(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	n := min(len(data), binaryProbeSize)
	sample := data[:n]
	// A multi-byte rune can straddle the probe boundary — a 1024-byte cut can
	// end mid-rune and read as invalid UTF-8 for a plain text file (CJK/emoji
	// hit this constantly). Back the sample off to the last complete rune.
	if len(data) > binaryProbeSize {
		sample = trimToLastRune(sample)
	}

	if !utf8.Valid(sample) {
		return true
	}

	var nul, ctrl int
	for _, b := range sample {
		switch {
		case b == 0x00:
			nul++
		case b == 0x1b:
			// ESC starts ANSI escape sequences (ls --color, grep --color) —
			// colored text is not binary, so don't count it as a control byte.
		case b < 0x20 && b != '\t' && b != '\n' && b != '\r' && b != 0x0b && b != 0x0c:
			ctrl++
		}
	}
	if nul > 0 {
		return true
	}
	return ctrl*10 > len(sample)
}

// trimToLastRune shortens s to end on a complete UTF-8 rune. Used when the
// sample was cut at a fixed byte count that may land mid-rune; the tail is
// dropped only when the final bytes are an incomplete encoding.
func trimToLastRune(s []byte) []byte {
	for len(s) > 0 {
		if utf8.Valid(s) {
			return s
		}
		_, size := utf8.DecodeLastRune(s)
		if size == 0 {
			size = 1
		}
		s = s[:len(s)-size]
	}
	return s
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
