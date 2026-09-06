package tui

import "strings"

// matchTier grades how well q matches file f (both compared lowercase):
//
//	0: q is a substring of the base name
//	1: q is a substring of the full path
//	2: q is a subsequence of the base name
//	3: q is a subsequence of the full path
//	-1: no match
func matchTier(f, q string) int {
	if q == "" {
		return 0
	}
	lf := strings.ToLower(f)
	base := lf[strings.LastIndexByte(lf, '/')+1:]
	if strings.Contains(base, q) {
		return 0
	}
	if strings.Contains(lf, q) {
		return 1
	}
	if subseq(base, q) {
		return 2
	}
	if subseq(lf, q) {
		return 3
	}
	return -1
}

// subseq reports whether every rune of q appears in s, in order.
func subseq(s, q string) bool {
	for _, r := range q {
		i := strings.IndexRune(s, r)
		if i < 0 {
			return false
		}
		s = s[i+len(string(r)):]
	}
	return true
}
