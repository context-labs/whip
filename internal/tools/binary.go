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

	// The control-byte density check only needs a prefix — a file that's
	// binary from the start trips it in the first 1KB; a text-then-binary
	// stream is already caught by the full-buffer NUL/UTF-8 checks above.
	n := min(len(data), binaryProbeSize)
	sample := data[:n]
	// A multi-byte rune can straddle the probe boundary — a 1024-byte cut can
	// end mid-rune and read as invalid UTF-8 for a plain text file (CJK/emoji
	// hit this constantly). Back the sample off to the last complete rune.
	if len(data) > binaryProbeSize {
		sample = trimToLastRune(sample)
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
// the cut landed mid-rune: the trailing bytes must be a valid prefix of a
// legal rune encoding whose continuation extends past the cut (RFC 3629
// second-byte constraints included — E0 requires A0–BF, ED requires 80–9F,
// F0 requires 90–BF, F4 requires 80–8F, leads outside C2–F4 are never legal).
// A genuinely-invalid trailing byte (Latin-1 smart quote, E0 80 overlong,
// BOM-less UTF-16) is NOT a rune prefix, so it stays invalid and the caller's
// !utf8.Valid still flags the sample as binary.
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
	tail := s[i:]
	if want, ok := runePrefixLen(tail); ok && i+want > len(s) {
		return s[:i]
	}
	return s
}

// runePrefixLen reports whether b is a valid prefix of a legal UTF-8 rune
// encoding, returning the rune's full length when it is. The check is the
// RFC 3629 table: lead legality (C2–F4), the second-byte constraint each lead
// carries (E0/F0 forbid overlong second bytes; ED/F4 bound the rune below the
// surrogate/ceiling ranges), and every present continuation byte in 80–BF.
func runePrefixLen(b []byte) (int, bool) {
	if len(b) == 0 {
		return 0, false
	}
	lead := b[0]
	var want int
	var secondLo, secondHi byte = 0x80, 0xBF
	switch {
	case lead >= 0xC2 && lead <= 0xDF:
		want = 2
	case lead == 0xE0:
		want, secondLo = 3, 0xA0
	case lead >= 0xE1 && lead <= 0xEC, lead == 0xEE, lead == 0xEF:
		want = 3
	case lead == 0xED:
		want, secondHi = 3, 0x9F
	case lead == 0xF0:
		want, secondLo = 4, 0x90
	case lead >= 0xF1 && lead <= 0xF3:
		want = 4
	case lead == 0xF4:
		want, secondHi = 4, 0x8F
	default:
		return 0, false // ASCII, bare continuation, C0/C1, F5+: not a rune start
	}
	for j := 1; j < len(b) && j < want; j++ {
		c := b[j]
		lo, hi := byte(0x80), byte(0xBF)
		if j == 1 {
			lo, hi = secondLo, secondHi
		}
		if c < lo || c > hi {
			return 0, false
		}
	}
	return want, true
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
