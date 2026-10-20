package asset

import "testing"

func TestInspectTickSkipsWhenModuleDisabled(t *testing.T) {
	s := &InspectScheduler{}
	s.SetModuleGate(func(key string) bool { return key == "inspect" })
	if !s.moduleDisabled() {
		t.Fatal("inspect 模块关闭时应跳过自动巡检")
	}

	s.SetModuleGate(func(string) bool { return false })
	if s.moduleDisabled() {
		t.Fatal("模块开启时不应跳过")
	}

	s.SetModuleGate(nil)
	if s.moduleDisabled() {
		t.Fatal("未注入闸门时不应跳过（测试与旧调用方）")
	}

	// 整域关闭也要停任务：关闭「资产与配置」域时只写 assets，不会单写 inspect
	s.SetModuleGate(func(key string) bool { return key == "assets" })
	if !s.moduleDisabled() {
		t.Fatal("所属域被关闭时应跳过自动巡检")
	}
}
