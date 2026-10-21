// Package screencfg 管理数据大屏模块显隐的独立配置文件。
// 配置默认存于 <DataDir>/screen.yaml（由 Config.ScreenFile 指定），与 server.yaml 解耦；
// 文件不存在时用默认全开配置初始化并落盘（向后兼容旧部署）。
package screencfg

import (
	"errors"
	"log/slog"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/mlmw92/yingzhao/internal/server/config"
	"github.com/mlmw92/yingzhao/internal/server/configdoc"
)

// Manager 大屏配置管理器。
type Manager struct {
	p       configdoc.Persister
	mu      sync.RWMutex
	current config.ScreenConfig
}

// New 用文件持久化（测试与迁移源）。
func New(path string, initial config.ScreenConfig) (*Manager, error) {
	return NewWithPersister(configdoc.NewFilePersister(path), initial)
}

// NewWithPersister 用指定持久化后端加载配置；文档不存在则用 initial 初始化并落盘。
func NewWithPersister(p configdoc.Persister, initial config.ScreenConfig) (*Manager, error) {
	m := &Manager{p: p}
	if err := m.loadOrInit(initial); err != nil {
		return nil, err
	}
	return m, nil
}

// loadOrInit 加载或初始化配置。
func (m *Manager) loadOrInit(initial config.ScreenConfig) error {
	data, err := m.p.Load()
	if err == nil {
		var c config.ScreenConfig
		if err := yaml.Unmarshal(data, &c); err != nil {
			return err
		}
		if c.Modules == nil {
			c.Modules = initial.Modules
		} else {
			// 老配置补齐新增模块 key（默认开启），避免升级后新板块不显示
			for k, v := range initial.Modules {
				if _, ok := c.Modules[k]; !ok {
					c.Modules[k] = v
				}
			}
		}
		// 老配置缺省或非预设档位时回退到默认值
		if !config.IsValidScreenRefreshInterval(c.RefreshInterval) {
			c.RefreshInterval = initial.RefreshInterval
		}
		// 老配置显式填写但非省级行政区时回退（留空表示自动探测，保持不变）
		if c.DeployLocation != "" && !config.IsValidDeployLocation(c.DeployLocation) {
			c.DeployLocation = initial.DeployLocation
		}
		m.current = c
		slog.Info("已加载大屏配置文件", "doc", m.p.Name())
		return nil
	}
	if errors.Is(err, configdoc.ErrNotFound) {
		m.current = initial
		if err := m.persist(); err != nil {
			return err
		}
		slog.Info("大屏配置不存在，已用默认配置初始化", "doc", m.p.Name())
		return nil
	}
	return err
}

// Get 返回当前内存中的配置。
func (m *Manager) Get() config.ScreenConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current
}

// Save 校验后原子落盘。
func (m *Manager) Save(c config.ScreenConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c.Modules == nil {
		c.Modules = map[string]bool{}
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	if err := m.p.Save(data); err != nil {
		return err
	}
	m.current = c
	slog.Info("大屏配置已保存", "doc", m.p.Name())
	return nil
}

// persist 原子写当前配置（初始化阶段使用）。
func (m *Manager) persist() error {
	data, err := yaml.Marshal(m.current)
	if err != nil {
		return err
	}
	return m.p.Save(data)
}
