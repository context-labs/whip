package tui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

type nativeRecallLoaded struct {
	recall     *nativeInputRecall
	generation uint64
	draft      nativeDraft
	entries    []nativeDraft
	partial    bool
	err        error
}

func (m *nativeModel) loadRecall() tea.Cmd {
	r := m.recall
	if r.loading || r.loaded || m.connection == nil {
		return nil
	}
	r.loading = true
	generation, draft := m.generation, m.captureDraft()
	m.status = "Reading bounded human input history; your draft is kept."
	return func() tea.Msg {
		result := nativeRecallLoaded{recall: r, generation: generation, draft: draft}
		ctx, done, err := m.work.begin()
		if err != nil {
			result.err = err
			return result
		}
		defer done()
		result.entries, result.partial, result.err = readNativeRecall(ctx, m.connection)
		return result
	}
}

func readNativeRecall(ctx context.Context, connection *client.Client) ([]nativeDraft, bool, error) {
	return collectNativeRecall(func(before *protocol.Counter) (protocol.InputTextPage, error) {
		var page protocol.InputTextPage
		err := connection.Call(ctx, "inputs.recent_text", protocol.RecentInputTextParams{Before: before, Limit: 500}, &page)
		return page, err
	})
}

func collectNativeRecall(read func(*protocol.Counter) (protocol.InputTextPage, error)) ([]nativeDraft, bool, error) {
	var before *protocol.Counter
	var entries []nativeDraft
	seen := map[string]bool{}
	bytes, partial := 0, false
	for range 8 {
		page, err := read(before)
		if err != nil {
			return nil, false, err
		}
		if len(page.Items) > page.ScannedCount || page.SkippedCount > page.ScannedCount {
			return nil, false, errors.New("invalid input history scan count")
		}
		last, pageBytes := before, 0
		for _, item := range page.Items {
			pageBytes += len(item.Text)
			if item.Ordinal <= 0 || last != nil && item.Ordinal >= *last || pageBytes > 256<<10 {
				return nil, false, errors.New("input history ordering or byte bound violated")
			}
			last = new(item.Ordinal)
			text := strings.TrimSpace(item.Text)
			if text == "" || seen[text] || nativeRecallSecret(text) {
				continue
			}
			seen[text] = true
			draft := nativeRecallTextDraft(text)
			if len(entries) == 500 || bytes+draft.bytes() > 256<<10 {
				partial = true
				continue
			}
			bytes += draft.bytes()
			entries = append(entries, draft)
		}
		partial = partial || page.SkippedCount > 0
		if page.NextCursor == nil {
			slices.Reverse(entries)
			return entries, partial, nil
		}
		if page.ScannedCount == 0 || *page.NextCursor <= 0 || last != nil && *page.NextCursor > *last || before != nil && *page.NextCursor >= *before {
			return nil, false, errors.New("input history cursor failed to advance")
		}
		before = page.NextCursor
		if len(entries) >= 500 || bytes >= 256<<10 {
			break
		}
	}
	slices.Reverse(entries)
	return entries, true, nil
}

func nativeRecallTextDraft(text string) nativeDraft {
	if strings.IndexFunc(text, func(r rune) bool { return unicode.IsControl(r) && r != '\n' }) >= 0 || strings.Count(text, "\n") >= 9999 {
		chip := fmt.Sprintf("[Recalled text · %d lines]", strings.Count(text, "\n")+1)
		return nativeDraft{text: chip, pastes: map[string]string{chip: text}}
	}
	return nativeDraft{text: text}
}

func nativeRecallSecret(text string) bool {
	fields := strings.Fields(text)
	return len(fields) > 0 && (fields[0] == "/auth" || fields[0] == "/connect" || fields[0] == "/setup")
}

func (m *nativeModel) recallLoaded(result nativeRecallLoaded) {
	r := result.recall
	if m.recall != r || result.generation != m.generation {
		return
	}
	r.loading = false
	if r.owner != m.owner.ID || r.revision != m.history.snapshot.Revision || !reflect.DeepEqual(result.draft, m.captureDraft()) {
		m.recall = nil
		m.status = "Input history read discarded because the displayed draft or owner changed."
		return
	}
	if result.err != nil {
		m.status = "Input history unavailable: " + result.err.Error() + ". Up reads again; nothing was sent."
		return
	}
	r.loaded = true
	local := map[string]bool{}
	for _, draft := range r.entries {
		local[strings.TrimSpace(draft.text)] = true
	}
	older := slices.DeleteFunc(result.entries, func(draft nativeDraft) bool { return local[strings.TrimSpace(draft.text)] })
	r.entries = append(older, r.entries...)
	r.index += len(older)
	if len(older) > 0 {
		r.index--
		m.applyDraft(r.entries[r.index])
		m.status = "Recalled human text as an unsent draft; foreign attachments and design metadata are never copied. Down restores your newer draft."
	} else {
		m.status = "No older human text in the bounded history scan."
	}
	if result.partial {
		m.status += " History is partial: its page, byte, or oversize bounds were reached."
	}
}
