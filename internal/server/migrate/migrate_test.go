package migrate

import "testing"

func TestPlanSkipsAppliedAndChecksChecksum(t *testing.T) {
	ms := []Migration{
		{Version: 2, Name: "b", SQL: "SELECT 2"},
		{Version: 1, Name: "a", SQL: "SELECT 1"},
	}
	pending, err := Plan(map[int]string{1: Checksum("SELECT 1")}, ms)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(pending) != 1 || pending[0].Version != 2 {
		t.Fatalf("pending = %+v", pending)
	}
}

func TestPlanRejectsModifiedAppliedMigration(t *testing.T) {
	ms := []Migration{{Version: 1, Name: "a", SQL: "SELECT 1"}}
	if _, err := Plan(map[int]string{1: Checksum("SELECT 999")}, ms); err == nil {
		t.Fatal("已应用迁移被修改应报错")
	}
}

func TestPlanRejectsDuplicateVersion(t *testing.T) {
	ms := []Migration{
		{Version: 1, Name: "a", SQL: "SELECT 1"},
		{Version: 1, Name: "b", SQL: "SELECT 2"},
	}
	if _, err := Plan(nil, ms); err == nil {
		t.Fatal("重复版本应报错")
	}
}

func TestPlanRejectsIncompleteDefinition(t *testing.T) {
	for _, m := range []Migration{
		{Version: 0, Name: "a", SQL: "SELECT 1"},
		{Version: 1, Name: "", SQL: "SELECT 1"},
		{Version: 1, Name: "a", SQL: "  "},
	} {
		if _, err := Plan(nil, []Migration{m}); err == nil {
			t.Fatalf("不完整定义应报错: %+v", m)
		}
	}
}
