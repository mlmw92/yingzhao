package approval

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mlmw92/yingzhao/internal/server/configdoc"
)

// 默认必须**关闭**：单管理员部署下若默认开启，升级后任何动作都将永远无法执行。
func TestDefaultConfig_Disabled(t *testing.T) {
	c := DefaultConfig()
	if c.Enabled {
		t.Fatal("默认必须关闭审批（否则单管理员部署升级后什么都做不了）")
	}
	// 关闭时无论什么入口都不审批——这就是"行为与今天完全一致"的定义
	for _, kind := range []string{"svc.restart", "file.push", "defense", "system:upgrade", "agent:upgrade", "docker:write"} {
		if c.RequiresApproval(kind, false) {
			t.Fatalf("关闭时不该拦 %s", kind)
		}
	}
	// 范围要预先填好：留空 + 打开开关 = "开了但什么都没拦"，那是一种安静的失效
	if len(c.RequireFor) != 5 {
		t.Fatalf("默认范围应预填 5 个入口，实际 %v", c.RequireFor)
	}
}

// **D1 的决定性判据**：写类 ops 动作要审批，只读查询不审批。
func TestRequiresApproval_OpsWriteOnly(t *testing.T) {
	c := DefaultConfig()
	c.Enabled = true

	if !c.RequiresApproval("svc.restart", false) {
		t.Fatal("写类 ops 动作（ReadOnly=false）应需要审批")
	}
	if !c.RequiresApproval("file.push", false) {
		t.Fatal("文件分发是写动作，应需要审批")
	}
	// 只读查询必须放行：给排障查询加审批，结果是大家绕开审批去用别的入口，反而更糟
	for _, kind := range []string{"node.diagnostics", "svc.status", "container.pods", "container.logs"} {
		if c.RequiresApproval(kind, true) {
			t.Fatalf("只读动作 %s 不该被拦", kind)
		}
	}
	// 非 ops 入口传 readOnly=true，不应被 `ops:write` 误命中
	for _, kind := range []string{EntryDefense, EntrySystemUpgrade, EntryAgentUpgrade} {
		if !c.RequiresApproval(kind, true) {
			t.Fatalf("%s 应需要审批", kind)
		}
	}
}

// 新增一个写动作时**自动**纳入审批（fail-closed）：这正是用"写类"而不是列清单的理由。
func TestRequiresApproval_NewWriteActionCoveredAutomatically(t *testing.T) {
	c := DefaultConfig()
	c.Enabled = true
	if !c.RequiresApproval("some.brand.new.write.action", false) {
		t.Fatal("新增写动作必须自动纳入审批——列清单的写法会让它默认不受管")
	}
}

// 支持精确到某个动作：只拦它，不拦同组的其它写动作。
func TestRequiresApproval_SpecificOpsKind(t *testing.T) {
	c := Config{Enabled: true, RequireFor: []string{"ops:file.push"}, TTLMinutes: 30, MaxRequests: 100}
	if !c.RequiresApproval("file.push", false) {
		t.Fatal("显式列出的动作应被拦")
	}
	if c.RequiresApproval("svc.restart", false) {
		t.Fatal("未列出的写动作不该被拦（这是显式清单的语义）")
	}
}

func TestConfigNormalize(t *testing.T) {
	c := Config{
		Enabled:     true,
		RequireFor:  []string{" ops:write ", "", "defense", "defense", "  "},
		TTLMinutes:  0,  // 0 会退化成"立即过期"，必须夹到下限
		MaxRequests: -5, // 同上
	}
	c.normalize()
	if len(c.RequireFor) != 2 || c.RequireFor[0] != EntryDefense || c.RequireFor[1] != EntryOpsWrite {
		t.Fatalf("应去空白/去重/排序，实际 %v", c.RequireFor)
	}
	if c.TTLMinutes != MinTTLMinutes {
		t.Fatalf("TTL 应夹到下限 %d，实际 %d", MinTTLMinutes, c.TTLMinutes)
	}
	if c.MaxRequests != MinMaxRequests {
		t.Fatalf("上限应夹到下限 %d，实际 %d", MinMaxRequests, c.MaxRequests)
	}

	c2 := Config{TTLMinutes: 99999, MaxRequests: 99999}
	c2.normalize()
	if c2.TTLMinutes != MaxTTLMinutes || c2.MaxRequests != MaxMaxRequests {
		t.Fatalf("应夹到上限，实际 %d / %d", c2.TTLMinutes, c2.MaxRequests)
	}
}

// 保存即热生效：Set 之后 Get 立刻是新值，且重启后仍在。
func TestConfigStore_HotAndPersistent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "approval.json")
	s := NewConfigStore(path)
	if s.Get().Enabled {
		t.Fatal("默认应为关闭")
	}

	cfg := s.Get()
	cfg.Enabled = true
	cfg.RequireFor = []string{EntryOpsWrite}
	cfg.TTLMinutes = 5
	if err := s.Set(cfg); err != nil {
		t.Fatalf("保存配置失败: %v", err)
	}
	if got := s.Get(); !got.Enabled || got.TTLMinutes != 5 || len(got.RequireFor) != 1 {
		t.Fatalf("保存后应立刻生效，实际 %+v", got)
	}
	if !s.Get().RequiresApproval("svc.restart", false) {
		t.Fatal("开启并配了 ops:write 后，写动作应需要审批")
	}

	// 重启（新建实例）后配置仍在
	s2 := NewConfigStore(path)
	if got := s2.Get(); !got.Enabled || got.TTLMinutes != 5 {
		t.Fatalf("重启后配置应保留，实际 %+v", got)
	}
}

// 文件不存在时用默认值，且**不**因为启动就凭空造出一个文件。
func TestConfigStore_MissingFileUsesDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope.json")
	s := NewConfigStore(path)
	if s.Get().Enabled {
		t.Fatal("文件不存在时应用默认（关闭）")
	}
	if _, err := filepath.Glob(path); err != nil {
		t.Fatalf("路径通配不该报错: %v", err)
	}
}

// 空路径（未配置独立文件）时不落盘也不 panic——单元测试与裁剪部署会走这条。
func TestConfigStore_EmptyPath(t *testing.T) {
	s := NewConfigStore("")
	cfg := s.Get()
	cfg.Enabled = true
	if err := s.Set(cfg); err != nil {
		t.Fatalf("空路径保存不该报错: %v", err)
	}
	if !s.Get().Enabled {
		t.Fatal("空路径下也应热生效")
	}
}

func TestEntryLabel(t *testing.T) {
	if EntryLabel(EntryOpsWrite) == EntryOpsWrite {
		t.Fatal("内置入口应有中文名")
	}
	// 配置里手写的 ops:<动作> 没有中文名，原样返回（界面上仍能看懂是哪个动作）
	if got := EntryLabel("ops:svc.restart"); got != "ops:svc.restart" {
		t.Fatalf("未知入口应原样返回，实际 %q", got)
	}
	if len(KnownEntries()) != 5 {
		t.Fatalf("内置入口应为 5 个，实际 %v", KnownEntries())
	}
}

// 旧版 approval.json 一次性导入状态库，之后只认库。
func TestConfigStoreMigratesLegacyFile(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "approval.json")
	if err := os.WriteFile(legacy, []byte(`{"enabled":true,"requireFor":["ops:write"],"ttlMinutes":5,"maxRequests":100}`), 0o600); err != nil {
		t.Fatal(err)
	}
	p := configdoc.NewFilePersister(filepath.Join(dir, "db.json"))
	s := NewConfigStoreWithPersister(p, legacy)
	if s.LoadError() != nil {
		t.Fatalf("LoadError: %v", s.LoadError())
	}
	if !s.Get().Enabled {
		t.Fatal("旧文件里的 enabled=true 未导入")
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("导入后旧文件应改名")
	}
	if _, err := os.Stat(legacy + ".bak-migrated"); err != nil {
		t.Fatalf("旧文件应改名为 .bak-migrated: %v", err)
	}

	cfg := DefaultConfig()
	cfg.RequireFor = []string{EntryDefense}
	if err := s.Set(cfg); err != nil {
		t.Fatalf("Set: %v", err)
	}
	s2 := NewConfigStoreWithPersister(p, "")
	if s2.Get().Enabled || len(s2.Get().RequireFor) != 1 || s2.Get().RequireFor[0] != EntryDefense {
		t.Fatalf("重新加载配置不对: %+v", s2.Get())
	}
}
