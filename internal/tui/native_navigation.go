package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

type nativeSessionPicker struct {
	items []protocol.RecentTree
	more  bool
	sel   int
}

type nativeNavigationResult struct {
	request uint64
	value   nativeControlResult
}

func (m *nativeModel) navigationRead(label string, call func(context.Context) nativeControlResult) tea.Cmd {
	m.navigationRequest++
	request := m.navigationRequest
	command := m.control(label, false, call)
	return func() tea.Msg {
		return nativeNavigationResult{request: request, value: command().(nativeControlResult)}
	}
}

func (m *nativeModel) navigationAllowed() bool {
	if m.uncertain != nil || m.retryControl != nil || m.standingDraft != nil || m.sending || m.controlling {
		m.status = "Resolve the pending input, control, or standing draft before switching sessions."
		return false
	}
	return true
}

func (m *nativeModel) resumeSession(id string) tea.Cmd {
	if !m.navigationAllowed() {
		return nil
	}
	if id == "" {
		return m.navigationRead("Recent sessions", func(ctx context.Context) nativeControlResult {
			var page protocol.RecentTreesResult
			if err := m.connection.Call(ctx, "trees.recent", protocol.RecentTreesParams{Limit: 50}, &page); err != nil {
				return nativeControlResult{err: err}
			}
			if len(page.Items) > 50 {
				return nativeControlResult{err: errors.New("recent session page exceeds limit")}
			}
			seen := map[protocol.ID]bool{}
			for _, item := range page.Items {
				if item.RootID == "" || item.Tree.ID == "" || seen[item.RootID] {
					return nativeControlResult{err: errors.New("invalid recent session identity")}
				}
				seen[item.RootID] = true
			}
			return nativeControlResult{picker: &nativeSessionPicker{items: page.Items, more: page.HasMore}}
		})
	}
	return m.navigationRead("Open session", func(ctx context.Context) nativeControlResult {
		owner, err := resolveNativeSession(ctx, m.connection, id)
		return nativeControlResult{attach: &owner, err: err}
	})
}

// Exact child or root IDs resolve directly. Prefix lookup applies only to root
// IDs and observes one catalog revision; an incomplete scan is never uniqueness.
func resolveNativeSession(ctx context.Context, connection *client.Client, id string) (protocol.Session, error) {
	handle, err := connection.Session(protocol.ID(id))
	if err != nil {
		return protocol.Session{}, err
	}
	owner, err := handle.Get(ctx)
	if err == nil {
		return owner, nil
	}
	if rejection, ok := errors.AsType[*client.Error](err); !ok || rejection.Kind != "NOT_FOUND" {
		return owner, err
	}
	if len(id) < 4 {
		return owner, errors.New("use a full session ID or a root ID prefix of at least four characters")
	}
	params := protocol.ListTreesParams{Search: id, Limit: 100}
	var match protocol.ID
	for range 100 {
		var page protocol.ListTreesResult
		if err := connection.Call(ctx, "trees.list", params, &page); err != nil {
			return owner, err
		}
		if params.ExpectedRevision != nil && page.Revision != *params.ExpectedRevision || len(page.Items) > params.Limit {
			return owner, errors.New("session catalog changed or exceeded its page limit")
		}
		params.ExpectedRevision = new(page.Revision)
		previous := protocol.ID("")
		if params.After != nil {
			previous = *params.After
		}
		for _, item := range page.Items {
			if item.Tree.ID <= previous || item.RootID == "" {
				return owner, errors.New("session catalog did not advance")
			}
			previous = item.Tree.ID
			if strings.HasPrefix(string(item.RootID), id) {
				if match != "" {
					return owner, errors.New("session prefix is ambiguous; use a full ID")
				}
				match = item.RootID
			}
		}
		if page.NextCursor == nil {
			if match == "" {
				return owner, fmt.Errorf("session %q was not found", id)
			}
			handle, err := connection.Session(match)
			if err != nil {
				return owner, err
			}
			return handle.Get(ctx)
		}
		if len(page.Items) == 0 || *page.NextCursor != previous {
			return owner, errors.New("invalid session catalog continuation")
		}
		params.After = page.NextCursor
	}
	return owner, errors.New("session prefix lookup exceeds 10000 rows; use a full ID")
}

func (m *nativeModel) invalidateRead() {
	if m.readCancel != nil {
		m.readCancel()
		m.readCancel = nil
	}
	m.generation++
	m.reading = false
}

func (m *nativeModel) attachSession(owner protocol.Session) error {
	if owner.ID == "" || owner.TreeID == "" || owner.ConfigRevision < 1 {
		return errors.New("invalid session attachment")
	}
	handle, err := m.connection.Session(owner.ID)
	if err != nil {
		return err
	}
	m.invalidateRead()
	m.navigationRequest++
	m.handle, m.owner = handle, owner
	m.observer, m.ready, m.cancelling = nil, false, false
	m.history = nativeTranscript{owner: owner.ID}
	m.activity = protocol.SessionActivity{SessionID: owner.ID, Lifecycle: owner.Lifecycle}
	m.usage, m.contextUsage = protocol.Usage{}, protocol.ContextUsage{}
	m.browseRequest++
	m.browse, m.browsing, m.follow = nil, false, true
	m.renderCache = nativeRenderCache{}
	m.picker, m.decision, m.hiddenDecision = nil, nil, nil
	m.decisions, m.decisionsHidden = nil, false
	m.notice, m.noteRevisions = "", [2]string{}
	m.input.Reset()
	m.polls = 0
	m.status = "Attached to " + string(owner.ID) + ". Other host work continues."
	m.refresh()
	return nil
}

func (m *nativeModel) pickerKey(key tea.KeyPressMsg) tea.Cmd {
	switch key.String() {
	case "esc":
		m.navigationRequest++
		m.controlling = false
		m.picker = nil
	case "up":
		m.picker.sel = max(m.picker.sel-1, 0)
	case "down":
		m.picker.sel = min(m.picker.sel+1, max(len(m.picker.items)-1, 0))
	case "enter":
		if len(m.picker.items) > 0 {
			return m.resumeSession(string(m.picker.items[m.picker.sel].RootID))
		}
	}
	return nil
}

func (p *nativeSessionPicker) view(width, height int) string {
	rows := []string{"Recent sessions · Enter opens · Esc closes", ""}
	count := max(height-7, 1)
	start := max(p.sel-count/2, 0)
	start = min(start, max(len(p.items)-count, 0))
	for i := start; i < min(start+count, len(p.items)); i++ {
		item := p.items[i]
		title := "(untitled)"
		if item.Tree.Metadata.Title != nil {
			title = *item.Tree.Metadata.Title
		}
		mark := "  "
		if i == p.sel {
			mark = "> "
		}
		row := fmt.Sprintf("%s%s · %s · %s", mark, item.RootID, title, item.Model.Name)
		rows = append(rows, ansi.Truncate(nativeDisplayText(row), width, "…"))
	}
	if len(p.items) == 0 {
		rows = append(rows, "No saved root sessions.")
	}
	if p.more {
		rows = append(rows, "Only 50 recent sessions shown. /resume <full ID or root prefix> opens older sessions.")
	}
	rows = append(rows, "", "Switching observes the selected owner; it does not stop work or change lifecycle.")
	return strings.Join(rows, "\n")
}
