package asset

import (
	"context"
	"os"
	"testing"

	"github.com/mlmw92/yingzhao/internal/server/db"
	"github.com/mlmw92/yingzhao/internal/server/sqlx"
)

// PostgreSQL 集成用例：建表、upsert、变更记录、关系优先级与分页都要在真实 PG 上跑通。
func TestAssetStorePostgres(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN 未设置")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, db.Config{Driver: "postgres", DSN: dsn})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	for _, tbl := range []string{
		"asset_attrs", "asset_links", "asset_link_suppressions", "asset_changes",
		"snapshots", "snapshot_fields", "asset_seen", "asset_ignored", "asset_labels",
		"inspect_run_manifest", "inspect_run_members", "inspect_findings",
		"inspect_finding_baselines", "inspect_baselines", "inspect_runs",
		"assets", "asset_types",
		"audit_events", "audit_chain", "audit_pruned", "alert_acks",
		"ops_tasks", "ops_meta", "ops_caps", "schema_version",
	} {
		if _, err := pool.Exec("DROP TABLE IF EXISTS " + tbl + " CASCADE"); err != nil {
			t.Fatalf("drop %s: %v", tbl, err)
		}
	}

	st, err := OpenSQL(pool, sqlx.Postgres())
	if err != nil {
		t.Fatalf("OpenSQL: %v", err)
	}
	svc := NewService(st)

	if _, _, err := svc.Apply(Observation{
		TypeKey: TypeHost, NaturalKey: "web-01", Name: "web-01", Node: "web-01",
		Source: SourceDiscovery, Attrs: map[string]string{"os": "linux", "cpuCores": "8"},
	}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got, ok, err := svc.GetHostByName("web-01")
	if err != nil || !ok || got.Name != "web-01" {
		t.Fatalf("GetHostByName = %+v, %v, %v", got, ok, err)
	}
	if _, _, err := svc.Apply(Observation{
		TypeKey: TypeHost, NaturalKey: "web-01", Name: "web-01", Node: "web-01",
		Source: SourceDiscovery, Attrs: map[string]string{"os": "linux", "cpuCores": "16"},
	}); err != nil {
		t.Fatalf("Apply 2: %v", err)
	}
	changes, err := svc.History(Ref{TypeKey: TypeHost, NaturalKey: "web-01"}, 10)
	if err != nil || len(changes) == 0 {
		t.Fatalf("History = %d, %v", len(changes), err)
	}
	list, err := svc.List(ListFilter{Limit: 10})
	if err != nil || len(list) != 1 {
		t.Fatalf("List = %d, %v", len(list), err)
	}

	other, _, err := svc.Apply(Observation{
		TypeKey: TypeMiddlewareInst, NaturalKey: "redis-01", Name: "redis-01", Node: "web-01",
		Source: SourceDiscovery,
	})
	if err != nil {
		t.Fatalf("Apply redis: %v", err)
	}
	if err := st.linkAssets(got.ID, other.ID, LinkDependsOn, SourceDiscovery, 1); err != nil {
		t.Fatalf("linkAssets: %v", err)
	}
	if err := st.linkAssets(got.ID, other.ID, LinkDependsOn, SourceManual, 2); err != nil {
		t.Fatalf("linkAssets manual: %v", err)
	}
	if err := st.linkAssets(got.ID, other.ID, LinkDependsOn, SourceDiscovery, 3); err != nil {
		t.Fatalf("linkAssets again: %v", err)
	}
	var src string
	if err := pool.QueryRow("SELECT source FROM asset_links WHERE from_id=$1 AND to_id=$2 AND kind=$3",
		got.ID, other.ID, string(LinkDependsOn)).Scan(&src); err != nil {
		t.Fatalf("查询关系失败: %v", err)
	}
	if src != string(SourceManual) {
		t.Fatalf("人工关系不应被采集降级，实际 source=%q", src)
	}

	// 类型模型保存（`schema` 是保留字，PG 下必须正确引用；重复保存也要成功）。
	typeKey := BuiltinTypes()[0].Key
	sch := TypeSchema{FocusFields: []string{"cpuCores"}}
	if err := st.saveTypeSchema(typeKey, sch); err != nil {
		t.Fatalf("保存类型模型: %v", err)
	}
	if err := st.saveTypeSchema(typeKey, sch); err != nil {
		t.Fatalf("重复保存同一份类型模型: %v", err)
	}
}

func TestAssetPostgresComments(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN 未设置")
	}
	pool, err := db.Open(context.Background(), db.Config{Driver: "postgres", DSN: dsn})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	if _, err := OpenSQL(pool, sqlx.Postgres()); err != nil {
		t.Fatalf("OpenSQL: %v", err)
	}
	var colComment, tableComment string
	if err := pool.QueryRow(`SELECT col_description('assets'::regclass, ordinal_position)
		FROM information_schema.columns WHERE table_name='assets' AND column_name='natural_key'`).Scan(&colComment); err != nil {
		t.Fatalf("查询列注释失败: %v", err)
	}
	if colComment == "" {
		t.Fatal("assets.natural_key 缺少数据库注释")
	}
	if err := pool.QueryRow(`SELECT obj_description('assets'::regclass)`).Scan(&tableComment); err != nil {
		t.Fatalf("查询表注释失败: %v", err)
	}
	if tableComment == "" {
		t.Fatal("assets 缺少表注释")
	}
}
