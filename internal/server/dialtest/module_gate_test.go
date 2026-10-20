package dialtest

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestDialtestSkipsWhenModuleOrDomainDisabled(t *testing.T) {
	s := &Scheduler{}
	s.SetModuleGate(func(key string) bool { return key == "dialtest" })
	if !s.moduleDisabled() {
		t.Fatal("dialtest 页面关闭时应跳过")
	}

	// 整域关闭：关闭「观测监控」时只写 monitoring
	s.SetModuleGate(func(key string) bool { return key == "monitoring" })
	if !s.moduleDisabled() {
		t.Fatal("所属域被关闭时应跳过")
	}

	s.SetModuleGate(nil)
	if s.moduleDisabled() {
		t.Fatal("未注入闸门时不应跳过")
	}
}

// 关闭期间 runOnce 每秒被调用一次：跳过日志只能在状态变化时打一条，
// 否则一天就是八万多行，真问题会被淹掉。
func TestDialtestSkipLogsOnlyOnTransition(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	s := &Scheduler{}
	s.SetModuleGate(func(string) bool { return true })
	s.runOnce()
	s.runOnce()
	s.runOnce()

	if got := strings.Count(buf.String(), "模块已关闭"); got != 1 {
		t.Fatalf("跳过日志应只打一次，实际 %d 次", got)
	}

	// 模块重新开启后再关闭：应再打一次（状态变化）。
	// 这里用 moduleDisabled() 观察开启状态，避免 runOnce 真正执行到未初始化的 store。
	s.SetModuleGate(func(string) bool { return false })
	if s.moduleDisabled() {
		t.Fatal("开启状态不应判为跳过")
	}
	s.SetModuleGate(func(string) bool { return true })
	s.runOnce()
	if got := strings.Count(buf.String(), "模块已关闭"); got != 2 {
		t.Fatalf("重新关闭后应再打一次，实际 %d 次", got)
	}
}
