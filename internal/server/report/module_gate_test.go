package report

import "testing"

func TestReportTickSkipsWhenModuleDisabled(t *testing.T) {
	s := &Scheduler{}
	s.SetModuleGate(func(key string) bool { return key == "report" })
	if !s.moduleDisabled() {
		t.Fatal("report 模块关闭时应跳过自动报告")
	}

	s.SetModuleGate(func(string) bool { return false })
	if s.moduleDisabled() {
		t.Fatal("模块开启时不应跳过")
	}
}
