package asset

import (
	"context"
	"os"
	"testing"

	"github.com/mlmw92/yingzhao/internal/server/db"
)

// 资产库的 MySQL 方言集成用例：建表、upsert、变更记录、关系与快照都要在真实 MySQL 上跑通。
// 需要 TEST_MYSQL_DSN（指向一个可清空的测试库）。
func TestAssetStoreMySQL(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN 未设置")
	}
	pool, err := db.Open(context.Background(), db.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	// 清空：先关外键检查，再按任意顺序删表。
	if _, err := pool.Exec("SET FOREIGN_KEY_CHECKS=0"); err != nil {
		t.Fatal(err)
	}
	for _, tbl := range []string{
		"asset_attrs", "asset_links", "asset_link_suppressions", "asset_changes",
		"snapshots", "snapshot_fields", "asset_seen", "asset_ignored", "asset_labels",
		"inspect_run_manifest", "inspect_run_members", "inspect_findings",
		"inspect_finding_baselines", "inspect_baselines", "inspect_runs",
		"assets", "asset_types",
		"audit_events", "audit_chain", "audit_pruned", "alert_acks",
		"ops_tasks", "ops_meta", "ops_caps", "schema_version",
	} {
		if _, err := pool.Exec("DROP TABLE IF EXISTS " + tbl); err != nil {
			t.Fatalf("drop %s: %v", tbl, err)
		}
	}
	if _, err := pool.Exec("SET FOREIGN_KEY_CHECKS=1"); err != nil {
		t.Fatal(err)
	}

	st, err := OpenMySQL(pool)
	if err != nil {
		t.Fatalf("OpenMySQL: %v", err)
	}
	svc := NewService(st)

	// ① 采集入库
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

	// ② 同一资产再上报不同值 → upsert 覆盖，并留下变更记录
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

	// 列表分页（IN 子查询 + LIMIT）
	list, err := svc.List(ListFilter{Limit: 10})
	if err != nil || len(list) != 1 {
		t.Fatalf("List = %d, %v", len(list), err)
	}

	// ③ 关系 upsert（人工优先的 CASE 表达式在 MySQL 下也要成立）
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
	// 采集侧再次认领不应把人工关系降级
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
}

// 表/列注释必须真的写进数据库（不是 SQL 文件里的 -- 注释）。
func TestAssetMySQLComments(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN 未设置")
	}
	pool, err := db.Open(context.Background(), db.Config{Driver: "mysql", DSN: dsn})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	if _, err := OpenMySQL(pool); err != nil {
		t.Fatalf("OpenMySQL: %v", err)
	}
	var colComment, tableComment string
	if err := pool.QueryRow(`SELECT COLUMN_COMMENT FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='assets' AND COLUMN_NAME='natural_key'`).Scan(&colComment); err != nil {
		t.Fatalf("查询列注释失败: %v", err)
	}
	if colComment == "" {
		t.Fatal("assets.natural_key 缺少数据库注释")
	}
	if err := pool.QueryRow(`SELECT TABLE_COMMENT FROM information_schema.TABLES
		WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='assets'`).Scan(&tableComment); err != nil {
		t.Fatalf("查询表注释失败: %v", err)
	}
	if tableComment == "" {
		t.Fatal("assets 缺少表注释")
	}
}

// 存量库（schema_version=2）在启动时执行版本化升级（v3 补注释），
// 而不是只跑 CREATE TABLE IF NOT EXISTS（那对已有表是 no-op）。
func TestAssetMySQLUpgradeFromV2(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN 未设置")
	}
	pool, err := db.Open(context.Background(), db.Config{Driver: "mysql", DSN: dsn})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	if _, err := OpenMySQL(pool); err != nil {
		t.Fatalf("OpenMySQL: %v", err)
	}
	// 模拟旧版本库：清掉注释、版本降到 2。
	if _, err := pool.Exec(`ALTER TABLE assets MODIFY COLUMN natural_key VARCHAR(255) NOT NULL COMMENT ''`); err != nil {
		t.Fatalf("清除注释失败: %v", err)
	}
	if _, err := pool.Exec(`UPDATE schema_version SET version=2 WHERE id=1`); err != nil {
		t.Fatalf("降版本失败: %v", err)
	}

	if _, err := OpenMySQL(pool); err != nil {
		t.Fatalf("升级启动失败: %v", err)
	}
	var comment string
	if err := pool.QueryRow(`SELECT COLUMN_COMMENT FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='assets' AND COLUMN_NAME='natural_key'`).Scan(&comment); err != nil {
		t.Fatalf("查询注释失败: %v", err)
	}
	if comment == "" {
		t.Fatal("v3 升级没有把注释补回来")
	}
	var version int
	if err := pool.QueryRow(`SELECT version FROM schema_version WHERE id=1`).Scan(&version); err != nil {
		t.Fatalf("读取版本失败: %v", err)
	}
	if version != schemaVersion {
		t.Fatalf("升级后版本 = %d, want %d", version, schemaVersion)
	}
}
