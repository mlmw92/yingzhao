package node

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/mlmw92/yingzhao/internal/model"
	"github.com/mlmw92/yingzhao/internal/server/db"
	"github.com/mlmw92/yingzhao/internal/server/migrate"
	"github.com/mlmw92/yingzhao/internal/server/sqlx"
)

// MySQL 后端集成用例：整份同步（upsert + 删除缺失）与心跳节流落库。
func TestMySQLPersist(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN 未设置")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, db.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	ms, err := migrate.Load("mysql")
	if err != nil {
		t.Fatalf("Load migrations: %v", err)
	}
	if _, err := migrate.Apply(ctx, pool, sqlx.MySQL(), ms); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	for _, tbl := range []string{"nodes", "node_groups"} {
		if _, err := pool.Exec("DELETE FROM " + tbl); err != nil {
			t.Fatalf("清空 %s 失败: %v", tbl, err)
		}
	}

	m, err := NewWithDB(pool, time.Minute)
	if err != nil {
		t.Fatalf("NewWithDB: %v", err)
	}
	m.Register(&model.ReportPayload{
		Node: "web-01", Group: "g1", IP: "10.0.0.1", OS: "linux", Arch: "amd64",
		Mode: model.ModeCollect, Version: "1.0.0",
		Labels:   map[string]string{"env": "prod"},
		HostInfo: model.HostInfo{CPUCores: 8},
	})
	m.Register(&model.ReportPayload{Node: "web-02", Group: "g1", IP: "10.0.0.2", Mode: model.ModeCollect})
	if err := m.SetNodeDisplayName("web-01", "前端一号"); err != nil {
		t.Fatal(err)
	}
	m.AddGroup("g1", "web")

	// 心跳落库（注册写入里也带了 last_seen，这里验证单独通道）
	m.FlushHeartbeats()

	reloaded, err := NewWithDB(pool, time.Minute)
	if err != nil {
		t.Fatalf("重新加载: %v", err)
	}
	n, ok := reloaded.GetNode("web-01")
	if !ok || n.Group != "g1" || n.IP != "10.0.0.1" || n.DisplayName != "前端一号" ||
		n.Labels["env"] != "prod" || n.HostInfo.CPUCores != 8 || n.LastSeen == 0 {
		t.Fatalf("重新加载 = %+v, %v", n, ok)
	}
	if _, ok := reloaded.GetNode("web-02"); !ok {
		t.Fatal("web-02 未落库")
	}
	found := false
	for _, g := range reloaded.ListGroups() {
		if g.Name == "g1" {
			found = true
		}
	}
	if !found {
		t.Fatal("分组未落库")
	}

	// 删除节点：整份同步必须把行删掉，否则重启后它又回来了
	reloaded.RemoveNode("web-02")
	again, err := NewWithDB(pool, time.Minute)
	if err != nil {
		t.Fatalf("再次加载: %v", err)
	}
	if _, ok := again.GetNode("web-02"); ok {
		t.Fatal("已删除的节点仍能读回")
	}

	// 心跳只更新 last_seen/status，不碰注册信息
	var before int64
	if err := pool.QueryRow("SELECT last_seen FROM nodes WHERE hostname='web-01'").Scan(&before); err != nil {
		t.Fatal(err)
	}
	again.FlushHeartbeats()
	var after int64
	if err := pool.QueryRow("SELECT last_seen FROM nodes WHERE hostname='web-01'").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after < before {
		t.Fatalf("心跳不应让 last_seen 回退：%d -> %d", before, after)
	}
}

// 心跳必须能补写缺失的节点行。
//
// 首次注册时 SaveMeta 若因瞬时 DB 错误失败，内存里有节点、库里没有行；
// 之后 regChanged=false，不会再走 SaveMeta，而 FlushHeartbeat 原来只 UPDATE，
// 影响 0 行且不报错——该节点在库里永久缺席，直到重启或注册字段再变化。
func TestMySQLFlushHeartbeatUpsertsMissingNode(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN 未设置")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, db.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	if _, err := migrate.Apply(ctx, pool, sqlx.MySQL(), mustLoadMigrations(t)); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, err := pool.Exec("DELETE FROM nodes WHERE hostname=?", "ghost-01"); err != nil {
		t.Fatal(err)
	}
	m, err := NewWithDB(pool, time.Minute)
	if err != nil {
		t.Fatalf("NewWithDB: %v", err)
	}
	if err := m.store.FlushHeartbeat([]heartbeat{{Hostname: "ghost-01", Status: "online", LastSeen: 1700000000000}}); err != nil {
		t.Fatalf("FlushHeartbeat: %v", err)
	}
	var lastSeen int64
	var status string
	if err := pool.QueryRow("SELECT last_seen, status FROM nodes WHERE hostname=?", "ghost-01").Scan(&lastSeen, &status); err != nil {
		t.Fatalf("缺失节点应被心跳补写出来: %v", err)
	}
	if lastSeen != 1700000000000 || status != "online" {
		t.Fatalf("补写的值不对: last_seen=%d status=%q", lastSeen, status)
	}
}

func mustLoadMigrations(t *testing.T) []migrate.Migration {
	t.Helper()
	ms, err := migrate.Load("mysql")
	if err != nil {
		t.Fatalf("Load migrations: %v", err)
	}
	return ms
}
