// Package approval 实现高风险操作的**双人复核**（审批单状态机）。
//
// 设计件：docs/superpowers/specs/2026-10-07-high-risk-approval-design.md
//
// **它防的是什么、不防什么**（写在包最上面，避免后来者误判它的量级）：
//   - 防：误操作、单人误判、**单账号被盗**——一个人点了不算数，要有第二个人同意。
//   - 不防：**中心被攻破**。审批人也是中心里的账号，攻破中心即可伪造两个人。
//     能力全景表 §3.4 后的「操作面安全边界」第 4 条写着"若将来确实要放开任意执行，
//     前置条件是**双人复核 + 不可篡改审计**"——本包只做了前者的状态机，
//     审计仍是可删的（`audit/store.go` 的 Prune），而且本批**没有**放开任意执行
//     （走的仍是参数化动作、参数仍由两端校验）。这条差别不能被含糊掉。
//
// 与 ops 任务的关系：审批单**自带完整动作**（kind + 参数 + 节点），批准后据此创建 ops 任务，
// 因此**不改** ops 的状态机——改它要同时动 Take/prune/OpsStateTerminal/FileRefInUse 四处，
// 漏一处就是"某个状态不落盘"。两件事分开，各自都能被完整测。
package approval

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"
)

// 状态取值。终态 = 除 pending 之外的全部（超时也会落到 expired）。
const (
	// StatePending 待审批：动作**尚未下发**，此时没有任何东西会碰目标机器。
	StatePending = "pending"
	// StateApproved 已批准（瞬时态：批准后立即执行，成功则 executed、失败则 failed）。
	StateApproved = "approved"
	// StateExecuted 审批通过且动作已创建为 ops 任务。
	StateExecuted = "executed"
	// StateFailed 审批通过但动作创建失败（例如节点已离线、参数不再合法）。
	StateFailed = "failed"
	// StateRejected 已驳回（附理由）。
	StateRejected = "rejected"
	// StateCancelled 申请人自己撤回。
	StateCancelled = "cancelled"
	// StateExpired 超时失效——**动作不会执行**，需重新申请。
	StateExpired = "expired"
)

// 终态判定。prune 只淘汰终态，界面也按它区分"还要不要管"。
func terminal(state string) bool {
	return state == StateExecuted || state == StateFailed || state == StateRejected ||
		state == StateCancelled || state == StateExpired
}

// Terminal 供 API 层判断是否终态（导出而不是让它自己列一遍状态）。
func Terminal(state string) bool { return terminal(state) }

// 审批单相关的错误。刻意分成可区分的几种，让 API 层给出不同状态码与**可操作的**提示：
//   - ErrSelfApproval：申请人不能批自己（双人复核的全部意义所在）；
//   - ErrNotPending：这条已经不是待审批状态了（已被别人批过/驳回/撤回/超时）；
//   - ErrExpired：已超时，**动作不会执行**，要重新申请；
//   - ErrNotRequester：只有申请人本人能撤回。
var (
	// ErrNotFound 审批单不存在。
	ErrNotFound = errors.New("审批单不存在")
	// ErrSelfApproval 申请人不能批准自己提交的审批单。
	ErrSelfApproval = errors.New("不能批准自己提交的申请：双人复核要求由另一个人批准")
	// ErrNotPending 审批单不在待审批状态。
	ErrNotPending = errors.New("审批单不在待审批状态")
	// ErrExpired 审批单已超时失效。
	ErrExpired = errors.New("审批单已超时失效（动作未执行，需重新申请）")
	// ErrNotRequester 只有申请人本人可以撤回。
	ErrNotRequester = errors.New("只有申请人本人可以撤回该申请")
)

const (
	// defaultStoreFile 审批单持久化文件名。
	//
	// 用文件而不是台账库：条数少（上限 500）、要能人工查看、且**丢一条只是"没批成"**
	// （动作不会执行，是 fail-safe 的方向），与 ops 任务"丢了会让人以为没执行过"不同。
	defaultStoreFile = "approvals.json"
	// requestTTL 审批单从创建到超时失效的时长。
	//
	// 30 分钟：与 ops 任务的 TTL 同口径（`ops/store.go` 的 taskTTL）。一个三天前的批准
	// 不该还能把机器重启——那正是"超时失效"要挡的场景。
	requestTTL = 30 * time.Minute
	// maxRequests 保留的审批单条数上限；超出时只淘汰**终态**的（待审批的永不淘汰：
	// 淘汰一条待审批的单子，等于让申请人提交的动作凭空消失）。
	maxRequests = 500
)

// Request 是一条审批单。
type Request struct {
	ID string `json:"id"`
	// Kind 是动作标识（`ops:svc.restart` 这类带前缀的形态，与配置里的 requireFor 对齐）。
	Kind string `json:"kind"`
	// OpsKind 是真正的 ops 动作名（Kind 去前缀后的部分）；非 ops 入口为空。
	OpsKind string `json:"opsKind,omitempty"`
	// Node 是目标节点（本机自身动作/升级类入口为空；批量下发时为清单里的第一个，仅供展示）。
	Node string `json:"node,omitempty"`
	// Nodes 是批量下发的完整目标清单（单条下发时为空，用 Node 即可）。
	//
	// 批量必须能进审批单：否则"批量下发"就是一条**绕过审批**的旁路——
	// 单条要审批、一次发 200 台反而不需要，那这条护栏形同虚设。
	Nodes []string `json:"nodes,omitempty"`
	// Params 是动作参数（已按目录规格归一化）。
	Params map[string]string `json:"params,omitempty"`
	// Extra 是入口特有的附加信息（如处置动作名、升级目标版本），仅用于展示与审计。
	Extra map[string]string `json:"extra,omitempty"`
	// Title 是给人看的一句话（"重启服务 nginx.service @ web-01"），避免界面各处自己拼。
	Title string `json:"title"`

	State     string `json:"state"`
	Requester string `json:"requester"`
	// RequesterIP 提交来源 IP（审计用）。
	RequesterIP string `json:"requesterIP,omitempty"`
	// Reason 是申请人填写的理由（可选，但强烈建议——回看历史时"为什么"往往比"谁"更重要）。
	Reason string `json:"reason,omitempty"`

	CreatedAt int64 `json:"createdAt"`
	ExpireAt  int64 `json:"expireAt"`

	// DecidedBy / DecidedAt / Decision 是审批结论（批准或驳回）。
	DecidedBy string `json:"decidedBy,omitempty"`
	DecidedAt int64  `json:"decidedAt,omitempty"`
	// Decision 是审批人写的说明（驳回时必填，批准时可选）。
	Decision string `json:"decision,omitempty"`

	// ExecutedAt / TaskID / Message 是执行结果（批准后自动执行）。
	ExecutedAt int64  `json:"executedAt,omitempty"`
	TaskID     string `json:"taskId,omitempty"`
	Message    string `json:"message,omitempty"`
}

// Expired 判断在给定时刻是否已超时。
func (r *Request) Expired(now int64) bool { return r.ExpireAt > 0 && now >= r.ExpireAt }

// Targets 返回本次动作涉及的全部节点（批量看 Nodes、单条看 Node）。
//
// 统一成一个入口是为了让**资源范围校验只写一遍**：漏掉批量那一支，就等于
// "单条受范围约束、批量不受"，而批量恰恰是影响面更大的那个。
// 返回空切片表示"系统级动作"（如升级），调用方据此要求全局范围。
func (r *Request) Targets() []string {
	if len(r.Nodes) > 0 {
		return r.Nodes
	}
	if r.Node != "" {
		return []string{r.Node}
	}
	return nil
}

// Store 持久化并管理审批单（JSON 文件 + 内存索引）。
//
// 为什么不做 ops 那样的"入库模式"：审批单条数上限 500、字段全是给人看的文本，
// 且**丢一条是 fail-safe 的**（动作不会执行，而不是静默执行了）。为它再加一张表与
// 一套 schema 迁移，收益不抵维护面。这条偏离在设计件 §8 有记录。
type Store struct {
	mu    sync.RWMutex
	reqs  map[string]*Request
	path  string
	seq   int64
	now   func() int64
	pmu   sync.Mutex
	limit int
}

// NewStore 创建审批单存储并加载既有数据（file 为空时用默认文件名）。
func NewStore(file string) *Store {
	if file == "" {
		file = defaultStoreFile
	}
	s := &Store{
		reqs:  map[string]*Request{},
		path:  file,
		now:   func() int64 { return time.Now().UnixMilli() },
		limit: maxRequests,
	}
	s.load()
	return s
}

// SetNow 注入时钟（测试用）。
func (s *Store) SetNow(f func() int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = f
}

// SetLimit 调整条数上限（测试用；生产走默认值）。
func (s *Store) SetLimit(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n > 0 {
		s.limit = n
	}
}

func (s *Store) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("读取审批单存储失败", "err", err, "path", s.path)
		}
		return
	}
	var snap struct {
		Requests []*Request `json:"requests"`
		Seq      int64      `json:"seq"`
	}
	if err := json.Unmarshal(data, &snap); err != nil {
		slog.Warn("审批单存储解析失败（按空库继续）", "err", err, "path", s.path)
		return
	}
	s.mu.Lock()
	for _, r := range snap.Requests {
		if r != nil && r.ID != "" {
			s.reqs[r.ID] = r
		}
	}
	s.seq = snap.Seq
	s.mu.Unlock()
	slog.Info("已加载审批单存储", "requests", len(snap.Requests))
}

func (s *Store) save() {
	// 落盘串行化：状态流转会在并发请求下调用 save，不加锁会让 tmp 文件互相覆盖。
	s.pmu.Lock()
	defer s.pmu.Unlock()

	s.mu.RLock()
	snap := struct {
		Requests []*Request `json:"requests"`
		Seq      int64      `json:"seq"`
	}{Requests: make([]*Request, 0, len(s.reqs)), Seq: s.seq}
	for _, r := range s.reqs {
		snap.Requests = append(snap.Requests, r)
	}
	s.mu.RUnlock()
	// 稳定顺序：文件要能被人 diff 与人工查看。
	sort.Slice(snap.Requests, func(i, j int) bool {
		if snap.Requests[i].CreatedAt != snap.Requests[j].CreatedAt {
			return snap.Requests[i].CreatedAt < snap.Requests[j].CreatedAt
		}
		return snap.Requests[i].ID < snap.Requests[j].ID
	})

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		slog.Warn("审批单序列化失败", "err", err)
		return
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		slog.Warn("审批单写入失败", "err", err)
		return
	}
	if err := os.Rename(tmp, s.path); err != nil {
		slog.Warn("审批单落盘失败", "err", err)
	}
}

func (s *Store) nextID() string {
	s.seq++
	return "ap-" + strconv.FormatInt(s.seq, 10)
}

// Create 登记一条待审批的申请（state=pending，动作**尚未**下发）。
func (s *Store) Create(r *Request, ttl time.Duration) *Request {
	if ttl <= 0 {
		ttl = requestTTL
	}
	s.mu.Lock()
	if r.ID == "" {
		r.ID = s.nextID()
	}
	if r.CreatedAt == 0 {
		r.CreatedAt = s.now()
	}
	r.ExpireAt = r.CreatedAt + ttl.Milliseconds()
	r.State = StatePending
	s.reqs[r.ID] = r
	s.mu.Unlock()
	s.prune()
	s.save()
	return r
}

// Get 取一条审批单。
func (s *Store) Get(id string) (*Request, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.reqs[id]
	return r, ok
}

// Filter 是列表过滤条件（资源范围由 API 层另外施加，本包不碰节点范围）。
type Filter struct {
	// State 非空时只返回该状态。
	State string
	// Requester 非空时只返回该申请人提交的。
	Requester string
	// ExcludeRequester 非空时**排除**该申请人提交的（"待我审批"要排除自己提交的：
	// 自己不能批自己的，让它出现在待办里只会白点一次）。
	ExcludeRequester string
	// Limit 上限（<=0 取默认 200）。
	Limit int
}

// List 按过滤条件返回审批单，**新的在前**。
func (s *Store) List(f Filter) []*Request {
	s.mu.RLock()
	out := make([]*Request, 0, len(s.reqs))
	now := s.now()
	for _, r := range s.reqs {
		// 已超时但仍显示为"待审批"会让人白点一次，因此读取时就把超时的呈现为 expired
		// （落盘的那次由 ExpireOverdue 统一做，这里只影响本次返回的副本）。
		//
		// **顺序要紧**：呈现必须在**状态过滤之前**。第一版写在过滤之后，
		// 于是 `State=expired` 查不到这些单子（它们落盘的状态还是 pending）——
		// "界面上看是过期、按过期筛选却查不到"这种自相矛盾，用例当场抓到了。
		cp := *r
		if cp.State == StatePending && cp.Expired(now) {
			cp.State = StateExpired
		}
		if f.State != "" && cp.State != f.State {
			continue
		}
		if f.Requester != "" && cp.Requester != f.Requester {
			continue
		}
		if f.ExcludeRequester != "" && cp.Requester == f.ExcludeRequester {
			continue
		}
		out = append(out, &cp)
	}
	s.mu.RUnlock()

	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt > out[j].CreatedAt
		}
		return out[i].ID > out[j].ID
	})
	limit := f.Limit
	if limit <= 0 {
		limit = 200
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Count 统计满足条件的条数（用于界面上"待我审批 N"这类提示）。
func (s *Store) Count(f Filter) int {
	return len(s.List(f))
}

// Decide 给出审批结论。approve=true 为批准、false 为驳回，note 是审批人写的说明。
//
// **这里是"不可自批"的唯一判定点**——刻意放在存储层而不是 API 层：任何调用方都绕不过它。
// 三层检查的顺序也有讲究：
//  1. 存在性 → 2. 状态必须是 pending（已被别人批过的不再受理）→ 3. **不能是自己提交的**。
//     先判状态再判自批：一条已被别人批准过的单子，就算申请人再来点一次，也该得到
//     "不在待审批状态"而不是"不能批准自己"——后者会让人以为"如果换个人点就行了"。
func (s *Store) Decide(id, approver string, approve bool, note string) (*Request, error) {
	s.mu.Lock()
	r, ok := s.reqs[id]
	if !ok {
		s.mu.Unlock()
		return nil, ErrNotFound
	}
	if r.State != StatePending {
		cur := r.State
		s.mu.Unlock()
		return nil, fmt.Errorf("%w（当前状态 %s）", ErrNotPending, cur)
	}
	now := s.now()
	if r.Expired(now) {
		r.State = StateExpired
		r.Message = "超时未审批，动作未执行"
		s.mu.Unlock()
		s.save()
		return nil, ErrExpired
	}
	if approver == r.Requester {
		s.mu.Unlock()
		return nil, ErrSelfApproval
	}
	if approve {
		r.State = StateApproved
	} else {
		r.State = StateRejected
	}
	r.DecidedBy = approver
	r.DecidedAt = now
	r.Decision = note
	s.mu.Unlock()
	s.save()
	return r, nil
}

// MarkExecuted 记录批准后的执行结果（成功则带上 ops 任务号）。
//
// 只有 approved 能落这里：终态不可被改写（与 ops 的 ApplyResult 同一取向——
// 一条已驳回的单子不该因为一次迟到的执行回执变成"已执行"）。
func (s *Store) MarkExecuted(id, taskID string, execErr error) (*Request, error) {
	s.mu.Lock()
	r, ok := s.reqs[id]
	if !ok {
		s.mu.Unlock()
		return nil, ErrNotFound
	}
	if r.State != StateApproved {
		cur := r.State
		s.mu.Unlock()
		return nil, fmt.Errorf("%w（当前状态 %s）", ErrNotPending, cur)
	}
	r.ExecutedAt = s.now()
	if execErr != nil {
		r.State = StateFailed
		r.Message = execErr.Error()
	} else {
		r.State = StateExecuted
		r.TaskID = taskID
		r.Message = "已按批准内容下发（任务 " + taskID + "）"
	}
	s.mu.Unlock()
	s.save()
	return r, nil
}

// Cancel 由**申请人本人**撤回一条待审批的申请。
//
// 只有 pending 能撤：一旦批准，动作已经执行了，"撤回"就变成了说谎。
func (s *Store) Cancel(id, actor string) (*Request, error) {
	s.mu.Lock()
	r, ok := s.reqs[id]
	if !ok {
		s.mu.Unlock()
		return nil, ErrNotFound
	}
	if r.Requester != actor {
		s.mu.Unlock()
		return nil, ErrNotRequester
	}
	if r.State != StatePending {
		cur := r.State
		s.mu.Unlock()
		return nil, fmt.Errorf("%w（当前状态 %s）", ErrNotPending, cur)
	}
	r.State = StateCancelled
	r.DecidedBy = actor
	r.DecidedAt = s.now()
	r.Decision = "申请人撤回"
	s.mu.Unlock()
	s.save()
	return r, nil
}

// ExpireOverdue 把超时未审批的置为 expired，返回本次新过期的条数。
//
// 惰性 + 主动两条路都要有：读取时（List）已经按超时呈现，但**落盘的那次**必须有人做，
// 否则重启后一条三小时前的 pending 会重新变成"待审批"。由 API 层在每次列取/提交前调用。
func (s *Store) ExpireOverdue() int {
	now := s.now()
	s.mu.Lock()
	n := 0
	for _, r := range s.reqs {
		if r.State == StatePending && r.Expired(now) {
			r.State = StateExpired
			r.Message = "超时未审批，动作未执行"
			n++
		}
	}
	s.mu.Unlock()
	if n > 0 {
		s.save()
	}
	return n
}

// prune 淘汰最老的**终态**审批单；待审批的永不淘汰。
//
// 淘汰一条待审批的单子等于让申请人提交的动作凭空消失——而申请人会以为它在排队。
// 因此上限只管"历史记录"这一侧。
func (s *Store) prune() {
	s.mu.Lock()
	if len(s.reqs) <= s.limit {
		s.mu.Unlock()
		return
	}
	term := make([]*Request, 0, len(s.reqs))
	for _, r := range s.reqs {
		if terminal(r.State) {
			term = append(term, r)
		}
	}
	sort.Slice(term, func(i, j int) bool {
		if term[i].CreatedAt != term[j].CreatedAt {
			return term[i].CreatedAt < term[j].CreatedAt
		}
		return term[i].ID < term[j].ID
	})
	for _, r := range term {
		if len(s.reqs) <= s.limit {
			break
		}
		delete(s.reqs, r.ID)
	}
	s.mu.Unlock()
}
