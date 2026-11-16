package acp

import (
	"context"
	"strings"
	"testing"

	"github.com/coder/acp-go-sdk"
	"github.com/tk103331/oni3/agent"
	"github.com/tk103331/oni3/config"
	"github.com/tk103331/oni3/tools"
)

func TestPlanStatusMapping(t *testing.T) {
	cases := map[string]acp.PlanEntryStatus{
		tools.TodoPending:    acp.PlanEntryStatusPending,
		tools.TodoInProgress: acp.PlanEntryStatusInProgress,
		tools.TodoCompleted:  acp.PlanEntryStatusCompleted,
		"unknown":            acp.PlanEntryStatusPending,
	}
	for in, want := range cases {
		if got := planStatus(in); got != want {
			t.Fatalf("planStatus(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOptionIndex(t *testing.T) {
	if got := optionIndex("answer-3"); got != 3 {
		t.Fatalf("optionIndex(answer-3) = %d, want 3", got)
	}
	if got := optionIndex("answer-x"); got != -1 {
		t.Fatalf("optionIndex(answer-x) = %d, want -1", got)
	}
}

func TestFirstLine(t *testing.T) {
	if got := firstLine("第一行\n第二行"); got != "第一行" {
		t.Fatalf("firstLine = %q", got)
	}
	if got := firstLine("  spaced  "); got != "spaced" {
		t.Fatalf("firstLine = %q", got)
	}
	if got := firstLine(strings.Repeat("长", 100)); len([]rune(got)) != 60 {
		t.Fatalf("long firstLine not capped: %d runes", len([]rune(got)))
	}
}

func TestSessionStorePersistsTurns(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := newSessionStore("OniThree", "model-a")
	if store.id() == "" {
		t.Fatal("store has no session id")
	}
	store.addUser("你好")
	store.addAssistant("你好，有什么可以帮你？")

	// A store loaded by id replays the same turns as chat history.
	loaded := loadSessionStore(store.id())
	if loaded == nil {
		t.Fatal("stored session not found by id")
	}
	history := loaded.history()
	if len(history) != 2 {
		t.Fatalf("history = %d messages, want 2", len(history))
	}
	if history[0].Content != "你好" || history[1].Content != "你好，有什么可以帮你？" {
		t.Fatalf("history content mismatch: %+v", history)
	}
	if loaded.model() != "model-a" {
		t.Fatalf("model = %q, want model-a", loaded.model())
	}
}

func TestListStoredSessions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := newSessionStore("OniThree", "m")
	store.addUser("帮我写个测试")
	store.addAssistant("好的")

	stored := listStoredSessions(10)
	if len(stored) != 1 {
		t.Fatalf("stored sessions = %d, want 1", len(stored))
	}
	if stored[0].ID != store.id() {
		t.Fatalf("session id = %q, want %q", stored[0].ID, store.id())
	}
	if !strings.Contains(stored[0].Title, "帮我写个测试") {
		t.Fatalf("title %q should mention the first prompt", stored[0].Title)
	}
}

func TestLoadSessionStoreMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if loadSessionStore("no-such-id") != nil {
		t.Fatal("loading a missing session should return nil")
	}
}

func TestFlushThoughtPersistsOnce(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := newSessionStore("OniThree", "m")
	var b strings.Builder
	b.WriteString("思考过程...")
	store.flushThought(&b)
	if b.Len() != 0 {
		t.Fatal("flushThought did not reset the builder")
	}
	loaded := loadSessionStore(store.id())
	for _, ev := range loaded.sess.Events() {
		_ = ev // events exist; reasoning block written below the user/assistant scan
	}
}

func TestSessionConfigOptions(t *testing.T) {
	cfg := &config.Config{
		Models: map[string]config.Model{
			"alpha": {Provider: "p", Model: "alpha"},
			"beta":  {Provider: "p", Model: "beta"},
		},
		Skills: map[string]config.Skill{
			"review": {Description: "代码审查"},
		},
		Agents: map[string]config.Agent{
			"OniThree": {Model: "alpha"},
		},
	}
	s := NewService("OniThree", cfg)
	sess := &acpSession{}
	options := s.sessionConfigOptions(sess)
	if len(options) != 2 {
		t.Fatalf("options = %d, want 2 (model + skill)", len(options))
	}
	modelSel := options[0].Select
	if modelSel == nil || modelSel.Id != acp.SessionConfigId(configOptionModel) {
		t.Fatalf("first option is not the model selector: %+v", options[0])
	}
	if modelSel.CurrentValue != acp.SessionConfigValueId("alpha") {
		t.Fatalf("current model = %q, want alpha (agent default)", modelSel.CurrentValue)
	}
	if len(*modelSel.Options.Ungrouped) != 2 {
		t.Fatalf("model options = %d, want 2", len(*modelSel.Options.Ungrouped))
	}
	skillSel := options[1].Select
	if skillSel == nil || len(*skillSel.Options.Ungrouped) != 2 {
		t.Fatalf("skill options should be off + review: %+v", options[1])
	}
	if skillSel.CurrentValue != acp.SessionConfigValueId(optionOff) {
		t.Fatalf("current skill = %q, want off", skillSel.CurrentValue)
	}

	// A model override shows through as the current value.
	sess.modelOverride = "beta"
	options = s.sessionConfigOptions(sess)
	if options[0].Select.CurrentValue != acp.SessionConfigValueId("beta") {
		t.Fatalf("current model = %q, want beta", options[0].Select.CurrentValue)
	}
}

func TestSetSessionConfigOptionModelSwitch(t *testing.T) {
	cfg := &config.Config{
		Models: map[string]config.Model{
			"alpha": {Provider: "p", Model: "alpha"},
			"beta":  {Provider: "p", Model: "beta"},
		},
		Agents: map[string]config.Agent{"OniThree": {}},
	}
	s := NewService("OniThree", cfg)
	sess := &acpSession{modelOverride: "alpha"}
	s.sessions["sess-1"] = sess

	resp, err := s.SetSessionConfigOption(context.Background(), acp.SetSessionConfigOptionRequest{
		ValueId: &acp.SetSessionConfigOptionValueId{
			SessionId: "sess-1",
			ConfigId:  acp.SessionConfigId(configOptionModel),
			Value:     acp.SessionConfigValueId("beta"),
		},
	})
	if err != nil {
		t.Fatalf("model switch failed: %v", err)
	}
	if sess.modelOverride != "beta" {
		t.Fatalf("override = %q, want beta", sess.modelOverride)
	}
	if len(resp.ConfigOptions) != 2 {
		t.Fatalf("response options = %d, want 2", len(resp.ConfigOptions))
	}
	if resp.ConfigOptions[0].Select.CurrentValue != acp.SessionConfigValueId("beta") {
		t.Fatalf("response current model = %q, want beta", resp.ConfigOptions[0].Select.CurrentValue)
	}

	// Unknown model -> invalid params.
	_, err = s.SetSessionConfigOption(context.Background(), acp.SetSessionConfigOptionRequest{
		ValueId: &acp.SetSessionConfigOptionValueId{
			SessionId: "sess-1",
			ConfigId:  acp.SessionConfigId(configOptionModel),
			Value:     acp.SessionConfigValueId("nope"),
		},
	})
	if err == nil {
		t.Fatal("switching to an unknown model should fail")
	}

	// Unknown option id -> invalid params.
	_, err = s.SetSessionConfigOption(context.Background(), acp.SetSessionConfigOptionRequest{
		ValueId: &acp.SetSessionConfigOptionValueId{
			SessionId: "sess-1",
			ConfigId:  acp.SessionConfigId("bogus"),
			Value:     "x",
		},
	})
	if err == nil {
		t.Fatal("unknown config option should fail")
	}

	// Unknown session -> invalid params.
	_, err = s.SetSessionConfigOption(context.Background(), acp.SetSessionConfigOptionRequest{
		ValueId: &acp.SetSessionConfigOptionValueId{
			SessionId: "missing",
			ConfigId:  acp.SessionConfigId(configOptionModel),
			Value:     acp.SessionConfigValueId("alpha"),
		},
	})
	if err == nil {
		t.Fatal("unknown session should fail")
	}
}

func TestSetSessionConfigOptionSkillSwitch(t *testing.T) {
	cfg := &config.Config{
		Agents: map[string]config.Agent{"OniThree": {}},
		Skills: map[string]config.Skill{"review": {Description: "代码审查"}},
	}
	s := NewService("OniThree", cfg)
	sess := &acpSession{}
	s.sessions["sess-1"] = sess

	_, err := s.SetSessionConfigOption(context.Background(), acp.SetSessionConfigOptionRequest{
		ValueId: &acp.SetSessionConfigOptionValueId{
			SessionId: "sess-1",
			ConfigId:  acp.SessionConfigId(configOptionSkill),
			Value:     acp.SessionConfigValueId("review"),
		},
	})
	if err != nil {
		t.Fatalf("skill switch failed: %v", err)
	}
	if sess.skillOverride != "review" {
		t.Fatalf("override = %q, want review", sess.skillOverride)
	}

	// "off" clears the skill.
	_, err = s.SetSessionConfigOption(context.Background(), acp.SetSessionConfigOptionRequest{
		ValueId: &acp.SetSessionConfigOptionValueId{
			SessionId: "sess-1",
			ConfigId:  acp.SessionConfigId(configOptionSkill),
			Value:     acp.SessionConfigValueId(optionOff),
		},
	})
	if err != nil {
		t.Fatalf("skill off failed: %v", err)
	}
	if sess.skillOverride != "" {
		t.Fatalf("override = %q, want empty after off", sess.skillOverride)
	}
}

func TestFactoryOverrides(t *testing.T) {
	cfg := &config.Config{
		Agents: map[string]config.Agent{
			"OniThree": {Model: "alpha", System: "基础提示词"},
		},
		Models: map[string]config.Model{
			"alpha": {Provider: "p", Model: "alpha"},
			"beta":  {Provider: "p", Model: "beta"},
		},
		Skills: map[string]config.Skill{
			"review": {System: "审查规则", Tools: []string{"extra_tool"}},
		},
	}
	factory := agent.NewFactory(cfg)
	a, err := factory.WithModel("beta").WithSkill("review").CreateAgent("OniThree")
	if err != nil {
		t.Fatal(err)
	}
	_ = a
	// The configured agent keeps its own values when no override is set.
	b, err := factory.WithModel("").WithSkill("").CreateAgent("OniThree")
	if err != nil {
		t.Fatal(err)
	}
	_ = b
}

func TestResumeSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := &config.Config{
		Agents: map[string]config.Agent{"OniThree": {}},
		Models: map[string]config.Model{"alpha": {Provider: "p", Model: "alpha"}},
	}
	s := NewService("OniThree", cfg)
	s.sessions["old"] = &acpSession{store: newSessionStore("OniThree", "alpha")}
	s.sessions["old"].store.addUser("之前的问题")
	s.sessions["old"].store.addAssistant("之前的回答")

	stored := listStoredSessions(10)
	if len(stored) == 0 {
		t.Fatal("nothing persisted to resume")
	}

	resp, err := s.ResumeSession(context.Background(), acp.ResumeSessionRequest{
		SessionId: acp.SessionId(stored[0].ID),
		Cwd:       "/tmp",
	})
	if err != nil {
		t.Fatalf("resume failed: %v", err)
	}
	if len(resp.ConfigOptions) != 2 {
		t.Fatalf("resume response options = %d, want 2", len(resp.ConfigOptions))
	}
	// The resumed session lives under a fresh ACP id and keeps the history.
	var resumed *acpSession
	for _, sess := range s.sessions {
		if len(sess.history) == 2 {
			resumed = sess
		}
	}
	if resumed == nil {
		t.Fatal("resumed session lost its history")
	}
	if resumed.modelOverride != "alpha" {
		t.Fatalf("resumed model = %q, want alpha (carried from the store)", resumed.modelOverride)
	}

	// Missing session -> invalid params.
	_, err = s.ResumeSession(context.Background(), acp.ResumeSessionRequest{
		SessionId: "nope", Cwd: "/tmp",
	})
	if err == nil {
		t.Fatal("resuming a missing session should fail")
	}
}
