package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/session"
)

func searchBytes(data []byte) func(context.Context, int64, int) ([]byte, error) {
	return func(_ context.Context, offset int64, length int) ([]byte, error) {
		return data[offset:min(int64(len(data)), offset+int64(length))], nil
	}
}

func TestLiteralSearchChunkBoundaries(t *testing.T) {
	t.Parallel()
	for _, query := range []string{"needle", "界🙂é", strings.Repeat("x", maxSearchQueryBytes-1) + "y"} {
		for displacement := -len(query); displacement <= len(query) && displacement <= 8; displacement++ {
			// The longest supported query covers both extremes and nearby edges
			// without generating 64,000 equivalent large fixtures.
			if len(query) > 20 && displacement < -8 && displacement != -len(query) {
				continue
			}
			t.Run(fmt.Sprintf("query%d/offset%d", len(query), displacement), func(t *testing.T) {
				start := session.MaxContentRead + displacement
				data := []byte(strings.Repeat("a", start) + query + strings.Repeat("b", 200))
				result, err := searchLiteral(t.Context(), searchBytes(data), int64(len(data)), query, 0, maxSearchScan, maxSearchMatches)
				if err != nil {
					t.Fatal(err)
				}
				if len(result.Matches) != 1 || result.Matches[0].Start != int64(start) || result.Matches[0].End != int64(start+len(query)) {
					t.Fatalf("matches = %+v, want one at %d", result.Matches, start)
				}
				if result.Truncated || result.StopReason != "end" || result.Scanned != int64(len(data)) || result.NextOffset != int64(len(data)) {
					t.Fatalf("completion = %+v", result)
				}
				if result.Matches[0].TextEnd-result.Matches[0].TextStart > maxSearchTextBytes || !utf8.ValidString(result.Matches[0].Text) {
					t.Fatalf("unbounded or invalid match excerpt: %+v", result.Matches[0])
				}
			})
		}
	}
}

func TestLiteralSearchLongEscapableMatchesKeepResponseBounded(t *testing.T) {
	t.Parallel()
	query := strings.Repeat("<", maxSearchQueryBytes)
	data := []byte(strings.Repeat(query+";", 21))
	first, err := searchLiteral(t.Context(), searchBytes(data), int64(len(data)), query, 0, maxSearchScan, maxSearchMatches)
	if err != nil || len(first.Matches) != 20 || !first.Truncated {
		t.Fatalf("long search: %v, count=%d, truncated=%v", err, len(first.Matches), first.Truncated)
	}
	for _, match := range first.Matches {
		if match.End-match.Start != int64(len(query)) || len(match.Text) > maxSearchTextBytes {
			t.Fatalf("match span or bounded excerpt incorrect: start=%d end=%d text bytes=%d", match.Start, match.End, len(match.Text))
		}
	}
	encoded, err := json.Marshal(first)
	if err != nil || len(encoded) > 48<<10 {
		t.Fatalf("escaped search response is not kernel-safe: bytes=%d err=%v", len(encoded), err)
	}
	next, err := searchLiteral(t.Context(), searchBytes(data), int64(len(data)), query, first.NextOffset, maxSearchScan, maxSearchMatches)
	if err != nil || len(next.Matches) != 1 || next.Truncated || next.Matches[0].Start != int64(20*(len(query)+1)) {
		t.Fatalf("continuation lost long match: %+v, %v", next, err)
	}
}

func TestLiteralSearchResultLimitAndContinuation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		data      string
		truncated bool
	}{
		{name: "twenty_at_end", data: strings.Repeat("--find", 20)},
		{name: "twenty_with_unexamined_suffix", data: strings.Repeat("--find", 20) + "suffix", truncated: true},
		{name: "twenty_one", data: strings.Repeat("--find", 21), truncated: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(tc.data)
			result, err := searchLiteral(t.Context(), searchBytes(data), int64(len(data)), "find", 0, maxSearchScan, maxSearchMatches)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Matches) != 20 || result.Scanned != 120 || result.NextOffset != 120 || result.Truncated != tc.truncated {
				t.Fatalf("result = %+v", result)
			}
			if !tc.truncated {
				if result.StopReason != "end" {
					t.Fatalf("stop reason = %q", result.StopReason)
				}
				return
			}
			if result.StopReason != "match_limit" {
				t.Fatalf("stop reason = %q", result.StopReason)
			}
			next, err := searchLiteral(t.Context(), searchBytes(data), int64(len(data)), "find", result.NextOffset, maxSearchScan, maxSearchMatches)
			if err != nil || next.Truncated || next.StopReason != "end" {
				t.Fatalf("continuation = %+v, %v", next, err)
			}
			if tc.name == "twenty_one" && (len(next.Matches) != 1 || next.Matches[0].Start != 122) {
				t.Fatalf("lost or duplicate continuation match: %+v", next.Matches)
			}
			if tc.name == "twenty_with_unexamined_suffix" && len(next.Matches) != 0 {
				t.Fatalf("unexpected continuation matches: %+v", next.Matches)
			}
		})
	}
}

func TestLiteralSearchScanLimitCrossingAndContinuation(t *testing.T) {
	t.Parallel()
	const query = "cross-ceiling"
	start := maxSearchScan - len(query)/2
	data := []byte(strings.Repeat("x", start) + query + " tail " + query)
	var readBytes int64
	read := func(ctx context.Context, offset int64, length int) ([]byte, error) {
		readBytes += int64(length)
		if length > session.MaxContentRead {
			t.Fatalf("unbounded read length %d", length)
		}
		return searchBytes(data)(ctx, offset, length)
	}
	first, err := searchLiteral(t.Context(), read, int64(len(data)), query, 0, maxSearchScan, maxSearchMatches)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Matches) != 0 || !first.Truncated || first.StopReason != "scan_limit" || first.Scanned != maxSearchScan || readBytes != maxSearchScan {
		t.Fatalf("first = %+v, fetched = %d", first, readBytes)
	}
	if first.NextOffset <= 0 || first.NextOffset > int64(start) {
		t.Fatalf("continuation skipped candidate: %d", first.NextOffset)
	}
	second, err := searchLiteral(t.Context(), read, int64(len(data)), query, first.NextOffset, maxSearchScan, maxSearchMatches)
	if err != nil || second.Truncated || len(second.Matches) != 2 || second.Matches[0].Start != int64(start) {
		t.Fatalf("continuation = %+v, %v", second, err)
	}
}

func TestLiteralSearchUTF8SnippetSpans(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		data string
	}{
		{name: "snippet_edges", data: strings.Repeat("🙂", 41) + "needle" + strings.Repeat("界", 60)},
		{name: "chunk_inside_rune", data: strings.Repeat("a", session.MaxContentRead-8) + "needle🙂" + strings.Repeat("界", 60)},
		{name: "invalid_source", data: "\xffneedle\xff"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(tc.data)
			result, err := searchLiteral(t.Context(), searchBytes(data), int64(len(data)), "needle", 0, maxSearchScan, maxSearchMatches)
			if err != nil || len(result.Matches) != 1 {
				t.Fatalf("search = %+v, %v", result, err)
			}
			match := result.Matches[0]
			if !utf8.ValidString(match.Text) || string(data[match.Start:match.End]) != "needle" {
				t.Fatalf("invalid snippet or exact match: %+v", match)
			}
			if tc.name != "invalid_source" && match.Text != string(data[match.TextStart:match.TextEnd]) {
				t.Fatalf("snippet does not match its source span: %+v", match)
			}
		})
	}
}

func TestLiteralSearchContinuationMatchesWholeSource(t *testing.T) {
	t.Parallel()
	random := rand.New(rand.NewPCG(8, 37))
	for caseID := range 200 {
		t.Run(fmt.Sprintf("case%d", caseID), func(t *testing.T) {
			data := make([]byte, 1+random.IntN(500))
			for i := range data {
				data[i] = "aab"[random.IntN(3)]
			}
			query := []string{"a", "aa", "aba", "baab", "bbbbbbb"}[random.IntN(5)]
			var want []int64
			for offset := 0; offset < len(data); {
				index := bytes.Index(data[offset:], []byte(query))
				if index < 0 {
					break
				}
				want = append(want, int64(offset+index))
				offset += index + len(query)
			}
			var got []int64
			for offset := int64(0); ; {
				budget := int64(len(query) + random.IntN(20))
				result, err := searchLiteral(t.Context(), searchBytes(data), int64(len(data)), query, offset, budget, 1+random.IntN(5))
				if err != nil {
					t.Fatal(err)
				}
				for _, match := range result.Matches {
					got = append(got, match.Start)
				}
				if !result.Truncated {
					break
				}
				if result.NextOffset <= offset || result.Scanned > budget {
					t.Fatalf("search did not advance within its budget: %+v", result)
				}
				offset = result.NextOffset
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("query %q in %q: continued %v, whole-source %v", query, data, got, want)
			}
		})
	}
}

func TestLiteralSearchPreservesCaseAndNonOverlappingMatches(t *testing.T) {
	t.Parallel()
	data := []byte("AAaaaaa")
	result, err := searchLiteral(t.Context(), searchBytes(data), int64(len(data)), "aa", 0, maxSearchScan, maxSearchMatches)
	if err != nil || len(result.Matches) != 2 || result.Matches[0].Start != 2 || result.Matches[1].Start != 4 {
		t.Fatalf("case-sensitive, non-overlapping search = %+v, %v", result, err)
	}
}

func TestLiteralSearchEmptyAndInvalidRanges(t *testing.T) {
	t.Parallel()
	called := false
	read := func(context.Context, int64, int) ([]byte, error) {
		called = true
		return nil, errors.New("unexpected read")
	}
	result, err := searchLiteral(t.Context(), read, 0, "needle", 0, maxSearchScan, maxSearchMatches)
	if err != nil || called || result.Truncated || result.Scanned != 0 || result.StopReason != "end" || len(result.Matches) != 0 {
		t.Fatalf("empty result = %+v, %v, reader called=%v", result, err, called)
	}
	for _, tc := range []struct {
		name   string
		query  string
		offset int64
		budget int64
	}{
		{name: "empty_query", query: "", budget: maxSearchScan},
		{name: "oversized_query", query: strings.Repeat("x", maxSearchQueryBytes+1), budget: maxSearchScan},
		{name: "negative_offset", query: "x", offset: -1, budget: maxSearchScan},
		{name: "offset_past_end", query: "x", offset: 101, budget: maxSearchScan},
		{name: "zero_budget", query: "x"},
		{name: "budget_cannot_advance", query: "needle", budget: 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := searchLiteral(t.Context(), read, 100, tc.query, tc.offset, tc.budget, maxSearchMatches)
			if err == nil || called {
				t.Fatalf("invalid input reached reader or was accepted: err=%v, called=%v", err, called)
			}
		})
	}
}

func TestLiteralSearchCancellationAndReadErrors(t *testing.T) {
	t.Parallel()
	readFailure := errors.New("lost content storage")
	for _, tc := range []struct {
		name string
		read func(context.Context, int64, int) ([]byte, error)
		want error
	}{
		{name: "read_failure", read: func(context.Context, int64, int) ([]byte, error) { return nil, readFailure }, want: readFailure},
		{name: "premature_eof", read: func(context.Context, int64, int) ([]byte, error) { return nil, nil }, want: io.ErrUnexpectedEOF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := searchLiteral(t.Context(), tc.read, 100, "needle", 0, maxSearchScan, maxSearchMatches)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
	t.Run("cancelled_before_read", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		read := func(context.Context, int64, int) ([]byte, error) {
			t.Fatal("cancelled search reached reader")
			return nil, nil
		}
		if _, err := searchLiteral(ctx, read, 100, "needle", 0, maxSearchScan, maxSearchMatches); !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("cancelled_during_read", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		read := func(context.Context, int64, int) ([]byte, error) {
			cancel()
			return []byte("needle"), nil
		}
		if _, err := searchLiteral(ctx, read, 100, "needle", 0, maxSearchScan, maxSearchMatches); !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("error_after_progress", func(t *testing.T) {
		reads := 0
		read := func(context.Context, int64, int) ([]byte, error) {
			reads++
			if reads == 1 {
				return []byte("needle"), nil
			}
			return nil, readFailure
		}
		if _, err := searchLiteral(t.Context(), read, 100, "needle", 0, maxSearchScan, maxSearchMatches); !errors.Is(err, readFailure) || reads != 2 {
			t.Fatalf("error after a found match = %v, reads = %d", err, reads)
		}
	})
	t.Run("reader_exceeds_limit", func(t *testing.T) {
		read := func(_ context.Context, _ int64, length int) ([]byte, error) {
			return make([]byte, length+1), nil
		}
		if _, err := searchLiteral(t.Context(), read, 100, "needle", 0, maxSearchScan, maxSearchMatches); err == nil {
			t.Fatal("accepted read beyond requested range")
		}
	})
	t.Run("short_reads", func(t *testing.T) {
		data := []byte("before-long-needle-after")
		read := func(ctx context.Context, offset int64, length int) ([]byte, error) {
			return searchBytes(data)(ctx, offset, min(length, 3))
		}
		result, err := searchLiteral(t.Context(), read, int64(len(data)), "long-needle", 0, maxSearchScan, maxSearchMatches)
		if err != nil || len(result.Matches) != 1 || result.Matches[0].Start != 7 || result.Truncated {
			t.Fatalf("short reads = %+v, %v", result, err)
		}
	})
}

func TestContentSearchUsesCallerAuthorityAndReturnsContinuation(t *testing.T) {
	_, root, child, _ := mailboxDeliveryFixture(t)
	data := []byte(strings.Repeat("--find", 21))
	value, err := root.StoreContent(t.Context(), child.id, session.RuntimePayload{Data: data, Source: "search-fixture"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := child.host.searchContent(t.Context(), value.ReferenceID, "find")
	if err != nil {
		t.Fatal(err)
	}
	result := first.(map[string]any)
	if result["scanned"] != int64(120) || result["truncated"] != true || result["stop_reason"] != "match_limit" || result["size"] != int64(len(data)) {
		t.Fatalf("result metadata = %+v", result)
	}
	second, err := child.host.searchContentFrom(t.Context(), value.ReferenceID, "find", result["next_offset"].(int64))
	if err != nil {
		t.Fatal(err)
	}
	last := second.(map[string]any)
	matches := last["matches"].([]map[string]any)
	if last["truncated"] != false || len(matches) != 1 || matches[0]["source"] != "search-fixture" || matches[0]["handle"] != value.ReferenceID {
		t.Fatalf("continuation = %+v", last)
	}
	rootHost := &recursiveHost{session: &AgentSession{id: root.ID(), root: root}}
	if _, err := rootHost.searchContent(t.Context(), value.ReferenceID, "find"); !errors.Is(err, session.ErrContentAccess) {
		t.Fatalf("another agent searched private content: %v", err)
	}
}
