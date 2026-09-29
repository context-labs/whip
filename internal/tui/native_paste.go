package tui

import (
	"errors"
	"fmt"
	"maps"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/context-labs/whip/internal/protocol"
)

const nativeDraftLimit = 256 << 10

// These registries are presentation only. InputRecord contains the original
// text, never a chip or an instruction to resolve client-local state later.
type nativeDraft struct {
	text   string
	pastes map[string]string
	images map[string]nativeImage
}

func (d nativeDraft) bytes() int {
	n := len(d.text)
	for chip, value := range d.pastes {
		n += len(chip) + len(value)
	}
	for chip, value := range d.images {
		n += len(chip) + len(value.reference.ID) + len(value.reference.Digest) + len(value.reference.MediaType) + 256
	}
	return n
}

func (m *nativeModel) livePastes(text string) map[string]string {
	live := maps.Clone(m.pastes)
	for chip := range live {
		if !strings.Contains(text, chip) {
			delete(live, chip)
		}
	}
	return live
}

func (m *nativeModel) expandPastes(text string) (string, error) {
	n := len(text)
	pairs := make([]string, 0, 2*len(m.pastes))
	for chip, original := range m.pastes {
		n += strings.Count(text, chip) * (len(original) - len(chip))
		pairs = append(pairs, chip, original)
	}
	if n > nativeDraftLimit {
		return "", errors.New("draft exceeds the 256 KiB text limit; shorten it before sending")
	}
	// Replacer does not rescan replacements: a pasted literal matching another
	// chip remains literal, rather than expanding recursively.
	return strings.NewReplacer(pairs...).Replace(text), nil
}

func (m *nativeModel) pasteText(text string) tea.Cmd {
	expanded, err := m.expandPastes(m.input.Value())
	if err != nil || len(expanded)+len(text) > nativeDraftLimit {
		m.status = "Paste refused: the resulting draft would exceed 256 KiB. Existing text was kept."
		return nil
	}
	m.pastes = m.livePastes(m.input.Value())
	// The pinned textarea normalizes controls and caps logical lines at 10,000.
	// Preserve such pastes as chips even with collapse off, rather than changing
	// tabs/CRs or silently dropping the tail of an otherwise bounded prompt.
	needsChip := strings.IndexFunc(text, func(r rune) bool { return unicode.IsControl(r) && r != '\n' }) >= 0 || strings.Count(m.input.Value(), "\n")+strings.Count(text, "\n") >= 10000
	if needsChip || nativePreferenceLabel(m.preferences.CollapsePaste, false) == "on" && strings.Count(text, "\n") >= 2 {
		if len(m.pastes) >= 8 {
			m.status = "Paste refused: eight text chips are already in this draft. Remove or send one first."
			return nil
		}
		m.pasteSequence++
		chip := fmt.Sprintf("[Pasted text #%d · %d lines]", m.pasteSequence, strings.Count(text, "\n")+1)
		if m.pastes == nil {
			m.pastes = map[string]string{}
		}
		m.pastes[chip] = text
		m.input.InsertString(chip)
		if needsChip {
			m.status = "Paste preserved as a text chip because the editor would change its whitespace or truncate its lines."
		}
		m.sizeInput()
		return nil
	}
	var command tea.Cmd
	m.input, command = m.input.Update(tea.PasteMsg{Content: text})
	m.sizeInput()
	return command
}

// Drafts remain with their exact owner. Hidden paste bytes count toward the
// same aggregate limit as visible text; navigation never publishes them.
func (m *nativeModel) switchDraft(owner protocol.ID) (nativeDraft, error) {
	current := nativeDraft{text: m.input.Value()}
	if strings.HasPrefix(strings.TrimSpace(current.text), "/") {
		current.text = ""
	}
	current.pastes = m.livePastes(current.text)
	current.images = m.liveImages(current.text)
	bytes, count := current.bytes(), 0
	for id, draft := range m.drafts {
		if id != m.owner.ID {
			bytes += draft.bytes()
			if draft.text != "" {
				count++
			}
		}
	}
	if current.text != "" {
		count++
	}
	if current.bytes() > nativeDraftLimit || bytes > 1<<20 || count > 16 {
		return nativeDraft{}, errors.New("local draft capacity reached; save or clear a draft before switching owners")
	}
	next := m.drafts[owner]
	if m.drafts == nil {
		m.drafts = map[protocol.ID]nativeDraft{}
	}
	if current.text == "" {
		delete(m.drafts, m.owner.ID)
	} else {
		m.drafts[m.owner.ID] = current
	}
	delete(m.drafts, owner)
	if owner == m.owner.ID {
		return current, nil
	}
	return next, nil
}

func (m *nativeModel) sizeInput() {
	m.input.DynamicHeight = true
	m.input.MaxHeight = min(24, max(m.height-8-m.dockHeight(), 1))
	m.input.MaxContentHeight = nativeDraftLimit + 1
	// SetWidth recalculates the dynamic height and scroll offset in the pinned
	// editor, retaining its cursor/selection rather than rebuilding the model.
	m.input.SetWidth(max(m.transcriptWidth()-2, 1))
	m.vp.SetHeight(max(m.height-m.input.Height()-4-m.dockHeight(), 1))
	if m.follow && m.browse == nil {
		m.vp.GotoBottom()
	}
}
