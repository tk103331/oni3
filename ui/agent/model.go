package agent

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/tk103331/oni3/agent"
)

// MessageType represents the type of a message
type MessageType int

const (
	UserMessage MessageType = iota
	AssistantMessage
	ToolStartMessage
	ToolEndMessage
	ErrorMessage
	ReasoningMessage
	InfoMessage
)

// ToolStatus represents the status of a tool call
type ToolStatus int

const (
	ToolWaiting ToolStatus = iota // Tool is being called (orange)
	ToolSuccess                   // Tool completed successfully (green)
	ToolError                     // Tool failed (red)
)

// Message represents a chat message
type Message struct {
	Type       MessageType
	Content    string
	Name       string     // Tool name (only used for tool messages)
	CallID     string     // Tool call id, pairs a call with its result
	ToolStatus ToolStatus // Status of tool execution (only used for tool messages)
	Arguments  string     // Tool arguments (only used for tool messages)
	Result     string     // Tool result (only used for tool messages)
	StartTime  int64      // Tool start time (Unix timestamp, only used for tool messages)
	EndTime    int64      // Tool end time (Unix timestamp, only used for tool messages)
	mdCache    string     // Cached markdown rendering (assistant messages)
}

// ViewModel is the model for the Agent interface
type ViewModel struct {
	messages         []Message
	input            string
	cursor           int  // Input cursor position (rune index)
	width            int  // Terminal width
	height           int  // Terminal height
	isWaiting        bool // Waiting for AI response
	errorMsg         string
	subtitle         string             // Header subtitle (agent or model name)
	onSendMsg        func(string) error // Callback for sending messages
	streamingContent string             // Currently streaming content
	reasoningContent string             // Currently streaming model reasoning
	renderer         *glamour.TermRenderer
	renderWidth      int  // Width the renderer was created for
	renderDark       bool // Theme the renderer was created for

	// Collapsible blocks: reasoning and tool call details. Indices refer to
	// positions in m.messages.
	expanded map[int]bool
	selected int // selected collapsible message index (-1 = none)

	// Host-registered slash commands (agent/model/skill switching etc.)
	slashCommands []SlashCommand
	onSlash       SlashHandler
	paletteSel    int // selected row in the slash command palette

	// Color theme state
	themeName string
	darkMode  bool
	theme     ThemeVariant

	// Ctrl+C confirmation: the first press only arms quit.
	confirmQuit bool

	// Esc confirmation while a reply runs: the first press only arms the
	// interrupt, a second press within stopArmTimeout stops the agent.
	stopArmed   bool
	onInterrupt func()

	// Mouse capture toggle (off by default so text selection works).
	onToggleMouse func() bool

	// onClear is notified when the transcript is cleared (/clear or Ctrl+L)
	// so the host can start a fresh persistence session.
	onClear func()

	// Task panel and pending ask_user question. A finished list folds away on
	// the next user turn; new pending work expands it again.
	todos          []TodoItem
	todosCollapsed bool
	question       *questionState
	provider       *providerState
	mcpPanelState  *mcpPanel
	// onMCPDisabled reads the host's disabled-server set so the MCP panel can
	// refresh after a toggle; nil means nothing is disabled.
	onMCPDisabled func() map[string]bool

	// Efficient line-based rendering cache: rebuilt only when content
	// changes (dirty) and at most once per renderInterval while streaming.
	dirty          bool
	lastRender     time.Time
	renderedLines  []string
	lineOwners     []int // message index owning each rendered line (-1 = none)
	viewStart      int   // absolute index of the first visible line (last View)
	scrollOffset   int   // Lines from the newest end (0 = pinned to bottom)
	spinnerFrame   int
	spinnerStarted bool
}

// Message type definitions
type ResponseMsg string
type StreamChunkMsg string
type ReasoningChunkMsg string
type StreamEndMsg struct{}
type ErrorMsg string

// NoticeMsg appends an informational line to the transcript.
type NoticeMsg string
type ToolStartMsg struct {
	Name      string
	CallID    string
	Arguments string
}
type ToolEndMsg struct {
	Name   string
	CallID string
	Result string
	Failed bool
}

// Task statuses shown in the TUI task panel.
const (
	TodoPending    = "pending"
	TodoInProgress = "in_progress"
	TodoCompleted  = "completed"
)

// TodoItem is one entry of the displayed task list.
type TodoItem struct {
	Content string
	Status  string
}

// TodoUpdateMsg replaces the task panel contents.
type TodoUpdateMsg struct {
	Items []TodoItem
}

// AskQuestionMsg asks the user a question from a running tool. The answer is
// sent back on Reply ("" means the user cancelled). Approval prompts reuse the
// question panel but only accept one of the offered options.
type AskQuestionMsg struct {
	Question string
	Options  []string
	Reply    chan string
	Approval bool   // tool-approval prompt: options only, no free text
	Detail   string // extra context shown dim below the question
}

// CancelQuestionMsg dismisses a pending question when the turn was cancelled
// before the user answered.
type CancelQuestionMsg struct{}

// questionState is the pending ask_user question rendered above the input.
type questionState struct {
	question string
	options  []string
	reply    chan string
	sel      int
	typing   bool   // user switched to a free-text answer
	approval bool   // tool-approval prompt (no free-text answer)
	detail   string // extra context (e.g. raw tool arguments)
}

// spinnerTickMsg drives the waiting spinner animation
type spinnerTickMsg time.Time

// confirmQuitTimeoutMsg cancels a pending Ctrl+C quit confirmation.
type confirmQuitTimeoutMsg struct{}

// confirmQuitTimeout is how long the "press Ctrl+C again" hint stays armed.
const confirmQuitTimeout = 3 * time.Second

// confirmQuitTick schedules the confirmation timeout.
func confirmQuitTick() tea.Cmd {
	return tea.Tick(confirmQuitTimeout, func(time.Time) tea.Msg {
		return confirmQuitTimeoutMsg{}
	})
}

// stopArmTimeoutMsg disarms a pending Esc-to-stop confirmation.
type stopArmTimeoutMsg struct{}

// stopArmTimeout is how long the "press Esc again to stop" hint stays armed.
const stopArmTimeout = 2 * time.Second

// stopArmTick schedules the stop-confirmation timeout.
func stopArmTick() tea.Cmd {
	return tea.Tick(stopArmTimeout, func(time.Time) tea.Msg {
		return stopArmTimeoutMsg{}
	})
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// renderInterval throttles expensive markdown re-rendering during streaming.
// The cost of a glamour render grows with the accumulated content, so the
// interval adapts to keep the UI responsive on long outputs.
const (
	baseRenderInterval   = 50 * time.Millisecond
	streamRenderInterval = 150 * time.Millisecond
	largeRenderInterval  = 400 * time.Millisecond
)

func renderIntervalFor(contentLen int) time.Duration {
	switch {
	case contentLen < 4*1024:
		return baseRenderInterval
	case contentLen < 32*1024:
		return streamRenderInterval
	default:
		return largeRenderInterval
	}
}

// markDirty flags the line cache as stale and forces the next View pass to
// rebuild it immediately. Used for state transitions; streaming chunks use
// m.dirty directly so View can throttle the expensive markdown re-render.
func (m *ViewModel) markDirty() {
	m.dirty = true
	m.lastRender = time.Time{}
}

// SetOnClear registers a callback invoked whenever the transcript is cleared.
func (m *ViewModel) SetOnClear(fn func()) {
	m.onClear = fn
}

// SetOnInterrupt registers the callback invoked when the user stops a running
// reply with Esc twice.
func (m *ViewModel) SetOnInterrupt(fn func()) {
	m.onInterrupt = fn
}

// SetTranscript replaces the conversation with the given messages (used when
// resuming a saved session).
func (m *ViewModel) SetTranscript(msgs []Message) {
	m.clearChat()
	for _, msg := range msgs {
		msg.mdCache = ""
		m.messages = append(m.messages, msg)
	}
	m.markDirty()
}

// SetTodos replaces the task panel contents (used when resuming a session).
func (m *ViewModel) SetTodos(items []TodoItem) {
	m.todos = append([]TodoItem{}, items...)
	m.todosCollapsed = false
	m.markDirty()
}

// updateTodos replaces the panel from a tool update. A folded finished list
// stays folded; new pending work expands it again.
func (m *ViewModel) updateTodos(items []TodoItem) {
	m.todos = append([]TodoItem{}, items...)
	m.todosCollapsed = m.todosCollapsed && todosAllDone(m.todos)
	m.markDirty()
}

// collapseFinishedTodos folds a completed task list when the user starts the
// next turn: the finished list is no longer actionable and takes up space.
func (m *ViewModel) collapseFinishedTodos() {
	if m.todosCollapsed || !todosAllDone(m.todos) {
		return
	}
	m.todosCollapsed = true
	m.markDirty()
}

// todosAllDone reports whether the list is non-empty and fully completed.
func todosAllDone(items []TodoItem) bool {
	if len(items) == 0 {
		return false
	}
	for _, item := range items {
		if item.Status != TodoCompleted {
			return false
		}
	}
	return true
}

// toggleTodosAt folds or expands the finished task panel at a screen row.
// Returns true when the click was on the panel. A running list is never
// folded: its checkmarks stay visible until the turn completes.
func (m *ViewModel) toggleTodosAt(screenY int) bool {
	if !todosAllDone(m.todos) {
		return false
	}
	top := 2 + m.viewportHeight() // header + rule + transcript
	if screenY < top || screenY >= top+len(m.todoPanelLines()) {
		return false
	}
	m.todosCollapsed = !m.todosCollapsed
	m.markDirty()
	return true
}

// finishQuestion sends the answer to the waiting tool and closes the prompt.
func (m *ViewModel) finishQuestion(answer string) {
	q := m.question
	if q == nil {
		return
	}
	m.question = nil
	m.input = ""
	m.cursor = 0
	answer = strings.TrimSpace(answer)
	if q.reply != nil {
		select {
		case q.reply <- answer:
		default: // the tool already gave up (e.g. the turn was cancelled)
		}
	}

	prefix := "❓ "
	if q.approval {
		prefix = "⚠ "
	}
	text := prefix + q.question
	switch {
	case answer == "":
		text += "\n（未回答）"
	case q.approval:
		text += "\n→ " + answer
	default:
		text += "\n答：" + answer
	}
	m.appendCommandMessage(text, false)
}

// clearQuestion dismisses a pending question without an answer. The waiting
// tool is released so it does not block on a reply that will never come.
func (m *ViewModel) clearQuestion() {
	q := m.question
	if q == nil {
		return
	}
	m.question = nil
	m.input = ""
	m.cursor = 0
	if q.reply != nil {
		select {
		case q.reply <- "":
		default: // the tool already gave up
		}
	}
	m.markDirty()
}

// handleQuestionKey routes keys to the pending ask_user question. It returns
// false when the key should keep its normal meaning (e.g. arrows with no
// options to choose from).
func (m *ViewModel) handleQuestionKey(msg tea.KeyMsg) bool {
	q := m.question
	if q == nil {
		return false
	}
	// Approval prompts only accept the offered options; other keys are
	// swallowed so the shared input box cannot preempt the decision.
	if q.approval {
		if len(q.options) == 0 {
			m.finishQuestion("")
			return true
		}
		switch msg.Type {
		case tea.KeyEsc:
			m.finishQuestion("")
		case tea.KeyEnter:
			m.finishQuestion(q.options[q.sel])
		case tea.KeyUp:
			q.sel = (q.sel - 1 + len(q.options)) % len(q.options)
			m.markDirty()
		case tea.KeyDown:
			q.sel = (q.sel + 1) % len(q.options)
			m.markDirty()
		}
		return true
	}

	// syncTyping returns to option selection when the free-text input is empty.
	syncTyping := func() { q.typing = len([]rune(m.input)) > 0 }

	switch msg.Type {
	case tea.KeyEsc:
		m.finishQuestion("")
		return true
	case tea.KeyEnter:
		if q.typing || len(q.options) == 0 {
			m.finishQuestion(m.input)
		} else {
			m.finishQuestion(q.options[q.sel])
		}
		return true
	case tea.KeyUp:
		if len(q.options) > 0 && !q.typing {
			q.sel = (q.sel - 1 + len(q.options)) % len(q.options)
			m.markDirty()
			return true
		}
		return false
	case tea.KeyDown:
		if len(q.options) > 0 && !q.typing {
			q.sel = (q.sel + 1) % len(q.options)
			m.markDirty()
			return true
		}
		return false
	case tea.KeySpace:
		m.insertRune(' ')
		syncTyping()
		return true
	case tea.KeyRunes:
		for _, r := range msg.Runes {
			if r == '\n' || r == '\r' {
				r = ' '
			}
			m.insertRune(r)
		}
		syncTyping()
		return true
	case tea.KeyBackspace:
		m.backspace()
		syncTyping()
		return true
	case tea.KeyDelete:
		m.deleteAtCursor()
		syncTyping()
		return true
	case tea.KeyLeft:
		if m.cursor > 0 {
			m.cursor--
		}
		return true
	case tea.KeyRight:
		if m.cursor < len([]rune(m.input)) {
			m.cursor++
		}
		return true
	case tea.KeyCtrlA:
		m.cursor = 0
		return true
	case tea.KeyCtrlE:
		m.cursor = len([]rune(m.input))
		return true
	case tea.KeyCtrlU:
		m.deleteBeforeCursor()
		syncTyping()
		return true
	case tea.KeyCtrlK:
		m.deleteAfterCursor()
		syncTyping()
		return true
	case tea.KeyCtrlW:
		m.deleteWord()
		syncTyping()
		return true
	}
	return false
}

// clearConversation empties the transcript and notifies the host.
func (m *ViewModel) clearConversation() {
	m.clearChat()
	if m.onClear != nil {
		m.onClear()
	}
}

// clearChat empties the conversation, keeping the input intact.
func (m *ViewModel) clearChat() {
	m.closeProvider()
	m.messages = nil
	m.streamingContent = ""
	m.reasoningContent = ""
	m.errorMsg = ""
	m.todos = nil
	m.todosCollapsed = false
	m.question = nil
	m.scrollOffset = 0
	m.expanded = make(map[int]bool)
	m.selected = -1
	m.markDirty()
}

// SetMouseToggle registers the callback used by Ctrl+Y to toggle mouse
// capture. It returns the new state (true = mouse captured).
func (m *ViewModel) SetMouseToggle(fn func() bool) {
	m.onToggleMouse = fn
}

// SetSubtitle sets the header subtitle (agent or model name).
func (m *ViewModel) SetSubtitle(subtitle string) {
	m.subtitle = strings.TrimSpace(subtitle)
}

// NewViewModel creates a new ViewModel
func NewViewModel(onSendMsg func(string) error) *ViewModel {
	dark := lipgloss.HasDarkBackground()
	return &ViewModel{
		messages:  []Message{},
		onSendMsg: onSendMsg,
		dirty:     true,
		expanded:  make(map[int]bool),
		selected:  -1,
		themeName: defaultThemeName,
		darkMode:  dark,
		theme:     resolveTheme(defaultThemeName, dark),
	}
}

// ensureRenderer creates (or recreates) the markdown renderer for a width
// and terminal theme.
func (m *ViewModel) ensureRenderer() {
	// Leave two cells for the conversation indent and two at the right edge.
	width := maxInt(m.width-4, 1)
	dark := m.darkMode
	if m.renderer != nil && m.renderWidth == width && m.renderDark == dark {
		return
	}

	renderer, err := glamour.NewTermRenderer(
		glamour.WithStyles(markdownStyles(m.theme, dark)),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		m.renderer = nil
		return
	}
	m.renderer = renderer
	m.renderWidth = width
	m.renderDark = dark
	// Width/theme change invalidates cached renders and the line cache.
	for i := range m.messages {
		m.messages[i].mdCache = ""
	}
	m.markDirty()
}

// sendMsgCmd runs the send callback and surfaces failures as an ErrorMsg.
func sendMsgCmd(text string, send func(string) error) tea.Cmd {
	return func() tea.Msg {
		if send == nil {
			return nil
		}
		if err := send(text); err != nil {
			return ErrorMsg(fmt.Sprintf("Failed to send message: %v", err))
		}
		return nil
	}
}

// spinnerTickCmd schedules the next spinner frame.
func spinnerTickCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg {
		return spinnerTickMsg(t)
	})
}

// Init initializes the model
func (m *ViewModel) Init() tea.Cmd {
	return nil
}

// Update handles messages
func (m *ViewModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ensureRenderer()
		m.markDirty()
		// A resize can leave stale cells behind (lines wrapped at the old
		// width); force a clean repaint.
		return m, tea.Batch(m.clampScroll(), func() tea.Msg { return tea.ClearScreen() })

	case spinnerTickMsg:
		if m.isWaiting {
			m.spinnerFrame = (m.spinnerFrame + 1) % len(spinnerFrames)
			return m, spinnerTickCmd()
		}
		m.spinnerStarted = false
		return m, nil

	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.scrollOffset += 3
			return m, m.clampScroll()
		case tea.MouseButtonWheelDown:
			m.scrollOffset -= 3
			return m, m.clampScroll()
		case tea.MouseButtonLeft:
			// A left click on a reasoning/tool line folds or expands it; the
			// finished task panel can be folded and unfolded the same way.
			if msg.Action == tea.MouseActionPress {
				if m.toggleTodosAt(msg.Y) || m.toggleBlockAt(msg.Y) {
					return m, m.clampScroll()
				}
			}
		}
		return m, nil

	case confirmQuitTimeoutMsg:
		if m.confirmQuit {
			m.confirmQuit = false
			m.markDirty()
		}
		return m, nil

	case stopArmTimeoutMsg:
		if m.stopArmed {
			m.stopArmed = false
			m.markDirty()
		}
		return m, nil

	case AskQuestionMsg:
		// One question at a time: a second tool asking concurrently is told
		// to proceed without an answer instead of deadlocking.
		if m.question != nil {
			if msg.Reply != nil {
				select {
				case msg.Reply <- "":
				default:
				}
			}
			return m, nil
		}
		m.question = &questionState{
			question: msg.Question,
			options:  msg.Options,
			reply:    msg.Reply,
			approval: msg.Approval,
			detail:   msg.Detail,
		}
		m.input = ""
		m.cursor = 0
		m.markDirty()
		return m, nil

	case CancelQuestionMsg:
		m.clearQuestion()
		return m, nil

	case TodoUpdateMsg:
		m.updateTodos(msg.Items)
		return m, nil

	case providerModelsMsg:
		m.receiveProviderModels(msg)
		return m, nil

	case tea.KeyMsg:
		// Ctrl+C quits only after a second confirmation press.
		if msg.Type == tea.KeyCtrlC {
			if m.confirmQuit {
				m.closeProvider()
				return m, tea.Quit
			}
			m.confirmQuit = true
			m.markDirty()
			return m, confirmQuitTick()
		}
		if m.confirmQuit {
			// Any other key cancels the pending quit.
			m.confirmQuit = false
			m.markDirty()
		}

		if m.provider != nil {
			return m, m.handleProviderKey(msg)
		}

		// The MCP panel owns the keyboard like the provider modal.
		if m.mcpPanelState != nil {
			return m, m.handleMCPPanelKey(msg)
		}

		// A pending ask_user question owns the keyboard: arrows pick an
		// option, enter submits, esc cancels, typing switches to free text.
		if m.question != nil && m.handleQuestionKey(msg) {
			return m, nil
		}

		// Esc twice stops a running reply: the first press only arms the
		// interrupt, the second one within stopArmTimeout cancels the agent.
		if msg.Type == tea.KeyEsc && m.isWaiting {
			if m.stopArmed {
				m.stopArmed = false
				m.markDirty()
				if m.onInterrupt != nil {
					m.onInterrupt()
				}
				return m, nil
			}
			m.stopArmed = true
			m.markDirty()
			return m, stopArmTick()
		}
		// Any other key cancels the pending stop confirmation.
		if m.stopArmed {
			m.stopArmed = false
			m.markDirty()
		}

		// Esc never quits: it only dismisses the command palette/input.
		if msg.Type == tea.KeyEsc {
			if m.paletteActive() {
				m.input = ""
				m.cursor = 0
				m.paletteSel = 0
				m.markDirty()
			}
			return m, nil
		}

		// Ctrl+Y toggles mouse capture. With capture off the terminal's
		// native text selection / copy works; with capture on the wheel
		// scrolls the transcript.
		if msg.Type == tea.KeyCtrlY {
			if m.onToggleMouse != nil {
				if m.onToggleMouse() {
					m.appendCommandMessage("鼠标捕获已启用：滚轮可滚动对话，点击思考/工具/错误行可展开或折叠；复制可在拖选时按住 Shift（多数终端），或再按 Ctrl+Y 关闭捕获", false)
				} else {
					m.appendCommandMessage("鼠标捕获已关闭：可直接用鼠标选择并复制文本；滚轮由终端转发（Ctrl+Y 可强制启用鼠标滚动）", false)
				}
			}
			return m, nil
		}

		// Scrolling works in all states, including while waiting for a
		// response — long streaming output can overflow the viewport.
		switch msg.Type {
		case tea.KeyUp:
			if m.hasPalette() {
				m.movePalette(-1)
				return m, nil
			}
			m.scrollOffset++
			return m, m.clampScroll()
		case tea.KeyDown:
			if m.hasPalette() {
				m.movePalette(1)
				return m, nil
			}
			m.scrollOffset--
			return m, m.clampScroll()
		case tea.KeyPgUp:
			m.scrollOffset += m.viewportHeight()
			return m, m.clampScroll()
		case tea.KeyPgDown:
			m.scrollOffset -= m.viewportHeight()
			return m, m.clampScroll()
		case tea.KeyHome:
			m.scrollOffset = 1 << 30
			return m, m.clampScroll()
		case tea.KeyEnd:
			m.scrollOffset = 0
			return m, nil
		}

		if m.isWaiting {
			// While waiting only scroll and fold keys are accepted;
			// editing is blocked.
			switch msg.Type {
			case tea.KeyTab:
				m.selectCollapsible(1)
				return m, nil
			case tea.KeyShiftTab:
				m.selectCollapsible(-1)
				return m, nil
			case tea.KeyCtrlO:
				m.toggleSelectedBlock()
				return m, nil
			}
			return m, nil
		}

		switch msg.Type {
		case tea.KeyTab:
			m.selectCollapsible(1)
			return m, nil
		case tea.KeyShiftTab:
			m.selectCollapsible(-1)
			return m, nil
		case tea.KeyCtrlO:
			m.toggleSelectedBlock()
			return m, nil
		case tea.KeyCtrlD:
			// Quit like a shell exit when the input is empty
			if m.input == "" {
				return m, tea.Quit
			}

		case tea.KeyCtrlL:
			m.clearConversation()
			return m, nil

		case tea.KeyLeft:
			if m.cursor > 0 {
				m.cursor--
			}
		case tea.KeyRight:
			if m.cursor < len([]rune(m.input)) {
				m.cursor++
			}
		case tea.KeyCtrlA:
			m.cursor = 0
		case tea.KeyCtrlE:
			m.cursor = len([]rune(m.input))
		case tea.KeyCtrlU:
			m.deleteBeforeCursor()
		case tea.KeyCtrlK:
			m.deleteAfterCursor()
		case tea.KeyCtrlW:
			m.deleteWord()

		case tea.KeyEnter:
			if m.input == "" {
				return m, nil
			}

			// Slash commands: /help, /clear, /agent, /model, /skill ...
			if strings.HasPrefix(strings.TrimSpace(m.input), "/") {
				if item, ok := m.selectedPaletteItem(); ok {
					if item.exec {
						m.input = ""
						m.cursor = 0
						m.paletteSel = 0
						return m, tea.Batch(m.executeSlashCommand(item.fill), m.clampScroll())
					} else {
						// Fill the command and continue typing its argument.
						// A value kept in the input gets a trailing space so
						// typing continues right away, and the palette stays
						// closed because the argument is already complete.
						fill := item.fill
						if item.complete {
							fill += " "
						}
						m.input = fill
						m.cursor = len([]rune(fill))
						m.paletteSel = 0
						m.markDirty()
					}
					return m, m.clampScroll()
				}
				line := strings.TrimSpace(m.input)
				m.input = ""
				m.cursor = 0
				m.paletteSel = 0
				return m, m.executeSlashCommand(line)
			}

			// A finished task list folds away as the next turn begins.
			m.collapseFinishedTodos()
			m.messages = append(m.messages, Message{
				Type:    UserMessage,
				Content: m.input,
			})

			userInput := m.input
			m.input = ""
			m.cursor = 0
			m.isWaiting = true
			m.streamingContent = ""
			m.reasoningContent = ""
			m.errorMsg = ""
			m.scrollOffset = 0
			m.markDirty()

			cmds := []tea.Cmd{sendMsgCmd(userInput, m.onSendMsg)}
			if !m.spinnerStarted {
				m.spinnerStarted = true
				cmds = append(cmds, spinnerTickCmd())
			}
			return m, tea.Batch(cmds...)

		case tea.KeyBackspace:
			m.backspace()
		case tea.KeyDelete:
			m.deleteAtCursor()
		case tea.KeySpace:
			m.insertRune(' ')
		case tea.KeyRunes:
			if !m.isWaiting {
				for _, r := range msg.Runes {
					// Pasted multi-line text collapses to spaces: the
					// single-line input box cannot show newlines.
					if r == '\n' || r == '\r' {
						r = ' '
					}
					m.insertRune(r)
				}
			}
		}
		return m, nil

	case ResponseMsg:
		// Complete response message; keep any reasoning that preceded it
		m.flushReasoning()
		m.messages = append(m.messages, Message{
			Type:    AssistantMessage,
			Content: string(msg),
		})
		m.isWaiting = false
		m.stopArmed = false
		m.streamingContent = ""
		m.scrollOffset = 0
		m.markDirty()
		m.lastRender = time.Time{} // Render immediately
		return m, nil

	case StreamChunkMsg:
		// Streaming response chunk
		m.streamingContent += string(msg)
		m.dirty = true // View throttles regeneration to renderInterval
		return m, nil

	case ReasoningChunkMsg:
		// Streaming model reasoning (thinking) chunk
		m.reasoningContent += string(msg)
		m.dirty = true
		return m, nil

	case StreamEndMsg:
		// Stream ended: persist reasoning, then the assistant answer
		m.flushReasoning()
		if m.streamingContent != "" {
			m.messages = append(m.messages, Message{
				Type:    AssistantMessage,
				Content: m.streamingContent,
			})
			m.streamingContent = ""
		}
		m.isWaiting = false
		m.stopArmed = false
		m.scrollOffset = 0
		m.markDirty()
		m.lastRender = time.Time{}
		return m, nil

	case ToolStartMsg:
		// Persist any pending reasoning first so the thinking block stays
		// above the tool call it led to.
		m.flushReasoning()

		// Tool execution started - create waiting state message with timestamp.
		// The tool call id identifies the call, so a repeated event for the
		// same call cannot leave a zombie "running..." line behind.
		for i := len(m.messages) - 1; i >= 0; i-- {
			msg2 := m.messages[i]
			if msg2.Type != ToolStartMessage {
				continue
			}
			if msg.CallID != "" && msg2.CallID == msg.CallID {
				return m, nil
			}
			if msg.CallID == "" && msg2.ToolStatus == ToolWaiting &&
				msg2.Name == msg.Name && msg2.Arguments == msg.Arguments {
				return m, nil
			}
		}
		m.messages = append(m.messages, Message{
			Type:       ToolStartMessage,
			Name:       msg.Name,
			CallID:     msg.CallID,
			ToolStatus: ToolWaiting,
			Arguments:  msg.Arguments,
			StartTime:  time.Now().Unix(),
		})
		m.markDirty()
		return m, nil

	case ToolEndMsg:
		// Tool execution ended - update the waiting message of that call.
		toolResult := strings.TrimSpace(msg.Result)

		newStatus := ToolSuccess
		if msg.Failed || looksLikeError(toolResult) {
			newStatus = ToolError
		}

		idx := m.findWaitingTool(msg.Name, msg.CallID)
		if idx < 0 {
			// If no waiting tool message was found, create a new one
			m.messages = append(m.messages, Message{
				Type:       ToolStartMessage,
				Name:       msg.Name,
				CallID:     msg.CallID,
				ToolStatus: newStatus,
				Result:     toolResult,
			})
			m.markDirty()
			return m, nil
		}

		m.messages[idx].ToolStatus = newStatus
		m.messages[idx].Result = toolResult
		m.messages[idx].EndTime = time.Now().Unix()
		m.markDirty()
		return m, nil

	case NoticeMsg:
		m.messages = append(m.messages, Message{Type: InfoMessage, Content: string(msg)})
		m.markDirty()
		m.lastRender = time.Time{}
		return m, nil

	case ErrorMsg:
		// Error message - display all error messages (filtering handled at application layer)
		errorText := string(msg)
		m.flushReasoning()
		m.messages = append(m.messages, Message{
			Type:    ErrorMessage,
			Content: errorText,
		})
		m.isWaiting = false
		m.stopArmed = false
		m.streamingContent = ""
		m.errorMsg = errorText
		// A question asked by a tool that just died must not stay pending.
		m.clearQuestion()
		m.markDirty()
		m.lastRender = time.Time{}
		return m, nil
	}

	return m, nil
}

// ---- collapsible block helpers ----

// findWaitingTool returns the index of the running tool call that a result
// belongs to. The tool call id identifies the call exactly; without it the
// oldest running call of the same tool is used.
func (m *ViewModel) findWaitingTool(name, callID string) int {
	for i := range m.messages {
		msg := m.messages[i]
		if msg.Type != ToolStartMessage || msg.ToolStatus != ToolWaiting {
			continue
		}
		if callID != "" {
			if msg.CallID == callID {
				return i
			}
			continue
		}
		if msg.Name == name {
			return i
		}
	}
	return -1
}

// collapsibleIndices lists message indices that can be folded (reasoning,
// tool call and error blocks) in conversation order.
func (m *ViewModel) collapsibleIndices() []int {
	var idx []int
	for i, msg := range m.messages {
		switch msg.Type {
		case ReasoningMessage, ToolStartMessage, ErrorMessage:
			idx = append(idx, i)
		}
	}
	return idx
}

// selectCollapsible moves the selection through foldable blocks.
func (m *ViewModel) selectCollapsible(dir int) {
	idx := m.collapsibleIndices()
	if len(idx) == 0 {
		return
	}
	if m.selected < 0 {
		if dir >= 0 {
			m.selected = idx[0]
		} else {
			m.selected = idx[len(idx)-1]
		}
		m.markDirty()
		return
	}
	pos := 0
	for i, v := range idx {
		if v == m.selected {
			pos = i
			break
		}
	}
	pos = (pos + dir + len(idx)) % len(idx)
	m.selected = idx[pos]
	m.markDirty()
}

// toggleSelectedBlock expands or folds the selected block.
func (m *ViewModel) toggleSelectedBlock() {
	if m.selected < 0 || m.selected >= len(m.messages) {
		m.selected = -1
		return
	}
	// Reasoning blocks default to folded once finalized; tools default to
	// folded always. Toggling flips the current state.
	m.expanded[m.selected] = !m.expanded[m.selected]
	m.markDirty()
}

// toggleBlockAt folds or expands the reasoning/tool block at a screen row.
// Returns true when a foldable block was toggled.
func (m *ViewModel) toggleBlockAt(screenY int) bool {
	const messageTop = 2 // header + rule above the transcript
	row := screenY - messageTop
	if row < 0 || row >= m.viewportHeight() {
		return false
	}
	abs := m.viewStart + row
	if abs < 0 || abs >= len(m.lineOwners) {
		return false
	}
	idx := m.lineOwners[abs]
	if idx < 0 || idx >= len(m.messages) {
		return false
	}
	switch m.messages[idx].Type {
	case ReasoningMessage, ToolStartMessage, ErrorMessage:
		m.expanded[idx] = !m.expanded[idx]
		m.selected = idx
		m.markDirty()
		return true
	}
	return false
}

// ---- input editing helpers ----

func (m *ViewModel) insertRune(r rune) {
	m.paletteSel = 0
	runes := []rune(m.input)
	if m.cursor > len(runes) {
		m.cursor = len(runes)
	}
	runes = append(runes[:m.cursor], append([]rune{r}, runes[m.cursor:]...)...)
	m.input = string(runes)
	m.cursor++
}

func (m *ViewModel) backspace() {
	m.paletteSel = 0
	runes := []rune(m.input)
	if m.cursor > 0 && m.cursor <= len(runes) {
		runes = append(runes[:m.cursor-1], runes[m.cursor:]...)
		m.input = string(runes)
		m.cursor--
	}
}

func (m *ViewModel) deleteAtCursor() {
	m.paletteSel = 0
	runes := []rune(m.input)
	if m.cursor < len(runes) {
		runes = append(runes[:m.cursor], runes[m.cursor+1:]...)
		m.input = string(runes)
	}
}

func (m *ViewModel) deleteBeforeCursor() {
	m.paletteSel = 0
	runes := []rune(m.input)
	if m.cursor > len(runes) {
		m.cursor = len(runes)
	}
	m.input = string(runes[m.cursor:])
	m.cursor = 0
}

func (m *ViewModel) deleteAfterCursor() {
	m.paletteSel = 0
	runes := []rune(m.input)
	if m.cursor > len(runes) {
		m.cursor = len(runes)
	}
	m.input = string(runes[:m.cursor])
}

func (m *ViewModel) deleteWord() {
	m.paletteSel = 0
	runes := []rune(m.input)
	if m.cursor > len(runes) {
		m.cursor = len(runes)
	}
	i := m.cursor
	for i > 0 && unicode.IsSpace(runes[i-1]) {
		i--
	}
	for i > 0 && !unicode.IsSpace(runes[i-1]) {
		i--
	}
	runes = append(runes[:i], runes[m.cursor:]...)
	m.input = string(runes)
	m.cursor = i
}

// ---- scroll helpers ----

// paletteView returns the palette rows to render, capped so the layout can
// never exceed the terminal height.
func (m *ViewModel) paletteView() []string {
	hints := m.slashHints()
	maxLines := min(8, maxInt(m.height-7-len(m.todoPanelLines())-len(m.questionPanelLines()), 0))
	if maxLines <= 0 {
		return nil
	}
	// A hint line sits above every candidate list.
	if items := m.paletteItems(); len(items) > 0 {
		maxItems := maxInt(maxLines-1, 1)
		if len(hints) > maxItems {
			// Follow selection instead of always showing the first candidates.
			start := maxInt(min(m.paletteSel-maxItems+1, len(hints)-maxItems), 0)
			hints = hints[start : start+maxItems]
		}
		header := m.muted().Render(truncate(m.paletteHint(len(items)), maxInt(m.width-2, 1)))
		return append([]string{header}, hints...)
	}
	if len(hints) > maxLines {
		hints = hints[:maxLines]
	}
	return hints
}

func (m *ViewModel) viewportHeight() int {
	// Fixed chrome: header(1) + rule(1) + help bar(1) + input box(3), plus
	// the task panel, question prompt and command palette when shown.
	h := m.height - 6 - len(m.paletteView()) - len(m.todoPanelLines()) - len(m.questionPanelLines())
	if h < 1 {
		h = 1
	}
	return h
}

// clampScroll is a tea.Cmd that keeps the scroll offset within bounds.
func (m *ViewModel) clampScroll() tea.Cmd {
	maxScroll := len(m.renderedLines) - m.viewportHeight()
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.scrollOffset > maxScroll {
		m.scrollOffset = maxScroll
	}
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
	return nil
}

// looksLikeError detects common error patterns in tool results.
func looksLikeError(result string) bool {
	lower := strings.ToLower(result)
	patterns := []string{
		"error", "failed", "exception", "timeout",
		"not found", "invalid", "unauthorized",
		"forbidden", "denied", "panic", "fatal",
	}
	for _, pattern := range patterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	for _, prefix := range []string{"❌", "⚠️", "❗", "✖️", "×"} {
		if strings.HasPrefix(result, prefix) {
			return true
		}
	}
	return false
}

// ---- rendering ----

// refreshCache regenerates the rendered line cache when the content changed,
// throttled during streaming to keep markdown rendering cheap.
func (m *ViewModel) refreshCache() {
	if !m.dirty {
		return
	}
	interval := renderIntervalFor(len(m.streamingContent))
	if !m.lastRender.IsZero() && time.Since(m.lastRender) < interval {
		return // Too soon; the next View pass will pick it up
	}
	m.updateRenderedLines()
	m.dirty = false
	m.lastRender = time.Now()
}

// updateRenderedLines rebuilds the cached rendered lines for efficient scrolling.
func (m *ViewModel) updateRenderedLines() {
	m.ensureRenderer()
	m.renderedLines = m.renderedLines[:0]
	m.lineOwners = m.lineOwners[:0]

	// appendOwned tracks which message produced each line so mouse clicks can
	// be mapped back to the block that was clicked (owner -1 = not clickable).
	appendOwned := func(owner int, lines []string) {
		for _, line := range lines {
			// Keep physical rows and mouse hit targets in sync, including
			// multiline user messages and wrapped markdown.
			for _, row := range strings.Split(line, "\n") {
				m.renderedLines = append(m.renderedLines, row)
				m.lineOwners = append(m.lineOwners, owner)
			}
		}
	}

	for i := range m.messages {
		msg := &m.messages[i]
		switch msg.Type {
		case UserMessage:
			appendOwned(i, m.renderUserMessage(msg.Content))
		case AssistantMessage:
			appendOwned(i, m.renderAssistantMessage(msg))
		case ReasoningMessage:
			appendOwned(i, m.renderReasoningMessage(msg, i, false))
		case ToolStartMessage:
			appendOwned(i, m.renderToolMessage(msg, i))
		case ErrorMessage:
			appendOwned(i, m.renderErrorMessage(msg, i))
		case InfoMessage:
			appendOwned(i, m.renderInfoMessage(msg.Content))
		}
	}

	// Streaming content: reasoning first (expanded while running), then the answer
	if m.reasoningContent != "" {
		live := &Message{Type: ReasoningMessage, Content: m.reasoningContent}
		appendOwned(-1, m.renderReasoningMessage(live, -1, true))
	}
	if m.streamingContent != "" {
		appendOwned(-1, m.renderAssistantContent(m.streamingContent, true))
	} else if m.isWaiting && m.reasoningContent == "" {
		// Spinner only shows while nothing streams yet — otherwise the
		// "(typing…)" label already indicates progress.
		spinner := m.warningStyle(true).Render(spinnerFrames[m.spinnerFrame])
		appendOwned(-1, []string{
			m.muted().Render("  " + spinner + " thinking..."), "",
		})
	}

	// System error (avoid duplicating a message that is already shown)
	if m.errorMsg != "" {
		dup := false
		for _, msg := range m.messages {
			if msg.Type == ErrorMessage && msg.Content == m.errorMsg {
				dup = true
				break
			}
		}
		if !dup {
			appendOwned(-1, m.renderErrorMessage(&Message{
				Type:    ErrorMessage,
				Content: "⚠️ " + m.errorMsg,
			}, -1))
		}
	}

	// Welcome screen when there is nothing to show
	if len(m.renderedLines) == 0 {
		appendOwned(-1, []string{
			"",
			lipgloss.NewStyle().Bold(true).Render("  Welcome to Oni3"),
			"",
			m.muted().Render("  描述任务，开始对话。"),
			"",
			m.muted().Render("  输入 / 选择模型、技能或恢复会话。"),
			"",
		})
	}

	// Trailing whitespace is pointless and widens the diff the renderer
	// has to repaint between frames. Rows that span the full width (the
	// selected highlight bar) keep their padding.
	for i := range m.renderedLines {
		if lipgloss.Width(m.renderedLines[i]) >= m.width {
			continue
		}
		m.renderedLines[i] = strings.TrimRight(m.renderedLines[i], " \t")
	}
}

// renderUserMessage renders a compact user message: violet prompt + plain text.
func (m *ViewModel) renderUserMessage(content string) []string {
	prompt := lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.theme.User)).
		Bold(true).
		Render("❯")
	wrapped := ansi.Hardwrap(content, maxInt(m.width-2, 1), true)
	lines := strings.Split(wrapped, "\n")
	for i := range lines {
		if i == 0 {
			lines[i] = prompt + " " + lines[i]
		} else {
			lines[i] = "  " + lines[i]
		}
	}
	return append(lines, "")
}

// renderAssistantMessage renders a finalized assistant message with cached markdown.
func (m *ViewModel) renderAssistantMessage(msg *Message) []string {
	if msg.mdCache == "" {
		msg.mdCache = m.renderMarkdown(msg.Content)
	}
	lines := []string{m.assistantLabel(false)}
	lines = append(lines, indentLines(strings.Split(msg.mdCache, "\n"), "  ")...)
	lines = append(lines, "")
	return lines
}

// flushReasoning persists the currently streaming reasoning block as a
// finalized message so it stays above the following answer.
func (m *ViewModel) flushReasoning() {
	if m.reasoningContent == "" {
		return
	}
	m.messages = append(m.messages, Message{
		Type:    ReasoningMessage,
		Content: m.reasoningContent,
	})
	m.reasoningContent = ""
}

// renderReasoningMessage renders the model's thinking as a muted
// block. While streaming the block is expanded; once finalized it collapses
// to a single summary line until the user expands it.
func (m *ViewModel) renderReasoningMessage(msg *Message, index int, streaming bool) []string {
	headStyle := m.muted()
	body := m.muted()

	if streaming {
		head := m.reasoningMarker(true, true) + " " + headStyle.Render("◇ Thinking…")
		lines := []string{head}
		lines = append(lines, m.reasoningBodyLines(msg.Content, body)...)
		return lines
	}

	expanded := m.expanded[index]
	label := "◇ Thinking"
	plain := foldGlyph(expanded) + " " + label
	head := m.reasoningMarker(expanded, false) + " " + headStyle.Render(label)
	if !expanded {
		// Folded: show a single-line snippet of the reasoning instead of
		// just a size summary.
		preview := firstLine(msg.Content)
		// Reserve cells for the label, separator and selection marker.
		maxW := m.width - lipgloss.Width(plain) - 3
		if m.selected == index {
			maxW -= 2
		}
		maxW = maxInt(maxW, 0)
		if preview != "" {
			preview = truncate(preview, maxW)
			plain += " · " + preview
			head += m.muted().Render(" · " + preview)
		}
	}
	if m.selected == index {
		// Highlight only the header row; an expanded block keeps its body.
		head = m.highlightRow(plain)
	}
	if !expanded {
		return []string{head}
	}

	lines := []string{head}
	lines = append(lines, m.reasoningBodyLines(msg.Content, body)...)
	return lines
}

// reasoningBodyLines wraps and styles the reasoning text as a dim block.
func (m *ViewModel) reasoningBodyLines(content string, style lipgloss.Style) []string {
	var lines []string
	for _, line := range wrapLines(strings.Split(strings.TrimSpace(content), "\n"), m.width) {
		if strings.TrimSpace(line) == "" {
			continue // compact: drop blank lines inside reasoning
		}
		lines = append(lines, "    "+style.Render(line))
	}
	return lines
}

// reasoningMarker returns the fold marker for a reasoning/tool block.
// Selected blocks show the marker in the accent color.
func (m *ViewModel) reasoningMarker(expanded bool, selected bool) string {
	glyph := foldGlyph(expanded)
	if selected {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Accent)).Bold(true).Render(glyph)
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Muted)).Render(glyph)
}

// foldGlyph returns the plain-text fold marker.
func foldGlyph(expanded bool) string {
	if expanded {
		return "▾"
	}
	return "▸"
}

// highlightRow marks keyboard focus without painting a full-width background.
// Text inherits the terminal foreground, including on custom light themes.
func (m *ViewModel) highlightRow(text string) string {
	marker := m.accentStyle(true).Render("│")
	row := marker + " " + ansi.Truncate(text, maxInt(m.width-2, 0), "…")
	return lipgloss.NewStyle().Width(m.width).Render(row)
}

// renderAssistantContent renders streaming (or generic) markdown content.
func (m *ViewModel) renderAssistantContent(content string, streaming bool) []string {
	lines := []string{m.assistantLabel(streaming)}
	rendered := m.renderMarkdown(content)
	lines = append(lines, indentLines(strings.Split(rendered, "\n"), "  ")...)
	lines = append(lines, "")
	return lines
}

// assistantLabel returns the small assistant header line.
func (m *ViewModel) assistantLabel(streaming bool) string {
	dot := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Accent)).Bold(true).Render("◆")
	label := "Assistant"
	if streaming {
		label = "Assistant " + m.muted().Render("(typing…)")
	}
	return dot + " " + label
}

// renderToolMessage renders a compact tool call line. Finished calls are
// folded to one line; expanding shows the full result.
func (m *ViewModel) renderToolMessage(msg *Message, index int) []string {
	var dotColor string
	glyph := "·"
	switch msg.ToolStatus {
	case ToolWaiting:
		dotColor, glyph = m.theme.Warning, "◌"
	case ToolSuccess:
		dotColor, glyph = m.theme.Success, "✓"
	case ToolError:
		dotColor, glyph = m.theme.Error, "✗"
	}

	dot := lipgloss.NewStyle().Foreground(lipgloss.Color(dotColor)).Render(glyph)
	displayName := toolDisplayName(msg.Name)
	name := lipgloss.NewStyle().Bold(true).Render(displayName)

	expanded := m.expanded[index]
	parts := []string{m.reasoningMarker(expanded, false), dot, name}
	plainParts := []string{foldGlyph(expanded), glyph, displayName}
	if args := summarizeArgs(msg.Arguments); args != "" {
		parts = append(parts, m.muted().Render(args))
		plainParts = append(plainParts, args)
	}
	meta := ""
	metaStyle := m.muted()
	switch {
	case msg.ToolStatus == ToolWaiting:
		meta = "running..."
	case msg.ToolStatus == ToolError:
		// A long failure must leave room for the tool name and arguments.
		reason := oneLine(strings.TrimPrefix(msg.Result, agent.ToolErrorPrefix))
		reason = strings.TrimPrefix(reason, "error: ")
		meta = truncate(reason, min(48, maxInt(m.width/3, 1)))
		metaStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Error))
	case msg.EndTime > 0 && msg.StartTime > 0:
		meta = fmt.Sprintf("%ds", msg.EndTime-msg.StartTime)
	}

	// Reserve the right edge for progress/duration even with long arguments.
	head := joinRow(strings.Join(parts, " "), metaStyle.Render(meta), m.width)
	if m.selected == index {
		head = m.highlightRow(joinRow(strings.Join(plainParts, " "), meta, m.width-2))
	}
	lines := []string{head}

	// Result details only when expanded; folded calls stay single-line.
	if expanded && msg.ToolStatus != ToolWaiting && msg.Result != "" {
		lines = append(lines, m.toolResultLines(msg)...)
	}
	return lines
}

// toolResultLines renders the (possibly long) tool result, capped to keep the
// TUI responsive.
func (m *ViewModel) toolResultLines(msg *Message) []string {
	style := m.muted()
	if msg.ToolStatus == ToolError {
		style = lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Error))
	}

	const maxLines = 12
	raw := strings.Split(strings.TrimRight(msg.Result, "\n"), "\n")
	var lines []string
	for i, line := range raw {
		if i >= maxLines {
			lines = append(lines, "    "+m.muted().Render(fmt.Sprintf("… 还有 %d 行（已截断）", len(raw)-maxLines)))
			break
		}
		lines = append(lines, "    "+style.Render(truncate(strings.TrimRight(line, " \t"), maxInt(m.width-8, 20))))
	}
	return lines
}

// renderErrorMessage renders an error block in red. Like reasoning and tool
// blocks it starts folded: only the first line is shown until the block is
// selected and expanded (Tab + Ctrl+O, or a click).
func (m *ViewModel) renderErrorMessage(msg *Message, index int) []string {
	icon := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Error)).Bold(true).Render("✗")
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Error))

	content := strings.TrimSpace(msg.Content)
	expanded := index >= 0 && m.expanded[index]
	selected := index >= 0 && m.selected == index
	glyph := foldGlyph(expanded)

	// Wrap once: the first line is the summary, the rest are details.
	var body []string
	for _, raw := range strings.Split(content, "\n") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		for _, wrapped := range wrapLines([]string{raw}, m.width) {
			body = append(body, strings.TrimSpace(wrapped))
		}
	}

	if !expanded {
		// Folded: keep only the first line (model errors often append a
		// multi-line node path after the actual message).
		if len(body) == 0 {
			if selected {
				return []string{m.highlightRow(glyph + " ✗")}
			}
			return []string{m.reasoningMarker(false, false) + " " + icon}
		}
		first := truncate(body[0], maxInt(m.width-5, 10)) // marker + space + icon + space
		if selected {
			return []string{m.highlightRow(glyph + " ✗ " + first)}
		}
		return []string{m.reasoningMarker(false, false) + " " + icon + " " + style.Render(first)}
	}

	lines := make([]string, 0, len(body)+1)
	if len(body) == 0 {
		if selected {
			return []string{m.highlightRow(glyph + " ✗"), ""}
		}
		return []string{m.reasoningMarker(true, false) + " " + icon, ""}
	}
	if selected {
		lines = append(lines, m.highlightRow(glyph+" ✗ "+body[0]))
	} else {
		lines = append(lines, m.reasoningMarker(true, false)+" "+icon+" "+style.Render(body[0]))
	}
	for _, text := range body[1:] {
		lines = append(lines, "   "+style.Render(text))
	}
	lines = append(lines, "")
	return lines
}

// renderInfoMessage renders command output / system notices in muted text.
func (m *ViewModel) renderInfoMessage(content string) []string {
	var lines []string
	for _, line := range wrapLines(strings.Split(strings.TrimSpace(content), "\n"), m.width) {
		if strings.TrimSpace(line) == "" {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, "  "+m.muted().Render(line))
	}
	lines = append(lines, "")
	return lines
}

// todoPanelLines renders the task list panel (empty when there are no tasks).
func (m *ViewModel) todoPanelLines() []string {
	if len(m.todos) == 0 {
		return nil
	}
	done := 0
	for _, item := range m.todos {
		if item.Status == TodoCompleted {
			done++
		}
	}

	width := maxInt(m.width-9, 10)
	head := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Primary)).Bold(true).
		Render(fmt.Sprintf("☑ 任务 %d/%d", done, len(m.todos)))
	budget := maxInt(m.height-7-len(m.questionPanelLines()), 0)
	if budget == 0 {
		return nil
	}
	if m.todosCollapsed {
		return []string{" " + head + m.muted().Render("  · 已折叠，点击展开")}
	}
	lines := []string{" " + head}
	// Keep room for the conversation and input; focus the active task when
	// the entire list cannot fit in a short terminal.
	maxShown := min(8, maxInt(budget-1, 0))
	if m.paletteActive() {
		maxShown = 0 // give command choices priority while completing input
	}
	start := 0
	for i, item := range m.todos {
		if item.Status == TodoInProgress {
			start = maxInt(min(i, len(m.todos)-maxShown), 0)
			break
		}
	}
	end := min(start+maxShown, len(m.todos))
	if end-start < len(m.todos) {
		lines[0] += m.muted().Render(fmt.Sprintf("  · 显示 %d/%d 项", end-start, len(m.todos)))
	}
	for _, item := range m.todos[start:end] {
		glyph := "○"
		style := m.muted()
		switch item.Status {
		case TodoCompleted:
			glyph = "✔"
			style = lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Success))
		case TodoInProgress:
			glyph = "▸"
			style = lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Warning)).Bold(true)
		}
		// A line break inside the task would make the panel taller than the
		// terminal and break the layout.
		lines = append(lines, "   "+style.Render(glyph+" "+truncate(oneLine(item.Content), width)))
	}
	return lines
}

// questionPanelLines renders the pending ask_user question and its options.
func (m *ViewModel) questionPanelLines() []string {
	if m.provider != nil {
		return m.providerPanelLines()
	}
	if m.mcpPanelState != nil {
		return m.mcpPanelLines()
	}
	q := m.question
	if q == nil {
		return nil
	}
	width := maxInt(m.width-10, 10)
	title := "❓ "
	if q.approval {
		title = "⚠ 工具审批："
	}
	head := m.warningStyle(true).Render(title + truncate(oneLine(q.question), width))
	lines := []string{" " + head}
	if q.detail != "" && q.approval && m.height >= 12 {
		lines = append(lines, m.muted().Render("   "+truncate(oneLine(q.detail), width)))
	}

	if len(q.options) > 0 && !q.typing {
		maxOptions := min(6, maxInt(m.height-8-len(lines), 1))
		sel := q.sel
		if sel < 0 || sel >= len(q.options) {
			sel = 0
		}
		start := maxInt(sel-maxOptions+1, 0)
		end := min(start+maxOptions, len(q.options))
		if len(q.options) > maxOptions {
			lines[0] += m.muted().Render(fmt.Sprintf("  · %d/%d", sel+1, len(q.options)))
		}
		for i := start; i < end; i++ {
			opt := q.options[i]
			marker := "  "
			if i == sel {
				marker = lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Accent)).Bold(true).Render("❯ ")
			}
			lines = append(lines, "   "+marker+fmt.Sprintf("%d. %s", i+1, truncate(oneLine(opt), width)))
		}
		hint := "   ↑↓ 选择 · enter 确认 · 直接输入自定义回答 · esc 取消"
		if q.approval {
			hint = "   ↑↓ 选择 · enter 确认 · esc 拒绝"
		}
		lines = append(lines, m.muted().Render(hint))
	} else {
		lines = append(lines, m.muted().Render("   enter 提交回答 · esc 取消"))
	}
	return lines
}

// View renders the interface
func (m *ViewModel) View() string {
	if m.width == 0 {
		return "Starting..."
	}

	m.refreshCache()

	viewport := m.viewportHeight()
	total := len(m.renderedLines)

	// Line-based scrolling: show the newest lines unless scrolled back.
	start := total - viewport - m.scrollOffset
	if start < 0 {
		start = 0
	}
	m.viewStart = start // remembered for mouse hit-testing
	end := start + viewport
	if end > total {
		end = total
	}
	visible := m.renderedLines[start:end]
	messageArea := strings.Join(visible, "\n")
	// Pad short conversations so the input box stays glued to the bottom of
	// the terminal instead of floating mid-screen.
	if pad := viewport - len(visible); pad > 0 {
		messageArea += strings.Repeat("\n", pad)
	}

	// ---- Header: title left, status right ----
	statusText := "ready"
	statusDot := m.theme.Success
	if m.provider != nil {
		statusText = "provider"
		statusDot = m.theme.Accent
	} else if m.mcpPanelState != nil {
		statusText = "mcp"
		statusDot = m.theme.Accent
	} else if m.question != nil {
		statusText = "question"
		statusDot = m.theme.Accent
		if m.question.approval {
			statusText = "approval"
			statusDot = m.theme.Warning
		}
	} else if m.isWaiting {
		statusText = "thinking"
		statusDot = m.theme.Warning
	} else if m.errorMsg != "" {
		statusText = "error"
		statusDot = m.theme.Error
	}
	dot := lipgloss.NewStyle().Foreground(lipgloss.Color(statusDot)).Render("●")
	status := dot + m.muted().Render(" "+statusText)
	title := lipgloss.NewStyle().Bold(true).Render("Oni3")
	if m.subtitle != "" {
		title += m.muted().Render("  /  " + oneLine(m.subtitle))
	}
	header := joinRow(title, status, m.width)
	rule := m.muted().Render(strings.Repeat("─", maxInt(m.width, 0)))

	// ---- Input area ----
	inputBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(m.theme.Muted)).
		Padding(0, 1).
		Width(maxInt(m.width-2, 1))

	var inputLine string
	if m.confirmQuit {
		inputLine = m.warningStyle(true).Render("⚠ 再按一次 Ctrl+C 退出（任意其他键取消）")
	} else if m.mcpPanelState != nil {
		inputLine = m.muted().Render("MCP 服务管理 · 空格 选中 · Tab 切换操作栏 · Enter 关闭 · Esc 取消")
	} else if m.provider != nil {
		inputLine = m.primaryStyle(true).Render("❯") + " " + m.renderEditableInput()
		if m.provider.stage == providerLoading {
			inputLine = m.muted().Render("正在获取模型列表… · esc 取消")
		}
	} else if m.question != nil {
		// Answering a question keeps the input editable while the turn waits.
		prompt := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Accent)).Bold(true).Render("?")
		inputLine = prompt + " " + m.renderEditableInput()
	} else if m.isWaiting {
		if m.stopArmed {
			inputLine = m.warningStyle(true).Render("⚠ 再按一次 Esc 停止当前回复（任意其他键取消）")
		} else {
			inputLine = m.muted().Render("正在处理任务…")
		}
	} else {
		prompt := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Primary)).Bold(true).Render("❯")
		inputLine = prompt + " " + m.renderEditableInput()
	}
	if m.input == "" && !m.isWaiting && !m.confirmQuit && m.question == nil && m.provider == nil {
		inputLine = m.primaryStyle(true).Render("❯") + " " + m.renderEditableInput() +
			m.muted().Render(" 描述任务，或输入 / 选择命令")
	}
	// Hints and confirmations must fit just like editable input.
	inputLine = ansi.Truncate(inputLine, maxInt(m.width-4, 1), "…")
	inputArea := inputBox.Render(inputLine)

	// ---- Slash command palette (rendered above the input box) ----
	palette := m.paletteView()
	paletteArea := ""
	if len(palette) > 0 {
		paletteArea = strings.Join(palette, "\n")
	}

	// ---- Help / status bar ----
	helpText := "↑↓ scroll · enter send · / commands"
	if m.width >= 90 {
		helpText += " · tab select · ctrl+o fold"
	}
	if m.provider != nil {
		helpText = "↑↓ select · enter confirm · esc cancel"
		if m.provider.stage == providerKey {
			helpText = "API Key · enter confirm · esc cancel"
		}
	} else if m.mcpPanelState != nil {
		helpText = "↑↓ select · space mark · tab actions · enter close · esc cancel"
	} else if m.question != nil {
		helpText = "↑↓ select · enter answer · esc cancel"
	} else if m.hasPalette() {
		helpText = "↑↓ select · tab complete · enter confirm"
	} else if m.isWaiting {
		helpText = "↑↓ scroll · esc esc stop"
	} else if m.selected >= 0 {
		helpText = "↑↓ scroll · tab next · ctrl+o fold · /help"
	}
	helpLeft := m.muted().Render(helpText)

	scrollInfo := ""
	if total > viewport {
		top := start + 1
		if m.scrollOffset == 0 {
			top = maxInt(total-viewport+1, 1)
		}
		scrollInfo = m.muted().Render(fmt.Sprintf("%d-%d/%d", top, end, total))
	}
	helpBar := joinRow(helpLeft, scrollInfo, m.width-2)

	// Layout (top to bottom): header, rule, messages, task panel, question
	// prompt / command palette, input box, help bar. The input box stays in a
	// fixed position when a panel appears, and the hint line sits directly
	// below it as the bottom-most row.
	parts := []string{header, rule, messageArea}
	if todoPanel := m.todoPanelLines(); len(todoPanel) > 0 {
		parts = append(parts, strings.Join(todoPanel, "\n"))
	}
	if questionPanel := m.questionPanelLines(); len(questionPanel) > 0 {
		parts = append(parts, strings.Join(questionPanel, "\n"))
	}
	if paletteArea != "" {
		parts = append(parts, paletteArea)
	}
	parts = append(parts, inputArea, helpBar)

	// Hard-clip every line to the terminal width: wrapped lines break the
	// renderer's cursor accounting and leave artifacts after resizes.
	lines := strings.Split(strings.Join(parts, "\n"), "\n")
	for i, line := range lines {
		if lipgloss.Width(line) > m.width {
			lines[i] = ansi.Truncate(line, m.width, "")
		}
	}
	// The frame must never be taller than the terminal: the renderer drops
	// the first line of an oversized frame, which hides the header, pushes
	// the input box off the screen and makes every repaint scroll.
	if m.height > 0 && len(lines) > m.height {
		excess := len(lines) - m.height
		if excess < viewport {
			lines = append(lines[:2], lines[2+excess:]...)
		} else {
			lines = lines[:m.height]
		}
	}
	return strings.Join(lines, "\n")
}

// renderEditableInput renders the input with a visible cursor position.
func (m *ViewModel) renderEditableInput() string {
	// Keep the editable cursor visible when moving back through a long
	// prompt. Newlines remain in the submitted text, but occupy one cell here.
	value, position := m.input, m.cursor
	if m.provider != nil && m.provider.stage == providerKey {
		value = strings.Repeat("•", len(m.provider.key))
		position = m.provider.keyCursor
	}
	runes := []rune(strings.NewReplacer("\n", "↵", "\r", " ", "\t", " ").Replace(value))
	cursor := min(maxInt(position, 0), len(runes))
	left := string(runes[:cursor])
	char, right := " ", ""
	if cursor < len(runes) {
		char = string(runes[cursor])
		right = string(runes[cursor+1:])
	}
	width := maxInt(m.width-6, 1) // border, padding and prompt
	leftRoom := maxInt(width-lipgloss.Width(char)-min(lipgloss.Width(right), width/3), 0)
	if w := lipgloss.Width(left); w > leftRoom {
		if leftRoom > 0 {
			left = "…" + ansi.Cut(left, w-leftRoom+1, w)
		} else {
			left = ""
		}
	}
	rightRoom := maxInt(width-lipgloss.Width(left)-lipgloss.Width(char), 0)
	return left + lipgloss.NewStyle().Reverse(true).Render(char) + ansi.Truncate(right, rightRoom, "…")
}

// joinRow places left and right text on one row with space between.
func joinRow(left, right string, width int) string {
	if width <= 0 {
		return ""
	}
	right = ansi.Truncate(right, width, "…")
	rightLen := lipgloss.Width(right)
	if rightLen >= width {
		return right
	}
	room := width - rightLen
	if rightLen > 0 {
		room-- // leave a gap before status / scroll position
	}
	left = ansi.Truncate(left, room, "…")
	gap := width - lipgloss.Width(left) - rightLen
	return left + strings.Repeat(" ", gap) + right
}

// ---- shared small styles ----

// indentLines prefixes each line with the given indent.
func indentLines(lines []string, indent string) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		if line == "" {
			out[i] = ""
		} else {
			out[i] = indent + line
		}
	}
	return out
}

// wrapLines hard-wraps plain text lines to the terminal width.
func wrapLines(lines []string, width int) []string {
	if width <= 0 {
		return lines
	}
	limit := width - 4
	if limit < 1 {
		limit = 1
	}
	var out []string
	for _, line := range lines {
		// Wrap by display cells: every rendered row must stay within the
		// terminal width, or the viewport math below miscounts.
		out = append(out, strings.Split(ansi.Hardwrap(line, limit, true), "\n")...)
	}
	return out
}

// firstLine returns the first non-empty line of a text, truncated to 160 runes.
func firstLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return truncate(line, 160)
		}
	}
	return ""
}

// truncate cuts plain text to maxWidth terminal cells with an ellipsis.
// Widths are cells, not runes: CJK text occupies two cells per character, so
// rune-based truncation used to overflow the row and wrap onto a second line.
func truncate(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	if maxWidth == 1 {
		return "…"
	}
	return ansi.Truncate(s, maxWidth, "…")
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// summarizeArgs condenses tool arguments into a short preview. The preview
// stays on one line: a newline in the arguments broke the tool row apart.
func summarizeArgs(args string) string {
	args = strings.TrimSpace(args)
	if args == "" || args == "{}" {
		return ""
	}
	if strings.HasPrefix(args, "{") && strings.HasSuffix(args, "}") {
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(args), &parsed); err == nil {
			keys := make([]string, 0, len(parsed))
			for k := range parsed {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			var parts []string
			for _, k := range keys {
				parts = append(parts, fmt.Sprintf("%s=%s", k, summarizeValue(parsed[k])))
			}
			if len(parts) > 0 {
				return truncate(strings.Join(parts, " "), 60)
			}
		}
	}
	return truncate(oneLine(args), 60)
}

// summarizeValue renders one argument value compactly. Nested structures are
// reduced to their most descriptive part instead of a Go map dump.
func summarizeValue(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return truncate(oneLine(v), 40)
	case bool:
		return strconv.FormatBool(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case []interface{}:
		if len(v) == 0 {
			return "[]"
		}
		if len(v) == 1 {
			return summarizeValue(v[0])
		}
		return fmt.Sprintf("%d 项", len(v))
	case map[string]interface{}:
		// Prefer a field that carries the meaning of the object.
		for _, key := range []string{"content", "title", "name", "text", "path", "url", "query", "command", "description"} {
			if s, ok := v[key]; ok {
				if str, ok := s.(string); ok && strings.TrimSpace(str) != "" {
					return truncate(oneLine(str), 40)
				}
			}
		}
		return fmt.Sprintf("%d 个字段", len(v))
	default:
		return truncate(oneLine(fmt.Sprintf("%v", v)), 40)
	}
}

// oneLine collapses a text into a single line.
func oneLine(text string) string {
	text = strings.ReplaceAll(text, "\r\n", " ")
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "\t", " ")
	text = strings.Join(strings.Fields(text), " ")
	return text
}

// renderMarkdown renders markdown content, falling back to plain text.
func (m *ViewModel) renderMarkdown(content string) string {
	if m.renderer == nil {
		return content
	}
	rendered, err := m.renderer.Render(content)
	if err != nil {
		return content
	}
	return strings.TrimSpace(rendered)
}
