package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRunMigrateRequiresDSN(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	if err := os.WriteFile(path, []byte("listen: \":8080\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code := runMigrate([]string{"-config", path}, &out); code == 0 {
		t.Fatalf("缺少 DSN 时 migrate 应失败，输出=%s", out.String())
	}
}
