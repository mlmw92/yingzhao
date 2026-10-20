package ai

import (
	"path/filepath"
	"testing"
	"time"
)

func TestInvestigationStoreByAlertAndPrune(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inv.json")
	s := NewInvestigationStore(path, 500, 30*24*time.Hour)
	inv := Investigation{ID: "inv-1", AlertID: "a1", Node: "n1", State: "done", Answer: "ok"}
	if err := s.Add(inv); err != nil {
		t.Fatalf("Add: %v", err)
	}
	got, ok := s.ByAlert("a1")
	if !ok || got.Answer != "ok" {
		t.Fatalf("ByAlert = %+v, %v", got, ok)
	}
	s.Update("inv-1", func(i *Investigation) { i.State = "failed"; i.Error = "x" })
	got, _ = s.ByAlert("a1")
	if got.State != "failed" || got.Error != "x" {
		t.Fatalf("Update 未生效: %+v", got)
	}
}

func TestInvestigationStorePrunesOldAndOverflow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inv.json")
	s := NewInvestigationStore(path, 2, time.Hour)
	old := time.Now().Add(-2 * time.Hour).UnixMilli()
	_ = s.Add(Investigation{ID: "old", AlertID: "a0", StartedAt: old})
	_ = s.Add(Investigation{ID: "new1", AlertID: "a1", StartedAt: time.Now().UnixMilli()})
	_ = s.Add(Investigation{ID: "new2", AlertID: "a2", StartedAt: time.Now().UnixMilli()})

	if _, ok := s.ByAlert("a0"); ok {
		t.Fatal("超过保留期的记录应被裁剪")
	}
	if got := s.List(10); len(got) != 2 {
		t.Fatalf("超过上限应只保留最近 2 条，实际 %d", len(got))
	}
}
