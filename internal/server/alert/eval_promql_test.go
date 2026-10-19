package alert

import (
	"errors"
	"fmt"
	"testing"

	"github.com/nebula/monitor/internal/model"
)

func promqlRule() model.AlertRule {
	return model.AlertRule{ID: "r1", Name: "prom", Type: model.RuleTypePromQL,
		Expr: `cpu_usage > 85`, For: "0s", Severity: model.SeverityWarning, Enabled: true}
}

func TestPromQLFiresOnSeriesPresence(t *testing.T) {
	h := newEvalHarness(t, false, nil)
	h.store.promql = []model.Series{{Labels: map[string]string{"node": "node-1", "instance": "i1"},
		Points: []model.Point{{Timestamp: model.NowMillis(), Value: 91}}}}
	h.addRule(promqlRule())
	h.eval()
	if h.firingCount() != 1 {
		t.Fatalf("序列出现应触发，实际 %d", h.firingCount())
	}
}

func TestPromQLResolvesWhenSeriesDisappears(t *testing.T) {
	h := newEvalHarness(t, false, nil)
	h.store.promql = []model.Series{{Labels: map[string]string{"node": "node-1", "instance": "i1"},
		Points: []model.Point{{Timestamp: model.NowMillis(), Value: 91}}}}
	h.addRule(promqlRule())
	h.eval()
	h.store.promql = nil
	h.eval()
	if h.firingCount() != 0 {
		t.Fatalf("序列消失应恢复，实际 %d", h.firingCount())
	}
}

func TestPromQLQueryErrorKeepsPreviousState(t *testing.T) {
	h := newEvalHarness(t, false, nil)
	h.store.promql = []model.Series{{Labels: map[string]string{"node": "node-1", "instance": "i1"},
		Points: []model.Point{{Timestamp: model.NowMillis(), Value: 91}}}}
	h.addRule(promqlRule())
	h.eval()
	h.store.promErr = errors.New("backend down")
	h.eval()
	if h.firingCount() != 1 {
		t.Fatalf("查询失败不应恢复告警，实际 %d", h.firingCount())
	}
}

func TestPromQLGlobalSeriesIgnoresNodeScope(t *testing.T) {
	h := newEvalHarness(t, false, nil)
	h.store.promql = []model.Series{{Labels: map[string]string{"job": "cluster"},
		Points: []model.Point{{Timestamp: model.NowMillis(), Value: 5}}}}
	r := promqlRule()
	r.Scope = "specified"
	r.Nodes = []string{"other-node"}
	h.addRule(r)
	h.eval()
	if h.firingCount() != 1 {
		t.Fatalf("全局序列不受节点范围限制，实际 %d", h.firingCount())
	}
}

func TestPromQLNodeScopedSeriesRespectsScope(t *testing.T) {
	h := newEvalHarness(t, false, nil)
	h.store.promql = []model.Series{{Labels: map[string]string{"node": "node-1", "instance": "i1"},
		Points: []model.Point{{Timestamp: model.NowMillis(), Value: 91}}}}
	r := promqlRule()
	r.Scope = "specified"
	r.Nodes = []string{"other-node"}
	h.addRule(r)
	h.eval()
	if h.firingCount() != 0 {
		t.Fatalf("范围外节点应被过滤，实际 %d", h.firingCount())
	}
}

func TestPromQLSeriesIdentityIsStable(t *testing.T) {
	a := seriesIdentity(map[string]string{"node": "n1", "instance": "i1", "__name__": "cpu_usage"})
	b := seriesIdentity(map[string]string{"__name__": "cpu_usage", "instance": "i1", "node": "n1"})
	if a != b {
		t.Fatalf("标签顺序不应影响身份: %q != %q", a, b)
	}
}

func TestPromQLTruncatesAtLimit(t *testing.T) {
	h := newEvalHarness(t, false, nil)
	for i := 0; i < maxPromQLSeries+5; i++ {
		h.store.promql = append(h.store.promql, model.Series{
			Labels: map[string]string{"node": "node-1", "instance": fmt.Sprintf("i%03d", i)},
			Points: []model.Point{{Timestamp: model.NowMillis(), Value: 1}},
		})
	}
	h.addRule(promqlRule())
	h.eval()
	if h.firingCount() != maxPromQLSeries {
		t.Fatalf("应截断到 %d，实际 %d", maxPromQLSeries, h.firingCount())
	}
}

// TestPromQLEventCarriesExpressionAsMetric 告警事件的「触发条件」取自 Metric 字段；
// PromQL 规则没有 Metric，必须把表达式写进去，否则详情页这一行是空的。
func TestPromQLEventCarriesExpressionAsMetric(t *testing.T) {
	h := newEvalHarness(t, false, nil)
	h.store.promql = []model.Series{{Labels: map[string]string{"node": "node-1", "instance": "i1"},
		Points: []model.Point{{Timestamp: model.NowMillis(), Value: 91}}}}
	h.addRule(promqlRule())
	h.eval()

	// firing 索引里保存的是引擎产生的原始事件。
	var ev model.AlertEvent
	found := false
	for _, fe := range h.engine.firing {
		ev = fe.event
		found = true
	}
	if !found {
		t.Fatal("未产生 firing 事件")
	}
	if ev.Metric != `cpu_usage > 85` {
		t.Fatalf("事件 Metric = %q，want 表达式（前端详情页的触发条件读这个字段）", ev.Metric)
	}
}
