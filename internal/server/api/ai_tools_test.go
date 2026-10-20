package api

import (
	"context"
	"strings"
	"testing"

	"github.com/nebula/monitor/internal/model"
	"github.com/nebula/monitor/internal/server/ai"
	"github.com/nebula/monitor/internal/server/logstore"
	"github.com/nebula/monitor/internal/server/ops"
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

func TestProposeActionRecordsValidatedSuggestion(t *testing.T) {
	a := scopeTestAPI(t)
	var got []ai.Suggestion
	tb := a.newToolbox(ai.ToolScope{Node: "web-01", Propose: func(s ai.Suggestion) error {
		got = append(got, s)
		return nil
	}})
	tool := toolByName(t, tb, "propose_action")

	out, err := tool.Handler(context.Background(), map[string]any{
		"kind":   ops.KindSvcRestart,
		"params": map[string]any{"unit": "nginx.service"},
		"reason": "nginx 单元处于 failed 状态",
	})
	if err != nil {
		t.Fatalf("propose_action: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("应记录 1 条建议，实际 %d", len(got))
	}
	s := got[0]
	if s.Node != "web-01" || s.Kind != ops.KindSvcRestart || s.Params["unit"] != "nginx.service" {
		t.Fatalf("建议内容不正确: %+v", s)
	}
	if s.ID == "" || s.Title == "" {
		t.Fatalf("建议缺少 ID 或标题: %+v", s)
	}
	if !strings.Contains(out, "不会自动执行") {
		t.Fatalf("工具回执必须告诉模型建议不会自动执行，实际 %q", out)
	}
}

func TestProposeActionRejects(t *testing.T) {
	a := scopeTestAPI(t)
	cases := []struct {
		name string
		args map[string]any
	}{
		{"未知动作", map[string]any{"kind": "nope"}},
		{"只读动作", map[string]any{"kind": ops.KindNodeDiagnostics}},
		{"文件分发", map[string]any{"kind": ops.KindFilePush,
			"params": map[string]any{"path": "/tmp/x", "fileId": "obf-1"}}},
		{"缺必填参数", map[string]any{"kind": ops.KindSvcRestart, "params": map[string]any{}}},
		{"参数格式错", map[string]any{"kind": ops.KindSvcRestart, "params": map[string]any{"unit": "nginx"}}},
		{"参数不是字符串", map[string]any{"kind": ops.KindSvcRestart, "params": map[string]any{"unit": 1}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got []ai.Suggestion
			tb := a.newToolbox(ai.ToolScope{Node: "web-01", Propose: func(s ai.Suggestion) error {
				got = append(got, s)
				return nil
			}})
			tool := toolByName(t, tb, "propose_action")
			if _, err := tool.Handler(context.Background(), c.args); err == nil {
				t.Fatal("应拒绝该建议")
			}
			if len(got) != 0 {
				t.Fatalf("被拒绝的建议不得记录: %+v", got)
			}
		})
	}
}

func TestProposeActionCapsAndDedupes(t *testing.T) {
	a := scopeTestAPI(t)
	var got []ai.Suggestion
	tb := a.newToolbox(ai.ToolScope{Node: "web-01", Propose: func(s ai.Suggestion) error {
		got = append(got, s)
		return nil
	}})
	tool := toolByName(t, tb, "propose_action")
	call := func(unit string) error {
		_, err := tool.Handler(context.Background(), map[string]any{
			"kind": ops.KindSvcRestart, "params": map[string]any{"unit": unit},
		})
		return err
	}

	if err := call("a.service"); err != nil {
		t.Fatal(err)
	}
	if err := call("a.service"); err == nil {
		t.Fatal("完全相同的建议应被去重")
	}
	for _, u := range []string{"b.service", "c.service"} {
		if err := call(u); err != nil {
			t.Fatal(err)
		}
	}
	if err := call("d.service"); err == nil {
		t.Fatal("第 4 条建议应被上限拒绝")
	}
	if len(got) != 3 {
		t.Fatalf("应记录 3 条建议，实际 %d", len(got))
	}
}

func TestProposeActionAbsentWithoutCallback(t *testing.T) {
	a := scopeTestAPI(t)
	tb := a.newToolbox(ai.ToolScope{Node: "web-01"})
	for _, tool := range tb.Tools() {
		if tool.Name == "propose_action" {
			t.Fatal("没有 Propose 回调时不应暴露 propose_action")
		}
	}
}
