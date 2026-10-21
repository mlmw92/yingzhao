package dialtest

import (
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/mlmw92/yingzhao/internal/server/configdoc"
)

// Store 管理拨测任务配置，持久化到配置文档。
type Store struct {
	mu          sync.RWMutex
	tasks       map[string]Task
	lastResults map[string]Result // 最近一次拨测结果（按 TaskID），含异常原因，供前端展示
	p           configdoc.Persister
}

// NewStore 用文件持久化（测试与迁移源）。
func NewStore(path string) *Store {
	return NewStoreWithPersister(configdoc.NewFilePersister(path))
}

// NewStoreWithPersister 用指定持久化后端创建拨测任务存储并加载。
func NewStoreWithPersister(p configdoc.Persister) *Store {
	s := &Store{
		tasks:       map[string]Task{},
		lastResults: map[string]Result{},
		p:           p,
	}
	s.load()
	return s
}

// List 返回所有任务。
func (s *Store) List() []Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		out = append(out, t)
	}
	return out
}

// Get 返回单个任务。
func (s *Store) Get(id string) (Task, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[id]
	return t, ok
}

// Create 创建任务。
func (s *Store) Create(t Task) Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t.ID == "" {
		t.ID = "dt-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	t = normalizeTask(t)
	s.tasks[t.ID] = t
	s.persistLocked()
	return t
}

// Update 更新任务。
func (s *Store) Update(t Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tasks[t.ID]; !ok {
		return os.ErrNotExist
	}
	s.tasks[t.ID] = normalizeTask(t)
	s.persistLocked()
	return nil
}

// Delete 删除任务。
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tasks[id]; !ok {
		return os.ErrNotExist
	}
	delete(s.tasks, id)
	s.persistLocked()
	return nil
}

func (s *Store) load() {
	data, err := s.p.Load()
	if err != nil {
		return
	}
	var list []Task
	if err := yaml.Unmarshal(data, &list); err != nil {
		return
	}
	for _, t := range list {
		if t.ID == "" {
			t.ID = "dt-" + strconv.FormatInt(time.Now().UnixNano(), 36)
		}
		s.tasks[t.ID] = normalizeTask(t)
	}
}

func normalizeTask(t Task) Task {
	if t.Interval <= 0 {
		t.Interval = defaultIntervalSeconds
	}
	if t.Timeout <= 0 {
		t.Timeout = 10
	}
	return t
}

func (s *Store) persistLocked() {
	list := make([]Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		list = append(list, t)
	}
	data, err := yaml.Marshal(list)
	if err != nil {
		return
	}
	_ = s.p.Save(data)
}

func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return "."
}

// Errorf 格式化错误。
func Errorf(format string, args ...interface{}) error {
	return fmt.Errorf(format, args...)
}

// RecordResult 记录某任务最近一次拨测结果（含异常原因），供 API 拼接到 latest 响应。
func (s *Store) RecordResult(r Result) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastResults[r.TaskID] = r
}

// LastResults 返回所有任务最近一次拨测结果快照（只读副本）。
func (s *Store) LastResults() map[string]Result {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]Result, len(s.lastResults))
	for k, v := range s.lastResults {
		out[k] = v
	}
	return out
}
