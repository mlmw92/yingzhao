package uicfg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveRejectsLockedModules(t *testing.T) {
	m, err := New(filepath.Join(t.TempDir(), "ui.yaml"), DefaultUIConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, key := range []string{"overview", "alerts", "alerting", "system"} {
		cfg := m.Get()
		cfg.Disabled = []string{key}
		if err := m.Save(cfg); err == nil {
			t.Errorf("%q 不应允许关闭", key)
		}
	}
}

func TestSaveAllowsClosableModules(t *testing.T) {
	m, err := New(filepath.Join(t.TempDir(), "ui.yaml"), DefaultUIConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cfg := m.Get()
	cfg.Disabled = []string{"assets", "logs"}
	if err := m.Save(cfg); err != nil {
		t.Fatalf("可关闭模块不应被拒: %v", err)
	}
	if !m.Disabled("assets") || m.Disabled("hosts") {
		t.Fatalf("Disabled 判定错误: %+v", m.Get().Disabled)
	}
}

func TestOldConfigWithoutDisabledIsAllEnabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ui.yaml")
	// 旧版本文件没有 disabled 字段
	if err := os.WriteFile(path, []byte("name: 英招\nsubtitle: 运维平台\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := New(path, DefaultUIConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if len(m.Get().Disabled) != 0 {
		t.Fatalf("缺字段应解析为空（=全开），实际 %v", m.Get().Disabled)
	}
}
