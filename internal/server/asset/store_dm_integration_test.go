package asset

import (
	"context"
	"os"
	"testing"

	"github.com/mlmw92/yingzhao/internal/server/db"
	"github.com/mlmw92/yingzhao/internal/server/sqlx"
)

// 达梦集成用例：建表、upsert、变更记录、关系优先级、分页与类型模型。
func TestAssetStoreDM(t *testing.T) {
	dsn := os.Getenv("TEST_DM_DSN")
	if dsn == "" {
		t.Skip("TEST_DM_DSN 未设置")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, db.Config{Driver: "dm", DSN: dsn})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	// 按依赖顺序删除（子表在前）；不存在的表忽略错误。
	for _, tbl := range []string{
		"inspect_finding_baselines", "inspect_findings", "inspect_run_members",
		"inspect_run_manifest", "inspect_runs", "inspect_baselines",
		"asset_link_suppressions", "asset_links", "asset_changes", "snapshot_fields",
		"snapshots", "asset_attrs", "asset_labels", "asset_ignored", "asset_seen",
		"assets", "asset_types",
		"audit_pruned", "audit_chain", "audit_events", "alert_acks",
		"ops_tasks", "ops_meta", "ops_caps", "schema_version",
	} {
		_, _ = pool.Exec("DROP TABLE " + tbl)
	}

	st, err := OpenSQL(pool, sqlx.DM())
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
	if err := pool.QueryRow("SELECT source FROM asset_links WHERE from_id=? AND to_id=? AND kind=?",
		got.ID, other.ID, string(LinkDependsOn)).Scan(&src); err != nil {
		t.Fatalf("查询关系失败: %v", err)
	}
	if src != string(SourceManual) {
		t.Fatalf("人工关系不应被采集降级，实际 source=%q", src)
	}

	typeKey := BuiltinTypes()[0].Key
	sch := TypeSchema{FocusFields: []string{"cpuCores"}}
	if err := st.saveTypeSchema(typeKey, sch); err != nil {
		t.Fatalf("保存类型模型: %v", err)
	}
	if err := st.saveTypeSchema(typeKey, sch); err != nil {
		t.Fatalf("重复保存同一份类型模型: %v", err)
	}
}

func TestAssetDMComments(t *testing.T) {
	dsn := os.Getenv("TEST_DM_DSN")
	if dsn == "" {
		t.Skip("TEST_DM_DSN 未设置")
	}
	pool, err := db.Open(context.Background(), db.Config{Driver: "dm", DSN: dsn})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	if _, err := OpenSQL(pool, sqlx.DM()); err != nil {
		t.Fatalf("OpenSQL: %v", err)
	}
	var colComment, tableComment string
	if err := pool.QueryRow(`SELECT COMMENTS FROM USER_COL_COMMENTS
		WHERE TABLE_NAME='ASSETS' AND COLUMN_NAME='NATURAL_KEY'`).Scan(&colComment); err != nil {
		t.Fatalf("查询列注释失败: %v", err)
	}
	if colComment == "" {
		t.Fatal("ASSETS.NATURAL_KEY 缺少数据库注释")
	}
	if err := pool.QueryRow(`SELECT COMMENTS FROM USER_TAB_COMMENTS WHERE TABLE_NAME='ASSETS'`).Scan(&tableComment); err != nil {
		t.Fatalf("查询表注释失败: %v", err)
	}
	if tableComment == "" {
		t.Fatal("ASSETS 缺少表注释")
	}
}
