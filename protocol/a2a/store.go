package a2a

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/tk103331/oni3/state"
)

// Store persists tasks as individual JSON files and keeps loaded tasks in
// memory for fast polling.
type Store struct {
	dir string

	mu    sync.RWMutex
	tasks map[string]*Task
}

// NewStore creates a task store under dir. An empty dir uses
// ~/.oni3/a2a/tasks.
func NewStore(dir string) *Store {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		if base := state.Dir(); base != "" {
			dir = filepath.Join(base, "a2a", "tasks")
		}
	}
	return &Store{dir: dir, tasks: make(map[string]*Task)}
}

func (s *Store) path(id string) string {
	if s.dir == "" {
		return ""
	}
	if validateTaskID(id) != nil {
		return ""
	}
	return filepath.Join(s.dir, id+".json")
}

// Save persists a snapshot of task. The caller must not mutate task while
// Save runs.
func (s *Store) Save(task *Task) error {
	if task == nil || task.ID == "" {
		return fmt.Errorf("task id is required")
	}
	if err := validateTaskID(task.ID); err != nil {
		return err
	}
	snapshot := cloneTask(task)
	normalizeTaskRoles(snapshot)
	s.mu.Lock()
	s.tasks[task.ID] = snapshot
	s.mu.Unlock()

	path := s.path(task.ID)
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".task-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	return os.Rename(name, path)
}

// Get returns a task snapshot, loading it from disk when necessary.
func (s *Store) Get(id string) (*Task, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, false
	}
	s.mu.RLock()
	task := s.tasks[id]
	s.mu.RUnlock()
	if task != nil {
		return cloneTask(task), true
	}
	path := s.path(id)
	if path == "" {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var loaded Task
	if err := json.Unmarshal(data, &loaded); err != nil || loaded.ID == "" {
		return nil, false
	}
	normalizeTaskRoles(&loaded)
	s.mu.Lock()
	s.tasks[loaded.ID] = cloneTask(&loaded)
	s.mu.Unlock()
	return cloneTask(&loaded), true
}

// List returns tasks, newest first, filtered by context and state.
func (s *Store) List(contextID, state string) []*Task {
	s.loadAll()
	s.mu.RLock()
	out := make([]*Task, 0, len(s.tasks))
	for _, task := range s.tasks {
		if contextID != "" && task.ContextID != contextID {
			continue
		}
		if state != "" && task.Status.State != state {
			continue
		}
		out = append(out, cloneTask(task))
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		return out[i].Status.Timestamp > out[j].Status.Timestamp
	})
	return out
}

func (s *Store) loadAll() {
	if s.dir == "" {
		return
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		s.Get(id)
	}
}

func cloneTask(task *Task) *Task {
	if task == nil {
		return nil
	}
	data, err := json.Marshal(task)
	if err != nil {
		return task
	}
	var out Task
	if err := json.Unmarshal(data, &out); err != nil {
		return task
	}
	return &out
}

func normalizeTaskRoles(task *Task) {
	if task == nil {
		return
	}
	for i := range task.History {
		task.History[i].Role = NormalizeRole(task.History[i].Role)
	}
	if task.Status.Message != nil {
		task.Status.Message.Role = NormalizeRole(task.Status.Message.Role)
	}
}
