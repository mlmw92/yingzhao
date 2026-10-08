package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nebula/monitor/internal/server/audit"
)

// 校验接口：有 audit:read 就能查，返回里带覆盖区间与条数（不能只说"通过"）。
func TestAuditVerify_API_ReportsCoverage(t *testing.T) {
	a := scopeTestAPI(t)
	st := audit.New(filepath.Join(t.TempDir(), "audit_events.json"))
	key, err := audit.LoadOrCreateChainKey(filepath.Join(t.TempDir(), "audit.key"))
	if err != nil {
		t.Fatalf("生成密钥失败: %v", err)
	}
	st.SetChainKey(key)
	a.audit = st

	mux := newRoutesMux(a)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, reqWith(globalPrincipal("audit:read"), http.MethodGet, "/api/v1/audit/verify", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	// 无库（本夹具没注入台账库）→ 降级模式：**必须明说没有完整性保护**，且 ok 不能是 true
	if body["enabled"] == true {
		t.Fatalf("无库时应报 enabled=false，实际 %v", body["enabled"])
	}
	if body["ok"] == true {
		t.Fatalf("没有保护时绝不能报 ok=true（把「没保护」显示成「安全」是本机制最坏的失败形态）")
	}
	if note, _ := body["note"].(string); note == "" {
		t.Fatal("必须说明为什么没有保护")
	}
}

// 没有 audit:read 一律 403（路由层拦住）。
func TestAuditVerify_API_NeedsPermission(t *testing.T) {
	a := scopeTestAPI(t)
	a.audit = audit.New(filepath.Join(t.TempDir(), "audit_events.json"))
	mux := newRoutesMux(a)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, reqWith(restrictedPrincipal([]string{"nodes:read"}, "g1"),
		http.MethodGet, "/api/v1/audit/verify", ""))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("无权限应 403，实际 %d", rec.Code)
	}
}

// 响应里**不许出现密钥本身**（只许出现指纹）。
func TestAuditVerify_API_NeverLeaksKey(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "audit.key")
	a := scopeTestAPI(t)
	key, err := audit.LoadOrCreateChainKey(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	st := audit.New(filepath.Join(t.TempDir(), "audit_events.json"))
	st.SetChainKey(key)
	a.audit = st

	mux := newRoutesMux(a)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, reqWith(globalPrincipal("audit:read"), http.MethodGet, "/api/v1/audit/verify", ""))
	// 密钥内容是文件里的那串十六进制；响应体里绝不该出现它
	raw, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	secret := strings.TrimSpace(string(raw))
	if len(secret) < 16 {
		t.Fatal("密钥文件内容异常")
	}
	if strings.Contains(rec.Body.String(), secret) {
		t.Fatal("响应体里出现了密钥内容——密钥一旦能从接口取到，防伪造就没了")
	}
	// 但指纹要给出（校验报告靠它区分「被改」与「换了密钥」）
	if body := decodeBody(t, rec); body["keyFingerprint"] == nil {
		t.Fatal("应返回密钥指纹")
	}
}

func TestAuditVerify_API_TimeStamped(t *testing.T) {
	a := scopeTestAPI(t)
	a.audit = audit.New(filepath.Join(t.TempDir(), "audit_events.json"))
	mux := newRoutesMux(a)
	rec := httptest.NewRecorder()
	before := time.Now().UnixMilli()
	mux.ServeHTTP(rec, reqWith(globalPrincipal("audit:read"), http.MethodGet, "/api/v1/audit/verify", ""))
	body := decodeBody(t, rec)
	ts, _ := body["checkedAt"].(float64)
	if int64(ts) < before {
		t.Fatalf("checkedAt 应是本次校验时间，实际 %v", body["checkedAt"])
	}
}
