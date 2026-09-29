package tui

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

type nativePendingEntry struct {
	owner    protocol.ID
	identity protocol.RequestIdentity
	method   string
	digest   string
	bytes    int
}

func nativePendingIdentity(command *client.InputCommand) (nativePendingEntry, error) {
	record := command.Record()
	record.Accepted = false
	var scope struct {
		SessionID protocol.ID              `json:"session_id"`
		Identity  protocol.RequestIdentity `json:"identity"`
	}
	if err := json.Unmarshal(record.Params, &scope); err != nil {
		return nativePendingEntry{}, err
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return nativePendingEntry{}, err
	}
	digest := sha256.Sum256(raw)
	return nativePendingEntry{owner: scope.SessionID, identity: scope.Identity, method: record.Method, digest: hex.EncodeToString(digest[:]), bytes: len(raw)}, nil
}

func (r *nativeRecovery) entries(connection *client.Client) (items []nativePendingEntry, other int, err error) {
	err = r.locked(func() error {
		directory, err := r.root.Open(".")
		if err != nil {
			return err
		}
		defer func() { _ = directory.Close() }()
		entries, err := directory.ReadDir(nativeRecoveryCount + 2)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		count, size := 0, 0
		for _, entry := range entries {
			if entry.Name() == ".write.lock" {
				continue
			}
			name := strings.TrimSuffix(entry.Name(), ".json")
			decoded, err := hex.DecodeString(name)
			if len(name) != 64 || name == entry.Name() || err != nil || hex.EncodeToString(decoded) != name {
				return errors.New("unexpected recovery file name; no records changed")
			}
			count++
			if count > nativeRecoveryCount {
				return errors.New("input recovery exceeds 64 files; no records changed")
			}
			raw, err := r.readName(entry.Name())
			if err != nil {
				return err
			}
			size += len(raw)
			if size > nativeRecoveryBytes {
				return errors.New("input recovery exceeds 32 MiB; no records changed")
			}
			var record client.InputRecord
			if err := json.Unmarshal(raw, &record); err != nil {
				return errors.New("invalid input recovery record; no records changed")
			}
			if record.Namespace != client.InputRecoveryNamespace || record.Version != 1 || record.RuntimeID == "" {
				return errors.New("invalid input recovery namespace; no records changed")
			}
			if record.RuntimeID != r.runtime {
				other++
				continue
			}
			command, err := connection.RestoreInput(raw)
			if err != nil {
				return err
			}
			owner, original, err := r.record(command)
			if err != nil {
				return err
			}
			if entry.Name() != r.name(owner) || !bytes.Equal(original, raw) {
				return errors.New("input recovery identity or immutable bytes differ; no records changed")
			}
			item, err := nativePendingIdentity(command)
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		return nil
	})
	slices.SortFunc(items, func(a, b nativePendingEntry) int { return strings.Compare(string(a.owner), string(b.owner)) })
	return items, other, err
}

func (m *nativeModel) pendingCommand(args string) tea.Cmd {
	if m.recovery == nil {
		m.status = "Local input recovery is not configured in this terminal."
		return nil
	}
	fields := strings.Fields(args)
	if len(fields) == 0 {
		fields = []string{"list"}
	}
	list := len(fields) == 1 && fields[0] == "list"
	inspect := len(fields) == 2 && fields[0] == "inspect"
	check := len(fields) == 2 && fields[0] == "check"
	forget := len(fields) == 5 && fields[0] == "forget"
	if !list && !inspect && !check && !forget {
		m.status = "usage: /pending list|inspect <owner>|check <owner>|forget <owner> <client ID> <request ID> <digest>"
		return nil
	}
	r, connection, uncertain := m.recovery, m.connection, m.uncertain
	// The intent and any in-memory fallback are captured before the local
	// operation begins. Neither a read nor explicit forget ever invokes Retry.
	return m.controlKeepingDraft("Saved local input", false, func(ctx context.Context) nativeControlResult {
		if list {
			items, other, err := r.entries(connection)
			if err != nil {
				return nativeControlResult{err: err}
			}
			var text strings.Builder
			fmt.Fprintf(&text, "Saved inputs for runtime %s · %d pending · %d records for other runtimes kept\nThese are local immutable intents, not host execution state.\n/pending inspect <owner> reads original bytes; check reads only its original receipt.\nExplicit forget only removes the matching local record; it never cancels accepted host work.", r.runtime, len(items), other)
			for _, item := range items {
				fmt.Fprintf(&text, "\n\nOwner %s · %s · %d bytes\nClient %s · request %s\nDigest %s", item.owner, item.method, item.bytes, item.identity.ClientID, item.identity.RequestID, item.digest)
			}
			return nativeControlResult{notice: nativeBoundedNotice(text.String())}
		}
		owner := protocol.ID(fields[1])
		command, err := r.restore(connection, owner)
		if err != nil {
			return nativeControlResult{err: err}
		}
		if command == nil && forget && uncertain != nil {
			// A prior unlink may have succeeded before directory-sync failed.
			// The explicit identity below still has to match the frozen intent.
			command = uncertain
		}
		if command == nil {
			return nativeControlResult{notice: "No saved local input for owner " + string(owner) + ". Nothing was sent or forgotten."}
		}
		item, err := nativePendingIdentity(command)
		if err != nil || item.owner != owner {
			return nativeControlResult{err: errors.New("saved input owner does not match")}
		}
		if inspect {
			var formatted bytes.Buffer
			if err := json.Indent(&formatted, command.Record().Params, "", "  "); err != nil {
				return nativeControlResult{err: err}
			}
			return nativeControlResult{notice: nativeBoundedNotice(fmt.Sprintf("Saved %s intent for %s\nDigest %s\nNothing was sent. Original parameters:\n%s", item.method, owner, item.digest, formatted.String()))}
		}
		if forget {
			if item.identity.ClientID != protocol.ID(fields[2]) || item.identity.RequestID != protocol.ID(fields[3]) || item.digest != fields[4] {
				return nativeControlResult{err: errors.New("saved input identity or digest changed; inspect the retained record before forgetting")}
			}
			if err := r.clear(command); err != nil {
				return nativeControlResult{err: err}
			}
			return nativeControlResult{recoveryCleared: command, notice: "Forgot only the matching local intent for " + string(owner) + ". Accepted host work was not cancelled or resent."}
		}
		admission, found, err := command.Check(ctx)
		if err != nil {
			return nativeControlResult{err: err}
		}
		if !found {
			return nativeControlResult{notice: "No matching receipt found for " + string(owner) + "; original local intent kept. Nothing was resent."}
		}
		if err := r.clear(command); err != nil {
			return nativeControlResult{err: fmt.Errorf("receipt confirmed; local cleanup failed: %w", err)}
		}
		state := "accepted"
		if admission.Receipt.DeletedAt != nil {
			state = "deleted (retained receipt)"
		} else if admission.Turn != nil {
			state += " · " + admission.Turn.State
		}
		return nativeControlResult{recoveryCleared: command, notice: fmt.Sprintf("Original input for %s: %s. Matching local intent cleared; nothing was resent.", owner, state)}
	})
}

func (m *nativeModel) clearPendingMemory(command *client.InputCommand) {
	if command == nil || m.uncertain == nil {
		return
	}
	cleared, err := nativePendingIdentity(command)
	if err != nil {
		return
	}
	current, err := nativePendingIdentity(m.uncertain)
	if err == nil && current == cleared {
		m.uncertain, m.recoveryCheck = nil, false
	}
}
