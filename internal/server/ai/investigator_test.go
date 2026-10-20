package ai

import (
	"context"
	"testing"
	"time"

	"github.com/nebula/monitor/internal/server/config"
)

type fakeSource struct{ refs []AlertRef }

func (f fakeSource) ActiveCritical() []AlertRef { return f.refs }

func TestInvestigatorDedupesByRuleAndNode(t *testing.T) {
	cfg := config.AIConfig{Enabled: true, BaseURL: "http://127.0.0.1:1", Model: "m", AutoInvestigate: true}
	ran := 0
	done := make(chan struct{})
	inv := NewInvestigator(
		func() config.AIConfig { return cfg },
		fakeSource{refs: []AlertRef{{ID: "e1", RuleID: "r1", Node: "n1"}}},
		nil,
		func(context.Context, AlertRef) { ran++; close(done) },
	)
	inv.now = func() time.Time { return time.Unix(1000, 0) }
	inv.sweep(context.Background())
	<-done // sweep 在 goroutine 里执行，等它结束再断言
	inv.sweep(context.Background())
	if ran != 1 {
		t.Fatalf("同一规则+节点 30 分钟内应只调查一次，实际 %d", ran)
	}
}

func TestInvestigatorDisabledDoesNothing(t *testing.T) {
	cfg := config.AIConfig{Enabled: true, AutoInvestigate: false}
	ran := 0
	inv := NewInvestigator(
		func() config.AIConfig { return cfg },
		fakeSource{refs: []AlertRef{{ID: "e1", RuleID: "r1", Node: "n1"}}},
		nil,
		func(context.Context, AlertRef) { ran++ },
	)
	inv.sweep(context.Background())
	if ran != 0 {
		t.Fatal("未开启 autoInvestigate 时不得发起调查")
	}
}
