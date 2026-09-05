package tools

import (
	"bytes"
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
	// NUL anywhere in the output is the strongest binary signal — and cheap to
	// check over the whole buffer (bytes.IndexByte is SIMD-friendly), so a
	// text-then-binary stream (cat README.md image.png) is still caught.
	if bytes.IndexByte(data, 0x00) >= 0 {
		return true
	}

	// UTF-8 validity and control-byte density only need a prefix — scanning an
	// entire huge file buys nothing for those.
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

	ctrl := 0
	for _, b := range sample {
		switch {
		case b == 0x1b:
			// ESC starts ANSI escape sequences (ls --color, grep --color) —
			// colored text is not binary, so don't count it as a control byte.
		case b < 0x20 && b != '\t' && b != '\n' && b != '\r' && b != 0x0b && b != 0x0c:
			ctrl++
		}
	}
	return ctrl*10 > len(sample)
}

// trimToLastRune shortens s to end on a complete UTF-8 rune, but only when
// the cut landed mid-rune: the trailing bytes must form a valid prefix of a
// rune whose continuation extends past the cut. A genuinely-invalid trailing
// byte (Latin-1 smart quote, BOM-less UTF-16) is NOT a rune prefix, so it
// stays invalid and the caller's !utf8.Valid still flags the sample as binary.
func trimToLastRune(s []byte) []byte {
	if len(s) == 0 || utf8.Valid(s) {
		return s
	}
	// Find the start of the last (possibly incomplete) rune: walk back past
	// continuation bytes (0x80–0xBF) to the rune's lead byte.
	i := len(s) - 1
	for i > 0 && (s[i]&0xC0) == 0x80 {
		i--
	}
	// The candidate rune starts at i. If the lead byte at i encodes a rune
	// whose full length extends past the end of s, the cut landed mid-rune —
	// drop the tail. Otherwise the trailing byte is genuinely invalid; keep it.
	if i < len(s) {
		// DecodeRune on an incomplete sequence returns RuneError, so use the
		// lead byte's encoded length instead: 0xC0–0xDF = 2 bytes, 0xE0–0xEF =
		// 3, 0xF0–0xF7 = 4. A lead byte outside those ranges is not a rune
		// start, so it's genuinely invalid.
		lead := s[i]
		var want int
		switch {
		case lead < 0x80:
			want = 1
		case lead < 0xC0:
			want = 0 // continuation byte without a lead: invalid
		case lead < 0xE0:
			want = 2
		case lead < 0xF0:
			want = 3
		case lead < 0xF8:
			want = 4
		default:
			want = 0 // 0xF8+ is never a valid UTF-8 lead
		}
		if want > 1 && i+want > len(s) {
			return s[:i]
		}
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
