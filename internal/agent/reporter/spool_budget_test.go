package reporter

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mlmw92/yingzhao/internal/model"
)

// budgetMetric 造一个体积可预期的指标点（标签够长，便于把 spool 撑到几 MB）。
func budgetMetric(i int) model.Metric {
	return model.Metric{
		Node: "web-01", Name: "test_metric_with_a_fairly_long_name",
		Labels:    map[string]string{"instance": "10.0.0.10:9100", "job": strings.Repeat("x", 120)},
		Value:     float64(i),
		Timestamp: 1791356749796,
	}
}

func fillSpool(t *testing.T, s *Spool, batches, perBatch int) {
	t.Helper()
	for b := 0; b < batches; b++ {
		ms := make([]model.Metric, 0, perBatch)
		for i := 0; i < perBatch; i++ {
			ms = append(ms, budgetMetric(b*perBatch+i))
		}
		s.Append(ms)
	}
}

// 预算由 Server 上限推出来，并夹在上下限之间。
//
// 这几条边界都要钉住：估大了会撞 413、估小了排空慢，而"夹紧"这件事写错了不会报错。
func TestSpoolDrainBudget_DerivesFromServerLimit(t *testing.T) {
	dir := t.TempDir()
	const mib = 1 << 20

	cases := []struct {
		name       string
		serverBody int64
		livePoints int
		want       int
	}{
		// 默认 16 MiB：预算是"一半 − 载荷估算"，不会正好顶到上限（16/2 = 8 MiB = 上限，再减载荷）
		{"默认上限 + 小载荷", 16 * mib, 100, int(8*mib - spoolNonMetricAllowance - 100*spoolMetricBytesEstimate)},
		// Server 上限更大时才会顶到上限（64/2 = 32 MiB > 8 MiB）
		{"Server 上限 64 MiB → 顶到上限", 64 * mib, 0, spoolDrainCeiling},
		// Server 上限小一半 → 预算约 1 MiB 减掉载荷估算（256 KiB + 100×256）
		{"Server 上限 2 MiB", 2 * mib, 100, int(mib - spoolNonMetricAllowance - 100*spoolMetricBytesEstimate)},
		// Server 上限很小 → 夹到下限
		{"Server 上限 512 KiB", 512 << 10, 100, spoolDrainFloor},
		// 本轮载荷极大（10 万个点 ≈ 24 MiB 估算）→ 夹到下限，绝不出现负预算
		{"载荷估算超过上限", 16 * mib, 100000, spoolDrainFloor},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, err := OpenSpool(filepath.Join(dir, c.name+".jsonl"), 16*mib, c.serverBody)
			if err != nil {
				t.Fatalf("OpenSpool: %v", err)
			}
			if got := s.drainBudget(c.livePoints); got != c.want {
				t.Fatalf("drainBudget = %d，期望 %d", got, c.want)
			}
			if got := s.drainBudget(c.livePoints); got < spoolDrainFloor || got > spoolDrainCeiling {
				t.Fatalf("预算越界：%d", got)
			}
		})
	}
}

// 撞 413 后预算**减半并记住**：这是"永远排不空"的兜底。
// 非 413 的失败不该动预算（网络抖动与"这一轮塞了多少"无关）。
func TestSpoolDrainBudget_ShrinksAfter413AndStaysDown(t *testing.T) {
	s, err := OpenSpool(filepath.Join(t.TempDir(), "s.jsonl"), 16<<20, 16<<20)
	if err != nil {
		t.Fatalf("OpenSpool: %v", err)
	}
	initial := int(8<<20 - spoolNonMetricAllowance) // 16 MiB/2 − 载荷估算（无指标点）
	if got := s.drainBudget(0); got != initial {
		t.Fatalf("初始预算应为 %d，实际 %d", initial, got)
	}

	// 网络错误不动预算
	s.noteFailure(errNoop{})
	if got := s.drainBudget(0); got != initial {
		t.Fatalf("非 413 的失败不该改预算，实际 %d", got)
	}

	// 413 逐次减半，直到下限为止
	want := spoolDrainCeiling
	for i := 0; i < 10; i++ {
		s.noteFailure(&HTTPStatusError{Code: 413})
		want /= 2
		if want < spoolDrainFloor {
			want = spoolDrainFloor
		}
		if got := s.drainBudget(0); got != want {
			t.Fatalf("第 %d 次 413 后预算应为 %d，实际 %d", i+1, want, got)
		}
	}
	// 学到的小上限不会被"估算看着够"顶回去：换一个更大的 serverBodyLimit 也一样
	s.serverBodyLimit = 64 << 20
	if got := s.drainBudget(0); got != spoolDrainFloor {
		t.Fatalf("413 学到的小上限不该回升，实际 %d", got)
	}
}

type errNoop struct{}

func (errNoop) Error() string { return "network error" }

// **决定性用例**：同样的积压下，一轮能排掉的量远多于原先固定的 512 KiB，
// 且**请求体不超 Server 上限**（补传预算的全部意义就在这两条同时成立）。
func TestSpoolSend_DrainsFarMoreThanFixedCapAndStaysUnderLimit(t *testing.T) {
	const mib = 1 << 20
	const serverBody = 16 * mib
	s, err := OpenSpool(filepath.Join(t.TempDir(), "s.jsonl"), 32*mib, serverBody)
	if err != nil {
		t.Fatalf("OpenSpool: %v", err)
	}
	// 灌约 3 MiB 积压：旧实现按 512 KiB/轮要 6 轮以上，新预算应当**一轮排空**。
	// （刻意灌得比预算小——"一轮排空"只在积压 ≤ 预算时成立，超出的部分按预算分批。）
	fillSpool(t, s, 6, 2000)
	if depth := s.Depth(); depth < 2*mib {
		t.Fatalf("积压应超过 2 MiB，实际 %d 字节（夹具太小，用例会失去意义）", depth)
	}

	var sentBytes int
	var sentPoints int
	_, err = s.Send(model.ReportPayload{}, func(p model.ReportPayload) (ReportResponse, error) {
		body, e := json.Marshal(p)
		if e != nil {
			return ReportResponse{}, e
		}
		sentBytes = len(body)
		sentPoints = len(p.Metrics)
		return ReportResponse{Status: "ok"}, nil
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if sentBytes <= 512<<10 {
		t.Fatalf("一轮应排掉远多于旧的 512 KiB 上限，实际请求体只有 %d 字节", sentBytes)
	}
	if sentBytes > serverBody {
		t.Fatalf("请求体超了 Server 上限：%d > %d", sentBytes, serverBody)
	}
	if got := s.Depth(); got != 0 {
		t.Fatalf("5 MiB 积压应一轮排空，剩余 %d 字节", got)
	}
	// 累计量要能回答"排到哪了"
	if s.DrainedBytes() <= 0 || s.DrainedPoints() != int64(sentPoints) {
		t.Fatalf("累计补传量不符：bytes=%d points=%d（发送点数 %d）",
			s.DrainedBytes(), s.DrainedPoints(), sentPoints)
	}
}
