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

// PostgreSQL 集成用例：整份同步、心跳合并、缺失节点补写。
func TestDMPersist(t *testing.T) {
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
	ms, err := migrate.Load("dm")
	if err != nil {
		t.Fatalf("Load migrations: %v", err)
	}
	if _, err := migrate.Apply(ctx, pool, sqlx.DM(), ms); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	for _, tbl := range []string{"nodes", "node_groups"} {
		if _, err := pool.Exec("DELETE FROM " + tbl); err != nil {
			t.Fatalf("清空 %s 失败: %v", tbl, err)
		}
	}

	m, err := NewWithSQLDB(pool, sqlx.DM(), time.Minute)
	if err != nil {
		t.Fatalf("NewWithSQLDB: %v", err)
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
	m.FlushHeartbeats()

	reloaded, err := NewWithSQLDB(pool, sqlx.DM(), time.Minute)
	if err != nil {
		t.Fatalf("重开: %v", err)
	}
	n, ok := reloaded.GetNode("web-01")
	if !ok || n.DisplayName != "前端一号" || n.Labels["env"] != "prod" {
		t.Fatalf("重开后节点信息不对: ok=%v %+v", ok, n)
	}
	if _, ok := reloaded.GetNode("web-02"); !ok {
		t.Fatal("重开后 web-02 丢失")
	}

	// 心跳补写缺失节点
	if _, err := pool.Exec("DELETE FROM nodes WHERE hostname=?", "ghost-01"); err != nil {
		t.Fatal(err)
	}
	if err := reloaded.store.FlushHeartbeat([]heartbeat{{Hostname: "ghost-01", Status: "online", LastSeen: 1700000000000}}); err != nil {
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
