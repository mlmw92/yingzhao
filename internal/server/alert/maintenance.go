package alert

import (
	"log/slog"
	"sync"

	"github.com/mlmw92/yingzhao/internal/model"
	"github.com/mlmw92/yingzhao/internal/server/configdoc"
	"gopkg.in/yaml.v3"
)

// MaintenanceStore 管理维护窗口配置，持久化到配置文档存储。
type MaintenanceStore struct {
	mu sync.RWMutex
	mw model.MaintenanceWindow
	p  configdoc.Persister
}

// NewMaintenanceStore 用文件持久化（测试与迁移源）。
func NewMaintenanceStore(path string) *MaintenanceStore {
	return NewMaintenanceStoreWithPersister(configdoc.NewFilePersister(path))
}

// NewMaintenanceStoreWithPersister 用指定持久化后端创建存储并加载。
func NewMaintenanceStoreWithPersister(p configdoc.Persister) *MaintenanceStore {
	s := &MaintenanceStore{p: p}
	s.load()
	return s
}

// Get 返回当前维护窗口配置。
func (s *MaintenanceStore) Get() model.MaintenanceWindow {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mw
}

// Set 更新维护窗口并持久化。
func (s *MaintenanceStore) Set(mw model.MaintenanceWindow) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mw = mw
	s.persistLocked()
}

// IsActive 判断维护窗口是否处于活跃状态（当前时间在窗口内）。
func (s *MaintenanceStore) IsActive(now int64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.mw.Enabled {
		return false
	}
	return now >= s.mw.Start && now <= s.mw.End
}

func (s *MaintenanceStore) load() {
	data, err := s.p.Load()
	if err != nil {
		return
	}
	_ = yaml.Unmarshal(data, &s.mw)
}

func (s *MaintenanceStore) persistLocked() {
	data, err := yaml.Marshal(s.mw)
	if err != nil {
		return
	}
	if err := s.p.Save(data); err != nil {
		slog.Warn("写入维护窗口配置失败", "err", err, "doc", s.p.Name())
	}
}
