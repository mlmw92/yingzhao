package db

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func TestNormalizeDSNAddsCharset(t *testing.T) {
	got, err := NormalizeDSN("u:p@tcp(127.0.0.1:3306)/yingzhao")
	if err != nil {
		t.Fatalf("NormalizeDSN: %v", err)
	}
	cfg, err := mysql.ParseDSN(got)
	if err != nil {
		t.Fatalf("结果不是合法 DSN: %q, %v", got, err)
	}
	if cfg.Params["charset"] != "utf8mb4" {
		t.Fatalf("charset = %q，want utf8mb4（dsn=%s）", cfg.Params["charset"], got)
	}
}

func TestNormalizeDSNKeepsExplicitCharset(t *testing.T) {
	got, err := NormalizeDSN("u:p@tcp(127.0.0.1:3306)/yingzhao?charset=utf8mb4&timeout=3s")
	if err != nil {
		t.Fatalf("NormalizeDSN: %v", err)
	}
	cfg, _ := mysql.ParseDSN(got)
	// timeout 是驱动 Config 的专用字段，不在 Params 里。
	if cfg.Params["charset"] != "utf8mb4" || cfg.Timeout != 3*time.Second {
		t.Fatalf("已有参数不应被改写：charset=%v timeout=%v", cfg.Params, cfg.Timeout)
	}
}

func TestNormalizeDSNRejectsMalformed(t *testing.T) {
	for _, in := range []string{"", "   ", "127.0.0.1:3306", "yingzhao"} {
		if _, err := NormalizeDSN(in); err == nil {
			t.Fatalf("NormalizeDSN(%q) 应报错", in)
		}
	}
}

func TestRedactDSNHidesPassword(t *testing.T) {
	for _, in := range []string{
		"u:secret@tcp(127.0.0.1:3306)/yingzhao?charset=utf8mb4",
		"u:p@ss@tcp(127.0.0.1:3306)/yingzhao",
	} {
		got := RedactDSN(in)
		if strings.Contains(got, "secret") || strings.Contains(got, "p@ss") {
			t.Fatalf("RedactDSN(%q) = %q，仍含密码", in, got)
		}
	}
	if got := RedactDSN("u@tcp(h:3306)/db"); strings.Contains(got, "***") {
		t.Fatalf("无密码时不应加掩码：%q", got)
	}
	if got := RedactDSN("not-a-dsn"); strings.Contains(got, "not-a-dsn") {
		t.Fatalf("无法解析时不应回显原文：%q", got)
	}
}

func TestOpenRejectsEmptyDSN(t *testing.T) {
	if _, err := Open(context.Background(), Config{}); err == nil {
		t.Fatal("空 DSN 应报错")
	}
}

// MySQL 的 UPDATE 默认只把「真正改了值的行」计入 RowsAffected；值没变时返回 0。
// 代码里有用它判断「这行是否存在」的地方（例如保存资产类型模型），
// 必须让驱动回报「匹配到的行」——clientFoundRows 就是为此存在的。
func TestNormalizeDSNEnablesClientFoundRows(t *testing.T) {
	got, err := NormalizeDSN("u:p@tcp(127.0.0.1:3306)/yingzhao")
	if err != nil {
		t.Fatalf("NormalizeDSN: %v", err)
	}
	cfg, err := mysql.ParseDSN(got)
	if err != nil {
		t.Fatalf("结果不是合法 DSN: %q, %v", got, err)
	}
	if !cfg.ClientFoundRows {
		t.Fatalf("clientFoundRows 未启用（dsn=%s）：UPDATE 值未变时 RowsAffected=0 会被误判为行不存在", got)
	}
}
