package security

import (
	"path/filepath"
	"testing"

	"github.com/nebula/monitor/internal/model"
)

func riskExec() model.GuardRisk {
	return model.GuardRisk{
		Kind: model.GuardRiskExec, AllowedDir: "/opt/app/bin",
		Exposed: "/opt/app/bin/app", Unit: "app.service",
	}
}

func riskPersistence() model.GuardRisk {
	return model.GuardRisk{
		Kind: model.GuardRiskPersistence, AllowedDir: "/etc/cron.d", Exposed: "/etc/cron.d",
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return New(filepath.Join(t.TempDir(), "security_store.json"))
}

func guardRiskEvents(s *Store) []model.SecurityEvent {
	return s.Events(0, model.SecurityCatGuardRisk, "")
}

// **决定性**：首次出现必须派生一条事件（否则"曾经有过"就没有留痕，
// 而这正是"配置等于 RCE"这件事唯一能被事后复盘的地方）。
func TestGuardRisks_FirstSightingDerivesEvent(t *testing.T) {
	s := newTestStore(t)
	s.SaveGuardRisks("web-01", []model.GuardRisk{riskExec()}, true)

	evs := guardRiskEvents(s)
	if len(evs) != 1 {
		t.Fatalf("首次出现应派生 1 条事件，实际 %d", len(evs))
	}
	e := evs[0]
	if e.Severity != model.SeverityCritical {
		t.Fatalf("应为 critical（这描述的是一台机器已经等于任意代码执行），实际 %s", e.Severity)
	}
	if e.Node != "web-01" {
		t.Fatalf("事件应带节点，实际 %q", e.Node)
	}
	// 消息要与人话一致（Agent 日志与界面共用同一句）
	if e.Message != riskExec().Describe() {
		t.Fatalf("消息应与 Describe() 一致，实际 %q", e.Message)
	}
	if e.Detail["allowedDir"] != "/opt/app/bin" || e.Detail["unit"] != "app.service" {
		t.Fatalf("Detail 应带目录与单元，实际 %v", e.Detail)
	}
}

// 每轮上报都会带同一个清单 → 绝不能每轮派发一条（否则事件列表会被刷满）。
func TestGuardRisks_RepeatedReportDoesNotDuplicate(t *testing.T) {
	s := newTestStore(t)
	for i := 0; i < 5; i++ {
		s.SaveGuardRisks("web-01", []model.GuardRisk{riskExec()}, true)
	}
	if n := len(guardRiskEvents(s)); n != 1 {
		t.Fatalf("重复上报应只留 1 条事件，实际 %d", n)
	}
	// 清单顺序变化也不算新风险：exec 已有、persistence 是新的一条 → 共 2 条
	s.SaveGuardRisks("web-01", []model.GuardRisk{riskPersistence(), riskExec()}, true)
	s.SaveGuardRisks("web-01", []model.GuardRisk{riskExec(), riskPersistence()}, true)
	if n := len(guardRiskEvents(s)); n != 2 {
		t.Fatalf("顺序变化不该产生新事件，实际 %d 条（期望 2）", n)
	}
}

// **决定性**：把配置收掉之后，**状态消失、事件仍在**——
// 状态回答"现在有没有"，事件回答"曾经有过"，两者不能互相顶替。
func TestGuardRisks_FixedConfigClearsStateButKeepsEvent(t *testing.T) {
	s := newTestStore(t)
	s.SaveGuardRisks("web-01", []model.GuardRisk{riskExec()}, true)
	if len(guardRiskEvents(s)) != 1 {
		t.Fatal("前置：应已派生一条事件")
	}

	// 收掉配置后重启 Agent → 上报空清单
	s.SaveGuardRisks("web-01", []model.GuardRisk{}, true)

	st := s.GetGuardRisks("web-01")
	if len(st.Risks) != 0 {
		t.Fatalf("状态应已清空，实际 %v", st.Risks)
	}
	if !st.Reported {
		t.Fatal("空清单但已上报 → Reported 应为 true（与「未上报」区分）")
	}
	if n := len(guardRiskEvents(s)); n != 1 {
		t.Fatalf("事件必须保留（历史留痕），实际 %d 条", n)
	}
}

// **决定性**：nil（旧 Agent 未上报）与空切片（已检查、无风险）必须区分——
// 把"没上报"显示成"没有风险"，会让人以为那台机器是安全的。
func TestGuardRisks_NotReportedIsDistinctFromClean(t *testing.T) {
	s := newTestStore(t)

	// 旧 Agent：压根没有这个字段
	s.SaveGuardRisks("old-01", nil, false)
	old := s.GetGuardRisks("old-01")
	if old.Reported {
		t.Fatal("未上报的节点 Reported 必须为 false")
	}
	if len(guardRiskEvents(s)) != 0 {
		t.Fatal("未上报不该派生任何事件")
	}

	// 新 Agent：报了，但是空的
	s.SaveGuardRisks("new-01", []model.GuardRisk{}, true)
	fresh := s.GetGuardRisks("new-01")
	if !fresh.Reported {
		t.Fatal("已上报的节点 Reported 必须为 true")
	}

	// 两者在列表里能区分开
	all := s.ListGuardRisks()
	if len(all) != 2 {
		t.Fatalf("应有 2 个节点的状态，实际 %d", len(all))
	}
	if all[0].Node != "new-01" || all[1].Node != "old-01" {
		t.Fatalf("应按节点名排序，实际 %s / %s", all[0].Node, all[1].Node)
	}
}

// 状态要能落盘再读回（重启后卡片不该空掉）。
func TestGuardRisks_SurviveReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "security_store.json")
	s1 := New(path)
	s1.SaveGuardRisks("web-01", []model.GuardRisk{riskExec()}, true)

	s2 := New(path)
	st := s2.GetGuardRisks("web-01")
	if len(st.Risks) != 1 || st.Risks[0].AllowedDir != "/opt/app/bin" {
		t.Fatalf("重启后状态应还在，实际 %+v", st)
	}
	if !st.Reported {
		t.Fatal("重启后 Reported 应保持 true")
	}
}

// sameGuardRisks：顺序无关（Agent 的清单顺序由配置遍历决定，顺序变了不等于内容变了）。
func TestSameGuardRisks_OrderInsensitive(t *testing.T) {
	a := []model.GuardRisk{riskExec(), riskPersistence()}
	b := []model.GuardRisk{riskPersistence(), riskExec()}
	if !sameGuardRisks(a, b) {
		t.Fatal("同一组风险、顺序不同，应判为相同（否则每轮都会白落盘一次）")
	}
	if sameGuardRisks(a, []model.GuardRisk{riskExec()}) {
		t.Fatal("条数不同应判为不同")
	}
	if sameGuardRisks(a, nil) {
		t.Fatal("与非空应判为不同")
	}
	if !sameGuardRisks(nil, []model.GuardRisk{}) {
		t.Fatal("nil 与空切片应判为相同（都是没有风险）")
	}
}

// 多个节点的同一条风险是独立事件。
func TestGuardRisks_PerNodeEvents(t *testing.T) {
	s := newTestStore(t)
	s.SaveGuardRisks("web-01", []model.GuardRisk{riskExec()}, true)
	s.SaveGuardRisks("web-02", []model.GuardRisk{riskExec()}, true)
	if n := len(guardRiskEvents(s)); n != 2 {
		t.Fatalf("两台机器各应有一条事件，实际 %d", n)
	}
}
