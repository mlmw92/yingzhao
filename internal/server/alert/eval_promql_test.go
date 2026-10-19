package alert

import (
	"errors"
	"fmt"
	"strings"
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

// TestPromQLTruncationDoesNotResolveOverflowedSeries 截断只影响本轮的评估范围，
// 不改变已有状态：被本轮截掉的序列即使曾经 firing，也不能因此被当成「已消失」。
func TestPromQLTruncationDoesNotResolveOverflowedSeries(t *testing.T) {
	h := newEvalHarness(t, false, nil)
	add := func(prefix string, from, to int) {
		for i := from; i < to; i++ {
			h.store.promql = append(h.store.promql, model.Series{
				Labels: map[string]string{"instance": fmt.Sprintf("%s%03d", prefix, i)},
				Points: []model.Point{{Timestamp: model.NowMillis(), Value: 1}},
			})
		}
	}

	// 第一轮：201 条，按标识排序后处理前 200 条（i200..i399）。
	add("i", 200, 401)
	h.addRule(promqlRule())
	h.eval()
	if h.firingCount() != maxPromQLSeries {
		t.Fatalf("第一轮应触发 %d 条，实际 %d", maxPromQLSeries, h.firingCount())
	}

	// 第二轮：加入 101 条排序更靠前的序列，把原来的尾部（i299..i399）挤出前 200。
	h.alerts.writes = nil
	add("a", 0, 101)
	h.eval()

	for _, w := range h.alerts.writes {
		if w.Labels["state"] == "resolved" {
			t.Fatalf("被截断的序列不应被恢复：%v", w.Labels["message"])
		}
	}
	if h.firingCount() == 0 {
		t.Fatal("第二轮应保持已有 firing 状态")
	}
}

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

// TestPromQLGlobalSeriesMessageUsesPlaceholder 聚合表达式（如 count(...)）的结果没有标签，
// 序列标识为空；消息里必须显示占位，否则会读成「命中： = 4.00」。
func TestPromQLGlobalSeriesMessageUsesPlaceholder(t *testing.T) {
	h := newEvalHarness(t, false, nil)
	h.store.promql = []model.Series{{Labels: map[string]string{},
		Points: []model.Point{{Timestamp: model.NowMillis(), Value: 4}}}}
	h.addRule(promqlRule())
	h.eval()

	var ev model.AlertEvent
	found := false
	for _, fe := range h.engine.firing {
		ev = fe.event
		found = true
	}
	if !found {
		t.Fatal("未产生 firing 事件")
	}
	if !strings.Contains(ev.Message, "(全局)") {
		t.Fatalf("全局序列的消息应显示占位，实际 %q", ev.Message)
	}
}
