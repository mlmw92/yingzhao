package configdoc

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFilePersisterRoundTrip(t *testing.T) {
	p := NewFilePersister(filepath.Join(t.TempDir(), "ui.yaml"))
	if _, err := p.Load(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("缺失文件应返回 ErrNotFound，实际 %v", err)
	}
	if err := p.Save([]byte("name: 英招\n")); err != nil {
		t.Fatal(err)
	}
	got, err := p.Load()
	if err != nil || string(got) != "name: 英招\n" {
		t.Fatalf("Load = %q, %v", got, err)
	}
}

func TestDBPersisterRoundTrip(t *testing.T) {
	inner := NewFileStore(t.TempDir())
	p := NewDBPersister(inner, "ui", nil)
	if _, err := p.Load(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("缺失命名空间应返回 ErrNotFound，实际 %v", err)
	}
	body := []byte("name: 英招\nsubtitle: 运维平台\n")
	if err := p.Save(body); err != nil {
		t.Fatal(err)
	}
	got, err := p.Load()
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("Load = %q, %v", got, err)
	}
}

func TestDBPersisterEncrypts(t *testing.T) {
	inner := NewFileStore(t.TempDir())
	key := bytes.Repeat([]byte{7}, 32)
	p := NewDBPersister(inner, "ai", key)
	plain := []byte("apiKey: sk-secret-123\n")
	if err := p.Save(plain); err != nil {
		t.Fatal(err)
	}

	// 底层存储里不能出现明文。
	payload, _, err := inner.Load(t.Context(), "ai")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "sk-secret-123") {
		t.Fatalf("加密后仍含明文: %s", payload)
	}

	got, err := p.Load()
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("解密后 = %q, %v", got, err)
	}

	// 错误主密钥必须失败，而不是返回垃圾。
	wrong := NewDBPersister(inner, "ai", bytes.Repeat([]byte{8}, 32))
	if _, err := wrong.Load(); err == nil {
		t.Fatal("错误主密钥应解密失败")
	}

	// 没有主密钥时不能静默返回密文。
	noKey := NewDBPersister(inner, "ai", nil)
	if _, err := noKey.Load(); err == nil {
		t.Fatal("缺少主密钥时应报错")
	}
}

func TestDBPersisterRejectsCorruptEnvelope(t *testing.T) {
	inner := NewFileStore(t.TempDir())
	if _, err := inner.Save(t.Context(), "ui", []byte(`{"not":"an envelope"}`), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := NewDBPersister(inner, "ui", nil).Load(); err == nil {
		t.Fatal("非法信封应报错")
	}
}

func TestLoadKeyCreatesAndReuses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.key")
	k1, err := LoadKey(path)
	if err != nil || len(k1) != 32 {
		t.Fatalf("首次 LoadKey = %d 字节, %v", len(k1), err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Windows 的权限模型不区分 0600，只在类 Unix 上断言。
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("主密钥文件权限 = %o，want 600", info.Mode().Perm())
	}
	k2, err := LoadKey(path)
	if err != nil || !bytes.Equal(k1, k2) {
		t.Fatalf("再次 LoadKey 应返回同一密钥: %v", err)
	}
}

func TestLoadKeyRejectsBadLength(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.key")
	if err := os.WriteFile(path, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadKey(path); err == nil {
		t.Fatal("长度不为 32 字节应报错")
	}
}
