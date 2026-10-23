package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 前端产物版本用于发现「二进制升级了、web 没换」这种静默错配：
// 页面能打开、接口也能通，但界面行为还停在旧版本。
func TestReadWebVersion(t *testing.T) {
	dir := t.TempDir()
	assets := filepath.Join(dir, "assets")
	if err := os.MkdirAll(assets, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assets, "version-abc123.js"),
		[]byte(`const o="1.43.0";export{o as W};`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readWebVersion(dir)
	if err != nil {
		t.Fatalf("readWebVersion: %v", err)
	}
	if got != "1.43.0" {
		t.Fatalf("version = %q, want 1.43.0", got)
	}
}

// 部署残留多个 version-*.js 时取最新的那个（旧产物可能没清干净）。
func TestReadWebVersionPicksNewest(t *testing.T) {
	dir := t.TempDir()
	assets := filepath.Join(dir, "assets")
	if err := os.MkdirAll(assets, 0o755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(assets, "version-old.js")
	fresh := filepath.Join(assets, "version-new.js")
	if err := os.WriteFile(old, []byte(`const o="1.30.0";`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fresh, []byte(`const o="1.43.0";`), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	got, err := readWebVersion(dir)
	if err != nil {
		t.Fatalf("readWebVersion: %v", err)
	}
	if got != "1.43.0" {
		t.Fatalf("version = %q, want 1.43.0（应取最新的产物）", got)
	}
}

func TestReadWebVersionMissing(t *testing.T) {
	if _, err := readWebVersion(t.TempDir()); err == nil {
		t.Fatal("没有前端产物时应返回错误")
	}
}
