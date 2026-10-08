package ai

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nebula/monitor/internal/server/config"
)

func TestManagerInitAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai.yaml")

	// 文件不存在：用 initial 初始化并落盘（默认关闭）。
	m, err := New(path, config.AIConfig{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if m.Get().Enabled {
		t.Fatal("默认必须是关闭的")
	}
	if m.Get().TimeoutSeconds != 60 {
		t.Fatalf("落盘前应归一化：%d", m.Get().TimeoutSeconds)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("初始化应落盘：%v", err)
	}

	// 保存后热生效（同进程读得到新值）。
	saved := config.AIConfig{Enabled: true, BaseURL: "https://api.example.com/v1", APIKey: "sk-x", Model: "m"}
	if err := m.Save(saved); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := m.Get(); !got.Enabled || got.APIKey != "sk-x" || got.Model != "m" {
		t.Fatalf("保存后应热生效：%+v", got)
	}

	// 重新打开：读到的是文件里的值（跨重启保留）。
	again, err := New(path, config.AIConfig{})
	if err != nil {
		t.Fatalf("New(2): %v", err)
	}
	if got := again.Get(); !got.Enabled || got.APIKey != "sk-x" {
		t.Fatalf("重启后应保留：%+v", got)
	}
}

// 手写配置文件里的零值要在加载时被补齐——否则一个漏写超时的 yaml 会让请求无限等待。
func TestManagerNormalizesOnLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai.yaml")
	raw := "enabled: true\nbaseUrl: https://api.example.com/v1\nmodel: m\n"
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := New(path, config.AIConfig{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := m.Get(); got.TimeoutSeconds != 60 || got.MaxTokens != 1024 {
		t.Fatalf("加载时应归一化：%+v", got)
	}
}
