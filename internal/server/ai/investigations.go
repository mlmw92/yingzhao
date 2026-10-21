package ai

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/mlmw92/yingzhao/internal/server/configdoc"
)

// Suggestion 是模型提出的一条处置建议。它只是一个记录，不代表任何动作已被提交。
type Suggestion struct {
	ID     string            `json:"id"`
	Kind   string            `json:"kind"`
	Title  string            `json:"title"`
	Params map[string]string `json:"params,omitempty"`
	Node   string            `json:"node"`
	Reason string            `json:"reason,omitempty"`
}

// Investigation 是一次 AI 调查的完整记录。
type Investigation struct {
	ID        string `json:"id"`
	AlertID   string `json:"alertId"`
	RuleID    string `json:"ruleId"`
	RuleName  string `json:"ruleName"`
	Node      string `json:"node"`
	State     string `json:"state"` // running|done|failed|skipped
	Stopped   string `json:"stopped,omitempty"`
	Protocol  string `json:"protocol,omitempty"`
	Answer    string `json:"answer,omitempty"`
	Steps     []Step `json:"steps,omitempty"`
	Tokens    int    `json:"tokens,omitempty"`
	Rounds    int    `json:"rounds,omitempty"`
	Error     string `json:"error,omitempty"`
	StartedAt int64  `json:"startedAt"`
	EndedAt   int64  `json:"endedAt,omitempty"`

	Suggestions []Suggestion `json:"suggestions,omitempty"`
}

// InvestigationStore 是调查结果的独立存储（不写进告警事件：长文本进时序库标签会放大基数与体积）。
type InvestigationStore struct {
	mu      sync.Mutex
	p       configdoc.Persister
	max     int
	keepFor time.Duration
	items   []Investigation
}

func NewInvestigationStore(path string, max int, keepFor time.Duration) *InvestigationStore {
	return NewInvestigationStoreWithPersister(configdoc.NewFilePersister(path), max, keepFor)
}

func NewInvestigationStoreWithPersister(p configdoc.Persister, max int, keepFor time.Duration) *InvestigationStore {
	s := &InvestigationStore{p: p, max: max, keepFor: keepFor}
	s.load()
	return s
}

func (s *InvestigationStore) load() {
	data, err := s.p.Load()
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, &s.items)
	// 进程在调查中途重启：running 记录不能永远停在「调查中」
	changed := false
	for i := range s.items {
		if s.items[i].State == "running" {
			s.items[i].State = "failed"
			s.items[i].Error = "服务重启，调查中断"
			changed = true
		}
	}
	if changed {
		s.mu.Lock()
		s.persistLocked()
		s.mu.Unlock()
	}
}

func (s *InvestigationStore) persistLocked() {
	data, _ := json.MarshalIndent(s.items, "", "  ")
	_ = s.p.Save(data)
}

func (s *InvestigationStore) pruneLocked() {
	cutoff := time.Now().Add(-s.keepFor).UnixMilli()
	out := s.items[:0]
	for _, it := range s.items {
		if it.StartedAt >= cutoff {
			out = append(out, it)
		}
	}
	s.items = out
	if s.max > 0 && len(s.items) > s.max {
		s.items = s.items[len(s.items)-s.max:]
	}
}

func (s *InvestigationStore) Add(inv Investigation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// 未填开始时间时补当前时间：否则会被保留期裁剪当成「很久以前」直接删掉
	if inv.StartedAt == 0 {
		inv.StartedAt = time.Now().UnixMilli()
	}
	s.items = append(s.items, inv)
	s.pruneLocked()
	s.persistLocked()
	return nil
}

func (s *InvestigationStore) Update(id string, fn func(*Investigation)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.items {
		if s.items[i].ID == id {
			fn(&s.items[i])
			s.items[i].EndedAt = time.Now().UnixMilli()
			break
		}
	}
	s.persistLocked()
}

func (s *InvestigationStore) ByAlert(alertID string) (Investigation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.items) - 1; i >= 0; i-- {
		if s.items[i].AlertID == alertID {
			return s.items[i], true
		}
	}
	return Investigation{}, false
}

// ByID 按调查 ID 查找。提交建议时需要它：告警 ID 与调查 ID 不是一回事。
func (s *InvestigationStore) ByID(id string) (Investigation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.items) - 1; i >= 0; i-- {
		if s.items[i].ID == id {
			return s.items[i], true
		}
	}
	return Investigation{}, false
}

func (s *InvestigationStore) List(limit int) []Investigation {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 || limit > len(s.items) {
		limit = len(s.items)
	}
	out := make([]Investigation, limit)
	copy(out, s.items[len(s.items)-limit:])
	return out
}
