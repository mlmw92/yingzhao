package sqlx

import (
	"strings"
	"testing"
)

func TestRewriteQueryQuotesReservedIdents(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		mysql bool
		want  string
	}{
		{
			name:  "key 列名加反引号",
			in:    "SELECT value FROM ops_meta WHERE key='seq'",
			mysql: true,
			want:  "SELECT value FROM ops_meta WHERE `key`='seq'",
		},
		{
			name:  "rows 列名加反引号（MySQL 8 保留字）",
			in:    "SELECT from_seq, rows FROM audit_pruned",
			mysql: true,
			want:  "SELECT from_seq, `rows` FROM audit_pruned",
		},
		{
			name:  "schema 列名加反引号（MySQL 8 保留字）",
			in:    "SELECT key, schema FROM asset_types",
			mysql: true,
			want:  "SELECT `key`, `schema` FROM asset_types",
		},
		{
			name:  "schema_version 表名不能被误伤",
			in:    "SELECT version FROM schema_version WHERE id = 1",
			mysql: true,
			want:  "SELECT version FROM schema_version WHERE id = 1",
		},
		{
			name:  "已加反引号的不重复处理",
			in:    "SELECT value FROM ops_meta WHERE `key`='seq' AND `rows`=1",
			mysql: true,
			want:  "SELECT value FROM ops_meta WHERE `key`='seq' AND `rows`=1",
		},
		{
			name:  "SQLite 模式不改写",
			in:    "SELECT value FROM ops_meta WHERE key='seq'",
			mysql: false,
			want:  "SELECT value FROM ops_meta WHERE key='seq'",
		},
		{
			name:  "COLLATE NOCASE 在 MySQL 下删除",
			in:    "SELECT * FROM nodes WHERE hostname=? COLLATE NOCASE",
			mysql: true,
			want:  "SELECT * FROM nodes WHERE hostname=?",
		},
		{
			name:  "COLLATE NOCASE 在 SQLite 下保留",
			in:    "SELECT * FROM nodes WHERE hostname=? COLLATE NOCASE",
			mysql: false,
			want:  "SELECT * FROM nodes WHERE hostname=? COLLATE NOCASE",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := rewriteQuery(tc.in, tc.mysql); got != tc.want {
				t.Fatalf("rewriteQuery(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestUpsert(t *testing.T) {
	t.Run("MySQL", func(t *testing.T) {
		got := Upsert(true, "ack_key", "status=excluded.status")
		if !strings.Contains(got, "ON DUPLICATE KEY UPDATE") {
			t.Fatalf("MySQL upsert 缺少 ON DUPLICATE KEY UPDATE: %q", got)
		}
		if !strings.Contains(got, "status=VALUES(status)") {
			t.Fatalf("MySQL upsert 未把 excluded 转成 VALUES(): %q", got)
		}
		if strings.Contains(got, "ON CONFLICT") {
			t.Fatalf("MySQL upsert 不应包含 ON CONFLICT: %q", got)
		}
	})
	t.Run("SQLite", func(t *testing.T) {
		got := Upsert(false, "ack_key", "status=excluded.status")
		if !strings.Contains(got, "ON CONFLICT(ack_key) DO UPDATE SET") {
			t.Fatalf("SQLite upsert 形状不对: %q", got)
		}
		if !strings.Contains(got, "status=excluded.status") {
			t.Fatalf("SQLite upsert 应保留 excluded: %q", got)
		}
	})
	t.Run("MySQL 的 MAX 转 GREATEST", func(t *testing.T) {
		got := Upsert(true, "k", "last_seen=MAX(last_seen, excluded.last_seen)")
		if !strings.Contains(got, "GREATEST(last_seen, VALUES(last_seen))") {
			t.Fatalf("MAX 未转成 GREATEST: %q", got)
		}
	})
}

func TestUpsertNothing(t *testing.T) {
	if got := UpsertNothing(true, "k"); !strings.Contains(got, "ON DUPLICATE KEY UPDATE") {
		t.Fatalf("MySQL no-op upsert 形状不对: %q", got)
	}
	if got := UpsertNothing(false, "k"); !strings.Contains(got, "ON CONFLICT(k) DO NOTHING") {
		t.Fatalf("SQLite no-op upsert 形状不对: %q", got)
	}
}

func TestDualFrom(t *testing.T) {
	if got := DualFrom(true); got != " FROM DUAL" {
		t.Fatalf("MySQL DualFrom = %q", got)
	}
	if got := DualFrom(false); got != "" {
		t.Fatalf("SQLite DualFrom = %q", got)
	}
}
