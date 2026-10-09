package upgrade

import (
	"os"
	"path/filepath"
	"testing"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
	m, err := New(Config{
		Dir:         filepath.Join(dir, "upgrades"),
		BinDir:      filepath.Join(dir, "bin"),
		WebDir:      filepath.Join(dir, "web"),
		AgentBinDir: filepath.Join(dir, "agent-dist"),
	}, nil)
	if err != nil {
		t.Fatalf("创建升级管理器失败: %v", err)
	}
	return m
}

// writeFakePackage 写一个假的升级包文件（归档只做拷贝，内容不重要）。
func writeFakePackage(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("fake-upgrade-package"), 0o644); err != nil {
		t.Fatalf("写入测试升级包失败: %v", err)
	}
}

// 归档不再按份数自动裁剪：保留到被显式删除为止，因此超过旧上限（5）的包也必须都在。
func TestArchiveKeepsAllPackagesUntilDeleted(t *testing.T) {
	m := newTestManager(t)
	srcDir := t.TempDir()
	versions := []string{"1.0.0", "1.0.1", "1.0.2", "1.0.3", "1.0.4", "1.0.5", "1.0.6"}
	for _, v := range versions {
		src := filepath.Join(srcDir, "pkg-"+v+".tar.gz")
		writeFakePackage(t, src)
		if err := m.archivePackage(v, src); err != nil {
			t.Fatalf("归档 %s 失败: %v", v, err)
		}
	}
	if got := len(m.Archive()); got != len(versions) {
		t.Fatalf("升级包不应被自动清理，期望 %d 个，实际 %d", len(versions), got)
	}
	for _, v := range versions {
		if _, err := os.Stat(filepath.Join(m.cfg.Dir, "archive", v+".tar.gz")); err != nil {
			t.Fatalf("版本 %s 的归档文件应保留: %v", v, err)
		}
	}
}

// 删除升级包后：归档文件与清单条目都消失，该版本不再允许切换。
func TestDeleteArchiveRemovesPackageAndSwitchTarget(t *testing.T) {
	m := newTestManager(t)
	src := filepath.Join(t.TempDir(), "pkg.tar.gz")
	writeFakePackage(t, src)
	if err := m.archivePackage("1.0.0", src); err != nil {
		t.Fatalf("归档失败: %v", err)
	}
	archiveFile := filepath.Join(m.cfg.Dir, "archive", "1.0.0.tar.gz")
	if _, err := os.Stat(archiveFile); err != nil {
		t.Fatalf("归档文件应存在: %v", err)
	}

	if err := m.DeleteArchive("1.0.0"); err != nil {
		t.Fatalf("删除升级包失败: %v", err)
	}
	if _, err := os.Stat(archiveFile); !os.IsNotExist(err) {
		t.Fatalf("删除后归档文件应不存在，实际 err=%v", err)
	}
	if got := len(m.Archive()); got != 0 {
		t.Fatalf("删除后归档清单应为空，实际 %d", got)
	}
	// 升级包已删除 → 该版本不能再被切换
	if _, err := m.RollbackTo("1.0.0", "tester"); err == nil {
		t.Fatal("升级包已删除，切换（RollbackTo）应报错")
	}
	// 重复删除应报错（版本已不在清单里）
	if err := m.DeleteArchive("1.0.0"); err == nil {
		t.Fatal("重复删除同一版本应报错")
	}
	if err := m.DeleteArchive(""); err == nil {
		t.Fatal("缺少版本号时应报错")
	}
}

// 磁盘上的归档文件被外部清掉时，清单不应再列出该版本（列表以实际存在的包为准）。
func TestArchiveSkipsMissingFile(t *testing.T) {
	m := newTestManager(t)
	src := filepath.Join(t.TempDir(), "pkg.tar.gz")
	writeFakePackage(t, src)
	if err := m.archivePackage("1.0.0", src); err != nil {
		t.Fatalf("归档失败: %v", err)
	}
	if err := os.Remove(filepath.Join(m.cfg.Dir, "archive", "1.0.0.tar.gz")); err != nil {
		t.Fatalf("删除归档文件失败: %v", err)
	}
	if got := len(m.Archive()); got != 0 {
		t.Fatalf("归档文件已丢失的版本不应出现在清单中，实际 %d 个", got)
	}
}

// 启动时清理 unpacked 残留：解压目录不跨进程保留（每个约 160MB，长期累积会撑满磁盘）。
func TestNewPrunesUnpackedLeftovers(t *testing.T) {
	dir := t.TempDir()
	upgradesDir := filepath.Join(dir, "upgrades")
	leftover := filepath.Join(upgradesDir, "unpacked", "1786788538530401105-b0715294")
	if err := os.MkdirAll(leftover, 0o755); err != nil {
		t.Fatalf("创建残留目录失败: %v", err)
	}
	writeFakePackage(t, filepath.Join(leftover, "manifest.json"))

	if _, err := New(Config{
		Dir:         upgradesDir,
		BinDir:      filepath.Join(dir, "bin"),
		WebDir:      filepath.Join(dir, "web"),
		AgentBinDir: filepath.Join(dir, "agent-dist"),
	}, nil); err != nil {
		t.Fatalf("创建升级管理器失败: %v", err)
	}
	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Fatalf("启动时应清理 unpacked 残留，实际 err=%v", err)
	}
}
