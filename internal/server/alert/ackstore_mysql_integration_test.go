package alert

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mlmw92/yingzhao/internal/server/asset"
	"github.com/mlmw92/yingzhao/internal/server/db"
	"github.com/mlmw92/yingzhao/internal/server/sqlx"
)

// MySQL 集成用例：JSON 回填必须走 MySQL 方言。
//
// 回填曾在 s.mysql 赋值之前执行，生成 SQLite 的 ON CONFLICT，MySQL 报 1064；
// UseSQL 返回错误后 main 只记一条日志、继续按本地 JSON 工作——升级部署于是
// 永远停在文件后端，而阶段 3 的实机验证因为表已由迁移工具填过而没暴露。
func TestAckMySQLBackfill(t *testing.T) {
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
	if _, err := asset.OpenMySQL(pool); err != nil {
		t.Fatalf("OpenMySQL: %v", err)
	}
	if _, err := pool.Exec("DELETE FROM alert_acks"); err != nil {
		t.Fatalf("清空 alert_acks 失败: %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "alert_acks.json")
	list := []AckInfo{{
		Rule: "CPU 高", Host: "web-01", Instance: "10.0.0.1:9100", StartsAt: 1700000000000,
		Status: StatusAck, User: "alice", Assignee: "alice", Time: 1700000001000,
		Comments: []Comment{{User: "alice", Text: "处理中", Time: 1700000002000}},
	}}
	raw, _ := json.Marshal(list)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	st := NewAckStore(path)
	if err := st.UseSQL(pool, sqlx.MySQL()); err != nil {
		t.Fatalf("UseSQL 回填失败: %v", err)
	}
	got, ok := st.Get("CPU 高", "web-01", "10.0.0.1:9100", 1700000000000)
	if !ok {
		t.Fatalf("回填后应能读到处置记录")
	}
	if got.Assignee != "alice" || len(got.Comments) != 1 {
		t.Fatalf("回填内容不对: %+v", got)
	}

	// 写路径也要走 MySQL 方言。
	if _, err := st.Mark("CPU 高", "web-01", "10.0.0.1:9100", 1700000000000, "bob"); err != nil {
		t.Fatalf("Mark 失败: %v", err)
	}
	// 重开：数据从库读回。
	st2 := NewAckStore(filepath.Join(dir, "missing.json"))
	if err := st2.UseSQL(pool, sqlx.MySQL()); err != nil {
		t.Fatalf("重开 UseSQL 失败: %v", err)
	}
	if got, ok := st2.Get("CPU 高", "web-01", "10.0.0.1:9100", 1700000000000); !ok || got.User != "bob" {
		t.Fatalf("重启后读回不对: ok=%v %+v", ok, got)
	}
}
