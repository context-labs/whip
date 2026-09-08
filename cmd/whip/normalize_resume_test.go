package main

import (
	"reflect"
	"testing"
)

func TestNormalizeBareResume(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"bare --resume → --browse", []string{"--resume"}, []string{"--browse"}},
		{"bare -r → --browse", []string{"-r"}, []string{"--browse"}},
		{"-r <id> unchanged", []string{"-r", "abc123"}, []string{"-r", "abc123"}},
		{"--resume <id> unchanged", []string{"--resume", "abc123"}, []string{"--resume", "abc123"}},
		{"-r=<id> unchanged", []string{"-r=abc123"}, []string{"-r=abc123"}},
		{"--resume=<id> unchanged", []string{"--resume=abc123"}, []string{"--resume=abc123"}},
		{"bare -r then another flag → --browse then flag", []string{"-r", "--cautious"}, []string{"--browse", "--cautious"}},
		// -r followed by a non-flag token is `-r <id>` (claude-consistent: the
		// next token is the session id/name), NOT bare. So `whip -r up do it`
		// tries to resume session "up" (then fails no-such-id) rather than
		// opening the picker — picker+prompt uses `--browse up do it` / `-c up …`.
		{"-r <non-flag> is -r <id>, not bare", []string{"-r", "up", "do", "it"}, []string{"-r", "up", "do", "it"}},
		{"-r <id> before up → unchanged", []string{"-r", "abc", "up", "go"}, []string{"-r", "abc", "up", "go"}},
		{"no resume flag → untouched", []string{"-m", "kimi", "up", "hi"}, []string{"-m", "kimi", "up", "hi"}},
		{"empty args", []string{}, []string{}},
		{"id that looks flag-ish (- prefix) → treated as flag, so -r is bare", []string{"-r", "-x"}, []string{"--browse", "-x"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := make([]string, len(c.in))
			copy(got, c.in) // copy so mutation doesn't corrupt the table (non-nil even when empty)
			normalizeBareResume(got)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("normalizeBareResume(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}
