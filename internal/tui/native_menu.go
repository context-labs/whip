package tui

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/tui/ui"
)

type nativeMenuOptions struct {
	Kind                 string
	Owner                *protocol.Session
	PreferencesDirectory string
}

type nativeMenuChoice struct {
	id, label, detail string
}

// nativeMenu owns one visible dialog, not host work. Its lifecycle cancels
// reads and detaches in-flight mutations without reconstructing or replaying
// them. All calls join the terminal's existing nativeWork owner.
type nativeMenu struct {
	setup                   nativeSetup
	setupProvider, setupKey string
	renameTree              *protocol.Tree
	work                    *nativeWork
	connection              *client.Client
	lifecycle               context.Context
	close                   context.CancelFunc
	stopCall                context.CancelFunc
	generation              uint64
	options                 nativeMenuOptions
	owner                   *protocol.Session
	mode                    string
	title                   string
	message                 string
	input                   textinput.Model
	choices                 []nativeMenuChoice
	selected                int
	busy, done              bool
	inventory               protocol.ProviderInventory
	catalog                 protocol.ProviderCatalog
	provider                protocol.ID
	selection               protocol.ModelSelection
	modelChoices            []protocol.ProviderModel
	preferences             nativePreferences
	originalTheme           string
	previewing              bool
}

type nativeMenuReply struct {
	tree            *protocol.Tree
	presets         []protocol.ProviderPreset
	openAI          *protocol.OpenAILoginFlow
	inference       *protocol.InferenceFlow
	openAIStatus    *protocol.OpenAIAccountStatus
	inferenceStatus *protocol.InferenceAccountStatus
	openAIFlows     []protocol.OpenAILoginFlow
	inferenceFlows  []protocol.InferenceFlow
	cleanup         *protocol.InferenceCleanupResult
	menu            *nativeMenu
	generation      uint64
	kind            string
	mutation        bool
	inventory       *protocol.ProviderInventory
	catalog         *protocol.ProviderCatalog
	owner           *protocol.Session
	message         string
	err             error
}

type nativeMenuInput struct {
	menu       *nativeMenu
	generation uint64
	message    tea.Msg
}

func newNativeMenu(work *nativeWork, connection *client.Client, options nativeMenuOptions) *nativeMenu {
	lifecycle, closeMenu := context.WithCancel(work.ctx)
	input := textinput.New()
	input.Prompt = ""
	input.CharLimit = 256
	input.Focus()
	m := &nativeMenu{
		work: work, connection: connection, lifecycle: lifecycle, close: closeMenu,
		options: options, input: input, choices: []nativeMenuChoice{}, modelChoices: []protocol.ProviderModel{},
	}
	if options.Owner != nil {
		owner := *options.Owner
		m.owner = &owner
	}
	return m
}

func (m *nativeMenu) Init() tea.Cmd {
	switch m.options.Kind {
	case "rename":
		return m.readRename()
	case "setup":
		return m.readSetup()
	case "model", "model-for-session":
		m.mode, m.title = "model-providers", "Choose model provider"
		return m.readInventory()
	case "settings", "theme":
		m.openLocalSettings()
		return nil
	default:
		m.message = "Unknown native menu"
		return nil
	}
}

func (m *nativeMenu) Done() bool { return m.done }

// Owner returns only acknowledged session configuration evidence.
func (m *nativeMenu) Owner() *protocol.Session { return m.owner }

func (m *nativeMenu) Close() {
	if m.done {
		return
	}
	m.done = true
	m.generation++
	m.close()
	if m.stopCall != nil {
		m.stopCall()
	}
	m.input.Reset()
	m.setupKey = ""
	if m.previewing {
		setSchemeOverride(m.originalTheme)
		m.previewing = false
	}
}

// Handles only this menu's replies and foreground input. Native transcript and
// decision observations continue through the main model while the dialog is open.
func (m *nativeMenu) Handles(message tea.Msg) bool {
	switch value := message.(type) {
	case nativeMenuReply:
		return value.menu == m
	case nativeMenuPoll:
		return value.menu == m
	case nativeMenuInput:
		return value.menu == m
	case tea.KeyPressMsg:
		return !m.done && value.String() != "ctrl+c"
	case tea.PasteMsg:
		return !m.done
	default:
		return false
	}
}

func (m *nativeMenu) call(kind string, mutation bool, operation func(context.Context) nativeMenuReply) tea.Cmd {
	if m.stopCall != nil {
		m.stopCall()
	}
	ctx, cancel := context.WithCancel(m.lifecycle)
	m.stopCall = cancel
	m.generation++
	generation := m.generation
	m.busy, m.message = true, ""
	return func() tea.Msg {
		defer cancel()
		bounded, done, err := m.work.beginFor(30 * time.Second)
		if err != nil {
			return nativeMenuReply{menu: m, generation: generation, kind: kind, mutation: mutation, err: err}
		}
		defer done()
		bounded, stop := context.WithCancel(bounded)
		defer stop()
		after := context.AfterFunc(ctx, stop)
		defer after()
		var result nativeMenuReply
		if err := ctx.Err(); err != nil {
			result.err = err
		} else {
			result = operation(bounded)
		}
		result.menu, result.generation, result.kind, result.mutation = m, generation, kind, mutation
		return result
	}
}

func (m *nativeMenu) inputCommand(command tea.Cmd) tea.Cmd {
	if command == nil {
		return nil
	}
	generation := m.generation
	return func() tea.Msg { return nativeMenuInput{menu: m, generation: generation, message: command()} }
}

func (m *nativeMenu) Update(message tea.Msg) tea.Cmd {
	if m.done {
		return nil
	}
	switch value := message.(type) {
	case nativeMenuPoll:
		if value.menu == m && value.generation == m.generation && !m.busy && m.mode == "account-flow" {
			return m.refreshAccount()
		}
		return nil
	case nativeMenuReply:
		if value.menu != m || value.generation != m.generation {
			return nil
		}
		m.busy = false
		if value.inventory != nil {
			m.inventory = *value.inventory
		}
		if value.owner != nil && m.owner != nil && value.owner.ID == m.owner.ID && value.owner.ConfigRevision >= m.owner.ConfigRevision {
			m.owner = value.owner
		}
		if value.err != nil {
			m.message = nativeDisplayText(value.err.Error())
			var rpcError *client.Error
			if value.mutation && !errors.As(value.err, &rpcError) {
				m.message += ". Outcome unknown. Refresh to inspect; no action was replayed."
				if strings.HasPrefix(value.kind, "setup-") || strings.HasPrefix(value.kind, "account-") {
					m.setupUnknown()
				}
			}
			return nil
		}
		if strings.HasPrefix(value.kind, "setup-") || strings.HasPrefix(value.kind, "account-") {
			return m.setupReply(value)
		}
		if strings.HasPrefix(value.kind, "rename-") {
			return m.renameReply(value)
		}
		return m.modelReply(value)
	case nativeMenuInput:
		if value.menu != m || value.generation != m.generation || m.busy {
			return nil
		}
		return m.edit(value.message)
	case tea.KeyPressMsg:
		if value.String() == "esc" {
			m.Close()
			return nil
		}
		if m.busy {
			return nil
		}
		if value.String() == "ctrl+r" {
			return m.refreshMenu()
		}
		choices := m.visibleChoices()
		switch value.String() {
		case "up", "shift+tab":
			if len(choices) > 0 {
				m.selected = (m.selected + len(choices) - 1) % len(choices)
				m.previewTheme()
			}
			return nil
		case "down", "tab":
			if len(choices) > 0 {
				m.selected = (m.selected + 1) % len(choices)
				m.previewTheme()
			}
			return nil
		case "enter":
			if m.mode == "rename" {
				return m.saveRename()
			}
			if m.setupInput() {
				return m.submitSetup()
			}
			if m.mode == "model-id" {
				id := strings.TrimSpace(m.input.Value())
				if id != "" {
					m.selectModel(id)
				}
				return nil
			}
			if len(choices) > 0 {
				return m.choose(choices[min(m.selected, len(choices)-1)])
			}
			return nil
		}
		return m.edit(value)
	case tea.PasteMsg:
		if !m.busy {
			return m.edit(value)
		}
	}
	return nil
}

func (m *nativeMenu) edit(message tea.Msg) tea.Cmd {
	before := m.input.Value()
	var command tea.Cmd
	m.input, command = m.input.Update(message)
	if before != m.input.Value() {
		m.selected = 0
		m.previewTheme()
	}
	return m.inputCommand(command)
}

func (m *nativeMenu) visibleChoices() []nativeMenuChoice {
	filter := strings.ToLower(strings.TrimSpace(m.input.Value()))
	choices := make([]nativeMenuChoice, 0, len(m.choices))
	for _, choice := range m.choices {
		if nativeMenuMatch(choice, filter) >= 0 {
			choices = append(choices, choice)
		}
	}
	slices.SortStableFunc(choices, func(a, b nativeMenuChoice) int { return nativeMenuMatch(a, filter) - nativeMenuMatch(b, filter) })
	return choices
}

func nativeMenuMatch(choice nativeMenuChoice, filter string) int {
	return matchTier(choice.label+" "+choice.id, filter)
}

func (m *nativeMenu) View(width, height int) string {
	width, height = max(width, 8), max(height, 4)
	choices := m.visibleChoices()
	items := make([]ui.ListItem, 0, len(choices))
	for _, choice := range choices {
		item := ui.ListItem{Left: nativeDisplayText(choice.label)}
		if m.mode == "theme" {
			item.Swatch = nativeThemeSwatch(choice.id)
		}
		items = append(items, item)
	}
	message := m.message
	if len(choices) > 0 {
		detail := choices[min(m.selected, len(choices)-1)].detail
		if detail != "" {
			message = detail + "\n" + message
		}
	}
	if m.busy {
		message = "Working… Esc closes this dialog; accepted host work may continue."
	}
	m.input.SetWidth(max(1, min(width-8, 60)))
	if height < 12 {
		rows := []string{m.title, m.input.View()}
		if len(choices) > 0 {
			rows = append(rows, "> "+choices[min(m.selected, len(choices)-1)].label)
		}
		rows = append(rows, strings.Split(message, "\n")...)
		rows = rows[:min(len(rows), height)]
		for i, row := range rows {
			rows[i] = ansi.Truncate(nativeDisplayText(row), width, "…")
		}
		return strings.Join(rows, "\n")
	}
	rows := ui.List{
		Title: m.title, Hint: "esc", Search: true, SearchView: m.input.View(),
		Groups: []ui.ListGroup{{Items: items}}, Sel: m.selected, Width: min(width, 72), Height: max(height-3, 4),
		Empty: "No matching choices", Footer: []string{"↑↓", "choose", "enter", "select", "ctrl+r", "refresh"},
	}.Render(currentTheme())
	rows = append(rows, strings.Split(ansi.Wrap(nativeDisplayText(message), max(width-2, 1), ""), "\n")...)
	rows = rows[:min(len(rows), height)]
	for i, row := range rows {
		rows[i] = ansi.Truncate(row, width, "…")
	}
	return strings.Join(rows, "\n")
}
