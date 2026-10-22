package security

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mlmw92/yingzhao/internal/model"
	"github.com/mlmw92/yingzhao/internal/server/configdoc"
)

// 安全事件与基线的服务端存储：内存索引 + JSON 文件持久化。
// 安全数据为低基数结构化数据，不进入高基数时序库；按节点保存最新基线，
// 事件列表设容量上限，防止内存膨胀。

const (
	// MaxEvents 内存中保留的最大安全事件条数。
	MaxEvents = 2000
	// defaultStoreFile 默认持久化文件名。
	defaultStoreFile = "security_store.json"
)

// Store 安全事件/基线存储。
type Store struct {
	mu            sync.RWMutex
	events        []model.SecurityEvent             // 全量事件（按时间倒序追加，裁剪到 MaxEvents）
	baseline      map[string]model.SecurityBaseline // node -> 最新基线
	defenseStatus map[string]*model.DefenseStatus   // node -> 最新入侵防护状态
	guardRisks    map[string]GuardRiskState         // node -> 最新护栏组合风险清单（含是否上报过）
	p             configdoc.Persister               // 持久化后端（文件仅测试用）
	// coalesce 表示写入由 RunFlusher 合并（状态库模式）。文件模式保持同步写，
	// 既有测试依赖「Ingest 后立即能读回」。
	coalesce bool
	dirty    atomic.Bool
	loadErr  error
}

// New 用文件持久化（测试与迁移源）。
func New(file string) *Store {
	if file == "" {
		file = defaultStoreFile
	}
	return NewWithPersister(configdoc.NewFilePersister(file), "")
}

// NewWithPersister 用状态库文档保存安全事件/基线/防护状态。
// legacyPath 存在且文档不存在时一次性导入，然后改名 .bak-migrated。
func NewWithPersister(p configdoc.Persister, legacyPath string) *Store {
	s := &Store{
		baseline:      map[string]model.SecurityBaseline{},
		defenseStatus: map[string]*model.DefenseStatus{},
		guardRisks:    map[string]GuardRiskState{},
		p:             p,
		coalesce:      true,
	}
	s.load(legacyPath)
	return s
}

// LoadError 返回构造时读取文档的错误（nil 表示正常；文档不存在不算错误）。
func (s *Store) LoadError() error { return s.loadErr }

// load 从持久化后端载入既有事件与基线。
func (s *Store) load(legacyPath string) {
	data, err := s.p.Load()
	if errors.Is(err, configdoc.ErrNotFound) {
		data = s.importLegacy(legacyPath)
		if data == nil {
			return
		}
	} else if err != nil {
		s.loadErr = fmt.Errorf("读取安全存储失败: %w", err)
		slog.Error("读取安全存储失败，拒绝启动（避免用空集合覆盖原数据）", "doc", s.p.Name(), "err", err)
		return
	}
	var snap struct {
		Events        []model.SecurityEvent             `json:"events"`
		Baseline      map[string]model.SecurityBaseline `json:"baseline"`
		DefenseStatus map[string]*model.DefenseStatus   `json:"defenseStatus"`
		GuardRisks    map[string]GuardRiskState         `json:"guardRisks"`
	}
	if err := json.Unmarshal(data, &snap); err != nil {
		s.loadErr = fmt.Errorf("解析安全存储失败: %w", err)
		slog.Error("解析安全存储失败，拒绝启动（避免用空集合覆盖原数据）", "doc", s.p.Name(), "err", err)
		return
	}
	if snap.Events != nil {
		s.events = snap.Events
	}
	if snap.Baseline != nil {
		s.baseline = snap.Baseline
	}
	if snap.DefenseStatus != nil {
		s.defenseStatus = snap.DefenseStatus
	}
	if snap.GuardRisks != nil {
		s.guardRisks = snap.GuardRisks
	}
	slog.Info("已加载安全存储", "events", len(s.events), "baselines", len(s.baseline),
		"defense", len(s.defenseStatus), "guardRisks", len(s.guardRisks))
}

// importLegacy 把旧版 JSON 文件导入状态库（只发生一次）。
func (s *Store) importLegacy(path string) []byte {
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	if err := s.p.Save(raw); err != nil {
		slog.Error("导入安全存储到状态库失败", "path", path, "err", err)
		return nil
	}
	_ = os.Rename(path, path+".bak-migrated")
	slog.Info("安全存储已导入状态库", "from", path)
	return raw
}

// markDirty 标记有变更。文件模式立即写；状态库模式由 RunFlusher 合并写入，
// 避免每次 Agent 上报都重写约 1.5MB 的文档。
func (s *Store) markDirty() {
	if !s.coalesce {
		s.save()
		return
	}
	s.dirty.Store(true)
}

// RunFlusher 周期把合并的变更写入状态库；ctx 取消时再补一次。
func (s *Store) RunFlusher(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.Flush()
			return
		case <-ticker.C:
			s.Flush()
		}
	}
}

// Flush 立即写入待落盘的变更（没有变更时是 no-op）。
func (s *Store) Flush() {
	if s.dirty.Swap(false) {
		s.save()
	}
}

// save 持久化事件与基线到磁盘。失败时仅记日志，不阻塞上报链路。
func (s *Store) save() {
	s.mu.RLock()
	snap := struct {
		Events        []model.SecurityEvent             `json:"events"`
		Baseline      map[string]model.SecurityBaseline `json:"baseline"`
		DefenseStatus map[string]*model.DefenseStatus   `json:"defenseStatus"`
		GuardRisks    map[string]GuardRiskState         `json:"guardRisks"`
	}{
		Events:        s.events,
		Baseline:      s.baseline,
		DefenseStatus: s.defenseStatus,
		GuardRisks:    s.guardRisks,
	}
	s.mu.RUnlock()
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		slog.Warn("安全存储序列化失败", "err", err)
		return
	}
	if err := s.p.Save(data); err != nil {
		slog.Warn("安全存储写入失败", "err", err, "doc", s.p.Name())
	}
}

// Ingest 接收来自某节点的安全事件与基线，更新内存索引并持久化。
// 事件先按 ID 去重（避免同一条事件重复入库），基线直接覆盖为最新。
func (s *Store) Ingest(node string, events []model.SecurityEvent, baseline *model.SecurityBaseline) {
	if len(events) == 0 && baseline == nil {
		return
	}
	s.mu.Lock()
	changed := false
	// 事件去重：已存在的 ID 跳过
	seen := make(map[string]struct{}, len(s.events))
	for _, e := range s.events {
		seen[e.ID] = struct{}{}
	}
	for _, e := range events {
		if e.Node == "" {
			e.Node = node
		}
		if _, dup := seen[e.ID]; dup {
			continue
		}
		seen[e.ID] = struct{}{}
		s.events = append(s.events, e)
		changed = true

		// 落点②：从 sudo 命令文本里**事后发现**高危命令（派生成 high_risk_cmd 事件）。
		//
		// 放在 Ingest 是刻意的**单一收口**：任何来路的 sudo 事件都会过一遍，
		// 将来多一条上报路径（或换个采集来源）也不会漏掉分析。
		// 派生事件同样走 seen 去重，其 ID 里带命令摘要与时间窗（见 highRiskEventID）。
		for _, d := range deriveHighRisk(e) {
			if _, dup := seen[d.ID]; dup {
				continue
			}
			seen[d.ID] = struct{}{}
			s.events = append(s.events, d)
			changed = true
		}
	}
	// 裁剪到最大容量（保留最新）
	if len(s.events) > MaxEvents {
		s.events = s.events[len(s.events)-MaxEvents:]
	}
	if baseline != nil {
		if baseline.Node == "" {
			baseline.Node = node
		}
		if !baselineEqual(s.baseline[baseline.Node], *baseline) {
			changed = true
		}
		// 内存里总是换成最新：CheckedAt 是界面上的「最近检查时间」，
		// 只是不参与「要不要写库」的判断。
		s.baseline[baseline.Node] = *baseline
	}
	s.mu.Unlock()
	// 只在真的有变化时标脏：Agent 每轮上报都会调 Ingest，但绝大多数轮次
	// 没有新事件、基线也没变——无条件标脏会让 1.5MB 文档每 10 秒重写一次。
	if changed {
		s.markDirty()
	}
}

// Count 返回当前保留的安全事件条数。
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.events)
}

// Events 返回安全事件列表（按时间倒序），支持按分类/节点筛选与数量上限。
func (s *Store) Events(limit int, category, node string) []model.SecurityEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.SecurityEvent, 0, len(s.events))
	for i := len(s.events) - 1; i >= 0; i-- {
		e := s.events[i]
		if category != "" && e.Category != category {
			continue
		}
		if node != "" && e.Node != node {
			continue
		}
		out = append(out, e)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// Baselines 返回各节点最新基线（按评分升序，风险最高在前）。
func (s *Store) Baselines() []model.SecurityBaseline {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.SecurityBaseline, 0, len(s.baseline))
	for _, b := range s.baseline {
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].Node < out[j].Node
		}
		return out[i].Score < out[j].Score
	})
	return out
}

// Summary 汇总安全态势：合规评分均值、事件总数、风险主机数、FIM 变化数。
func (s *Store) Summary() SecuritySummary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var nodes = map[string]struct{}{}
	var totalScore float64
	for _, b := range s.baseline {
		nodes[b.Node] = struct{}{}
		totalScore += b.Score
	}
	var fimChanges int
	var riskNodes = map[string]struct{}{}
	for _, e := range s.events {
		switch e.Category {
		case model.SecurityCatFIM:
			fimChanges++
		}
		// 关键事件（critical/warning）视为风险主机
		if e.Severity == model.SeverityCritical || e.Severity == model.SeverityWarning {
			riskNodes[e.Node] = struct{}{}
		}
	}
	scoreAvg := 0.0
	if len(s.baseline) > 0 {
		scoreAvg = totalScore / float64(len(s.baseline))
	}
	return SecuritySummary{
		Score:         scoreAvg,
		EventCount:    len(s.events),
		RiskNodes:     len(riskNodes),
		FIMChanges:    fimChanges,
		BaselineHosts: len(nodes),
		GeneratedAt:   time.Now().UnixMilli(),
	}
}

// SecuritySummary 安全态势概览。
type SecuritySummary struct {
	Score         float64 `json:"score"`         // 合规评分均值
	EventCount    int     `json:"eventCount"`    // 安全事件总数
	RiskNodes     int     `json:"riskNodes"`     // 风险主机数（有关键事件）
	FIMChanges    int     `json:"fimChanges"`    // FIM 变化数
	BaselineHosts int     `json:"baselineHosts"` // 已上报基线的主机数
	GeneratedAt   int64   `json:"generatedAt"`   // 统计时间（毫秒）
}

// SaveDefenseStatus 更新某节点最新入侵防护状态并持久化。
func (s *Store) SaveDefenseStatus(node string, st *model.DefenseStatus) {
	if st == nil || node == "" {
		return
	}
	if st.Node == "" {
		st.Node = node
	}
	s.mu.Lock()
	prev := s.defenseStatus[node]
	s.defenseStatus[node] = st
	s.mu.Unlock()
	// UpdatedAt 每轮上报都会变，不参与比较（但内存里保留最新值给界面看）：
	// 否则 1.5MB 文档每 15 秒重写一次。
	if prev == nil || !defenseStatusEqual(*prev, *st) {
		s.markDirty()
	}
}

// defenseStatusEqual 比较除采集时间外的全部字段。
func defenseStatusEqual(a, b model.DefenseStatus) bool {
	a.UpdatedAt, b.UpdatedAt = 0, 0
	return reflect.DeepEqual(a, b)
}

// baselineEqual 比较除检查时间外的全部字段。
//
// CheckedAt 每轮上报都会刷新，算进比较的话基线永远「有变化」，
// 1.5MB 的安全文档就会每 10 秒重写一次——实测 dev-server 上每分钟 6 次。
func baselineEqual(a, b model.SecurityBaseline) bool {
	a.CheckedAt, b.CheckedAt = 0, 0
	return reflect.DeepEqual(a, b)
}

// GetDefenseStatus 返回某节点最新入侵防护状态（无则 nil）。
func (s *Store) GetDefenseStatus(node string) *model.DefenseStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.defenseStatus[node]
}

// ListDefenseStatus 返回所有节点的入侵防护状态。
func (s *Store) ListDefenseStatus() []*model.DefenseStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.DefenseStatus, 0, len(s.defenseStatus))
	for _, st := range s.defenseStatus {
		out = append(out, st)
	}
	return out
}
