package asset

import (
	"strings"
	"testing"
)

// 达梦建表语句的静态检查：这些错误没有 DM 实例时也能提前发现。
func TestDMSchemaStatements(t *testing.T) {
	if len(dmSchemaStatements) != 25 {
		t.Fatalf("达梦建表语句 %d 条，want 25", len(dmSchemaStatements))
	}
	if len(dmIndexStatements) != 23 {
		t.Fatalf("达梦索引语句 %d 条，want 23", len(dmIndexStatements))
	}
	for _, stmt := range dmSchemaStatements {
		switch {
		case strings.Contains(stmt, "AUTO_INCREMENT"):
			t.Fatalf("达梦不支持 AUTO_INCREMENT: %s", stmt)
		case strings.Contains(stmt, "ENGINE="):
			t.Fatalf("达梦建表不应带 ENGINE: %s", stmt)
		case strings.Contains(stmt, "`"):
			t.Fatalf("达梦 SQL 不应出现反引号: %s", stmt)
		}
	}
	joined := strings.Join(dmSchemaStatements, "\n")
	if !strings.Contains(joined, `"rows"`) {
		t.Fatal("rows 是达梦保留字，建表时应加双引号")
	}
	if strings.Contains(joined, `"key"`) || strings.Contains(joined, `"schema"`) {
		t.Fatal("key/schema 在达梦不是保留字，加引号会造成大小写不一致")
	}
}
