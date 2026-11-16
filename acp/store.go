package acp

import (
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/tk103331/oni3/session"
)

// sessionStore persists one ACP conversation to the same on-disk session
// files the TUI uses, so `oni3 /resume` (or another ACP session/load) can
// pick the conversation up later. It is a thin wrapper so the ACP code does
// not reach into session internals directly.
type sessionStore struct {
	sess *session.Session
}

// newSessionStore creates the persisted conversation for a fresh ACP session.
func newSessionStore(agentName, model string) *sessionStore {
	return &sessionStore{sess: session.New(session.KindAgent, agentName, model, "")}
}

// loadSessionStore reopens a stored conversation; nil when it cannot be found.
func loadSessionStore(id string) *sessionStore {
	sess, err := session.Find(session.KindAgent, id)
	if err != nil || sess == nil {
		return nil
	}
	return &sessionStore{sess: sess}
}

// id exposes the persisted session id (the ACP session id after a load).
func (s *sessionStore) id() string {
	if s == nil || s.sess == nil {
		return ""
	}
	return s.sess.ID
}

// model returns the model recorded in the session header.
func (s *sessionStore) model() string {
	if s == nil || s.sess == nil {
		return ""
	}
	return s.sess.Model
}

// setModel records a runtime model switch in the persisted conversation.
func (s *sessionStore) setModel(model string) {
	if s == nil || s.sess == nil {
		return
	}
	_ = s.sess.SetConfig(s.sess.Agent, model, s.sess.Skill)
}

// setSkill records a runtime skill switch in the persisted conversation.
func (s *sessionStore) setSkill(skill string) {
	if s == nil || s.sess == nil {
		return
	}
	_ = s.sess.SetConfig(s.sess.Agent, s.sess.Model, skill)
}

// addUser/addAssistant persist one completed turn pair. Errors are logged by
// the session package itself; a failed write must not break the live turn.
func (s *sessionStore) addUser(text string) {
	if s == nil || s.sess == nil {
		return
	}
	_ = s.sess.AddUser(text)
}

func (s *sessionStore) addAssistant(text string) {
	if s == nil || s.sess == nil {
		return
	}
	_ = s.sess.AddAssistant(text)
}

// flushThought persists the reasoning streamed during the turn as one block.
func (s *sessionStore) flushThought(thought *strings.Builder) {
	if s == nil || s.sess == nil || thought == nil || thought.Len() == 0 {
		return
	}
	_ = s.sess.AddReasoning(thought.String())
	thought.Reset()
}

func (s *sessionStore) addReasoning(text string) {
	if s == nil || s.sess == nil || strings.TrimSpace(text) == "" {
		return
	}
	_ = s.sess.AddReasoning(text)
}

func (s *sessionStore) addToolStart(name, arguments string) {
	if s == nil || s.sess == nil {
		return
	}
	_ = s.sess.AddToolStart(name, arguments)
}

func (s *sessionStore) addToolEnd(name, result string, failed bool) {
	if s == nil || s.sess == nil {
		return
	}
	_ = s.sess.AddToolEnd(name, result, failed)
}

// history replays the persisted user/assistant turns as chat context.
func (s *sessionStore) history() []*schema.Message {
	if s == nil || s.sess == nil {
		return nil
	}
	events := s.sess.Events()
	out := make([]*schema.Message, 0, len(events))
	for _, ev := range events {
		switch ev.Type {
		case session.EventUser:
			if strings.TrimSpace(ev.Content) != "" {
				out = append(out, schema.UserMessage(ev.Content))
			}
		case session.EventAssistant:
			if strings.TrimSpace(ev.Content) != "" {
				out = append(out, schema.AssistantMessage(ev.Content, nil))
			}
		}
	}
	return out
}

// listStoredSessions returns the persisted agent conversations, newest first,
// as ACP session info. The ACP id is the persisted session id so
// session/load and the TUI /resume refer to the same conversation.
func listStoredSessions(limit int) []acpStoredSession {
	sessions, err := session.List(session.KindAgent, limit)
	if err != nil {
		return nil
	}
	out := make([]acpStoredSession, 0, len(sessions))
	for _, sess := range sessions {
		out = append(out, acpStoredSession{
			ID:    sess.ID,
			Cwd:   "",
			Title: fmt.Sprintf("%s · %s", sess.Agent, firstLine(sess.LastUserMessage())),
			Time:  sess.Updated,
		})
	}
	return out
}

// acpStoredSession is the flattened view used by ListSessions.
type acpStoredSession struct {
	ID    string
	Cwd   string
	Title string
	Time  time.Time
}

// firstLine trims a message to its first line for a compact title.
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	if len([]rune(s)) > 60 {
		r := []rune(s)
		s = string(r[:57]) + "..."
	}
	return s
}
