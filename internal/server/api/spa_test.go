package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSPACacheHeaders(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app-abc123.js"), []byte("console.log(1)"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := scopeTestAPI(t)
	a.webDir = dir
	mux := http.NewServeMux()
	a.RegisterDashboard(mux)

	// index 必须每次回源：否则部署后浏览器一直用旧 index，引用已删除的旧 chunk
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("index Cache-Control = %q，want no-cache", got)
	}

	// 带内容哈希的 assets 可以长缓存
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/app-abc123.js", nil))
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("assets Cache-Control = %q", got)
	}

	// 已删除的旧 chunk 必须是 404（不能回退 index，否则浏览器会把 HTML 当 JS 解析）
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/gone-old.js", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("缺失 chunk code = %d，want 404", rec.Code)
	}
}
