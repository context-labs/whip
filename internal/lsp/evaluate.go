package lsp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxDocumentBytes    = 256 << 10
	maxDocuments        = 128
	maxDocumentsBytes   = 4 << 20
	maxDiagnosticFiles  = 128
	maxDiagnosticOutput = 32 << 10
)

// Result is observational evidence. An unavailable diagnostic result does not
// change whether the preceding file publication succeeded.
type Result struct {
	State     string `json:"state"`
	Output    string `json:"output"`
	Truncated bool   `json:"truncated"`
	Reason    string `json:"reason"`
}

func (m *Manager) begin(ctx context.Context) (context.Context, func(), error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, nil, errors.New("language server manager closed")
	}
	m.calls.Add(1)
	m.mu.Unlock()
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(m.ctx, cancel)
	finish := func() { stop(); cancel(); m.calls.Done() }
	select {
	case m.callSlot <- struct{}{}:
		return ctx, func() { <-m.callSlot; finish() }, nil
	case <-ctx.Done():
		finish()
		return nil, nil, ctx.Err()
	}
}

func within(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && (relative == "." || filepath.IsLocal(relative))
}

// Diagnostics accepts the caller's bounded captured content. New-core callers
// authorize the workspace and record durable dispatch before invoking it.
func (m *Manager) Diagnostics(ctx context.Context, path, text string) (Result, error) {
	ctx, finish, err := m.begin(ctx)
	if err != nil {
		return Result{}, err
	}
	defer finish()
	if len(text) > maxDocumentBytes || !utf8.ValidString(text) || len(path) > 4096 {
		return Result{State: "unavailable", Reason: "document exceeds the 256 KiB text limit"}, nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return Result{}, err
	}
	m.mu.Lock()
	workspace, identity := m.workspace, m.workspaceInfo
	m.mu.Unlock()
	if workspace != "" {
		canonical, resolveErr := filepath.EvalSymlinks(abs)
		current, statErr := os.Stat(workspace)
		if resolveErr != nil || statErr != nil || identity == nil || !os.SameFile(identity, current) || !within(workspace, canonical) {
			return Result{}, errors.New("language server workspace identity or path changed")
		}
		abs = canonical
	}
	cs, err := m.clientFor(ctx, abs)
	if err != nil {
		return Result{State: "unavailable", Reason: "language server startup failed"}, err
	}
	if cs == nil {
		return Result{State: "unavailable", Reason: "no enabled language server covers this file"}, nil
	}
	m.mu.Lock()
	if cs.docBytes == nil {
		cs.docBytes = map[string]int{}
	}
	count, total := 0, 0
	for _, client := range m.clients {
		count += len(client.docs)
		total += client.bytes
	}
	if (cs.docs[abs] == 0 && count >= maxDocuments) || total-cs.docBytes[abs]+len(text) > maxDocumentsBytes {
		m.mu.Unlock()
		return Result{State: "unavailable", Truncated: true, Reason: "language server document capacity reached (128 documents or 4 MiB)"}, nil
	}
	cs.bytes += len(text) - cs.docBytes[abs]
	cs.docBytes[abs] = len(text)
	cs.docs[abs]++
	version := cs.docs[abs]
	delete(m.diags, abs)
	delete(m.truncated, abs)
	wake := make(chan struct{})
	m.waiters[abs] = []chan struct{}{wake}
	m.mu.Unlock()
	defer func() { m.mu.Lock(); delete(m.waiters, abs); m.mu.Unlock() }()
	if version == 1 {
		err = cs.cli.notifyContext(ctx, "textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": fileURI(abs), "languageId": strings.TrimPrefix(filepath.Ext(abs), "."), "version": version, "text": text}})
	} else {
		err = cs.cli.notifyContext(ctx, "textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": fileURI(abs), "version": version}, "contentChanges": []map[string]any{{"text": text}}})
	}
	if err != nil {
		return Result{State: "unavailable", Reason: "language server notification failed"}, err
	}
	timer := time.NewTimer(diagWait)
	defer timer.Stop()
	select {
	case <-wake:
	case <-ctx.Done():
		return Result{}, ctx.Err()
	case <-cs.cli.dead:
		return Result{State: "unavailable", Reason: "language server connection closed"}, nil
	case <-timer.C:
		return Result{State: "unavailable", Reason: "diagnostics did not arrive within 1.5 seconds"}, nil
	}
	// Servers commonly send sibling diagnostics immediately after the edited file.
	grace := time.NewTimer(50 * time.Millisecond)
	defer grace.Stop()
	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	case <-grace.C:
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Result{}, errors.New("language server manager closed")
	}
	siblings := siblingErrors(abs, m.diags)
	result := Result{State: "ready", Output: Report(abs, m.diags[abs], siblings), Truncated: m.cacheTruncated || m.truncated[abs] || len(siblings) > maxSiblingFiles}
	for path := range siblings {
		result.Truncated = result.Truncated || m.truncated[path]
	}
	if len(result.Output) > maxDiagnosticOutput {
		result.Output = result.Output[:maxDiagnosticOutput]
		for !utf8.ValidString(result.Output) {
			result.Output = result.Output[:len(result.Output)-1]
		}
		result.Truncated = true
	}
	if result.Truncated {
		result.Reason = "diagnostic count, message or output limit reached"
	}
	return result, nil
}
