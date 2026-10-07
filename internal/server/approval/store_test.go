package approval

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// 固定时钟：超时相关用例必须能精确推进时间，靠 time.Sleep 会让测试慢且不稳。
func newTestStore(t *testing.T) (*Store, *int64) {
	t.Helper()
	s := NewStore(filepath.Join(t.TempDir(), "approvals.json"))
	clock := new(int64)
	*clock = 1_700_000_000_000
	s.SetNow(func() int64 { return *clock })
	return s, clock
}

func sampleReq() *Request {
	return &Request{
		Kind: "ops:svc.restart", OpsKind: "svc.restart", Node: "web-01",
		Params: map[string]string{"unit": "nginx.service"},
		Title:  "重启服务 nginx.service @ web-01",
		Requester: "alice", RequesterIP: "10.0.0.1", Reason: "内存泄漏",
	}
}

func TestCreate_StartsPendingWithTTL(t *testing.T) {
	s, _ := newTestStore(t)
	r := s.Create(sampleReq(), 30*time.Minute)
	if r.State != StatePending {
		t.Fatalf("新建审批单应为 pending，实际 %q", r.State)
	}
	if r.ID == "" || r.CreatedAt == 0 || r.ExpireAt <= r.CreatedAt {
		t.Fatalf("ID/时间戳未填：%+v", r)
	}
	if got := r.ExpireAt - r.CreatedAt; got != (30 * time.Minute).Milliseconds() {
		t.Fatalf("有效期应为 30 分钟，实际 %d ms", got)
	}
	// 动作参数必须原样带上：批准后要据此执行，丢了参数等于批了一个空动作
	if r.Params["unit"] != "nginx.service" || r.OpsKind != "svc.restart" {
		t.Fatalf("审批单应自带完整动作，实际 %+v", r)
	}
}

// **决定性判据（验证矩阵用例 7-07）**：申请人不可批准本人任务。
func TestDecide_SelfApprovalRejected(t *testing.T) {
	s, _ := newTestStore(t)
	r := s.Create(sampleReq(), time.Hour)

	_, err := s.Decide(r.ID, "alice", true, "")
	if !errors.Is(err, ErrSelfApproval) {
		t.Fatalf("申请人自批应被拒（ErrSelfApproval），实际 %v", err)
	}
	// 关键：被拒之后状态必须**原封不动**，否则"自己批自己被拒"却把单子变成了别的状态
	got, _ := s.Get(r.ID)
	if got.State != StatePending {
		t.Fatalf("自批被拒后状态应仍为 pending，实际 %q", got.State)
	}
	if got.DecidedBy != "" {
		t.Fatalf("自批被拒后不该留下审批人，实际 %q", got.DecidedBy)
	}
}

func TestDecide_OtherUserApprovesThenExecutes(t *testing.T) {
	s, _ := newTestStore(t)
	r := s.Create(sampleReq(), time.Hour)

	decided, err := s.Decide(r.ID, "bob", true, "确认过工单")
	if err != nil {
		t.Fatalf("他人批准应成功，实际 %v", err)
	}
	if decided.State != StateApproved || decided.DecidedBy != "bob" || decided.DecidedAt == 0 {
		t.Fatalf("批准后应记下审批人与时间，实际 %+v", decided)
	}
	// 批准是瞬时态：紧接着必须能被落成"已执行"，否则界面上会永远显示"已批准"却不知道有没有下发
	done, err := s.MarkExecuted(r.ID, "ops-42", nil)
	if err != nil {
		t.Fatalf("记录执行结果应成功，实际 %v", err)
	}
	if done.State != StateExecuted || done.TaskID != "ops-42" {
		t.Fatalf("应落成 executed 且带任务号，实际 %+v", done)
	}
}

func TestDecide_RejectKeepsNoTask(t *testing.T) {
	s, _ := newTestStore(t)
	r := s.Create(sampleReq(), time.Hour)
	decided, err := s.Decide(r.ID, "bob", false, "变更窗口外")
	if err != nil {
		t.Fatalf("驳回应成功，实际 %v", err)
	}
	if decided.State != StateRejected || decided.Decision != "变更窗口外" {
		t.Fatalf("驳回应记下理由，实际 %+v", decided)
	}
	// 驳回的单子绝不能被"执行"——这是"拒了却跑了"的那类事故的防线
	if _, err := s.MarkExecuted(r.ID, "ops-1", nil); !errors.Is(err, ErrNotPending) {
		t.Fatalf("已驳回的单子不该能落成已执行，实际 %v", err)
	}
}

// **决定性判据（验证矩阵用例 7-07）**：超时自动失效。
func TestDecide_ExpiredRejectedAndNotExecuted(t *testing.T) {
	s, clock := newTestStore(t)
	r := s.Create(sampleReq(), 30*time.Minute)

	*clock += (30 * time.Minute).Milliseconds() // 正好到点即算超时（>=）
	if _, err := s.Decide(r.ID, "bob", true, ""); !errors.Is(err, ErrExpired) {
		t.Fatalf("超时后批准应被拒（ErrExpired），实际 %v", err)
	}
	got, _ := s.Get(r.ID)
	if got.State != StateExpired {
		t.Fatalf("超时应落成 expired，实际 %q", got.State)
	}
	if got.TaskID != "" {
		t.Fatalf("超时的单子不该有任务号（动作绝不能执行），实际 %q", got.TaskID)
	}
}

func TestExpireOverdue_OnlyTouchesPending(t *testing.T) {
	s, clock := newTestStore(t)
	a := s.Create(sampleReq(), 10*time.Minute)
	b := s.Create(sampleReq(), 10*time.Minute)
	if _, err := s.Decide(b.ID, "bob", false, "不同意"); err != nil {
		t.Fatalf("驳回应成功: %v", err)
	}

	*clock += (20 * time.Minute).Milliseconds()
	if n := s.ExpireOverdue(); n != 1 {
		t.Fatalf("应只有 1 条新过期，实际 %d", n)
	}
	ga, _ := s.Get(a.ID)
	gb, _ := s.Get(b.ID)
	if ga.State != StateExpired {
		t.Fatalf("超时的待审批应变 expired，实际 %q", ga.State)
	}
	// 终态不能被"超时"覆盖：一条已驳回的单子若变成 expired，就再也说不清是被谁拒的
	if gb.State != StateRejected {
		t.Fatalf("终态不该被超时改写，实际 %q", gb.State)
	}
	// 幂等：再调一次不该再数一遍
	if n := s.ExpireOverdue(); n != 0 {
		t.Fatalf("重复调用应为 0，实际 %d", n)
	}
}

func TestCancel_OnlyRequesterAndOnlyPending(t *testing.T) {
	s, _ := newTestStore(t)
	r := s.Create(sampleReq(), time.Hour)

	if _, err := s.Cancel(r.ID, "bob"); !errors.Is(err, ErrNotRequester) {
		t.Fatalf("非申请人撤回应被拒，实际 %v", err)
	}
	if _, err := s.Cancel(r.ID, "alice"); err != nil {
		t.Fatalf("申请人撤回应成功，实际 %v", err)
	}
	// 已撤回的单子不能再被批准：否则"撤回"就成了摆设
	if _, err := s.Decide(r.ID, "bob", true, ""); !errors.Is(err, ErrNotPending) {
		t.Fatalf("已撤回的单子不该能被批准，实际 %v", err)
	}

	r2 := s.Create(sampleReq(), time.Hour)
	if _, err := s.Decide(r2.ID, "bob", true, ""); err != nil {
		t.Fatalf("批准应成功: %v", err)
	}
	if _, err := s.Cancel(r2.ID, "alice"); !errors.Is(err, ErrNotPending) {
		t.Fatalf("已批准的单子不该能撤回（动作已在执行），实际 %v", err)
	}
}

func TestList_NewestFirstAndExcludeOwn(t *testing.T) {
	s, clock := newTestStore(t)
	a := s.Create(sampleReq(), time.Hour) // alice
	*clock++
	b := s.Create(&Request{Kind: EntryDefense, Node: "web-02", Title: "封禁 1.2.3.4", Requester: "bob"}, time.Hour)
	*clock++
	c := s.Create(sampleReq(), time.Hour) // alice
	_ = c

	all := s.List(Filter{})
	if len(all) != 3 || all[0].ID != c.ID || all[2].ID != a.ID {
		t.Fatalf("应按创建时间新的在前，实际 %v", ids(all))
	}
	// "待我审批"要排除自己提交的：自己不能批自己，列出来只会白点一次
	mine := s.List(Filter{ExcludeRequester: "alice"})
	if len(mine) != 1 || mine[0].ID != b.ID {
		t.Fatalf("排除自己提交后应只剩 bob 那条，实际 %v", ids(mine))
	}
	if got := s.List(Filter{Requester: "alice"}); len(got) != 2 {
		t.Fatalf("按申请人过滤应得 2 条，实际 %v", ids(got))
	}
}

// 读取时就把超时的呈现为 expired：界面上还显示"待审批"会让人白点一次。
func TestList_PresentsExpiredAsExpired(t *testing.T) {
	s, clock := newTestStore(t)
	r := s.Create(sampleReq(), 5*time.Minute)
	*clock += (6 * time.Minute).Milliseconds()

	got := s.List(Filter{State: StateExpired})
	if len(got) != 1 || got[0].ID != r.ID {
		t.Fatalf("超时的待审批应在列表里呈现为 expired，实际 %v", ids(got))
	}
	if n := len(s.List(Filter{State: StatePending})); n != 0 {
		t.Fatalf("超时的不该再出现在 pending 里，实际 %d 条", n)
	}
}

// **决定性判据**：上限只淘汰终态——淘汰一条待审批的单子等于让申请人的动作凭空消失。
func TestPrune_NeverDropsPending(t *testing.T) {
	s, clock := newTestStore(t)
	s.SetLimit(3)

	// 3 条待审批 + 3 条终态，上限 3
	var pending []string
	for i := 0; i < 3; i++ {
		pending = append(pending, s.Create(sampleReq(), time.Hour).ID)
		*clock++
	}
	for i := 0; i < 3; i++ {
		r := s.Create(sampleReq(), time.Hour)
		if _, err := s.Decide(r.ID, "bob", false, "不同意"); err != nil {
			t.Fatalf("驳回应成功: %v", err)
		}
		*clock++
	}

	for _, id := range pending {
		if _, ok := s.Get(id); !ok {
			t.Fatalf("待审批的 %s 被淘汰了——申请人的动作会凭空消失", id)
		}
	}
	if n := len(s.List(Filter{})); n > 3+1 {
		t.Fatalf("终态应被淘汰到接近上限，实际仍有 %d 条", n)
	}
}

// 重启后待审批的单子必须还在：否则"提交了、等审批、服务重启"就变成一张空列表。
func TestStore_ReloadKeepsPending(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "approvals.json")
	s1 := NewStore(path)
	r := s1.Create(sampleReq(), time.Hour)

	s2 := NewStore(path)
	got, ok := s2.Get(r.ID)
	if !ok {
		t.Fatal("重启后审批单应仍在")
	}
	if got.State != StatePending || got.Title != r.Title || got.Params["unit"] != "nginx.service" {
		t.Fatalf("重启后内容应完整，实际 %+v", got)
	}
	// 序号也要续上：否则新单会拿到已用过的 ID，把旧单覆盖掉
	r2 := s2.Create(sampleReq(), time.Hour)
	if r2.ID == r.ID {
		t.Fatalf("重启后新单不该复用旧 ID %s", r.ID)
	}
}

func TestDecide_NotFound(t *testing.T) {
	s, _ := newTestStore(t)
	if _, err := s.Decide("ap-404", "bob", true, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在的单子应报 ErrNotFound，实际 %v", err)
	}
}

func ids(rs []*Request) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.ID)
	}
	return out
}
