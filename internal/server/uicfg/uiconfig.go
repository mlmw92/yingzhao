// Package uicfg 管理系统 UI 品牌配置：系统名称、Logo 与侧栏副标题。
// 配置独立持久化（生产为 MySQL 的 config_documents，测试用文件替身），由 Web 端设置写入，
// 与 notify/screen 配置同级，独立于 server.yaml，便于运行时修改且不受升级覆盖。
package uicfg

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/mlmw92/yingzhao/internal/server/configdoc"
)

const (
	// MaxNameLen 系统名称最大字符数（按 rune 计）。
	MaxNameLen = 64
	// MaxLogoLen Logo（data URL 明文）最大字节数，约对应 3MB 原图。
	MaxLogoLen = 4 * 1024 * 1024
	// MaxFooterLen 页脚文本最大字符数（按 rune 计）。
	MaxFooterLen = 512
	// MaxSubtitleLen 侧栏副标题最大字符数（按 rune 计）。
	MaxSubtitleLen = 32
)

// UIConfig 系统 UI 品牌配置。
type UIConfig struct {
	Name     string `yaml:"name" json:"name"`         // 系统名称
	Logo     string `yaml:"logo" json:"logo"`         // 可选 Logo：图片 data URL（data:image/...;base64,）或 http(s) 链接
	Footer   string `yaml:"footer" json:"footer"`     // 页脚文本（支持 HTML），空则隐藏
	Subtitle string `yaml:"subtitle" json:"subtitle"` // 侧栏副标题（如「运维平台」），留空则不显示
	// Disabled 是已关闭的模块或页面 key（见前端 modules/catalog.js）。
	//
	// **用关闭清单而不是开启清单**：全量开启时不需要任何配置，
	// 且升级新增的页面会自动出现；开启清单会让存量部署升级后静默看不到新功能。
	Disabled []string `yaml:"disabled" json:"disabled,omitempty"`
}

// LockedModuleKeys 是不可关闭的模块与页面：
// overview/alerts 是「发现与处置」主线的两端，system 是唯一能改回配置的入口，
// alerting 整域不可关（组内单页仍可关）。
var LockedModuleKeys = map[string]bool{
	"overview": true,
	"alerts":   true,
	"alerting": true,
	"system":   true,
}

// Disabled 判断某个模块/页面是否被关闭。
func (m *Manager) Disabled(key string) bool {
	if key == "" {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, k := range m.cfg.Disabled {
		if k == key {
			return true
		}
	}
	return false
}

// normalizeDisabled 去空白、去重，并拒绝不可关闭项。
func normalizeDisabled(list []string) ([]string, error) {
	out := make([]string, 0, len(list))
	seen := map[string]bool{}
	for _, raw := range list {
		k := strings.TrimSpace(raw)
		if k == "" || seen[k] {
			continue
		}
		if LockedModuleKeys[k] {
			return nil, fmt.Errorf("模块 %q 不允许关闭", k)
		}
		seen[k] = true
		out = append(out, k)
	}
	return out, nil
}

// DefaultUIConfig 返回默认品牌（系统名 英招，无自定义 Logo 时使用前端默认徽标）。
func DefaultUIConfig() UIConfig {
	return UIConfig{Name: "英招", Logo: "", Footer: "", Subtitle: "运维平台"}
}

// Manager 负责 UI 配置的加载与落盘。
type Manager struct {
	p configdoc.Persister
	// mu 保护 cfg：Disabled 会被调度器每秒读，Web 保存可能同时写。
	mu  sync.RWMutex
	cfg UIConfig
}

// New 用文件持久化（测试与迁移源）。
func New(path string, initial UIConfig) (*Manager, error) {
	return NewWithPersister(configdoc.NewFilePersister(path), initial)
}

// NewWithPersister 用指定持久化后端加载配置；文档不存在则用 initial 初始化并落盘。
func NewWithPersister(p configdoc.Persister, initial UIConfig) (*Manager, error) {
	m := &Manager{p: p, cfg: initial}
	data, err := p.Load()
	if err == nil {
		var c UIConfig
		if err := yaml.Unmarshal(data, &c); err != nil {
			return nil, fmt.Errorf("解析 UI 配置失败: %w", err)
		}
		if strings.TrimSpace(c.Name) == "" {
			c.Name = initial.Name
		}
		m.cfg = c
		return m, nil
	}
	if errors.Is(err, configdoc.ErrNotFound) {
		if err := m.Save(initial); err != nil {
			return nil, err
		}
		return m, nil
	}
	return nil, fmt.Errorf("读取 UI 配置失败: %w", err)
}

// Get 返回当前配置的副本。
func (m *Manager) Get() UIConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

// Save 校验并持久化新配置（先写内存，成功后再落盘）。
func (m *Manager) Save(c UIConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" {
		c.Name = DefaultUIConfig().Name
	}
	if len([]rune(c.Name)) > MaxNameLen {
		return fmt.Errorf("系统名称过长（上限 %d 字符）", MaxNameLen)
	}
	if len(c.Logo) > MaxLogoLen {
		return fmt.Errorf("Logo 数据过大（上限 %d 字节，建议小于 3MB）", MaxLogoLen)
	}
	if c.Logo != "" && !isValidLogo(c.Logo) {
		return fmt.Errorf("Logo 仅支持图片 data URL（data:image/...;base64,）或 http(s) 链接")
	}
	if len([]rune(c.Footer)) > MaxFooterLen {
		return fmt.Errorf("页脚文本过长（上限 %d 字符）", MaxFooterLen)
	}
	if len([]rune(c.Subtitle)) > MaxSubtitleLen {
		return fmt.Errorf("侧栏副标题过长（上限 %d 字符）", MaxSubtitleLen)
	}
	normalized, err := normalizeDisabled(c.Disabled)
	if err != nil {
		return err
	}
	c.Disabled = normalized
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	if err := m.p.Save(data); err != nil {
		return err
	}
	m.cfg = c
	return nil
}

// isValidLogo 校验 Logo 为图片 data URL 或 http(s) URL。
func isValidLogo(s string) bool {
	if strings.HasPrefix(s, "data:image/") && strings.Contains(s, "base64,") {
		return true
	}
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return true
	}
	return false
}
