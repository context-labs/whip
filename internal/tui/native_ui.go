package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

// nativeWork joins terminal-owned readers on detach. InputCommand owns the
// admission send independently; cancelling this observation never cancels a
// host turn. The caller separately closes its borrowed native Client.
type nativeWork struct {
	ctx    context.Context
	stop   context.CancelFunc
	mu     sync.Mutex
	closed bool
	active int
	wg     sync.WaitGroup
}

func (w *nativeWork) begin() (context.Context, func(), error) {
	return w.beginFor(10 * time.Second)
}

func (w *nativeWork) beginFor(timeout time.Duration) (context.Context, func(), error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil, nil, errors.New("terminal detached")
	}
	if err := w.ctx.Err(); err != nil {
		return nil, nil, err
	}
	if w.active == 4 {
		return nil, nil, errors.New("terminal request capacity is busy")
	}
	w.active++
	w.wg.Add(1)
	ctx, cancel := context.WithTimeout(w.ctx, timeout)
	return ctx, func() { cancel(); w.mu.Lock(); w.active--; w.mu.Unlock(); w.wg.Done() }, nil
}

func (w *nativeWork) close() { w.mu.Lock(); w.closed = true; w.stop(); w.mu.Unlock(); w.wg.Wait() }

// nativeModel is the native chat composition. Commands and menus are added
// directly over typed host operations; it does not adapt retired RootActions.
type nativeModel struct {
	localFilesystem                 bool
	execution                       *nativeExecution
	replBefore, replFocus           *protocol.ID
	replGeneration                  uint64
	replFocused                     bool
	replVP                          transcriptView
	replDisplay                     []string
	agents                          *nativeAgentTree
	agentSelection                  protocol.ID
	agentsFocus, dock               bool
	drafts                          map[protocol.ID]nativeDraft
	pastes                          map[string]string
	pasteSequence                   uint64
	images                          map[string]nativeImage
	imageSequence                   uint64
	attachment                      *nativeImageUpload
	attachmentBusy                  bool
	historyDialog                   *nativeHistoryDialog
	redraft                         *nativeRedraft
	draftDesign                     *protocol.DesignContext
	selection                       *nativeSelection
	selectionClick                  nativeSelectionClick
	messageRows                     []nativeMessageRows
	toolExpansion                   map[protocol.ID]bool
	clipboard                       *nativeClipboardOwner
	copyBusy                        bool
	clientDirectory                 string
	completion                      *nativeCompletion
	palette                         *nativeCommandPalette
	leaderAt                        time.Time
	work                            nativeWork
	connection                      *client.Client
	handle                          *client.Session
	owner                           protocol.Session
	permissionPolicy                *protocol.PermissionPolicy
	observer                        *client.Observer
	readCancel                      context.CancelFunc
	picker                          *nativeSessionPicker
	menu                            *nativeMenu
	preferencesDirectory            string
	preferences                     nativePreferences
	initialPrompt                   string
	recovery                        *nativeRecovery
	recoveryCheck                   bool
	navigationRequest               uint64
	history                         nativeTranscript
	activity                        protocol.SessionActivity
	usage                           protocol.Usage
	contextUsage                    protocol.ContextUsage
	input                           textarea.Model
	vp                              transcriptView
	rows                            []string
	width, height                   int
	ready, reading, sending, follow bool
	polls                           int
	status                          string
	uncertain                       *client.InputCommand
	rejected                        *client.InputCommand
	quitArmed                       bool
	cancelling                      bool
	controlling                     bool
	retryControl                    tea.Cmd
	generation                      uint64
	notesHome                       string
	noteRevisions                   [2]string
	notice                          string
	standingDraft                   *protocol.WriteHostStandingInstructionsParams
	decisions                       []nativeDecision
	decision                        *nativeDecisionDialog
	hiddenDecision                  *nativeDecisionDialog
	decisionsHidden                 bool
	browse                          *nativeBrowse
	browsing                        bool
	browseRequest                   uint64
	renderCache                     nativeRenderCache
	expandTools, showReasoning      bool
}

type (
	nativePoll struct{}
	nativeRead struct {
		execution           *nativeExecution
		executionGeneration uint64
		agents              *nativeAgentTree
		generation          uint64
		owner               *protocol.Session
		activity            protocol.SessionActivity
		page                *protocol.HistoryPageResult
		observation         *client.Observation
		observer            *client.Observer
		output              protocol.CellOutput
		usage               *protocol.Usage
		context             *protocol.ContextUsage
		err                 error
		evidenceError       error
		decisions           *nativeDecisionPage
	}
)

type nativeSubmission struct {
	command       *client.InputCommand
	admission     protocol.Admission
	err           error
	uncertain     bool
	recoveryError error
}
type nativeCancelled struct {
	generation uint64
	turn       protocol.ID
	err        error
}

func newNativeModel(ctx context.Context, c *client.Client, owner protocol.Session) (*nativeModel, error) {
	if c == nil {
		return nil, errors.New("native client is required")
	}
	handle, err := c.Session(owner.ID)
	if err != nil {
		return nil, err
	}
	lifecycle, cancel := context.WithCancel(ctx)
	localDirectory, _ := os.Getwd() // Relative attachment paths fail closed if unavailable.
	return &nativeModel{
		work: nativeWork{ctx: lifecycle, stop: cancel}, connection: c, handle: handle, owner: owner,
		history: nativeTranscript{owner: owner.ID}, input: newInput(), width: 80, height: 24, follow: true,
		clientDirectory: localDirectory,
	}, nil
}

func (m *nativeModel) Init() tea.Cmd {
	if m.menu != nil {
		return tea.Batch(m.read(), m.menu.Init())
	}
	return m.read()
}

func (m *nativeModel) read() tea.Cmd {
	if m.reading {
		return nil
	}
	m.reading = true
	readScope, readStop := context.WithCancel(m.work.ctx)
	m.readCancel = readStop
	observer, handle, generation, owner := m.observer, m.handle, m.generation, m.owner
	evidence := m.polls%5 == 0
	agentsVisible, selectedAgent := m.agentsVisible(), m.agentSelection
	replVisible, replBefore, replFocus, replGeneration := m.replVisible(), m.replBefore, m.replFocus, m.replGeneration
	m.polls++
	return func() tea.Msg {
		defer readStop()
		ctx, done, err := m.work.begin()
		if err != nil {
			return nativeRead{generation: generation, err: err}
		}
		defer done()
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		stop := context.AfterFunc(readScope, cancel)
		defer stop()
		if err := readScope.Err(); err != nil {
			return nativeRead{generation: generation, err: err}
		}
		result := nativeRead{generation: generation, observer: observer}
		if observer == nil || evidence {
			owner, err := handle.Get(ctx)
			if err != nil {
				result.err = err
				return result
			}
			result.owner = &owner
		}
		result.activity, result.err = handle.Activity(ctx)
		if result.err != nil {
			return result
		}
		load := func() error {
			page, err := handle.History(ctx, protocol.HistoryPageParams{Direction: "backward", Limit: 64})
			if err != nil {
				return err
			}
			result.page = &page
			result.observer, err = handle.Observer(client.ObservationCursor{After: page.Snapshot.ThroughSequence, Revision: new(page.Snapshot.Revision)})
			return err
		}
		if observer == nil {
			result.err = load()
		} else {
			page, err := observer.Next(ctx)
			result.err = err
			if err == nil {
				if page.Reset {
					result.err = load()
				} else {
					result.observation = &page
				}
			}
		}
		if result.err != nil {
			return result
		}
		// Primary history is delivered even if a later auxiliary read fails.
		// Otherwise Observer's advanced cursor would silently skip messages.
		result.output, result.evidenceError = handle.CellOutput(ctx)
		if evidence {
			result.decisions, err = readNativeDecisions(ctx, m.connection, owner)
			result.evidenceError = errors.Join(result.evidenceError, err)
			if err != nil {
				result.decisions = nil
			}
			usage, err := handle.Usage(ctx)
			if err == nil {
				result.usage = &usage
			} else {
				result.evidenceError = errors.Join(result.evidenceError, err)
			}
			if agentsVisible {
				result.agents, err = readNativeAgents(ctx, m.connection, owner, selectedAgent)
				result.evidenceError = errors.Join(result.evidenceError, err)
			}
			if replVisible {
				snapshot := result.observation
				revision := owner.HistoryRevision
				if result.page != nil {
					revision = result.page.Snapshot.Revision
				} else if snapshot != nil {
					revision = snapshot.Snapshot.Revision
				}
				result.executionGeneration = replGeneration
				result.execution, err = readNativeExecution(ctx, m.connection, owner.ID, revision, result.output.Epoch, replBefore, replFocus)
				if err != nil {
					result.execution = &nativeExecution{owner: owner.ID, revision: revision, epoch: result.output.Epoch, err: err}
				}
				result.evidenceError = errors.Join(result.evidenceError, err)
			}
			value, err := handle.ContextUsage(ctx)
			if err == nil {
				result.context = &value
			} else {
				result.evidenceError = errors.Join(result.evidenceError, err)
			}
		}
		return result
	}
}

func nativeTick() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(time.Time) tea.Msg { return nativePoll{} })
}

func (m *nativeModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message.(type) {
	case tea.KeyPressMsg, tea.PasteMsg, tea.WindowSizeMsg:
		m.selection = nil
		m.selectionClick = nativeSelectionClick{}
	}
	if mouse, ok := message.(tea.MouseMsg); ok {
		if command, handled := m.selectionMouse(mouse); handled {
			return m, command
		}
	}
	if m.menu != nil && m.menu.Handles(message) {
		return m, m.updateMenu(message)
	}
	switch value := message.(type) {
	case nativeSelectionTick:
		return m, m.selectionScroll(value)
	case nativeCopyResult:
		m.copied(value)
		return m, nil
	case nativeCompletionResult:
		m.applyCompletion(value)
		return m, nil
	case nativeImageLoaded:
		return m, m.imageLoaded(value)
	case nativeImageUploaded:
		m.imageUploaded(value)
		return m, nil
	case nativeNavigationResult:
		if value.request != m.navigationRequest {
			return m, nil
		}
		return m.Update(value.value)
	case tea.WindowSizeMsg:
		m.width, m.height = max(value.Width, 8), max(value.Height, 4)
		m.refresh()
	case nativeBrowseResult:
		m.applyBrowse(value)
	case nativePoll:
		return m, m.read()
	case nativeRead:
		if value.generation != m.generation {
			return m, nativeTick()
		}
		m.reading = false
		if value.err != nil {
			m.ready = false
			m.status = "Connection read failed: " + value.err.Error()
			return m, nativeTick()
		}
		var err error
		renderChanged := value.page != nil || value.observation != nil && (len(value.observation.Messages) > 0 || value.observation.Preview != nil) || m.history.preview != nil || m.history.cellOutput != nil || value.output.Preview != nil
		if value.page != nil {
			err = m.history.replace(*value.page)
		}
		if err == nil && value.observation != nil {
			err = m.history.observe(*value.observation)
		}
		if err != nil {
			// Reload a bounded canonical page; never continue past a failed
			// projection with the Observer cursor that already advanced.
			m.ready, m.observer = false, nil
			m.status = "Transcript reload required: " + err.Error()
			return m, nativeTick()
		}
		m.ready, m.observer, m.activity = true, value.observer, value.activity
		if value.owner != nil && value.owner.ID == m.owner.ID && value.owner.ConfigRevision >= m.owner.ConfigRevision {
			m.owner = *value.owner
		}
		m.history.output(value.output)
		if m.browse != nil && m.browse.transcript.snapshot.Revision != m.history.snapshot.Revision {
			m.latest()
			m.status = "History changed; the older page was closed."
		}
		if value.usage != nil {
			m.usage = *value.usage
		}
		if value.context != nil {
			m.contextUsage = *value.context
		}
		if value.evidenceError != nil {
			m.status = "Some live evidence is unavailable: " + value.evidenceError.Error()
		}
		if m.execution != nil && (m.execution.revision != m.history.snapshot.Revision || m.execution.epoch != m.history.epoch) {
			m.execution = nil
		}
		if value.execution != nil && value.executionGeneration == m.replGeneration {
			m.execution = value.execution
			renderChanged = true
		}
		if value.agents != nil && value.agents.tree == m.owner.TreeID {
			m.agents = value.agents
			if m.agentSelection == "" {
				m.agentSelection = m.owner.ID
			}
			renderChanged = true
		}
		if value.decisions != nil {
			m.applyDecisions(value.decisions)
		}
		if renderChanged {
			m.refresh()
		}
		if m.recoveryCheck && m.uncertain != nil && !m.sending {
			m.recoveryCheck = false
			return m, tea.Batch(nativeTick(), m.sendInput(m.uncertain, "check"))
		}
		if m.menu == nil && m.initialPrompt != "" && m.owner.Configuration.Model.Provider != "" && m.owner.Configuration.Model.Name != "" {
			text := m.initialPrompt
			m.initialPrompt = ""
			return m, tea.Batch(nativeTick(), m.prompt(text, "auto"))
		}
		return m, nativeTick()
	case nativeSubmission:
		m.sending = false
		if value.recoveryError != nil {
			m.uncertain = value.command
			outcome := "Input accepted"
			if value.err != nil {
				outcome = "Input rejected: " + value.err.Error()
			} else if value.admission.Input == nil {
				outcome = "Original input was deleted"
			}
			m.status = outcome + "; local recovery cleanup failed: " + value.recoveryError.Error() + ". /check rereads the original receipt and retries cleanup only."
		} else if value.uncertain {
			m.uncertain = value.command
			m.status = "Input acceptance is uncertain; inspect the original request before submitting again. " + value.err.Error()
		} else if value.err != nil {
			m.status = "Input rejected: " + value.err.Error()
			if value.command != nil && value.command.Record().Method == "sessions.submit" {
				m.rejected = value.command
			}
			if m.input.Value() == "" && m.restoreRejectedDraft() {
				m.status += ". Original draft restored."
			} else if m.rejected != nil {
				m.status += ". Rejected draft retained: /rejected restore or /rejected discard."
			}
		} else {
			m.uncertain = nil
			if value.admission.Input == nil {
				m.status = "The original input was deleted; it has not been resubmitted."
			} else {
				m.status = "Accepted input " + string(value.admission.Input.ID)
			}
		}
	case nativeStandingResult:
		m.controlling = false
		if value.err != nil {
			m.standingDraft = value.draft
			m.status = "Standing instructions: " + value.err.Error()
			if value.draft != nil {
				m.status += " Draft retained: /me draft, /me retry (same revision), or /me discard."
			}
		} else {
			m.standingDraft = nil
			m.notice = ""
			m.status = "Standing instructions saved. Authorized sessions capture them on their next turn."
			m.refresh()
		}
	case nativeNotesResult:
		m.controlling = false
		if value.err != nil {
			m.status = "Local notes: " + value.err.Error() + "; list again before any further edit."
			m.noteRevisions = [2]string{}
		} else {
			for i, snapshot := range value.values {
				if snapshot != nil {
					m.noteRevisions[i] = snapshot.Revision
				}
			}
			m.status = "Client-local notes"
			m.notice = nativeNotesText(value.values)
			m.refresh()
		}
	case nativeControlResult:
		if value.generation != m.generation {
			return m, nil
		}
		m.controlling = false
		if value.mutation {
			m.retryControl = nil
		}
		if value.err != nil {
			m.status = value.label + ": " + value.err.Error()
			if _, definitive := errors.AsType[*client.Error](value.err); !definitive && value.retry != nil {
				m.retryControl = value.retry
				m.status += " Outcome may be unknown; /retry repeats only this original control."
			} else if !definitive && value.inspectOnError {
				m.status += " Outcome may be unknown; /status reads current lifecycle. This action is never replayed."
			}
		} else {
			m.status = value.label
			if value.input != nil {
				return m, m.submitPreparedInput(value.input)
			}
			if value.attach != nil {
				if err := m.attachSession(*value.attach); err != nil {
					m.status = "Session attachment failed: " + err.Error()
					return m, nil
				}
				m.stageRedraft(value.redraft)
				return m, m.read()
			}
			if value.policy != nil && value.policy.TreeID == m.owner.TreeID && (m.permissionPolicy == nil || value.policy.Revision >= m.permissionPolicy.Revision) {
				m.permissionPolicy = value.policy
			}
			if value.picker != nil {
				m.picker = value.picker
			}
			if value.decisionID != "" {
				m.decision = nil
				m.applyDecisions(&nativeDecisionPage{items: slices.DeleteFunc(m.decisions, func(item nativeDecision) bool { return item.id == value.decisionID })})
			}
			if value.notice != "" {
				m.notice = nativeBoundedNotice(value.notice)
				m.refresh()
			}
			if value.owner != nil && value.owner.ID == m.owner.ID && value.owner.ConfigRevision >= m.owner.ConfigRevision {
				m.owner = *value.owner
			}
			if value.reset {
				m.invalidateRead()
				m.browseRequest++
				m.browse, m.browsing = nil, false
				m.history = nativeTranscript{owner: m.handle.ID()}
				m.ready, m.observer = false, nil
				m.refresh()
			}
		}
		if value.err == nil {
			m.stageRedraft(value.redraft)
		}
	case nativeCancelled:
		if value.generation != m.generation {
			return m, nil
		}
		m.cancelling = false
		if value.err != nil {
			m.status = "Cancel " + string(value.turn) + ": " + value.err.Error()
		} else {
			m.status = "Cancellation requested for turn " + string(value.turn)
		}
	case tea.KeyPressMsg:
		if value.String() != "ctrl+c" {
			m.initialPrompt = ""
		}
		if m.historyDialog != nil && value.String() != "ctrl+c" {
			return m, m.historyDialogKey(value)
		}
		if m.picker != nil && value.String() != "ctrl+c" {
			return m, m.pickerKey(value)
		}
		if m.decision != nil && value.String() != "ctrl+c" {
			return m, m.decisionKey(value)
		}
		if m.palette != nil && value.String() != "ctrl+c" {
			return m, m.paletteKey(value)
		}
		if value.String() == "tab" && len(m.decisions) > 0 {
			m.closeCompletion(false)
			m.decisionsHidden = false
			if m.hiddenDecision != nil {
				m.decision, m.hiddenDecision = m.hiddenDecision, nil
			} else {
				m.decision = newNativeDecision(m.decisions[0], m.width)
			}
			return m, nil
		}
		if value.String() != "ctrl+c" {
			if command, handled := m.completionKey(value); handled {
				return m, command
			}
			if command, handled := m.shortcut(value); handled {
				return m, command
			}
		}
		if command, handled := m.agentKey(value); handled {
			return m, command
		}
		switch value.String() {
		case "tab", "shift+tab":
			return m, m.completeInput(true)
		case "ctrl+e":
			if m.toggleLatestTool() {
				return m, nil
			}
		case "ctrl+v":
			return m, m.attachCommand("clipboard")
		case "ctrl+r":
			return m, m.commandKeepingDraft("/repl")
		case "ctrl+c":
			m.historyDialog = nil
			m.closeCompletion(false)
			m.palette = nil
			if m.quitArmed {
				return m, tea.Quit
			}
			m.quitArmed = true
			m.status = "Press Ctrl+C again to detach. Host work continues. Esc cancels the displayed active turn."
			return m, nil
		case "esc":
			if m.ready && !m.cancelling && m.activity.ActiveTurn != nil {
				return m, m.cancelTurn(m.activity.ActiveTurn.ID)
			}
		case "enter":
			return m, m.submit()
		case "pgup", "pgdown":
			if m.replFocused && m.replVisible() {
				m.replVP, _ = m.replVP.Update(value)
				return m, nil
			}
			if value.String() == "pgup" && m.vp.YOffset() == 0 {
				return m, m.browseHistory("backward")
			}
			if value.String() == "pgdown" && m.vp.AtBottom() && m.browse != nil {
				return m, m.browseHistory("forward")
			}
			m.vp, _ = m.vp.Update(value)
			m.follow = m.vp.AtBottom()
			return m, nil
		default:
			m.quitArmed = false
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(value)
		m.sizeInput()
		return m, tea.Batch(cmd, m.completeInput(false))
	case tea.MouseWheelMsg:
		if m.replVisible() && value.X >= m.width-m.replWidth() {
			m.replVP, _ = m.replVP.Update(value)
			m.replFocused = true
			return m, nil
		}
		if m.decision == nil {
			m.vp, _ = m.vp.Update(value)
			m.follow = m.browse == nil && m.vp.AtBottom()
		}
	case tea.PasteMsg:
		if m.historyDialog != nil {
			m.historyDialogPaste(value.Content)
			return m, nil
		}
		m.initialPrompt = ""
		m.closeCompletion(false)
		if m.picker != nil {
			return m, nil
		}
		if m.decision != nil {
			if m.decision.form != nil && m.decision.form.editing {
				var cmd tea.Cmd
				m.decision.form.input, cmd = m.decision.form.input.Update(value)
				return m, cmd
			}
			return m, nil
		}
		if m.palette != nil {
			if len(m.palette.query)+len(value.Content) <= 512 {
				m.palette.query += value.Content
				m.palette.filter()
			}
			return m, nil
		}
		if path, ok := nativePastedPath(value.Content, m.clientDirectory); ok {
			return m, m.loadImage(path, value.Content)
		}
		return m, m.pasteText(value.Content)
	}
	return m, nil
}

func (m *nativeModel) submit() tea.Cmd {
	text := m.input.Value()
	if strings.TrimSpace(text) == "" || !m.ready || m.sending || m.controlling {
		return nil
	}
	if strings.HasPrefix(strings.TrimSpace(text), "/") {
		return m.command(strings.TrimSpace(text))
	}
	if command, ok := strings.CutPrefix(text, "!"); ok {
		return m.directShell(command)
	}
	return m.prompt(text, "auto")
}

func (m *nativeModel) prompt(text, delivery string) tea.Cmd {
	if m.attachmentBusy || m.attachment != nil {
		m.status = "Resolve the image upload with /attach check, /attach retry, or /attach discard before sending."
		return nil
	}
	parts, err := m.promptParts(text)
	if err != nil {
		m.status = err.Error()
		return nil
	}
	if m.redraft != nil {
		m.status = "An original input is staged. /redraft restore, replace, or discard before another submission."
		return nil
	}
	if m.rejected != nil {
		m.status = "A rejected draft is retained. /rejected restore or /rejected discard before another submission."
		return nil
	}
	if m.standingDraft != nil {
		m.status = "An unsaved standing draft remains: /me draft, /me retry, or /me discard."
		return nil
	}
	if m.uncertain != nil || m.retryControl != nil {
		m.status = "Inspect or explicitly retry the original uncertain action before another submission."
		return nil
	}
	if m.owner.Configuration.Model.Provider == "" || m.owner.Configuration.Model.Name == "" {
		m.status = "Choose a provider and model with /setup or /model before submitting. Your draft has been kept."
		return nil
	}
	if !nativeDesignPartsPresent(m.draftDesign, parts) {
		m.status = "Design context attachments were removed. Restore them, or explicitly /redraft clear-context before sending the edited draft."
		return nil
	}
	params := protocol.SubmitParams{DesignContext: m.draftDesign, Source: "user", Identity: protocol.RequestIdentity{ClientID: "tui", RequestID: protocol.ID(uuid.NewString())}, Parts: parts}
	if delivery != "queue" && m.activity.ActiveTurn != nil && m.activity.ActiveTurn.Kind == "prompt" {
		params.Delivery, params.TargetTurnID = "steer", new(m.activity.ActiveTurn.ID)
	}
	if delivery == "steer" && params.TargetTurnID == nil {
		m.status = "There is no active prompt turn to steer. Use /queue to submit for a later turn."
		return nil
	}
	command, err := m.handle.Submission(params)
	if err != nil {
		m.status = err.Error()
		return nil
	}
	return m.submitPreparedInput(command)
}

func (m *nativeModel) submitPreparedInput(command *client.InputCommand) tea.Cmd {
	if m.uncertain != nil || m.rejected != nil || m.retryControl != nil || m.sending {
		m.status = "Resolve the original pending input before another admission."
		return nil
	}
	if m.recovery != nil {
		if err := m.recovery.save(command); err != nil {
			m.status = "Input was not sent: " + err.Error()
			retained, restoreErr := m.recovery.restore(m.connection, m.owner.ID)
			if restoreErr == nil && retained != nil {
				m.uncertain, m.recoveryCheck = retained, true
			}
			return nil
		}
	}
	m.input.Reset()
	m.pastes = nil
	m.images = nil
	m.draftDesign = nil
	m.sizeInput()
	m.notice = ""
	m.latest()
	return m.sendInput(command, "send")
}

func (m *nativeModel) sendInput(command *client.InputCommand, action string) tea.Cmd {
	m.sending = true
	recovery := m.recovery
	return func() tea.Msg {
		ctx, done, err := m.work.begin()
		if err != nil {
			return nativeSubmission{command: command, err: err, uncertain: recovery != nil}
		}
		defer done()
		settled := func(admission protocol.Admission, resultErr error) nativeSubmission {
			result := nativeSubmission{command: command, admission: admission, err: resultErr}
			if recovery != nil {
				result.recoveryError = recovery.clear(command)
			}
			return result
		}
		if action != "check" && recovery != nil {
			if err := recovery.save(command); err != nil {
				return nativeSubmission{command: command, err: fmt.Errorf("recovery could not be durably published; this send was not dispatched: %w", err), uncertain: true}
			}
		}
		if action == "check" {
			value, found, err := command.Check(ctx)
			if err == nil && found {
				return settled(value, nil)
			}
			if err == nil {
				err = errors.New("original request not found; /retry explicitly repeats the same request")
			}
			return nativeSubmission{command: command, err: err, uncertain: true}
		}
		var value protocol.Admission
		if action == "retry" {
			value, err = command.Retry(ctx)
		} else {
			value, err = command.Send(ctx)
		}
		if err == nil {
			return settled(value, nil)
		}
		if _, rejected := errors.AsType[*client.Error](err); rejected {
			return settled(protocol.Admission{}, err)
		}
		value, found, checkErr := command.Check(ctx)
		if checkErr == nil && found {
			return settled(value, nil)
		}
		return nativeSubmission{command: command, err: errors.Join(err, checkErr), uncertain: true}
	}
}

func (m *nativeModel) cancelTurn(id protocol.ID) tea.Cmd {
	m.cancelling = true
	handle, generation := m.handle, m.generation
	return func() tea.Msg {
		ctx, done, err := m.work.begin()
		if err != nil {
			return nativeCancelled{generation: generation, turn: id, err: err}
		}
		defer done()
		_, err = handle.CancelTurn(ctx, id)
		return nativeCancelled{generation: generation, turn: id, err: err}
	}
}

func (m *nativeModel) refresh() {
	width := m.transcriptWidth()
	m.input.SetWidth(max(width-2, 1))
	m.sizeInput()
	var rows []string
	appendText := func(text string) {
		rows = append(rows, nativePlainRows(text, max(width-2, 1), false)...)
	}
	v := &m.history
	if m.browse != nil {
		v = &m.browse.transcript
		rows = append(rows, "Historical page · live activity continues · /older /newer /latest", "")
	} else if v.earlier {
		rows = append(rows, "Older messages: /older or Page Up at the top.", "")
	}
	m.renderCache.prepare(v.messages, max(width-2, 1), m.expandTools)
	size := nativeRowBytes(rows)
	m.messageRows = nil
	retained := make(map[protocol.ID]bool, len(v.messages))
	for _, message := range v.messages {
		retained[message.ID] = true
		block := m.renderCache.messageExpanded(message, m.toolExpanded(message.ID))
		size += nativeRowBytes(block) + 1
		if size > nativeRenderBytes || len(rows)+len(block)+1 > nativeRenderRows {
			rows = append(rows, "Display limit reached; complete message bodies remain in host history.")
			break
		}
		tool := false
		for _, part := range message.Parts {
			tool = tool || part.Type == "tool_call" || part.Type == "tool_result"
		}
		m.messageRows = append(m.messageRows, nativeMessageRows{id: message.ID, start: len(rows), end: len(rows) + len(block), tool: tool})
		rows = append(rows, block...)
		rows = append(rows, "")
	}
	for id := range m.toolExpansion {
		if !retained[id] {
			delete(m.toolExpansion, id)
		}
	}
	if m.browse == nil {
		if p := v.preview; p != nil {
			if m.showReasoning && p.Reasoning != "" {
				rows = append(rows, "Reasoning · live preview only")
				appendText(p.Reasoning)
			}
			rows = append(rows, "assistant · provisional")
			text, cut := nativeTextPrefix(p.Text, nativeRenderInput)
			rows = append(rows, strings.Split(nativeMarkdown(text, max(width-2, 1)), "\n")...)
			if p.Truncated || cut {
				rows = append(rows, "Preview truncated; committed content will replace it.")
			}
		}
		if p := v.cellOutput; p != nil {
			rows = append(rows, "REPL output · provisional")
			appendText(p.Text)
			if p.Truncated {
				rows = append(rows, "Output preview truncated.")
			}
		}
	}
	if m.notice != "" {
		rows = append(rows, "", "Terminal command output · not conversation history")
		appendText(m.notice)
	}
	m.rows = boundNativeRows(rows, nativeRenderBytes, nativeRenderRows)
	m.vp.rows = func(y int) string { return m.selectionRow(nativeSelectTranscript, y, m.rows[y]) }
	m.vp.SetWidth(width)
	m.vp.SetHeight(max(m.height-m.input.Height()-4-m.dockHeight()-m.completionHeight(), 1))
	m.vp.setTotal(len(m.rows))
	if m.follow && m.browse == nil {
		m.vp.GotoBottom()
	}
	if m.replVisible() {
		m.replDisplay = m.replRows(max(m.replWidth()-5, 1))
		m.replVP.rows = func(y int) string { return m.selectionRow(nativeSelectREPL, y, m.replDisplay[y]) }
		m.replVP.SetWidth(max(m.replWidth()-5, 1))
		m.replVP.SetHeight(max(m.height-4, 1))
		m.replVP.setTotal(len(m.replDisplay))
		if !m.replFocused {
			m.replVP.GotoBottom()
		}
	}
	m.validateSelection()
}

func (m *nativeModel) View() tea.View {
	if m.historyDialog != nil {
		view := tea.NewView(m.historyDialog.view(m.width, m.height))
		view.AltScreen = true
		return view
	}
	if m.menu != nil {
		view := tea.NewView(m.menu.View(m.width, m.height))
		view.AltScreen = true
		return view
	}
	if m.picker != nil {
		view := tea.NewView(m.picker.view(m.width, m.height) + "\n" + ansi.Truncate(nativeDisplayText(m.status), m.width, "…"))
		view.AltScreen = true
		return view
	}
	if m.decision != nil {
		view := tea.NewView(m.decision.view(m.width, m.height) + "\n" + ansi.Truncate(nativeDisplayText(m.status), m.width, "…"))
		view.AltScreen = true
		return view
	}
	if m.palette != nil {
		view := tea.NewView(m.palette.view(m.width, m.height))
		view.AltScreen = true
		return view
	}
	state := m.activity.Lifecycle
	if m.activity.ActiveTurn != nil {
		state = m.activity.ActiveTurn.State
	}
	footer := fmt.Sprintf("%s · queued %d · permissions %d · questions %d", state, m.activity.QueuedInputCount, m.activity.PendingPermissionCount, m.activity.PendingQuestionCount)
	width := m.transcriptWidth()
	main := m.vp.View() + "\n" + ansi.Truncate(nativeDisplayText(m.status), width, "…") + "\n"
	if completions := m.completionView(); completions != "" {
		main += completions + "\n"
	}
	main += m.selectedInputView()
	if height := m.dockHeight(); height > 0 {
		main += "\n" + nativeFixedRows("Agents · Ctrl+T focuses\n"+m.agentRows(width, height-1), width, height)
	}
	main += "\n" + ansi.Truncate(nativeContextLabel(m.contextUsage), width, "…") + "\n" + ansi.Truncate(footer, width, "…")
	frame := m.layoutFrame(main)
	if m.localFilesystem {
		frame = nativeFileLinks(frame, m.owner.WorkingDirectory)
	}
	view := tea.NewView(frame)
	view.AltScreen = true
	if nativePreferenceLabel(m.preferences.Mouse, true) == "on" {
		view.MouseMode = tea.MouseModeCellMotion
	}
	return view
}

func nativeDisplayText(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return unicode.ReplacementChar
		}
		return r
	}, ansi.Strip(text))
}

func nativeContextLabel(value protocol.ContextUsage) string {
	if value.Prefill == nil {
		return "Latest prefill: unavailable"
	}
	p := value.Prefill
	label := fmt.Sprintf("Latest prefill: %d tokens (%s)", p.InputTokens, p.InputSource)
	if p.ContextWindowTokens == nil {
		label += " · capacity unknown"
	} else {
		label += fmt.Sprintf(" · capacity %d", *p.ContextWindowTokens)
	}
	if p.Stale {
		label += " · earlier history tail"
	}
	return label
}

func (m *nativeModel) restoreRejectedDraft() bool {
	if m.rejected == nil || m.rejected.Record().Method != "sessions.submit" {
		return false
	}
	var params protocol.SubmitParams
	if err := json.Unmarshal(m.rejected.Record().Params, &params); err != nil || params.SessionID != m.owner.ID {
		return false
	}
	if !m.restoreParts(params.Parts) {
		return false
	}
	m.draftDesign = params.DesignContext
	m.rejected = nil
	return true
}
