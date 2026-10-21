package audit

import (
	"strings"
	"testing"
)

// MySQL 不支持 IN 子查询里的 LIMIT，必须再套一层派生表。
func TestPruneRowsSQLDialect(t *testing.T) {
	if got := pruneRowsSQL(false); strings.Contains(got, ") t") {
		t.Fatalf("SQLite 不应套派生表: %s", got)
	}
	if got := pruneRowsSQL(true); !strings.Contains(got, "LIMIT ?) t") {
		t.Fatalf("MySQL 应套派生表: %s", got)
	}
}
