package receiver

import (
	"testing"
	"time"
)

func TestAssetBreakerOpensAfterConsecutiveFailures(t *testing.T) {
	var b assetBreaker
	for i := 0; i < assetBreakerThreshold; i++ {
		if !b.allow() {
			t.Fatalf("第 %d 次失败前应放行", i+1)
		}
		b.failure()
	}
	if b.allow() {
		t.Fatal("连续失败达到阈值后应打开熔断")
	}
	// 冷却结束后恢复。
	b.mu.Lock()
	b.openUntil = time.Now().Add(-time.Millisecond)
	b.mu.Unlock()
	if !b.allow() {
		t.Fatal("冷却结束后应恢复放行")
	}
	// 一次成功清空计数。
	b.success()
	if b.failures != 0 || !b.openUntil.IsZero() {
		t.Fatalf("成功后应清空状态: failures=%d openUntil=%v", b.failures, b.openUntil)
	}
}

// 中间成功一次就重置计数：偶发失败不该累积成熔断。
func TestAssetBreakerSuccessResetsFailures(t *testing.T) {
	var b assetBreaker
	for i := 0; i < assetBreakerThreshold-1; i++ {
		b.failure()
	}
	b.success()
	for i := 0; i < assetBreakerThreshold-1; i++ {
		b.failure()
	}
	if !b.allow() {
		t.Fatal("中间有成功时不应熔断")
	}
}
