package api

import (
	"context"
	"strings"
	"testing"

	"github.com/nebula/monitor/internal/model"
	"github.com/nebula/monitor/internal/server/ai"
	"github.com/nebula/monitor/internal/server/logstore"
)

type fakeLogStore struct{}

func (f *fakeLogStore) Append(model.LogBatch) (int, int, string, error) { return 0, 0, "", nil }
func (f *fakeLogStore) Query(model.LogQuery, logstore.Cursor) (model.LogQueryResult, error) {
	return model.LogQueryResult{Lines: []model.LogHit{
		{Source: "app", Node: "web-01", Text: "SECRET password=hunter2"},
	}}, nil
}
func (f *fakeLogStore) Backend() string                 { return "fake" }
func (f *fakeLogStore) Sources() []string               { return []string{"app"} }
func (f *fakeLogStore) FieldNames(string) []string      { return nil }
func (f *fakeLogStore) Capability() logstore.Capability { return logstore.Capability{} }

func toolByName(t *testing.T, tb interface{ Tools() []ai.Tool }, name string) ai.Tool {
	t.Helper()
	for _, tool := range tb.Tools() {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("缺少工具 %s", name)
	return ai.Tool{}
}

func TestToolScopeRejectsOtherNode(t *testing.T) {
	a := scopeTestAPI(t)
	a.alerts = &alertStoreStub{}
	tb := a.newToolbox(ai.ToolScope{Node: "web-01"})

	tool := toolByName(t, tb, "asset_lookup")
	out, err := tool.Handler(context.Background(), map[string]any{"node": "db-01"})
	if err == nil {
		t.Fatalf("越界节点必须报错，实际返回 %q", out)
	}
}

func TestLogStatsNeverReturnsBody(t *testing.T) {
	a := scopeTestAPI(t)
	a.logs = &fakeLogStore{}
	tb := a.newToolbox(ai.ToolScope{Node: "web-01"})

	tool := toolByName(t, tb, "log_stats")
	out, err := tool.Handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("log_stats: %v", err)
	}
	if strings.Contains(out, "hunter2") || strings.Contains(out, "SECRET") {
		t.Fatalf("log_stats 不得返回日志正文: %q", out)
	}
	if !strings.Contains(out, "app") {
		t.Fatalf("应包含来源名: %q", out)
	}
}

func TestSevenToolsRegistered(t *testing.T) {
	a := scopeTestAPI(t)
	tb := a.newToolbox(ai.ToolScope{Node: "web-01"})
	want := []string{"list_alerts", "query_metric", "node_detail", "asset_lookup", "inspect_findings", "dialtest_status", "log_stats"}
	for _, name := range want {
		toolByName(t, tb, name)
	}
}
