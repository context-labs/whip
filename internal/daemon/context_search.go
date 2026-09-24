package daemon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	sessionstore "github.com/context-labs/whip/internal/session"
)

const (
	maxSearchScan       = 8 << 20
	maxSearchMatches    = 20
	maxSearchQueryBytes = sessionstore.MaxContentRead
	searchSnippetBytes  = 80
	maxSearchTextBytes  = 320
)

type literalMatch struct {
	Start, End         int64
	Text               string
	TextStart, TextEnd int64
}

type literalSearchResult struct {
	Matches    []literalMatch
	Scanned    int64
	NextOffset int64
	StopReason string
	Truncated  bool
}

// searchLiteral finds case-sensitive, non-overlapping byte literals. Scanned
// counts examined source bytes from offset, excluding fetched bytes abandoned
// after a result limit. NextOffset is the first unresolved candidate start; a
// scan-limit continuation may reread up to len(query)-1 trailing bytes. Continuing
// at that offset neither repeats a match nor loses one crossing the scan limit.
// The reader must return at most length bytes, with an error on unavailable data.
func searchLiteral(ctx context.Context, read func(context.Context, int64, int) ([]byte, error), size int64, query string, offset, maxScan int64, maxMatches int) (literalSearchResult, error) {
	result := literalSearchResult{Matches: []literalMatch{}, NextOffset: offset, StopReason: "end"}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if query == "" || len(query) > maxSearchQueryBytes {
		return result, fmt.Errorf("query must contain 1 to %d bytes", maxSearchQueryBytes)
	}
	if size < 0 || offset < 0 || offset > size {
		return result, errors.New("invalid search offset or source size")
	}
	if maxScan <= 0 || maxMatches <= 0 {
		return result, errors.New("search limits must be positive")
	}
	if maxScan < int64(len(query)) && size-offset > maxScan {
		return result, errors.New("remaining search byte budget is smaller than the query")
	}
	// Subtract before adding so caller-provided limits cannot overflow.
	limit := offset + min(maxScan, size-offset)
	readOffset, searchOffset, bufferOffset := offset, offset, offset
	var buffer []byte
	for readOffset < limit {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		length := int(min(int64(sessionstore.MaxContentRead), limit-readOffset))
		chunk, err := read(ctx, readOffset, length)
		if err != nil {
			return result, err
		}
		if len(chunk) == 0 {
			return result, io.ErrUnexpectedEOF
		}
		if len(chunk) > length {
			return result, errors.New("search reader exceeded the requested byte range")
		}
		buffer = append(buffer, chunk...)
		readOffset += int64(len(chunk))
		for {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			index := bytes.Index(buffer[searchOffset-bufferOffset:], []byte(query))
			if index < 0 {
				break
			}
			start := searchOffset + int64(index)
			end := start + int64(len(query))
			text, textStart, textEnd := searchSnippet(buffer, int(start-bufferOffset), int(end-bufferOffset))
			result.Matches = append(result.Matches, literalMatch{
				Start: start, End: end, Text: text,
				TextStart: bufferOffset + int64(textStart), TextEnd: bufferOffset + int64(textEnd),
			})
			searchOffset = end
			if len(result.Matches) == maxMatches {
				result.Scanned, result.NextOffset = end-offset, end
				result.Truncated = end < size
				if result.Truncated {
					result.StopReason = "match_limit"
				}
				return result, nil
			}
		}
		// All earlier candidates have enough following bytes to rule them out.
		searchOffset = max(searchOffset, readOffset-int64(len(query))+1)
		keepFrom := max(bufferOffset, searchOffset-searchSnippetBytes-utf8.UTFMax)
		drop := int(keepFrom - bufferOffset)
		copy(buffer, buffer[drop:])
		buffer = buffer[:len(buffer)-drop]
		bufferOffset = keepFrom
	}
	result.Scanned = readOffset - offset
	result.NextOffset = size
	if readOffset < size {
		result.NextOffset, result.StopReason, result.Truncated = searchOffset, "scan_limit", true
	}
	return result, nil
}

// Snippet coordinates continue to address the original bytes, even when an
// invalid source byte must be rendered as a replacement character. For valid
// UTF-8 sources, trim only partial runes at the snippet's outer boundaries.
func searchSnippet(data []byte, start, end int) (string, int, int) {
	start = max(0, start-searchSnippetBytes)
	end = min(len(data), end+searchSnippetBytes, start+maxSearchTextBytes)
	for start < end && !utf8.RuneStart(data[start]) {
		start++
	}
	if end > start {
		last := end - 1
		for last > start && !utf8.RuneStart(data[last]) {
			last--
		}
		if !utf8.FullRune(data[last:end]) {
			end = last
		}
	}
	return strings.ToValidUTF8(string(data[start:end]), "\uFFFD"), start, end
}

func (host *recursiveHost) searchContent(ctx context.Context, reference, query string) (any, error) {
	return host.searchContentFrom(ctx, reference, query, 0)
}

func (host *recursiveHost) searchContentFrom(ctx context.Context, reference, query string, offset int64) (any, error) {
	node := host.session
	_, metadata, err := node.root.ReadContent(ctx, node.id, reference, 0, 0)
	if err != nil {
		return nil, err
	}
	read := func(ctx context.Context, offset int64, length int) ([]byte, error) {
		body, _, err := node.root.ReadContent(ctx, node.id, reference, offset, length)
		return body, err
	}
	result, err := searchLiteral(ctx, read, metadata.Size, query, offset, maxSearchScan, maxSearchMatches)
	if err != nil {
		return nil, err
	}
	matches := make([]map[string]any, 0, len(result.Matches))
	for _, match := range result.Matches {
		matches = append(matches, map[string]any{
			"handle": reference, "source": metadata.Source,
			"span": map[string]any{"start": match.Start, "end": match.End},
			"text": match.Text, "text_span": map[string]any{"start": match.TextStart, "end": match.TextEnd},
		})
	}
	return map[string]any{
		"handle": reference, "source": metadata.Source,
		"matches": matches, "scanned": result.Scanned, "size": metadata.Size,
		"scanned_span": map[string]any{"start": offset, "end": offset + result.Scanned},
		"truncated":    result.Truncated, "next_offset": result.NextOffset, "stop_reason": result.StopReason,
	}, nil
}
