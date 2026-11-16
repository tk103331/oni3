package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/tk103331/oni3/config"
	"github.com/tk103331/oni3/models"
)

type providerStage uint8

const (
	providerChoose providerStage = iota
	providerKey
	providerLoading
	providerModels
	providerFailed
)

// paneFocus is the pane that owns the arrow keys while models are listed:
// the action bar on top, or the model list below it.
type paneFocus uint8

const (
	paneFocusActions paneFocus = iota
	paneFocusList
)

// providerModelActions are the top action bar entries of the model stage.
const (
	providerActionSelectAll = "全部选中"
	providerActionClearAll  = "全部取消"
	providerActionAddAll    = "添加全部"
	providerActionAddPicked = "添加选中"
)

func providerModelActionNames() []string {
	return []string{
		providerActionSelectAll,
		providerActionClearAll,
		providerActionAddAll,
		providerActionAddPicked,
	}
}

// This modal owns its secret separately from the chat input. It is never
// recorded as a user message, command, ask_user answer or session event.
type providerState struct {
	cfg          *config.Config
	ctx          context.Context
	stage        providerStage
	name         string
	sel          int
	key          []rune
	keyCursor    int
	candidateKey string
	models       []models.DiscoveredModel
	// picked holds the model IDs marked with space in the model stage.
	picked map[string]bool
	focus  paneFocus
	action int // selected entry of the model action bar
	// savedKey reports that a key typed in this flow is now persisted.
	savedKey bool
	failure  string
	cancel   context.CancelFunc
	onAdd    func(string, []string, bool) CommandResult
}

type providerModelsMsg struct {
	flow   *providerState // identity only: a canceled/replaced flow ignores it
	models []models.DiscoveredModel
	err    error
}

type providerOption struct {
	label  string
	value  string
	name   string // emphasized part of the row (provider name)
	detail string // muted annotation (endpoint and description)
}

func (m *ViewModel) openProvider(cfg *config.Config, ctx context.Context, name string, onAdd func(string, []string, bool) CommandResult) CommandResult {
	if m.isWaiting || m.question != nil {
		return CommandResult{Message: "请等待当前回复结束后再选择 provider", Error: true}
	}
	if len(cfg.Providers) == 0 {
		return CommandResult{Message: "没有可用的 provider，请在配置或插件中添加 providers", Error: true}
	}
	if name != "" {
		if _, ok := cfg.Providers[name]; !ok {
			return CommandResult{Message: "provider 不存在: " + name, Error: true}
		}
	}
	m.closeProvider()
	if ctx == nil {
		ctx = context.Background()
	}
	m.provider = &providerState{cfg: cfg, ctx: ctx, stage: providerChoose, onAdd: onAdd}
	m.input, m.cursor = "", 0
	m.markDirty()
	if name != "" {
		return CommandResult{Cmd: m.chooseProvider(name)}
	}
	return CommandResult{}
}

func (m *ViewModel) chooseProvider(name string) tea.Cmd {
	p := m.provider
	p.name, p.sel, p.failure = name, 0, ""
	m.input, m.cursor = "", 0
	provider := p.cfg.Providers[name]
	if strings.TrimSpace(provider.APIKey) == "" && !provider.APIKeyOptional {
		p.stage = providerKey
		m.markDirty()
		return nil
	}
	return m.fetchProviderModels()
}

func (m *ViewModel) fetchProviderModels() tea.Cmd {
	p := m.provider
	if p.cancel != nil {
		p.cancel()
	}
	provider := p.cfg.Providers[p.name] // copy; the worker never accesses mutable config
	if p.candidateKey != "" {
		provider.APIKey = p.candidateKey
	}
	ctx, cancel := context.WithCancel(p.ctx)
	p.cancel = cancel
	p.stage, p.failure = providerLoading, ""
	m.input, m.cursor = "", 0
	m.markDirty()
	return func() tea.Msg {
		defer cancel()
		entries, err := models.DiscoverModels(ctx, provider)
		return providerModelsMsg{flow: p, models: entries, err: err}
	}
}

func (m *ViewModel) receiveProviderModels(msg providerModelsMsg) {
	p := m.provider
	if p == nil || p != msg.flow || p.stage != providerLoading {
		return
	}
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	if msg.err != nil {
		// Errors from external services must never echo the submitted key.
		text := msg.err.Error()
		for _, key := range []string{p.candidateKey, p.cfg.Providers[p.name].APIKey} {
			if key != "" {
				text = strings.ReplaceAll(text, key, "[redacted]")
			}
		}
		p.stage, p.failure, p.sel = providerFailed, oneLine(ansi.Strip(text)), 0
		m.markDirty()
		return
	}
	if len(msg.models) == 0 {
		p.stage, p.failure, p.sel = providerFailed, "provider 返回了空模型列表", 0
		m.markDirty()
		return
	}
	if p.candidateKey != "" {
		if err := p.cfg.SetProviderAPIKey(p.name, p.candidateKey); err != nil {
			p.stage, p.failure, p.sel = providerFailed, "API Key 保存失败，请检查本地状态目录的写入权限", 0
			m.markDirty()
			return
		}
		p.candidateKey = ""
		p.savedKey = true
	}
	p.models, p.stage, p.sel = msg.models, providerModels, 0 // default to the first model
	p.picked = map[string]bool{}
	p.focus, p.action = paneFocusList, 0
	m.input, m.cursor = "", 0
	m.markDirty()
}

// pickedIDs returns the marked model IDs in discovery order.
func (p *providerState) pickedIDs() []string {
	ids := make([]string, 0, len(p.picked))
	for _, model := range p.models {
		if p.picked[model.ID] {
			ids = append(ids, model.ID)
		}
	}
	return ids
}

// allIDs returns every discovered model ID in discovery order.
func (p *providerState) allIDs() []string {
	ids := make([]string, 0, len(p.models))
	for _, model := range p.models {
		ids = append(ids, model.ID)
	}
	return ids
}

func (m *ViewModel) closeProvider() {
	p := m.provider
	if p == nil {
		return
	}
	if p.cancel != nil {
		p.cancel()
	}
	clear(p.key)
	p.key, p.candidateKey = nil, ""
	m.provider = nil
	m.input, m.cursor = "", 0
	m.markDirty()
}

// providerChooseOrder lists providers for the picker: the built-in catalog
// order first (the vendors most users start with), then custom and plugin
// providers alphabetically.
func providerChooseOrder(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}
	rank := make(map[string]int)
	for i, preset := range config.BuiltinProviders() {
		rank[preset.Name] = i
	}
	names := sortedKeys(cfg.Providers)
	sort.SliceStable(names, func(i, j int) bool {
		ri, iBuiltin := rank[names[i]]
		rj, jBuiltin := rank[names[j]]
		switch {
		case iBuiltin && jBuiltin:
			return ri < rj
		case iBuiltin:
			return true
		case jBuiltin:
			return false
		}
		return names[i] < names[j]
	})
	return names
}

func (p *providerState) options(filter string) []providerOption {
	filter = strings.ToLower(strings.TrimSpace(filter))
	var result []providerOption
	switch p.stage {
	case providerChoose:
		for _, name := range providerChooseOrder(p.cfg) {
			provider := p.cfg.Providers[name]
			detail := strings.TrimSpace(provider.BaseURL)
			if description := strings.TrimSpace(provider.Description); description != "" {
				if detail != "" {
					detail += " · " + description
				} else {
					detail = description
				}
			}
			label := name
			if detail != "" {
				label += " · " + detail
			}
			if filter == "" || strings.Contains(strings.ToLower(label), filter) {
				result = append(result, providerOption{label: label, value: name, name: name, detail: detail})
			}
		}
	case providerModels:
		for _, model := range p.models {
			label := model.ID
			if model.Name != "" && model.Name != model.ID {
				label += " · " + model.Name
			}
			if filter == "" || strings.Contains(strings.ToLower(label), filter) {
				result = append(result, providerOption{label: label, value: model.ID})
			}
		}
	case providerFailed:
		result = []providerOption{{label: "重试获取模型列表"}, {label: "重新输入 API Key"}}
	}
	return result
}

func (m *ViewModel) handleProviderKey(msg tea.KeyMsg) tea.Cmd {
	p := m.provider
	if msg.Type == tea.KeyEsc {
		m.closeProvider()
		return nil
	}
	if p.stage == providerLoading {
		return nil
	}
	if p.stage == providerKey {
		return m.handleProviderSecretKey(msg)
	}
	options := p.options(m.input)
	if p.stage == providerModels {
		return m.handleProviderModelKey(msg, options)
	}
	switch msg.Type {
	case tea.KeyUp, tea.KeyDown, tea.KeyPgUp, tea.KeyPgDown:
		if len(options) > 0 {
			delta := 1
			if msg.Type == tea.KeyUp || msg.Type == tea.KeyPgUp {
				delta = -1
			}
			if msg.Type == tea.KeyPgUp || msg.Type == tea.KeyPgDown {
				delta *= 5
			}
			p.sel = (p.sel + delta%len(options) + len(options)) % len(options)
		}
	case tea.KeyEnter:
		if len(options) == 0 {
			return nil
		}
		p.sel = min(max(p.sel, 0), len(options)-1)
		option := options[p.sel]
		switch p.stage {
		case providerChoose:
			return m.chooseProvider(option.value)
		case providerFailed:
			if p.sel == 1 {
				p.stage, p.failure, p.sel = providerKey, "", 0
				p.candidateKey = ""
			} else {
				return m.fetchProviderModels()
			}
		}
	default:
		if p.stage == providerFailed {
			return nil
		}
		m.editProviderFilter(msg)
		p.sel = 0
	}
	m.markDirty()
	return nil
}

// handleProviderModelKey drives the multi-select model stage: Tab switches
// between the action bar and the list, space marks a model, Enter adds.
func (m *ViewModel) handleProviderModelKey(msg tea.KeyMsg, options []providerOption) tea.Cmd {
	p := m.provider
	actions := providerModelActionNames()
	switch msg.Type {
	case tea.KeyTab:
		if p.focus == paneFocusActions {
			p.focus = paneFocusList
		} else {
			p.focus = paneFocusActions
		}
	case tea.KeyShiftTab:
		if p.focus == paneFocusActions {
			p.focus = paneFocusList
		} else {
			p.focus = paneFocusActions
		}
	case tea.KeyLeft, tea.KeyRight:
		if p.focus == paneFocusActions && len(actions) > 0 {
			delta := 1
			if msg.Type == tea.KeyLeft {
				delta = -1
			}
			p.action = (p.action%len(actions) + delta + len(actions)) % len(actions)
		}
	case tea.KeyUp, tea.KeyDown, tea.KeyPgUp, tea.KeyPgDown:
		if p.focus == paneFocusActions {
			// Down leaves the action bar and enters the list.
			if msg.Type == tea.KeyDown || msg.Type == tea.KeyPgDown {
				p.focus = paneFocusList
			}
			break
		}
		if len(options) > 0 {
			delta := 1
			if msg.Type == tea.KeyUp || msg.Type == tea.KeyPgUp {
				delta = -1
			}
			if msg.Type == tea.KeyPgUp || msg.Type == tea.KeyPgDown {
				delta *= 5
			}
			p.sel = (p.sel + delta%len(options) + len(options)) % len(options)
		}
	case tea.KeySpace:
		if p.focus == paneFocusActions {
			break
		}
		if len(options) == 0 {
			break
		}
		p.sel = min(max(p.sel, 0), len(options)-1)
		p.failure = ""
		id := options[p.sel].value
		if p.picked == nil {
			p.picked = map[string]bool{}
		}
		if p.picked[id] {
			delete(p.picked, id)
		} else {
			p.picked[id] = true
		}
	case tea.KeyEnter:
		if p.focus == paneFocusActions {
			p.action = min(max(p.action, 0), len(actions)-1)
			return m.runProviderAction(actions[p.action])
		}
		if len(options) == 0 {
			return nil
		}
		p.sel = min(max(p.sel, 0), len(options)-1)
		ids := p.pickedIDs()
		if len(ids) == 0 {
			// Nothing marked: Enter adds the highlighted model.
			ids = []string{options[p.sel].value}
		}
		return m.addProviderModels(p.name, ids, len(ids) == 1)
	default:
		// Typing filters again, so the list takes the focus back.
		p.focus = paneFocusList
		m.editProviderFilter(msg)
		p.sel = 0
	}
	m.markDirty()
	return nil
}

// runProviderAction executes one entry of the model action bar.
func (m *ViewModel) runProviderAction(action string) tea.Cmd {
	p := m.provider
	p.failure = ""
	switch action {
	case providerActionSelectAll:
		p.picked = map[string]bool{}
		for _, id := range p.allIDs() {
			p.picked[id] = true
		}
	case providerActionClearAll:
		p.picked = map[string]bool{}
	case providerActionAddAll:
		return m.addProviderModels(p.name, p.allIDs(), false)
	case providerActionAddPicked:
		ids := p.pickedIDs()
		if len(ids) == 0 {
			p.failure = "请先用空格选择模型"
			m.markDirty()
			return nil
		}
		return m.addProviderModels(p.name, ids, len(ids) == 1)
	}
	m.markDirty()
	return nil
}

// addProviderModels applies a model import and closes the modal on success.
func (m *ViewModel) addProviderModels(provider string, ids []string, single bool) tea.Cmd {
	p := m.provider
	res := p.onAdd(provider, ids, single)
	if !res.Error {
		m.closeProvider()
	}
	m.applyCommandResult(res)
	return res.Cmd
}

func (m *ViewModel) editProviderFilter(msg tea.KeyMsg) {
	switch msg.Type {
	case tea.KeyRunes:
		for _, r := range msg.Runes {
			if !unicode.IsControl(r) {
				m.insertRune(r)
			}
		}
	case tea.KeySpace:
		m.insertRune(' ')
	case tea.KeyBackspace:
		m.backspace()
	case tea.KeyDelete:
		m.deleteAtCursor()
	case tea.KeyLeft:
		m.cursor = max(m.cursor-1, 0)
	case tea.KeyRight:
		m.cursor = min(m.cursor+1, len([]rune(m.input)))
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
	}
}

func (m *ViewModel) handleProviderSecretKey(msg tea.KeyMsg) tea.Cmd {
	p := m.provider
	switch msg.Type {
	case tea.KeyEnter:
		key := strings.TrimSpace(string(p.key))
		if key == "" {
			p.failure = "请输入 API Key，或按 Esc 取消"
			break
		}
		p.candidateKey = key
		clear(p.key)
		p.key, p.keyCursor = nil, 0
		return m.fetchProviderModels()
	case tea.KeyRunes:
		for _, r := range msg.Runes {
			if unicode.IsControl(r) || unicode.IsSpace(r) {
				continue
			}
			if len(p.key) >= 4096 {
				break
			}
			p.key = append(p.key, 0)
			copy(p.key[p.keyCursor+1:], p.key[p.keyCursor:])
			p.key[p.keyCursor] = r
			p.keyCursor++
		}
	case tea.KeyBackspace:
		if p.keyCursor > 0 {
			copy(p.key[p.keyCursor-1:], p.key[p.keyCursor:])
			p.key[len(p.key)-1] = 0
			p.key = p.key[:len(p.key)-1]
			p.keyCursor--
		}
	case tea.KeyDelete:
		if p.keyCursor < len(p.key) {
			copy(p.key[p.keyCursor:], p.key[p.keyCursor+1:])
			p.key[len(p.key)-1] = 0
			p.key = p.key[:len(p.key)-1]
		}
	case tea.KeyLeft:
		p.keyCursor = max(p.keyCursor-1, 0)
	case tea.KeyRight:
		p.keyCursor = min(p.keyCursor+1, len(p.key))
	case tea.KeyCtrlA:
		p.keyCursor = 0
	case tea.KeyCtrlE:
		p.keyCursor = len(p.key)
	case tea.KeyCtrlU:
		copy(p.key, p.key[p.keyCursor:])
		clear(p.key[len(p.key)-p.keyCursor:])
		p.key = p.key[:len(p.key)-p.keyCursor]
		p.keyCursor = 0
	case tea.KeyCtrlK:
		clear(p.key[p.keyCursor:])
		p.key = p.key[:p.keyCursor]
	}
	m.markDirty()
	return nil
}

func (m *ViewModel) providerPanelLines() []string {
	p := m.provider
	width := max(m.width-2, 1)
	title := "选择 provider"
	switch p.stage {
	case providerKey:
		title = p.name + " · 输入 API Key（隐藏输入）"
	case providerLoading:
		title = p.name + " · 正在获取模型列表…"
	case providerModels:
		title = fmt.Sprintf("%s · 选择模型（已选 %d 个）", p.name, len(p.pickedIDs()))
		if p.savedKey {
			title += " · API Key 已保存"
		}
	case providerFailed:
		title = p.name + " · 获取模型列表失败"
	}
	lines := []string{m.primaryStyle(true).Render(truncate(" "+oneLine(ansi.Strip(title)), width))}
	if p.stage == providerKey {
		if keyURL := strings.TrimSpace(p.cfg.Providers[p.name].KeyURL); keyURL != "" {
			lines = append(lines, m.muted().Render(truncate(" 获取 API Key："+keyURL, width)))
		}
	}
	if p.failure != "" {
		lines = append(lines, m.errorStyle(false).Render(truncate(" "+oneLine(ansi.Strip(p.failure)), width)))
	}
	hint := " 输入关键字筛选 · ↑↓ 选择（首尾循环） · Enter 确认 · Esc 取消"
	if p.stage == providerKey {
		hint = " Enter 提交 · Esc 取消；成功获取列表后保存 Key"
	}
	if p.stage == providerLoading {
		hint = " Esc 取消请求"
	}
	options := p.options(m.input)
	// The model stage has its own action bar and keys.
	if p.stage == providerModels {
		hint = " 空格 选中/取消 · ↑↓ 选择 · Enter 添加 · Tab 切换操作栏 · Esc 取消"
		lines = append(lines, m.providerActionBar(p, width))
	}
	// Keep the key hints directly above the candidate list.
	if len(options) > 0 {
		lines = append(lines, m.muted().Render(truncate(hint, width)))
	}
	if len(options) > 0 {
		maxOptions := min(8, max(m.height-10-len(lines), 1))
		sel := min(max(p.sel, 0), len(options)-1)
		start := max(sel-maxOptions+1, 0)
		end := min(start+maxOptions, len(options))
		for i := start; i < end; i++ {
			marker := "   "
			if i == sel {
				marker = " ❯ "
			}
			if p.stage == providerChoose {
				lines = append(lines, m.formatProviderOptionRow(marker, options[i], width, i == sel))
				continue
			}
			text := options[i].label
			if p.stage == providerModels {
				box := "[ ]"
				if p.picked[options[i].value] {
					box = "[x]"
				}
				text = box + " " + text
			}
			line := truncate(marker+oneLine(ansi.Strip(text)), width)
			if i == sel && (p.stage != providerModels || p.focus == paneFocusList) {
				line = m.accentStyle(true).Render(line)
			}
			lines = append(lines, line)
		}
	}
	// Without candidates (key entry, loading, failure) the hint stays at the
	// bottom of the panel.
	if len(options) == 0 {
		lines = append(lines, m.muted().Render(truncate(hint, width)))
	}
	return lines
}

// formatProviderOptionRow renders one provider candidate: the name is
// emphasized (accent when selected, bold otherwise) while the endpoint and
// description stay muted.
func (m *ViewModel) formatProviderOptionRow(marker string, option providerOption, width int, selected bool) string {
	avail := maxInt(width-lipgloss.Width(marker), 1)
	name := truncate(oneLine(option.name), avail)
	nameStyled := lipgloss.NewStyle().Bold(true).Render(name)
	if selected {
		nameStyled = m.accentStyle(true).Render(name)
	}
	line := marker + nameStyled
	if rest := avail - lipgloss.Width(name) - 2; rest > 0 && option.detail != "" {
		line += m.muted().Render("  " + truncate(oneLine(option.detail), rest))
	}
	return line
}

// providerActionBar renders the top actions of the model stage; the focused
// action is emphasized while Tab owns the bar.
func (m *ViewModel) providerActionBar(p *providerState, width int) string {
	names := providerModelActionNames()
	parts := make([]string, 0, len(names))
	sep := m.muted().Render("│")
	for i, name := range names {
		label := " " + name + " "
		if p.focus == paneFocusActions && i == p.action {
			label = m.accentStyle(true).Render(" ❯ " + name + " ")
		} else {
			label = m.muted().Render(label)
		}
		parts = append(parts, label)
	}
	return truncate(strings.Join(parts, sep), width)
}

// addProviderModels runs on the TUI update goroutine after discovery completes.
func (app *AgentApp) addProviderModels(provider string, ids []string, single bool) CommandResult {
	cfg := config.GetConfig()
	previous := app.currentModelName()
	names, err := cfg.AddProviderModels(provider, ids)
	if err != nil {
		return CommandResult{Message: fmt.Sprintf("添加模型失败: %v", err), Error: true}
	}
	if single {
		if len(names) != 1 {
			return CommandResult{Message: "请选择一个模型", Error: true}
		}
		res := app.selectModel(names[0])
		if res.Error {
			res.Message = "模型已添加；" + res.Message
		}
		return res
	}
	// Importing can change the alphabetically first default. Pin the previous
	// effective model and refresh its credentials without changing selection.
	if previous != "" {
		res := app.selectModel(previous)
		if res.Error {
			res.Message = fmt.Sprintf("已添加 %d 个模型；", len(names)) + res.Message
			return res
		}
	} else if app.currentModelName() != "" {
		if err := app.rebuildAgent(); err != nil {
			return CommandResult{Message: fmt.Sprintf("已添加 %d 个模型；当前模型初始化失败: %v", len(names), err), Error: true}
		}
	}
	app.registerCommands()
	app.syncSessionConfig()
	return CommandResult{Message: fmt.Sprintf("已添加 %d 个模型，可通过 /model 选择", len(names)), Subtitle: app.headerSubtitle()}
}
