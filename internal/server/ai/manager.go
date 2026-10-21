// Package ai 管理 AI 助手（大模型）的独立配置文件。
//
// 与 notify 包同构：配置存于 <DataDir>/ai.yaml（由 Config.AIFile 指定），与 server.yaml 解耦。
// 独立文件的理由是"**保存即生效**"——换模型/换端点不该重启服务，而 server.yaml 是启动期配置。
//
// **本包不做任何自动调用**：所有外呼都由显式请求触发（配置测试、诊断），
// 默认关闭时连一条外呼路径都不存在。
package ai

import (
	"errors"
	"log/slog"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/mlmw92/yingzhao/internal/server/config"
	"github.com/mlmw92/yingzhao/internal/server/configdoc"
)

// Manager AI 配置管理器（内存态 + 配置文档，读写都带锁）。
type Manager struct {
	p   configdoc.Persister
	mu  sync.RWMutex
	cur config.AIConfig
}

// New 用文件持久化（测试与迁移源）。
func New(path string, initial config.AIConfig) (*Manager, error) {
	return NewWithPersister(configdoc.NewFilePersister(path), initial)
}

// NewWithPersister 用指定持久化后端加载配置；文档不存在则用 initial 初始化并落盘。
func NewWithPersister(p configdoc.Persister, initial config.AIConfig) (*Manager, error) {
	m := &Manager{p: p}
	if err := m.loadOrInit(initial); err != nil {
		return nil, err
	}
	return m, nil
}

// loadOrInit 加载或初始化配置。
func (m *Manager) loadOrInit(initial config.AIConfig) error {
	data, err := m.p.Load()
	if err == nil {
		var c config.AIConfig
		if err := yaml.Unmarshal(data, &c); err != nil {
			return err
		}
		// 归一化：配置文件可能是手写的（超时为 0 会变成"无限等待"）。
		m.cur = c.Normalize()
		slog.Info("已加载 AI 配置", "doc", m.p.Name(), "enabled", m.cur.Enabled, "model", m.cur.Model)
		return nil
	}
	if errors.Is(err, configdoc.ErrNotFound) {
		m.cur = initial.Normalize()
		if err := m.persist(); err != nil {
			return err
		}
		slog.Info("AI 配置不存在，已用默认值初始化（默认关闭）", "doc", m.p.Name())
		return nil
	}
	return err
}

// Get 返回当前配置（**含明文 APIKey**，仅供服务端内部使用，不要直接返回给前端）。
func (m *Manager) Get() config.AIConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cur
}

// Save 归一化后原子落盘并热生效。
//
// 敏感字段（APIKey）由调用方在传入前处理（空值沿用旧值，照 handleNotifyPut 的做法）——
// 放这里会让"空值"与"清空"两种意图无法区分。
func (m *Manager) Save(c config.AIConfig) error {
	c = c.Normalize()
	m.mu.Lock()
	defer m.mu.Unlock()
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	if err := m.p.Save(data); err != nil {
		return err
	}
	m.cur = c
	// 只记端点主机与模型，**不记 APIKey**。
	slog.Info("AI 配置已保存并生效", "doc", m.p.Name(), "enabled", c.Enabled, "model", c.Model)
	return nil
}

// persist 写当前配置（初始化阶段使用，调用方已持锁或尚无并发）。
func (m *Manager) persist() error {
	data, err := yaml.Marshal(m.cur)
	if err != nil {
		return err
	}
	return m.p.Save(data)
}
