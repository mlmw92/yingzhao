package approval

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mlmw92/yingzhao/internal/server/configdoc"
)

// 受管入口的标识（配置里 requireFor 的取值）。
//
// 为什么是"入口"而不是"权限点"：权限点是**谁能做**，审批要问的是**哪件事需要第二个人**。
// 同一个 ops:exec 既覆盖只读查询也覆盖重启服务，按权限点开关会把排障查询也拦下来——
// 那会让人绕开审批走别的入口，反而更糟（设计件 D1）。
const (
	// EntryOpsWrite 所有**写类** ops 动作（按动作目录的 ReadOnly 判定，不是列清单）。
	//
	// 用"写类"而不是逐个列动作名：新增一个写动作时**自动**纳入审批（fail-closed），
	// 而列清单的写法会让新动作默认不受管（fail-open）——安全开关的方向不能反。
	EntryOpsWrite = "ops:write"
	// EntryDefense fail2ban 处置下发（封禁/解封会改变目标机器状态）。
	EntryDefense = "defense"
	// EntrySystemUpgrade Server 自身升级/回滚。
	EntrySystemUpgrade = "system:upgrade"
	// EntryAgentUpgrade Agent 升级下发。
	EntryAgentUpgrade = "agent:upgrade"
)

// 权限点：看 / 批 / 改配置分开。
//
// 批准单独设点而不是复用某个既有权限：审批是"给别人放行"，与"自己能做"是两件事——
// 只有同时具备两者才批得动（见 UnderlyingPerm）。
const (
	// PermRead 查看审批单（自己的与待自己审批的）。
	PermRead = "approval:read"
	// PermApprove 批准或驳回。
	PermApprove = "approval:approve"
	// PermManage 修改审批开关与范围。
	PermManage = "approval:manage"
)

// UnderlyingPerm 返回"批准该动作还需要它本身的哪个权限点"。
//
// 为什么批准要**两个**权限同时具备：否则一个"只会点批准"的角色可以批准它自己都做不到的事，
// 审批就退化成了橡皮图章——真正的把关人反而是那个没有执行权的角色，这与双人复核的初衷相反。
// 返回空串表示没有可对应的权限点（未知入口，按"只查 approval:approve"处理）。
func UnderlyingPerm(entry string) string {
	switch {
	case entry == EntryDefense:
		return "security:write"
	case entry == EntrySystemUpgrade:
		return "system:upgrade"
	case entry == EntryAgentUpgrade:
		return "agent:upgrade"
	case entry == EntryOpsWrite || strings.HasPrefix(entry, "ops:"):
		return "ops:exec"
	}
	return ""
}

// entryLabels 是入口的中文名（界面与提示里用，避免各处自己拼）。
var entryLabels = map[string]string{
	EntryOpsWrite:      "写类操作动作（重启服务、文件分发等）",
	EntryDefense:       "入侵防御处置下发（封禁/解封）",
	EntrySystemUpgrade: "Server 自身升级与回滚",
	EntryAgentUpgrade:  "Agent 升级下发",
}

// EntryLabel 返回入口的中文名（未知入口原样返回，便于配置里手写的 ops:<动作> 也能显示）。
func EntryLabel(entry string) string {
	if v, ok := entryLabels[entry]; ok {
		return v
	}
	return entry
}

// KnownEntries 返回内置入口清单（供界面上的多选框使用）。
func KnownEntries() []string {
	out := make([]string, 0, len(entryLabels))
	for k := range entryLabels {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Config 是审批的开关与范围。
//
// **默认关闭**（设计件 D2）：单管理员部署下若默认开启，任何动作都将永远无法执行（没人能批），
// 而升级后突然"什么都做不了"是最糟的形态。关闭时行为与今天**完全一致**。
type Config struct {
	// Enabled 是否启用审批。关闭时不产生任何审批单，动作照旧直接下发。
	Enabled bool `json:"enabled" yaml:"enabled"`
	// RequireFor 是受管入口清单，取值见 Entry* 常量，也支持直接写 `ops:<动作名>` 只拦某个动作。
	RequireFor []string `json:"requireFor" yaml:"requireFor"`
	// TTLMinutes 审批单有效期（分钟）。
	TTLMinutes int `json:"ttlMinutes" yaml:"ttlMinutes"`
	// MaxRequests 保留的审批单条数上限（只淘汰终态）。
	MaxRequests int `json:"maxRequests" yaml:"maxRequests"`
}

// 边界。TTL 上限 24 小时：再长就等于"批一次管很久"，与超时失效的初衷相悖。
const (
	DefaultTTLMinutes = 30
	MinTTLMinutes     = 1
	MaxTTLMinutes     = 1440
	MinMaxRequests    = 10
	MaxMaxRequests    = 5000
)

// DefaultConfig 返回默认配置：**关闭**，但范围已经填好——管理员只需把开关打开。
//
// 把范围预先填上而不是留空：留空 + 打开开关 = "开了但什么都没拦"，那是一种安静的失效。
func DefaultConfig() Config {
	return Config{
		Enabled:     false,
		RequireFor:  []string{EntryOpsWrite, EntryDefense, EntrySystemUpgrade, EntryAgentUpgrade},
		TTLMinutes:  DefaultTTLMinutes,
		MaxRequests: maxRequests,
	}
}

// normalize 归一化：去空白、去重、丢掉空项，并把数值夹进边界。
//
// 夹边界而不是报错：这是"保存即生效"的配置，超出范围的数值多半是笔误，
// 夹到边界比让整份配置保存失败更好用；而 0/负数会退化成"立即过期"这种危险取值，必须夹掉。
func (c *Config) normalize() {
	seen := map[string]bool{}
	out := make([]string, 0, len(c.RequireFor))
	for _, e := range c.RequireFor {
		e = strings.TrimSpace(e)
		if e == "" || seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	sort.Strings(out)
	c.RequireFor = out

	if c.TTLMinutes < MinTTLMinutes {
		c.TTLMinutes = MinTTLMinutes
	}
	if c.TTLMinutes > MaxTTLMinutes {
		c.TTLMinutes = MaxTTLMinutes
	}
	if c.MaxRequests < MinMaxRequests {
		c.MaxRequests = MinMaxRequests
	}
	if c.MaxRequests > MaxMaxRequests {
		c.MaxRequests = MaxMaxRequests
	}
}

// TTL 把分钟折算成时长。
func (c Config) TTL() time.Duration {
	if c.TTLMinutes <= 0 {
		return requestTTL
	}
	return time.Duration(c.TTLMinutes) * time.Minute
}

// RequiresApproval 判定一次动作是否需要人审。
//
// kind 对 ops 入口传**动作名**（如 `svc.restart`），其余入口传入口标识本身（如 `defense`）。
// readOnly 只对 ops 入口有意义（来自动作目录的 ReadOnly）——非 ops 入口一律传 true，
// 这样即使配置里同时写了 `ops:write`，也不会把处置/升级误判成"写类 ops 动作"。
func (c Config) RequiresApproval(kind string, readOnly bool) bool {
	if !c.Enabled {
		return false
	}
	for _, e := range c.RequireFor {
		switch {
		case e == EntryOpsWrite && !readOnly:
			return true
		case e == "ops:"+kind:
			// 精确到某个动作（配置里手写的形态）：只拦它，不拦同组的其它写动作。
			return true
		case e == kind:
			return true
		}
	}
	return false
}

// ConfigStore 持有审批配置（内存 + 状态库文档），保存即热生效。
type ConfigStore struct {
	mu      sync.RWMutex
	p       configdoc.Persister
	cfg     Config
	loadErr error
}

// NewConfigStore 用文件持久化（测试与迁移源）。
func NewConfigStore(file string) *ConfigStore {
	if file == "" {
		return &ConfigStore{cfg: DefaultConfig()}
	}
	return NewConfigStoreWithPersister(configdoc.NewFilePersister(file), "")
}

// NewConfigStoreWithPersister 用状态库文档保存审批配置。
//
// legacyPath 是旧版本的文件位置：文档不存在且文件存在时一次性导入，
// 然后把文件改名 .bak-migrated。文件不存在时用默认配置，且**不**立即写库。
func NewConfigStoreWithPersister(p configdoc.Persister, legacyPath string) *ConfigStore {
	s := &ConfigStore{p: p, cfg: DefaultConfig()}
	if p == nil {
		return s
	}
	data, err := p.Load()
	if errors.Is(err, configdoc.ErrNotFound) {
		s.importLegacy(legacyPath)
		return s
	}
	if err != nil {
		s.loadErr = fmt.Errorf("读取审批配置失败: %w", err)
		slog.Error("读取审批配置失败，拒绝启动（避免审批开关静默回到默认关闭）", "err", err, "doc", p.Name())
		return s
	}
	if err := json.Unmarshal(data, &s.cfg); err != nil {
		s.loadErr = fmt.Errorf("解析审批配置失败: %w", err)
		slog.Error("解析审批配置失败，拒绝启动（避免审批开关静默回到默认关闭）", "err", err, "doc", p.Name())
		return s
	}
	s.cfg.normalize()
	return s
}

// importLegacy 把旧版 JSON 文件导入状态库（只发生一次）。
func (s *ConfigStore) importLegacy(path string) {
	if path == "" {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		s.loadErr = fmt.Errorf("解析旧审批配置失败: %w", err)
		slog.Error("解析旧审批配置失败，拒绝启动", "path", path, "err", err)
		return
	}
	cfg.normalize()
	s.cfg = cfg
	if err := s.p.Save(raw); err != nil {
		slog.Error("导入审批配置到状态库失败", "path", path, "err", err)
		return
	}
	_ = os.Rename(path, path+".bak-migrated")
	slog.Info("审批配置已导入状态库", "from", path)
}

// LoadError 返回构造时读取文档的错误（nil 表示正常；文档不存在不算错误）。
func (s *ConfigStore) LoadError() error { return s.loadErr }

// Get 返回当前配置（副本）。
func (s *ConfigStore) Get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c := s.cfg
	c.RequireFor = append([]string(nil), s.cfg.RequireFor...)
	return c
}

// Set 保存配置（归一化后写入状态库并立即生效）。
func (s *ConfigStore) Set(cfg Config) error {
	cfg.normalize()
	s.mu.Lock()
	s.cfg = cfg
	s.mu.Unlock()
	if s.p == nil {
		return nil
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return s.p.Save(data)
}
