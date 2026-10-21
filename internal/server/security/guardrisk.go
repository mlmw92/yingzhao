package security

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"time"

	"github.com/mlmw92/yingzhao/internal/model"
)

// 护栏组合风险的**服务端状态**与**首次出现的事件**（能力全景表 §3.4 后第 3 条的既有 TODO）。
//
// 判定在 Agent 侧（`internal/agent/ops/guardcheck.go`）：只有它知道本机 `guards.ops` 的真实配置。
// 本文件负责把它送出来的结论**存成状态**、并在**首次出现**时派生一条事件。
//
// 为什么状态与事件都要（这是本设计最要紧的一处取舍）：
//   - **状态**回答"现在有没有"——把配置收掉之后，卡片应当干净；
//   - **事件**回答"曾经有过"——事后复盘要能回答"这台机器什么时候配出过 RCE 组合"。
// 只做事件的话会留下一条永久的旧告警，人会被训练成忽略它；只做状态则推不出去（没人会天天翻这一页）。

// GuardRiskState 是某节点的护栏风险状态。
type GuardRiskState struct {
	Node  string            `json:"node"`
	Risks []model.GuardRisk `json:"risks"`
	// Reported 表示该节点**是否上报过**这个字段。
	//
	// 与"风险清单为空"是两件事：`false` = 旧 Agent 压根没报过（该节点从未检查过），
	// `true` + 空清单 = 已检查且无风险。界面必须分开显示——把"没上报"显示成"没有风险"
	// 会让人以为那台机器是安全的。
	Reported   bool  `json:"reported"`
	ReportedAt int64 `json:"reportedAt"`
}

// SaveGuardRisks 更新某节点的护栏风险状态；首次出现的风险各派生一条去重事件。
//
// reported 来自上报体里该字段是否为 nil（`payload.GuardRisks != nil`）：
// 旧 Agent 不报这个字段，那种情况下**不能**把该节点的状态说成"无风险"。
func (s *Store) SaveGuardRisks(node string, risks []model.GuardRisk, reported bool) {
	if node == "" {
		return
	}
	now := time.Now().UnixMilli()
	if risks == nil {
		risks = []model.GuardRisk{}
	}

	s.mu.Lock()
	prev, had := s.guardRisks[node]
	s.guardRisks[node] = GuardRiskState{Node: node, Risks: risks, Reported: reported, ReportedAt: now}

	// 首次出现才派生事件。去重键**不含时间窗**（与高危命令那批不同）：
	// 护栏配置是状态、不是"还在发生的行为"——同一条风险出现过一次就够了，
	// 修好再犯时状态会重新亮起，不需要再刷一条事件。
	seen := make(map[string]struct{}, len(s.events))
	for _, e := range s.events {
		seen[e.ID] = struct{}{}
	}
	added := 0
	for _, r := range risks {
		id := guardRiskEventID(node, r)
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		added++
		s.events = append(s.events, model.SecurityEvent{
			ID:       id,
			Node:     node,
			Category: model.SecurityCatGuardRisk,
			// critical：这条描述的是一台机器**已经等于任意代码执行**的配置，
			// 不是"建议改进"那一档。
			Severity:  model.SeverityCritical,
			Message:   r.Describe(),
			Detail:    map[string]string{"kind": r.Kind, "allowedDir": r.AllowedDir, "exposed": r.Exposed, "unit": r.Unit},
			Timestamp: now,
		})
	}
	if len(s.events) > MaxEvents {
		s.events = s.events[len(s.events)-MaxEvents:]
	}
	// 内容没变就不落盘：这个状态**每轮上报都会来**（与"变了才写"的防御状态不同），
	// 每轮重写整份存储（含最多 2000 条事件）在多节点下是笔真实的开销。
	// ReportedAt 随之停留在"最后一次变化"的时刻，界面按"最近上报"理解也说得通。
	changed := added > 0 || !had || prev.Reported != reported || !sameGuardRisks(prev.Risks, risks)
	s.mu.Unlock()
	if changed {
		s.save()
	}
}

// sameGuardRisks 比较两份风险清单是否等价（顺序无关：Agent 的清单顺序由配置遍历决定，
// 我们不希望"顺序变了"被当成"内容变了"而白白落盘）。
func sameGuardRisks(a, b []model.GuardRisk) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
	for _, r := range a {
		seen[r.Summary()]++
	}
	for _, r := range b {
		k := r.Summary()
		if seen[k] == 0 {
			return false
		}
		seen[k]--
	}
	return true
}

// guardRiskEventID 生成派生事件的稳定 ID：同一节点 + 同一条风险 → 同一个 ID → 只报一次。
func guardRiskEventID(node string, r model.GuardRisk) string {
	sum := sha256.Sum256([]byte(r.Summary()))
	return node + "|" + model.SecurityCatGuardRisk + "|" + hex.EncodeToString(sum[:8])
}

// ListGuardRisks 返回全部节点的护栏风险状态（按节点名排序，便于界面稳定展示）。
func (s *Store) ListGuardRisks() []GuardRiskState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]GuardRiskState, 0, len(s.guardRisks))
	for _, e := range s.guardRisks {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Node < out[j].Node })
	return out
}

// GetGuardRisks 返回某节点的护栏风险状态（无记录时返回零值，Reported=false）。
func (s *Store) GetGuardRisks(node string) GuardRiskState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.guardRisks[node]
}
