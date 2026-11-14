package agent

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
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
	failure      string
	cancel       context.CancelFunc
	onAdd        func(string, []string, bool) CommandResult
}

type providerModelsMsg struct {
	flow   *providerState // identity only: a canceled/replaced flow ignores it
	models []models.DiscoveredModel
	err    error
}

type providerOption struct {
	label string
	value string
	all   bool
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
	}
	p.models, p.stage, p.sel = msg.models, providerModels, 1 // default to one model
	m.input, m.cursor = "", 0
	m.markDirty()
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

func (p *providerState) options(filter string) []providerOption {
	filter = strings.ToLower(strings.TrimSpace(filter))
	var result []providerOption
	switch p.stage {
	case providerChoose:
		for _, name := range sortedKeys(p.cfg.Providers) {
			label := name
			if description := p.cfg.Providers[name].Description; description != "" {
				label += " · " + description
			}
			if filter == "" || strings.Contains(strings.ToLower(label), filter) {
				result = append(result, providerOption{label: label, value: name})
			}
		}
	case providerModels:
		if filter == "" {
			result = append(result, providerOption{label: fmt.Sprintf("全部添加（%d 个模型）", len(p.models)), all: true})
		}
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
		case providerModels:
			ids := []string{option.value}
			if option.all {
				ids = make([]string, 0, len(p.models))
				for _, model := range p.models {
					ids = append(ids, model.ID)
				}
			}
			res := p.onAdd(p.name, ids, !option.all)
			if !res.Error {
				m.closeProvider()
			}
			m.applyCommandResult(res)
			return res.Cmd
		}
	default:
		if p.stage == providerFailed {
			return nil
		}
		m.editProviderFilter(msg)
		p.sel = 0
		if p.stage == providerModels && m.input == "" && len(p.options(m.input)) > 1 {
			p.sel = 1
		}
	}
	m.markDirty()
	return nil
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
		title = p.name + " · 选择模型添加并切换，或全部添加"
	case providerFailed:
		title = p.name + " · 获取模型列表失败"
	}
	lines := []string{m.primaryStyle(true).Render(truncate(" "+oneLine(ansi.Strip(title)), width))}
	if p.failure != "" {
		lines = append(lines, m.errorStyle(false).Render(truncate(" "+oneLine(ansi.Strip(p.failure)), width)))
	}
	options := p.options(m.input)
	if len(options) > 0 {
		maxOptions := min(6, max(m.height-10-len(lines), 1))
		sel := min(max(p.sel, 0), len(options)-1)
		start := max(sel-maxOptions+1, 0)
		end := min(start+maxOptions, len(options))
		for i := start; i < end; i++ {
			marker := "   "
			if i == sel {
				marker = " ❯ "
			}
			line := truncate(marker+oneLine(ansi.Strip(options[i].label)), width)
			if i == sel {
				line = m.accentStyle(true).Render(line)
			}
			lines = append(lines, line)
		}
	}
	hint := " 输入关键字筛选 · ↑↓ 选择 · Enter 确认 · Esc 取消"
	if p.stage == providerKey {
		hint = " Enter 提交 · Esc 取消；成功获取列表后保存 Key"
	}
	if p.stage == providerLoading {
		hint = " Esc 取消请求"
	}
	lines = append(lines, m.muted().Render(truncate(hint, width)))
	return lines
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
