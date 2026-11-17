package acp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/coder/acp-go-sdk"
	"github.com/tk103331/oni3/config"
)

func TestAvailableCommands(t *testing.T) {
	cfg := &config.Config{
		Models: map[string]config.Model{"alpha": {Provider: "p", Model: "alpha"}},
		Skills: map[string]config.Skill{"review": {Description: "代码审查"}},
	}
	cmds := availableCommands(cfg)

	var names []string
	for _, c := range cmds {
		names = append(names, c.Name)
		if c.Description == "" {
			t.Fatalf("command %q has no description", c.Name)
		}
	}
	for _, want := range []string{"clear", "model", "skill", "tools", "status"} {
		if !contains(names, want) {
			t.Fatalf("availableCommands missing %q: %v", want, names)
		}
	}

	// Without configured skills the /skill command is not advertised.
	cfg.Skills = nil
	cmds = availableCommands(cfg)
	for _, c := range cmds {
		if c.Name == "skill" {
			t.Fatal("/skill advertised without configured skills")
		}
	}

	// Commands with arguments carry an input hint.
	cfg.Skills = map[string]config.Skill{"review": {}}
	for _, c := range availableCommands(cfg) {
		if c.Name == "model" && (c.Input == nil || c.Input.Unstructured == nil || c.Input.Unstructured.Hint == "") {
			t.Fatalf("/model missing input hint: %+v", c.Input)
		}
		if c.Name == "clear" && c.Input != nil {
			t.Fatal("/clear should not take input")
		}
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func TestRunSlashCommand(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := &config.Config{
		Models: map[string]config.Model{
			"alpha": {Provider: "p", Model: "alpha"},
			"beta":  {Provider: "p", Model: "beta"},
		},
		Skills: map[string]config.Skill{"review": {System: "审查规则"}},
		Agents: map[string]config.Agent{
			"OniThree": {Model: "alpha", Tools: []string{"file_system"}, MCPServers: []string{"search"}},
		},
	}
	s := NewService("OniThree", cfg)
	s.sessions["sess-1"] = &acpSession{
		id:            "sess-1",
		cwd:           "/tmp",
		store:         newSessionStore("OniThree", "alpha"),
		modelOverride: "alpha",
	}
	sess := s.sessions["sess-1"]
	ctx := context.Background()

	cases := []struct {
		prompt string
		check  func(t *testing.T, reply string, sess *acpSession)
	}{
		{"/model beta", func(t *testing.T, reply string, sess *acpSession) {
			if !strings.Contains(reply, "beta") {
				t.Fatalf("model switch reply: %q", reply)
			}
			if sess.modelOverride != "beta" {
				t.Fatalf("modelOverride = %q, want beta", sess.modelOverride)
			}
		}},
		{"/model", func(t *testing.T, reply string, sess *acpSession) {
			if !strings.Contains(reply, "alpha") || !strings.Contains(reply, "beta") {
				t.Fatalf("model list reply: %q", reply)
			}
		}},
		{"/model bogus", func(t *testing.T, reply string, sess *acpSession) {
			if !strings.Contains(reply, "不存在") {
				t.Fatalf("unknown model reply: %q", reply)
			}
		}},
		{"/skill review", func(t *testing.T, reply string, sess *acpSession) {
			if !strings.Contains(reply, "review") {
				t.Fatalf("skill switch reply: %q", reply)
			}
			if sess.skillOverride != "review" {
				t.Fatalf("skillOverride = %q, want review", sess.skillOverride)
			}
		}},
		{"/skill:review", func(t *testing.T, reply string, sess *acpSession) {
			// A colon right after the command name is the same as a space.
			if sess.skillOverride != "review" {
				t.Fatalf("colon form not applied: %q (%q)", sess.skillOverride, reply)
			}
		}},
		{"/skill review 看看这个补丁", func(t *testing.T, reply string, sess *acpSession) {
			// A trailing message is accepted, but an ACP turn comes from the
			// next prompt, so the reply must say so.
			if sess.skillOverride != "review" || !strings.Contains(reply, "prompt") {
				t.Fatalf("skill with message: %q / %q", sess.skillOverride, reply)
			}
		}},
		{"/skill off", func(t *testing.T, reply string, sess *acpSession) {
			if sess.skillOverride != "" {
				t.Fatalf("skillOverride = %q, want empty after off", sess.skillOverride)
			}
		}},
		{"/tools", func(t *testing.T, reply string, sess *acpSession) {
			if !strings.Contains(reply, "file_system") || !strings.Contains(reply, "search") {
				t.Fatalf("tools reply: %q", reply)
			}
		}},
		{"/status", func(t *testing.T, reply string, sess *acpSession) {
			if !strings.Contains(reply, "OniThree") || !strings.Contains(reply, "/tmp") {
				t.Fatalf("status reply: %q", reply)
			}
		}},
		{"/bogus", func(t *testing.T, reply string, sess *acpSession) {
			if !strings.Contains(reply, "未知命令") {
				t.Fatalf("unknown command reply: %q", reply)
			}
		}},
	}
	for _, tc := range cases {
		reply := s.runSlashCommand(ctx, sess, tc.prompt)
		if strings.TrimSpace(reply) == "" {
			t.Fatalf("%s produced an empty reply", tc.prompt)
		}
		tc.check(t, reply, sess)
	}

	// The model switch recorded on the session survives a store reload.
	if model := sess.store.model(); model != "beta" {
		t.Fatalf("stored model = %q, want beta", model)
	}
}

func TestClearSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := &config.Config{
		Models: map[string]config.Model{"alpha": {Provider: "p", Model: "alpha"}},
		Agents: map[string]config.Agent{"OniThree": {Model: "alpha"}},
	}
	s := NewService("OniThree", cfg)
	sess := &acpSession{
		id:            "sess-1",
		history:       nil,
		store:         newSessionStore("OniThree", "alpha"),
		modelOverride: "beta",
	}
	oldID := sess.store.id()
	sess.store.addUser("旧消息")
	sess.history = sess.store.history()

	s.clearSession(sess)

	if len(sess.history) != 0 {
		t.Fatalf("history = %d messages after /clear, want 0", len(sess.history))
	}
	if sess.store.model() != "beta" {
		t.Fatalf("model selection lost after /clear: %q", sess.store.model())
	}
	if sess.titled {
		t.Fatal("titled should reset after /clear so the next prompt sets a title")
	}
	// The fresh store is a different on-disk conversation; the old one keeps
	// its content.
	if sess.store.id() == oldID {
		t.Fatal("/clear kept writing to the same on-disk conversation")
	}
	if loaded := loadSessionStore(oldID); loaded == nil || len(loaded.history()) != 1 {
		t.Fatalf("old conversation lost after /clear: %v", loaded)
	}
}

func TestSessionModel(t *testing.T) {
	cfg := &config.Config{
		Models: map[string]config.Model{"alpha": {Provider: "p", Model: "alpha"}},
		Agents: map[string]config.Agent{"OniThree": {Model: "alpha"}},
	}
	s := NewService("OniThree", cfg)
	sess := &acpSession{store: newSessionStore("OniThree", "alpha")}
	if got := s.sessionModel(sess); got != "alpha" {
		t.Fatalf("sessionModel = %q, want alpha", got)
	}
	sess.modelOverride = "beta"
	if got := s.sessionModel(sess); got != "beta" {
		t.Fatalf("sessionModel with override = %q, want beta", got)
	}
}

func TestSendAvailableCommands(t *testing.T) {
	cfg := &config.Config{
		Models: map[string]config.Model{"alpha": {Provider: "p", Model: "alpha"}},
		Agents: map[string]config.Agent{"OniThree": {Model: "alpha"}},
	}
	s := NewService("OniThree", cfg)
	// No connection: sendAvailableCommands must not panic.
	s.sendAvailableCommands(context.Background(), "sess-1")
}

// The available_commands_update notification serializes with the documented
// sessionUpdate discriminator and hint field.
func TestAvailableCommandsUpdateSerialization(t *testing.T) {
	update := acp.SessionUpdate{
		AvailableCommandsUpdate: &acp.SessionAvailableCommandsUpdate{
			AvailableCommands: []acp.AvailableCommand{
				{
					Name:        "model",
					Description: "切换模型",
					Input:       unstructuredInput("模型名"),
				},
			},
		},
	}
	data, err := update.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m["sessionUpdate"] != "available_commands_update" {
		t.Fatalf("sessionUpdate = %v, want available_commands_update", m["sessionUpdate"])
	}
	cmds, ok := m["availableCommands"].([]any)
	if !ok || len(cmds) != 1 {
		t.Fatalf("availableCommands wrong shape: %v", m["availableCommands"])
	}
	cmd := cmds[0].(map[string]any)
	if cmd["name"] != "model" {
		t.Fatalf("command name = %v", cmd["name"])
	}
	input, ok := cmd["input"].(map[string]any)
	if !ok || input["hint"] != "模型名" {
		t.Fatalf("input hint wrong: %v", cmd["input"])
	}
}
