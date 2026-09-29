package tui

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/context-labs/whip/internal/protocol"
)

func (m *nativeMenu) readRename() tea.Cmd {
	if m.owner == nil {
		m.message = "Attach a session before renaming."
		return nil
	}
	connection, treeID := m.connection, m.owner.TreeID
	return m.call("rename-read", false, func(ctx context.Context) nativeMenuReply {
		var tree protocol.Tree
		err := connection.Call(ctx, "trees.get", protocol.TreeParams{TreeID: treeID}, &tree)
		if err == nil && tree.ID != treeID {
			err = errors.New("rename tree ownership mismatch")
		}
		return nativeMenuReply{tree: &tree, err: err}
	})
}

func (m *nativeMenu) renameReply(reply nativeMenuReply) tea.Cmd {
	if reply.kind == "rename-saved" {
		m.Close()
		return nil
	}
	m.renameTree = reply.tree
	m.mode, m.title = "rename", "Session name"
	m.resetInput()
	m.choices = nil
	if m.renameTree.Metadata.Title != nil {
		m.input.SetValue(*m.renameTree.Metadata.Title)
	}
	m.message = "Enter saves this tree title using the captured revision; Escape keeps it unchanged."
	return nil
}

func (m *nativeMenu) saveRename() tea.Cmd {
	title := strings.TrimSpace(m.input.Value())
	if m.renameTree == nil || title == "" || utf8.RuneCountInString(title) > 256 || strings.IndexFunc(title, unicode.IsControl) >= 0 {
		m.message = "Enter a nonempty name of at most 256 characters without control characters."
		return nil
	}
	metadata := m.renameTree.Metadata
	metadata.Title = new(title)
	params := protocol.UpdateTreeParams{TreeID: m.renameTree.ID, ExpectedRevision: m.renameTree.Revision, Metadata: metadata}
	connection := m.connection
	return m.call("rename-saved", true, func(ctx context.Context) nativeMenuReply {
		var tree protocol.Tree
		err := connection.Call(ctx, "trees.update", params, &tree)
		if err == nil && (tree.ID != params.TreeID || tree.Metadata.Title == nil || *tree.Metadata.Title != title) {
			err = errors.New("rename acknowledgment mismatch")
		}
		return nativeMenuReply{err: err}
	})
}
