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

// PostgreSQL 集成用例：JSON 回填、写路径、重启读回。
func TestAckDMBackfill(t *testing.T) {
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
	if _, err := asset.OpenSQL(pool, sqlx.DM()); err != nil {
		t.Fatalf("OpenSQL: %v", err)
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
	if err := st.UseSQL(pool, sqlx.DM()); err != nil {
		t.Fatalf("UseSQL 回填失败: %v", err)
	}
	got, ok := st.Get("CPU 高", "web-01", "10.0.0.1:9100", 1700000000000)
	if !ok || got.Assignee != "alice" || len(got.Comments) != 1 {
		t.Fatalf("回填内容不对: ok=%v %+v", ok, got)
	}
	if _, err := st.Mark("CPU 高", "web-01", "10.0.0.1:9100", 1700000000000, "bob"); err != nil {
		t.Fatalf("Mark 失败: %v", err)
	}
	st2 := NewAckStore(filepath.Join(dir, "missing.json"))
	if err := st2.UseSQL(pool, sqlx.DM()); err != nil {
		t.Fatalf("重开 UseSQL 失败: %v", err)
	}
	if got, ok := st2.Get("CPU 高", "web-01", "10.0.0.1:9100", 1700000000000); !ok || got.User != "bob" {
		t.Fatalf("重启后读回不对: ok=%v %+v", ok, got)
	}
}
