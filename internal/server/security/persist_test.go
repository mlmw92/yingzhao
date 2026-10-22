package security

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mlmw92/yingzhao/internal/model"
	"github.com/mlmw92/yingzhao/internal/server/configdoc"
)

func TestDefenseStoreMigratesLegacyFile(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "defense_tasks.json")
	raw := `{"tasks":[{"id":"d1","node":"web-01","state":"queued"}],"caps":{"web-01":true}}`
	if err := os.WriteFile(legacy, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	p := configdoc.NewFilePersister(filepath.Join(dir, "db.json"))
	s := NewDefenseStoreWithPersister(p, legacy)
	if s.LoadError() != nil {
		t.Fatalf("LoadError: %v", s.LoadError())
	}
	if len(s.List("")) != 1 {
		t.Fatalf("旧文件任务未导入: %d", len(s.List("")))
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("导入后旧文件应改名")
	}
	if _, err := os.Stat(legacy + ".bak-migrated"); err != nil {
		t.Fatalf("旧文件应改名为 .bak-migrated: %v", err)
	}

	s.Create(model.DefenseCommand{ID: "d2", Node: "web-02"}, "alice", "1.2.3.4")
	s2 := NewDefenseStoreWithPersister(p, "")
	if len(s2.List("")) != 2 {
		t.Fatalf("新建任务未写入状态库: %d", len(s2.List("")))
	}
}

func TestSecurityStoreMigratesLegacyFile(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "security_store.json")
	raw := `{"events":[{"id":"e1","node":"web-01","category":"ssh"}],"baseline":{"web-01":{"node":"web-01"}}}`
	if err := os.WriteFile(legacy, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	p := configdoc.NewFilePersister(filepath.Join(dir, "db.json"))
	s := NewWithPersister(p, legacy)
	if s.LoadError() != nil {
		t.Fatalf("LoadError: %v", s.LoadError())
	}
	if s.Count() != 1 {
		t.Fatalf("旧文件事件未导入: %d", s.Count())
	}
	if _, err := os.Stat(legacy + ".bak-migrated"); err != nil {
		t.Fatalf("旧文件应改名为 .bak-migrated: %v", err)
	}
}

// 状态库模式下 Ingest 只标脏，由 Flush 合并写入（避免每次上报重写约 1.5MB）。
func TestSecurityStoreCoalescesWrites(t *testing.T) {
	dir := t.TempDir()
	p := configdoc.NewFilePersister(filepath.Join(dir, "db.json"))
	s := NewWithPersister(p, "")
	s.Ingest("web-01", []model.SecurityEvent{{ID: "e1", Node: "web-01"}}, nil)
	if _, err := p.Load(); !errors.Is(err, configdoc.ErrNotFound) {
		t.Fatal("Ingest 不应立即写库")
	}
	s.Flush()
	if _, err := p.Load(); err != nil {
		t.Fatalf("Flush 后应写入状态库: %v", err)
	}
	if s.Count() != 1 {
		t.Fatalf("事件未保留: %d", s.Count())
	}
}
