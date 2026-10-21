package configdoc

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
)

// runStoreContract 是文件替身与 MySQL 实现共用的行为契约。
func runStoreContract(t *testing.T, newStore func(t *testing.T) Store) {
	t.Helper()
	ctx := context.Background()

	t.Run("missing namespace", func(t *testing.T) {
		if _, _, err := newStore(t).Load(ctx, "notify"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Load 缺失命名空间应返回 ErrNotFound，实际 %v", err)
		}
	})

	t.Run("create and load", func(t *testing.T) {
		s := newStore(t)
		v, err := s.Save(ctx, "notify", []byte(`{"a":1}`), 0)
		if err != nil || v != 1 {
			t.Fatalf("首次保存 = %d, %v", v, err)
		}
		payload, gotV, err := s.Load(ctx, "notify")
		if err != nil || gotV != 1 {
			t.Fatalf("Load = %s, %d, %v", payload, gotV, err)
		}
		// MySQL 的 JSON 列会规范化空白（{"a":1} → {"a": 1}），按语义比较而不是字节。
		var got map[string]any
		if err := json.Unmarshal(payload, &got); err != nil || got["a"] != float64(1) {
			t.Fatalf("Load payload = %s, %v", payload, err)
		}
	})

	t.Run("version conflict", func(t *testing.T) {
		s := newStore(t)
		if _, err := s.Save(ctx, "ui", []byte(`{"v":1}`), 0); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Save(ctx, "ui", []byte(`{"v":2}`), 0); !errors.Is(err, ErrVersionConflict) {
			t.Fatalf("错误版本应返回 ErrVersionConflict，实际 %v", err)
		}
		v, err := s.Save(ctx, "ui", []byte(`{"v":2}`), 1)
		if err != nil || v != 2 {
			t.Fatalf("正确版本保存 = %d, %v", v, err)
		}
	})

	t.Run("concurrent create", func(t *testing.T) {
		s := newStore(t)
		var wg sync.WaitGroup
		results := make(chan error, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := s.Save(ctx, "concurrent", []byte(`{"n":1}`), 0)
				results <- err
			}()
		}
		wg.Wait()
		close(results)

		var okCount, conflictCount int
		for err := range results {
			switch {
			case err == nil:
				okCount++
			case errors.Is(err, ErrVersionConflict):
				conflictCount++
			default:
				t.Fatalf("并发创建返回了非契约错误: %v", err)
			}
		}
		if okCount != 1 || conflictCount != 1 {
			t.Fatalf("并发创建应 1 成功 1 冲突，实际 ok=%d conflict=%d", okCount, conflictCount)
		}
	})

	t.Run("reject invalid namespace", func(t *testing.T) {
		s := newStore(t)
		for _, ns := range []string{"", "../x", "Notify", "a-b"} {
			if _, err := s.Save(ctx, ns, []byte(`{}`), 0); err == nil {
				t.Fatalf("非法命名空间 %q 应被拒绝", ns)
			}
		}
	})
}
