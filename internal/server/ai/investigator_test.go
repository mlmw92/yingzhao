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

func TestInvestigatorRecoversFromPanic(t *testing.T) {
	cfg := config.AIConfig{Enabled: true, BaseURL: "http://127.0.0.1:1", Model: "m", AutoInvestigate: true}
	inv := NewInvestigator(
		func() config.AIConfig { return cfg },
		fakeSource{refs: []AlertRef{{ID: "e1", RuleID: "r1", Node: "n1"}}},
		nil,
		func(context.Context, AlertRef) { panic("boom") },
	)
	// 后台 goroutine 里的 panic 不能把整个进程带走
	inv.sweep(context.Background())
	time.Sleep(50 * time.Millisecond)
}

// 并发已满时只是「本轮跳过」，不能吃掉 30 分钟去重名额：
// 否则一次短暂的忙碌会让真实告警半小时内不再被调查。
func TestInvestigatorBusyDoesNotConsumeDedupe(t *testing.T) {
	cfg := config.AIConfig{Enabled: true, BaseURL: "http://127.0.0.1:1", Model: "m", AutoInvestigate: true}
	inv := NewInvestigator(
		func() config.AIConfig { return cfg },
		fakeSource{refs: []AlertRef{{ID: "e1", RuleID: "r1", Node: "n1"}}},
		nil,
		nil,
	)
	inv.now = func() time.Time { return time.Unix(1000, 0) }

	// 占满并发（容量 2）
	inv.sem <- struct{}{}
	inv.sem <- struct{}{}
	inv.sweep(context.Background())
	<-inv.sem
	<-inv.sem

	done := make(chan struct{})
	inv.runOne = func(context.Context, AlertRef) { close(done) }
	inv.sweep(context.Background())
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("并发恢复后应重新触发（忙碌不应消耗去重名额）")
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
