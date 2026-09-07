package daemon

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

func (s *Server) sessionSummaries(ctx context.Context, p protocol.SessionSummariesParams) (protocol.SessionSummariesResult, error) {
	for _, id := range p.RootIDs {
		if len(id) > session.MaxSessionSummaryIDBytes {
			return protocol.SessionSummariesResult{}, rpcFailure(-32602, "session summary root IDs must be at most 256 UTF-8 bytes")
		}
	}
	items, err := s.daemon.store.SessionSummaries(ctx, p.RootIDs)
	if err != nil {
		return protocol.SessionSummariesResult{}, err
	}
	// Copy only requested, ready pointers while holding the daemon registry
	// lock. No SQL, actor admission or question inspection occurs under it.
	roots := make(map[string]*Session, len(items))
	s.daemon.mu.Lock()
	for _, item := range items {
		if entry := s.daemon.roots[item.RootID]; !item.Missing && entry != nil {
			select {
			case <-entry.ready:
				if entry.root != nil {
					roots[item.RootID] = entry.root
				}
			default:
			}
		}
	}
	s.daemon.mu.Unlock()
	for index := range items {
		if root := roots[items[index].RootID]; root != nil {
			root.questions.mu.Lock()
			items[index].PendingQuestions = int64(len(root.questions.pending))
			root.questions.mu.Unlock()
		}
	}
	return boundSessionSummaries(items)
}

func boundSessionSummaries(items []session.SessionNavigationSummary) (protocol.SessionSummariesResult, error) {
	result := protocol.SessionSummariesResult{Items: items}
	for {
		encoded, err := json.Marshal(result)
		if err != nil {
			return protocol.SessionSummariesResult{}, err
		}
		if len(encoded) <= session.MaxSessionSummariesBytes {
			return result, nil
		}
		// Escaped strings can exceed their source size. Preserve every identity
		// and count, reducing presentation only until the encoded result fits.
		changed := false
		for index := range result.Items {
			item := &result.Items[index]
			for _, field := range []*string{&item.Title, &item.CWD} {
				if *field != "" {
					runes := []rune(*field)
					*field = string(runes[:len(runes)/2])
					item.Truncated = true
					changed = true
				}
			}
		}
		if !changed {
			return protocol.SessionSummariesResult{}, errors.New("session summary identities exceed presentation budget")
		}
	}
}
