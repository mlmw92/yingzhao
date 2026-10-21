// Package dashboard 管理用户自定义仪表盘。
//
// 仪表盘配置独立持久化（生产为 MySQL 的 config_documents，测试用文件替身），
// 与 ui/screen 配置同级，由 Web 端增删改写入，不受升级覆盖；保存即生效。
package dashboard

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/mlmw92/yingzhao/internal/server/configdoc"
	"gopkg.in/yaml.v3"
)

// ChartType 面板图表类型，与指标目录 ChartType 对应。
type ChartType string

const (
	// ChartLine 折线图。
	ChartLine ChartType = "line"
	// ChartArea 面积图。
	ChartArea ChartType = "area"
	// ChartBar 柱状图。
	ChartBar ChartType = "bar"
	// ChartGauge 仪表盘（单值）。
	ChartGauge ChartType = "gauge"
)

// PanelSize 面板栅格宽度档位。
type PanelSize string

const (
	// SizeHalf 半宽（默认，一行两个面板）。
	SizeHalf PanelSize = "half"
	// SizeFull 全宽（独占一行）。
	SizeFull PanelSize = "full"
)

// Panel 仪表盘中的一个面板。
type Panel struct {
	Title     string            `yaml:"title" json:"title"`         // 面板标题
	ChartType ChartType         `yaml:"chartType" json:"chartType"` // 图表类型
	Metric    string            `yaml:"metric" json:"metric"`       // 指标名（来自指标目录）
	Node      string            `yaml:"node,omitempty" json:"node,omitempty"`         // 限定主机（空=全部）
	Instance  string            `yaml:"instance,omitempty" json:"instance,omitempty"` // 限定中间件实例标识（可选）
	Labels    map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`     // 附加筛选标签（如 disk/iface）
	Range     string            `yaml:"range,omitempty" json:"range,omitempty"`       // 时间范围，如 1h/6h/24h/7d
	Step      int64             `yaml:"step,omitempty" json:"step,omitempty"`         // 步长（毫秒），0 用默认
	Size      PanelSize         `yaml:"size,omitempty" json:"size,omitempty"`         // 宽度档位：half/full（空=half）
}

// Dashboard 一块自定义看板。
type Dashboard struct {
	ID      string  `yaml:"id" json:"id"`
	Name    string  `yaml:"name" json:"name"`
	Updated int64   `yaml:"updated" json:"updated"` // 最后更新毫秒时间戳
	Panels  []Panel `yaml:"panels" json:"panels"`
}

// fileModel 磁盘文件结构。
type fileModel struct {
	Dashboards []Dashboard `yaml:"dashboards"`
}

// Manager 仪表盘配置管理。
type Manager struct {
	p          configdoc.Persister
	dashboards []Dashboard
}

// New 用文件持久化（测试与迁移源）。
func New(path string) (*Manager, error) {
	return NewWithPersister(configdoc.NewFilePersister(path))
}

// NewWithPersister 用指定持久化后端加载仪表盘配置；文档不存在则初始化为空。
func NewWithPersister(p configdoc.Persister) (*Manager, error) {
	m := &Manager{p: p}
	data, err := p.Load()
	if errors.Is(err, configdoc.ErrNotFound) {
		m.dashboards = []Dashboard{}
		return m, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取仪表盘配置失败: %w", err)
	}
	var fm fileModel
	if err := yaml.Unmarshal(data, &fm); err != nil {
		return nil, fmt.Errorf("解析仪表盘配置失败: %w", err)
	}
	// 补全空 ID（兼容旧数据）。
	for i := range fm.Dashboards {
		if fm.Dashboards[i].ID == "" {
			fm.Dashboards[i].ID = fmt.Sprintf("db-%d", time.Now().UnixNano())
		}
	}
	m.dashboards = fm.Dashboards
	return m, nil
}

// List 返回全部看板（按名称排序的副本）。
func (m *Manager) List() []Dashboard {
	out := make([]Dashboard, len(m.dashboards))
	copy(out, m.dashboards)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Get 按 ID 取看板。
func (m *Manager) Get(id string) (Dashboard, bool) {
	for _, d := range m.dashboards {
		if d.ID == id {
			return d, true
		}
	}
	return Dashboard{}, false
}

// Create 新建看板（生成 ID 并落盘）。
func (m *Manager) Create(name string, panels []Panel) (Dashboard, error) {
	if name == "" {
		return Dashboard{}, fmt.Errorf("看板名称不能为空")
	}
	d := Dashboard{
		ID:      fmt.Sprintf("db-%d", time.Now().UnixNano()),
		Name:    name,
		Updated: time.Now().UnixMilli(),
		Panels:  panels,
	}
	m.dashboards = append(m.dashboards, d)
	if err := m.save(); err != nil {
		// 回滚
		m.dashboards = m.dashboards[:len(m.dashboards)-1]
		return Dashboard{}, err
	}
	return d, nil
}

// Update 更新看板（名称/面板），不存在则返回错误。
func (m *Manager) Update(id, name string, panels []Panel) error {
	for i, d := range m.dashboards {
		if d.ID == id {
			m.dashboards[i].Name = name
			m.dashboards[i].Panels = panels
			m.dashboards[i].Updated = time.Now().UnixMilli()
			return m.save()
		}
	}
	return fmt.Errorf("看板不存在: %s", id)
}

// Delete 删除看板。
func (m *Manager) Delete(id string) error {
	for i, d := range m.dashboards {
		if d.ID == id {
			m.dashboards = append(m.dashboards[:i], m.dashboards[i+1:]...)
			return m.save()
		}
	}
	return fmt.Errorf("看板不存在: %s", id)
}

// save 写入持久化后端。
//
// 不再保留旧文件的 .bak 副本：dev-server 迁移前会单独备份现有文件，
// 生产环境的历史由数据库备份负责。
func (m *Manager) save() error {
	fm := fileModel{Dashboards: m.dashboards}
	data, err := yaml.Marshal(fm)
	if err != nil {
		return err
	}
	return m.p.Save(data)
}
