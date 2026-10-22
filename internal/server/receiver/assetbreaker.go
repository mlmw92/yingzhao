package receiver

import (
	"log/slog"
	"sync"
	"time"

	"github.com/mlmw92/yingzhao/internal/server/asset"
)

// assetBreaker 是资产写入的熔断器。
//
// 为什么需要：每轮上报会同步写主机 + 每个中间件实例（一次上报 3–6 个事务）。
// 数据库不可用时每个事务都要等到超时，整轮上报会超过 Agent 的 10 秒 HTTP 超时，
// 把本来健康的指标一起丢掉——台账是次要数据，不该拖垮主链路。
// 连续失败若干次后跳过资产写入一段时间，恢复后自动放行。
type assetBreaker struct {
	mu        sync.Mutex
	failures  int
	openUntil time.Time
}

const (
	// assetBreakerThreshold 是连续失败多少次后打开熔断。
	assetBreakerThreshold = 3
	// assetBreakerCooldown 是熔断持续时间（期间完全跳过资产写入）。
	assetBreakerCooldown = 30 * time.Second
)

// allow 报告当前是否允许尝试资产写入。
func (b *assetBreaker) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !time.Now().Before(b.openUntil)
}

// success 清空失败计数（任何一次成功都说明库可用）。
func (b *assetBreaker) success() {
	b.mu.Lock()
	b.failures = 0
	b.openUntil = time.Time{}
	b.mu.Unlock()
}

// failure 记录一次失败；达到阈值时打开熔断并记一条日志。
func (b *assetBreaker) failure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures++
	if b.failures >= assetBreakerThreshold {
		b.openUntil = time.Now().Add(assetBreakerCooldown)
		b.failures = 0
		slog.Warn("资产写入连续失败，暂停写台账（不影响指标与日志上报）",
			"cooldown", assetBreakerCooldown)
	}
}

// applyAsset 写一条资产并记录结果（供熔断器判断数据库是否可用）。
func (r *Receiver) applyAsset(ob asset.Observation) (asset.Asset, bool, error) {
	a, changed, err := r.assets.Apply(ob)
	if err != nil {
		r.assetBreaker.failure()
	} else {
		r.assetBreaker.success()
	}
	return a, changed, err
}
