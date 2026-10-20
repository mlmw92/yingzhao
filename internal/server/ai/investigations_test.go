package ai

import (
	"os"
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

// 进程在调查中途重启：running 记录不能永远停在「调查中」。
func TestInvestigationStoreMarksInterruptedRunningAsFailed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inv.json")
	if err := os.WriteFile(path, []byte(`[{"id":"inv-x","alertId":"a1","state":"running","startedAt":1}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewInvestigationStore(path, 500, 30*24*time.Hour)
	got, ok := s.ByAlert("a1")
	if !ok {
		t.Fatal("记录应仍在")
	}
	if got.State != "failed" || got.Error == "" {
		t.Fatalf("中断的 running 记录应标为 failed，实际 %+v", got)
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

func TestInvestigationStorePersistsSuggestions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inv.json")
	s := NewInvestigationStore(path, 10, time.Hour)
	_ = s.Add(Investigation{ID: "inv-1", AlertID: "e1", State: "done",
		Suggestions: []Suggestion{{ID: "sug-1", Kind: "svc.restart", Node: "web-01"}}})

	reloaded := NewInvestigationStore(path, 10, time.Hour)
	inv, ok := reloaded.ByAlert("e1")
	if !ok || len(inv.Suggestions) != 1 || inv.Suggestions[0].Kind != "svc.restart" {
		t.Fatalf("建议未落盘: %+v", inv)
	}
}

func TestInvestigationStoreByID(t *testing.T) {
	s := NewInvestigationStore(filepath.Join(t.TempDir(), "inv.json"), 10, time.Hour)
	_ = s.Add(Investigation{ID: "inv-1", AlertID: "e1", State: "done"})
	_ = s.Add(Investigation{ID: "inv-2", AlertID: "e2", State: "done"})

	if inv, ok := s.ByID("inv-2"); !ok || inv.AlertID != "e2" {
		t.Fatalf("ByID = %+v, %v", inv, ok)
	}
	if _, ok := s.ByID("nope"); ok {
		t.Fatal("不存在的 ID 应返回 false")
	}
}
