package tools

import (
	"strings"
	"testing"
)

func TestIsBinary(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{"empty", nil, false},
		{"text", []byte("hello\nworld\t"), false},
		{"ansi", []byte("\x1b[31mred\x1b[0m"), false},
		{"nul", []byte{'a', 0, 'b'}, true},
		{"invalid utf8", []byte{0xff}, true},
		{"few controls", append([]byte{1}, []byte("0123456789")...), false},
		{"many controls", append([]byte{1, 2}, []byte("0123456789")...), true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := IsBinary(test.data); got != test.want {
				t.Errorf("IsBinary(%q) = %v, want %v", test.data, got, test.want)
			}
		})
	}
}

func TestBinaryPlaceholderSizes(t *testing.T) {
	for _, test := range []struct {
		size int
		want string
	}{{12, "12 bytes"}, {1 << 10, "1.0 KB"}, {1 << 20, "1.0 MB"}, {1 << 30, "1.0 GB"}} {
		if got := BinaryPlaceholder("file.bin", test.size); !strings.Contains(got, test.want) {
			t.Errorf("BinaryPlaceholder size %d = %q, want %q", test.size, got, test.want)
		}
	}
	if got := BinaryPlaceholder("", 12); got != "[binary output: 12 bytes, not shown]" {
		t.Errorf("unnamed placeholder = %q", got)
	}
}
