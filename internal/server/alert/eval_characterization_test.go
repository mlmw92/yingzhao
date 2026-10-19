package alert

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/nebula/monitor/internal/model"
	"github.com/nebula/monitor/internal/server/node"
)

// evalStore 是评估测试用的存储替身：按指标名返回预置序列，并可为 PromQL 注入结果/错误。
type evalStore struct {
	instant map[string][]model.Series
	promql  []model.Series
	promErr error
}

func (s *evalStore) Write([]model.Metric) error { return nil }
func (s *evalStore) QueryRange(string, string, map[string]string, int64, int64, int64) ([]model.Series, error) {
	return nil, nil
}
func (s *evalStore) QueryLatest(string, string, map[string]string) (*model.Point, error) {
	return nil, nil
}
func (s *evalStore) QueryInstant(_, name string, _ map[string]string) ([]model.Series, error) {
	return s.instant[name], nil
}
func (s *evalStore) QueryInstantWithLookback(_, name string, _ map[string]string, _ time.Duration) ([]model.Series, error) {
	return s.instant[name], nil
}
func (s *evalStore) QueryAllLatest(string, map[string]string) ([]model.Series, error) {
	return nil, nil
}
func (s *evalStore) QueryPromQL(string) ([]model.Series, error) { return s.promql, s.promErr }
func (s *evalStore) Close() error                              { return nil }
func (s *evalStore) Backend() string                           { return "fake" }

type evalHarness struct {
	engine *Engine
	store  *evalStore
	alerts *captureAlertStorage
}

func newEvalHarness(t *testing.T, offline bool, series map[string][]model.Series) *evalHarness {
	t.Helper()
	mgr := node.New(filepath.Join(t.TempDir(), "nodes.json"), time.Hour)
	mgr.Register(&model.ReportPayload{Node: "node-1", Group: "g1", Mode: model.ModeCollect, IP: "10.0.0.1"})
	if offline {
		mgr = node.New(filepath.Join(t.TempDir(), "nodes.json"), 0)
		mgr.Register(&model.ReportPayload{Node: "node-1", Group: "g1", Mode: model.ModeCollect, IP: "10.0.0.1"})
		time.Sleep(2 * time.Millisecond)
		mgr.OfflineStale()
	}
	store := &evalStore{instant: series}
	rules := &RulesStore{rules: map[string]model.AlertRule{}}
	alertStorage := &captureAlertStorage{}
	eng := NewEngine(store, mgr, rules, NewVMAlertStore(alertStorage), nil, nil, nil, 15, nil, nil, nil)
	return &evalHarness{engine: eng, store: store, alerts: alertStorage}
}

func (h *evalHarness) addRule(r model.AlertRule) {
	h.engine.rules.rules[r.ID] = r
}
func (h *evalHarness) eval()                       { h.engine.evaluate() }
func (h *evalHarness) firingCount() int            { return len(h.engine.firing) }
func (h *evalHarness) state(key string) *ruleState { return h.engine.states[key] }

func TestCharacterizeThresholdFiresAndResolves(t *testing.T) {
	h := newEvalHarness(t, false, map[string][]model.Series{
		"cpu_usage": {{Labels: map[string]string{"node": "node-1", "instance": "i1"},
			Points: []model.Point{{Timestamp: model.NowMillis(), Value: 90}}}},
	})
	h.addRule(model.AlertRule{ID: "r1", Name: "cpu", Metric: "cpu_usage", Operator: ">",
		Threshold: 85, For: "1m", Severity: model.SeverityWarning, Enabled: true})
	h.eval()
	if h.firingCount() != 0 {
		t.Fatalf("For 未满不应触发，实际 %d", h.firingCount())
	}
	h.state("r1|node-1|i1").aboveSince -= 61000
	h.eval()
	if h.firingCount() != 1 {
		t.Fatalf("For 满应触发，实际 %d", h.firingCount())
	}
	h.store.instant["cpu_usage"][0].Points[0].Value = 10
	h.eval()
	if h.firingCount() != 0 {
		t.Fatalf("恢复后应清除 firing，实际 %d", h.firingCount())
	}
}

func TestCharacterizeNodeOfflineFires(t *testing.T) {
	h := newEvalHarness(t, true, nil)
	h.addRule(model.AlertRule{ID: "r1", Name: "offline", Type: model.RuleTypeNodeOffline,
		For: "1m", Severity: model.SeverityCritical, Enabled: true})
	h.eval()
	h.state("r1|node-1|").aboveSince -= 61000
	h.eval()
	if h.firingCount() != 1 {
		t.Fatalf("离线满 For 应触发，实际 %d", h.firingCount())
	}
}

func TestCharacterizeServiceDownFires(t *testing.T) {
	h := newEvalHarness(t, false, map[string][]model.Series{
		"mysql_instance_up": {{Labels: map[string]string{"node": "node-1", "instance": "db1:3306"},
			Points: []model.Point{{Timestamp: model.NowMillis(), Value: 0}}}},
	})
	h.addRule(model.AlertRule{ID: "r1", Name: "mysql down", Type: model.RuleTypeServiceDown,
		Service: "mysql", Operator: "<=", Threshold: 0, For: "0s",
		Severity: model.SeverityCritical, Enabled: true})
	h.eval()
	if h.firingCount() != 1 {
		t.Fatalf("存活指标为 0 应触发，实际 %d", h.firingCount())
	}
}

func TestCharacterizeRoleChangeFires(t *testing.T) {
	h := newEvalHarness(t, false, map[string][]model.Series{
		"mysql_instance_up": {{Labels: map[string]string{"node": "node-1", "instance": "db1:3306",
			"role": "primary", "topology": "cluster"},
			Points: []model.Point{{Timestamp: model.NowMillis(), Value: 1}}}},
	})
	h.addRule(model.AlertRule{ID: "r1", Name: "role", Type: model.RuleTypeRoleChange,
		Service: "mysql", Topology: "cluster", For: "0s",
		Severity: model.SeverityWarning, Enabled: true})
	h.eval() // 第一轮只记基线
	if h.firingCount() != 0 {
		t.Fatalf("首轮不应触发，实际 %d", h.firingCount())
	}
	h.store.instant["mysql_instance_up"][0].Labels["role"] = "secondary"
	h.eval()
	if h.firingCount() != 1 {
		t.Fatalf("角色变化应触发，实际 %d", h.firingCount())
	}
}

func TestCharacterizeClusterFaultFires(t *testing.T) {
	h := newEvalHarness(t, false, map[string][]model.Series{
		"mysql_instance_up": {
			{Labels: map[string]string{"node": "node-1", "instance": "db1:3306", "role": "primary", "group": "g", "topology": "cluster"},
				Points: []model.Point{{Timestamp: model.NowMillis(), Value: 1}}},
			{Labels: map[string]string{"node": "node-1", "instance": "db2:3306", "role": "primary", "group": "g", "topology": "cluster"},
				Points: []model.Point{{Timestamp: model.NowMillis(), Value: 1}}},
		},
	})
	h.addRule(model.AlertRule{ID: "r1", Name: "cluster", Type: model.RuleTypeClusterFault,
		Service: "mysql", Topology: "cluster", For: "0s",
		Severity: model.SeverityCritical, Enabled: true})
	h.eval()
	if h.firingCount() != 2 {
		t.Fatalf("多主应触发每个成员，实际 %d", h.firingCount())
	}
}
