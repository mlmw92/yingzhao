package ai

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/nebula/monitor/internal/server/config"
)

// AlertRef 是自动调查看到的告警投影（不把 alert 包的类型带进 ai 包）。
type AlertRef struct {
	ID       string
	RuleID   string
	RuleName string
	Node     string
	Message  string
	StartsAt int64
}

type AlertSource interface {
	ActiveCritical() []AlertRef
}

const investigateCooldown = 30 * time.Minute

// Investigator 轮询活跃 critical 告警并按「规则+节点」去重后触发调查。
// 用轮询而不是挂进告警引擎：引擎的评估循环是最不能出错的部分，不为 AI 侵入它。
type Investigator struct {
	cfg    func() config.AIConfig
	src    AlertSource
	store  *InvestigationStore
	runOne func(context.Context, AlertRef)

	mu   sync.Mutex
	last map[string]time.Time
	sem  chan struct{}
	now  func() time.Time
}

func NewInvestigator(cfg func() config.AIConfig, src AlertSource, store *InvestigationStore,
	run func(context.Context, AlertRef)) *Investigator {
	return &Investigator{
		cfg: cfg, src: src, store: store, runOne: run,
		last: map[string]time.Time{}, sem: make(chan struct{}, 2), now: time.Now,
	}
}

func (i *Investigator) Run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			i.sweep(ctx)
		}
	}
}

func (i *Investigator) sweep(ctx context.Context) {
	cfg := i.cfg().Normalize()
	if !cfg.Enabled || !cfg.Ready() || !cfg.AutoInvestigate {
		return
	}
	for _, ref := range i.src.ActiveCritical() {
		key := ref.RuleID + "|" + ref.Node
		if !i.claim(key) {
			continue
		}
		select {
		case i.sem <- struct{}{}:
			go func(r AlertRef) {
				defer func() {
					// 工具或模型代码里的 panic 不能把整个进程带走
					if p := recover(); p != nil {
						slog.Error("AI 自动调查 panic", "rule", r.RuleID, "node", r.Node, "panic", p)
					}
					<-i.sem
				}()
				i.runOne(ctx, r)
			}(ref)
		default:
			// 并发已满：直接跳过，不排队（告警风暴时排队只会更慢）
			slog.Info("AI 自动调查跳过：并发已满", "rule", ref.RuleID, "node", ref.Node)
		}
	}
}

func (i *Investigator) claim(key string) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	now := i.now()
	if t, ok := i.last[key]; ok && now.Sub(t) < investigateCooldown {
		return false
	}
	i.last[key] = now
	return true
}
