package sqlx

import (
	"strings"
	"testing"
)

func TestRewriteQuotesReservedIdents(t *testing.T) {
	cases := []struct {
		name string
		d    Dialect
		in   string
		want string
	}{
		{
			name: "MySQL key",
			d:    MySQL(),
			in:   "SELECT value FROM ops_meta WHERE key='seq'",
			want: "SELECT value FROM ops_meta WHERE `key`='seq'",
		},
		{
			name: "MySQL rows",
			d:    MySQL(),
			in:   "SELECT from_seq, rows FROM audit_pruned",
			want: "SELECT from_seq, `rows` FROM audit_pruned",
		},
		{
			name: "MySQL schema",
			d:    MySQL(),
			in:   "SELECT key, schema FROM asset_types",
			want: "SELECT `key`, `schema` FROM asset_types",
		},
		{
			name: "MySQL schema_version 不误伤",
			d:    MySQL(),
			in:   "SELECT version FROM schema_version WHERE id = 1",
			want: "SELECT version FROM schema_version WHERE id = 1",
		},
		{
			name: "已加引号的不重复处理",
			d:    MySQL(),
			in:   "SELECT value FROM ops_meta WHERE `key`='seq' AND `rows`=1",
			want: "SELECT value FROM ops_meta WHERE `key`='seq' AND `rows`=1",
		},
		{
			name: "PostgreSQL level（金仓保留字）",
			d:    Postgres(),
			in:   "SELECT level FROM inspect_findings WHERE level=?",
			want: `SELECT "level" FROM inspect_findings WHERE "level"=?`,
		},
		{
			name: "MySQL 不引用 level",
			d:    MySQL(),
			in:   "SELECT level FROM inspect_findings",
			want: "SELECT level FROM inspect_findings",
		},
		{
			name: "PostgreSQL key/rows/schema",
			d:    Postgres(),
			in:   "SELECT key, schema, rows FROM t WHERE key=?",
			want: `SELECT "key", "schema", "rows" FROM t WHERE "key"=?`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.d.Rewrite(tc.in); got != tc.want {
				t.Fatalf("Rewrite(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestBindDollar(t *testing.T) {
	cases := map[string]string{
		"SELECT * FROM t WHERE a=? AND b=?":          "SELECT * FROM t WHERE a=$1 AND b=$2",
		"SELECT '?' AS q, a FROM t WHERE a=?":        "SELECT '?' AS q, a FROM t WHERE a=$1",
		"SELECT 'it''s ?' AS q FROM t WHERE a=?":     "SELECT 'it''s ?' AS q FROM t WHERE a=$1",
		`SELECT "weird?" FROM t WHERE a=?`:           `SELECT "weird?" FROM t WHERE a=$1`,
		"SELECT 1":                                   "SELECT 1",
		"UPDATE t SET a=? WHERE b=? AND c=? AND d=?": "UPDATE t SET a=$1 WHERE b=$2 AND c=$3 AND d=$4",
	}
	for in, want := range cases {
		if got := Postgres().Bind(in); got != want {
			t.Fatalf("Bind(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUpsert(t *testing.T) {
	t.Run("MySQL", func(t *testing.T) {
		got := MySQL().Upsert("ack_key", "status=excluded.status")
		if !strings.Contains(got, "ON DUPLICATE KEY UPDATE") {
			t.Fatalf("MySQL upsert 缺少 ON DUPLICATE KEY UPDATE: %q", got)
		}
		if !strings.Contains(got, "status=VALUES(status)") {
			t.Fatalf("MySQL upsert 未把 excluded 转成 VALUES(): %q", got)
		}
	})
	t.Run("PostgreSQL", func(t *testing.T) {
		got := Postgres().Upsert("ack_key", "status=excluded.status")
		if !strings.Contains(got, `ON CONFLICT("ack_key") DO UPDATE SET`) {
			t.Fatalf("PG upsert 形状不对: %q", got)
		}
		if !strings.Contains(got, "status=excluded.status") {
			t.Fatalf("PG upsert 应保留 excluded: %q", got)
		}
	})
	t.Run("PostgreSQL 复合冲突键", func(t *testing.T) {
		got := Postgres().Upsert("a,b", "x=excluded.x")
		if !strings.Contains(got, `ON CONFLICT("a", "b")`) {
			t.Fatalf("PG 复合冲突键不对: %q", got)
		}
	})
	t.Run("MAX 转 GREATEST", func(t *testing.T) {
		for _, d := range []Dialect{MySQL(), Postgres()} {
			got := d.Upsert("k", "last_seen=MAX(last_seen, excluded.last_seen)")
			if !strings.Contains(got, "GREATEST(") {
				t.Fatalf("%s 的 MAX 未转成 GREATEST: %q", d.Name(), got)
			}
		}
	})
}

func TestUpsertNothing(t *testing.T) {
	if got := MySQL().UpsertNothing("id"); !strings.Contains(got, "ON DUPLICATE KEY UPDATE") {
		t.Fatalf("MySQL no-op upsert 形状不对: %q", got)
	}
	if got := Postgres().UpsertNothing("id"); !strings.Contains(got, `ON CONFLICT("id") DO NOTHING`) {
		t.Fatalf("PG no-op upsert 形状不对: %q", got)
	}
}

func TestFromDual(t *testing.T) {
	if got := MySQL().FromDual(); got != " FROM DUAL" {
		t.Fatalf("MySQL FromDual = %q", got)
	}
	if got := Postgres().FromDual(); got != "" {
		t.Fatalf("PG FromDual = %q", got)
	}
}

func TestParseDialect(t *testing.T) {
	for _, name := range []string{"", "mysql"} {
		d, err := Parse(name)
		if err != nil || d.Name() != "mysql" {
			t.Fatalf("Parse(%q) = %v, %v", name, d, err)
		}
	}
	for _, name := range []string{"postgres", "postgresql", "pg", "highgo", "kingbase"} {
		d, err := Parse(name)
		if err != nil || d.Name() != "postgres" {
			t.Fatalf("Parse(%q) = %v, %v", name, d, err)
		}
	}
	if d, err := Parse("dm"); err != nil || d.Name() != "dm" {
		t.Fatalf("Parse(dm) = %v, %v", d, err)
	}
}
