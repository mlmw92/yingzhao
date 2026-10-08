package audit

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// chainStore 建一个已启用链的库模式 store，并写入 n 条审计（时间递增）。
func chainStore(t *testing.T, n int) (*Store, *ChainKey) {
	t.Helper()
	store := New(filepath.Join(t.TempDir(), "audit_events.json"))
	if err := store.UseSQLite(openTestDB(t).DB()); err != nil {
		t.Fatalf("切入库模式失败: %v", err)
	}
	key := testChainKey(t)
	store.SetChainKey(key)
	for i := 0; i < n; i++ {
		if err := store.Record(Event{
			Time: time.UnixMilli(1000 + int64(i)*1000), User: "ops", Method: "PUT",
			Path: "/api/v1/assets/1", Status: 200, Succeeded: true,
			Category: "asset", Action: "update", Detail: "name: a -> b",
		}); err != nil {
			t.Fatalf("写入第 %d 条失败: %v", i+1, err)
		}
	}
	return store, key
}

// **决定性**：改一行必须报出该行的 hash 不匹配，且**改回之后要恢复通过**
// （只验"改坏能报"不够——那只证明它总在报错）。
func TestVerify_DetectsTamperedRowAndRecovers(t *testing.T) {
	store, _ := chainStore(t, 3)
	if rep := store.Verify(); !rep.OK {
		t.Fatalf("初始应通过，实际 %+v", rep)
	}

	var original string
	if err := store.db.QueryRow(`SELECT detail FROM audit_events WHERE seq = 2`).Scan(&original); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE audit_events SET detail = 'name: a -> EVIL' WHERE seq = 2`); err != nil {
		t.Fatal(err)
	}

	rep := store.Verify()
	if rep.OK {
		t.Fatal("改了一行之后不该报通过")
	}
	if len(rep.Breaks) != 1 || rep.Breaks[0].Seq != 2 || rep.Breaks[0].Reason != ReasonHashMismatch {
		t.Fatalf("应报出第 2 行的 hash-mismatch，实际 %+v", rep.Breaks)
	}
	// 报口径：必须报出覆盖区间与条数，不能只说"失败"
	if rep.VerifiedFrom != 1 || rep.VerifiedTo != 3 || rep.RowsChecked != 3 {
		t.Fatalf("应报出覆盖区间 1..3、3 条，实际 %d..%d/%d",
			rep.VerifiedFrom, rep.VerifiedTo, rep.RowsChecked)
	}

	if _, err := store.db.Exec(`UPDATE audit_events SET detail = ? WHERE seq = 2`, original); err != nil {
		t.Fatal(err)
	}
	if rep := store.Verify(); !rep.OK {
		t.Fatalf("改回之后应恢复通过，实际 %+v", rep.Breaks)
	}
}

// **决定性**：删掉中间一行必须报**无法归因的缺口**——而不是笼统的"失败"，
// 否则看的人不知道该去查什么。
func TestVerify_DetectsDeletedMiddleRow(t *testing.T) {
	store, _ := chainStore(t, 3)
	if _, err := store.db.Exec(`DELETE FROM audit_events WHERE seq = 2`); err != nil {
		t.Fatal(err)
	}
	rep := store.Verify()
	if rep.OK {
		t.Fatal("删了中间一行不该报通过")
	}
	if len(rep.Breaks) != 1 || rep.Breaks[0].Reason != ReasonGap {
		t.Fatalf("应报出 unattributable-gap，实际 %+v", rep.Breaks)
	}
}

// **决定性（负例，最容易被做错）**：保留策略的**正常裁剪不算篡改**。
// 没有这条，校验会把每天例行的清理报成"有人删过审计"——假警报会训练人忽略真警报。
func TestVerify_RetentionPruneIsNotTampering(t *testing.T) {
	store, _ := chainStore(t, 4)
	// 裁掉第 1、2 条（时间 < 2500）
	n, err := store.Prune(2500)
	if err != nil {
		t.Fatalf("裁剪失败: %v", err)
	}
	if n != 2 {
		t.Fatalf("应裁掉 2 条，实际 %d", n)
	}

	rep := store.Verify()
	if !rep.OK {
		t.Fatalf("正常裁剪不该报篡改，实际 %+v", rep.Breaks)
	}
	// 而且必须**报出那段被合法移除的区间**，否则"通过"无从判断
	if len(rep.PrunedSegments) != 1 {
		t.Fatalf("应记录 1 段裁剪区间，实际 %+v", rep.PrunedSegments)
	}
	seg := rep.PrunedSegments[0]
	if seg.FromSeq != 1 || seg.ToSeq != 2 || !seg.MACValid {
		t.Fatalf("裁剪区间应为 [1,2] 且签名有效，实际 %+v", seg)
	}
	// 幸存行的续接：第 3 条的 prev_hash 应等于裁剪记录里的末条签名
	if rep.VerifiedFrom != 3 || rep.LinkUnverifiedRows != 0 {
		t.Fatalf("第 3 条应能续接上裁剪记录，实际 from=%d linkUnverified=%d",
			rep.VerifiedFrom, rep.LinkUnverifiedRows)
	}
}

// 按条数兜底裁剪同样不该被当成篡改。
func TestVerify_PruneRowsIsNotTampering(t *testing.T) {
	store, _ := chainStore(t, 5)
	if _, err := store.PruneRows(3); err != nil {
		t.Fatalf("裁剪失败: %v", err)
	}
	rep := store.Verify()
	if !rep.OK || rep.RowsChecked != 3 {
		t.Fatalf("按条数裁剪后应通过且剩 3 条，实际 ok=%v rows=%d breaks=%+v",
			rep.OK, rep.RowsChecked, rep.Breaks)
	}
}

// **决定性**：换了密钥必须报成"密钥不匹配"，**不能**报成"被篡改"——
// 前者是运维动作（恢复备份、换机器），后者是安全事故，混淆会造成假警报。
func TestVerify_KeyChangeIsNotReportedAsTampering(t *testing.T) {
	store, _ := chainStore(t, 2)
	// 换一把密钥，再写一条（模拟"换了机器/恢复了备份之后继续记录"）
	store.SetChainKey(testChainKey(t))
	if err := store.Record(Event{Time: time.UnixMilli(9000), User: "ops", Succeeded: true}); err != nil {
		t.Fatal(err)
	}

	rep := store.Verify()
	if !rep.OK {
		t.Fatalf("换密钥不该报成被篡改，实际 %+v", rep.Breaks)
	}
	if rep.KeyMismatchRows != 2 {
		t.Fatalf("应有 2 条是别的密钥签的，实际 %d", rep.KeyMismatchRows)
	}
	if rep.Note == "" {
		t.Fatal("应说明有行因换密钥而不可验（不能悄悄略过）")
	}
}

// 伪造一条裁剪记录来遮盖删除：MAC 对不上，必须报出来。
func TestVerify_ForgedPrunedRecordDetected(t *testing.T) {
	store, _ := chainStore(t, 3)
	if _, err := store.db.Exec(`INSERT INTO audit_pruned(from_seq, to_seq, head_hash, rows, pruned_ms, mac)
		VALUES(1, 2, 'deadbeef', 2, 1000, 'forged-mac')`); err != nil {
		t.Fatal(err)
	}
	rep := store.Verify()
	if rep.OK {
		t.Fatal("伪造的裁剪记录不该被接受")
	}
	found := false
	for _, b := range rep.Breaks {
		if b.Reason == ReasonPrunedMAC {
			found = true
		}
	}
	if !found {
		t.Fatalf("应报出 pruned-record-invalid，实际 %+v", rep.Breaks)
	}
}

// 未启用链时**不许说"通过"**——那正是这套机制要避免的失败形态（把"没保护"显示成"安全"）。
func TestVerify_NotEnabledNeverSaysOK(t *testing.T) {
	// ① 库模式但没有密钥
	store := New(filepath.Join(t.TempDir(), "audit_events.json"))
	store.UseSQLite(openTestDB(t).DB())
	if err := store.Record(Event{Time: time.UnixMilli(1), User: "ops"}); err != nil {
		t.Fatal(err)
	}
	rep := store.Verify()
	if rep.Enabled || rep.OK {
		t.Fatalf("无密钥时不该报 enabled/ok，实际 %+v", rep)
	}
	if rep.Note == "" {
		t.Fatal("必须说明「为什么没保护」")
	}

	// ② 降级模式
	fallback := New(filepath.Join(t.TempDir(), "audit_events.json"))
	fallback.SetChainKey(testChainKey(t))
	rep2 := fallback.Verify()
	if rep2.Enabled || rep2.OK || rep2.Mode != "json" {
		t.Fatalf("降级模式不该报 enabled/ok，实际 %+v", rep2)
	}
}

// 链上还没有记录时，不能说"通过"也不能说"失败"——要说清楚"还没有链上记录"。
func TestVerify_EmptyChain(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "audit_events.json"))
	store.UseSQLite(openTestDB(t).DB())
	store.SetChainKey(testChainKey(t))
	rep := store.Verify()
	if !rep.Enabled || !rep.OK || rep.RowsChecked != 0 {
		t.Fatalf("空链应 enabled 且 ok、0 条，实际 %+v", rep)
	}
	if rep.Note == "" {
		t.Fatal("空链应说明情况（否则界面只会显示一个空白的通过）")
	}
}

// captureHandler 把日志收进内存，用来断言锚点真的写出来了。
type captureHandler struct{ msgs []string }

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	var sb strings.Builder
	sb.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		sb.WriteString(" " + a.Key + "=" + a.Value.String())
		return true
	})
	h.msgs = append(h.msgs, sb.String())
	return nil
}
func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler      { return h }

// 锚点必须真的写进日志：库内的链**无法**证明"尾部没被删"（尾部没有后继指认它），
// 日志锚点是"删尾部/清表"唯一能被发现的依据——它没写出来，整条防线就少了最关键的一环。
func TestAnchorOnce_WritesChainHeadToLog(t *testing.T) {
	store, key := chainStore(t, 2)
	h := &captureHandler{}
	old := slog.Default()
	slog.SetDefault(slog.New(h))
	defer slog.SetDefault(old)

	store.AnchorOnce()

	if len(h.msgs) != 1 {
		t.Fatalf("应写 1 条锚点日志，实际 %d 条：%v", len(h.msgs), h.msgs)
	}
	got := h.msgs[0]
	// 消息用 ASCII：它是给脚本与运维 grep 的稳定标记，不该随界面文案调整而变
	if !strings.Contains(got, "audit chain head") {
		t.Fatalf("锚点消息应含稳定标记 audit chain head，实际 %q", got)
	}
	if !strings.Contains(got, "seq=2") {
		t.Fatalf("锚点应带当前链头 seq，实际 %q", got)
	}
	if !strings.Contains(got, key.Fingerprint()) {
		t.Fatalf("锚点应带密钥指纹，实际 %q", got)
	}
}

// 未启用链时不写锚点（写了会让人以为有保护）。
func TestAnchorOnce_SilentWhenChainDisabled(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "audit_events.json"))
	h := &captureHandler{}
	old := slog.Default()
	slog.SetDefault(slog.New(h))
	defer slog.SetDefault(old)

	store.AnchorOnce()
	if len(h.msgs) != 0 {
		t.Fatalf("未启用链时不该写锚点，实际 %v", h.msgs)
	}
}

// 启用链之前写的历史（seq=0）不可验，但要**如实计数**，不能混进"已验证"。
func TestVerify_UnchainedHistoryIsCounted(t *testing.T) {
	store, _ := chainStore(t, 2)
	if _, err := store.db.Exec(`INSERT INTO audit_events(time_ms, user_name, method, path, status, succeeded)
		VALUES(500, 'legacy', 'POST', '/api/v1/x', 200, 1)`); err != nil {
		t.Fatal(err)
	}
	rep := store.Verify()
	if rep.UnchainedRows != 1 {
		t.Fatalf("应有 1 条无链历史，实际 %d", rep.UnchainedRows)
	}
	if rep.RowsChecked != 2 {
		t.Fatalf("已验证条数不该把无链历史算进去，实际 %d", rep.RowsChecked)
	}
	if !rep.OK {
		t.Fatalf("无链历史不该导致失败（它是「启用之前」，不是「被改过」），实际 %+v", rep.Breaks)
	}
}
