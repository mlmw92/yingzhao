package reporter

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mlmw92/yingzhao/internal/model"
)

// Spool 是上报的**磁盘缓冲**：Server 不可达时把时序点落盘，恢复后随下一次上报补传。
//
// 为什么只缓冲时序点（Metrics），而不是整份 payload：
//   - HostInfo / 实例清单 / 监听端口是"最新状态"，补传旧值等于用过期信息覆盖现状；
//   - OpsResult / DefenseResult 是指令回执，补传可能让一次已经确认过的动作被重复确认；
//   - 而指标点自带时间戳，晚到就是晚到：Server 侧对指标**没有时间窗口校验**，照收。
//
// 为什么不新开一条"补传"接口：直接并入下一次正常上报即可 —— Server 零改动，
// 而且下行指令（升级 / ops / 防护）仍然走那条正常响应，不需要为补传单独处理响应体。
//
// 落盘形态是 JSONL（一行 = 一轮的指标数组）+ 一个记录"已确认字节数"的偏移文件：
// 送出成功才推进偏移，因此崩溃在发送与确认之间只会产生**重复点**，
// 而同一 series + 同一时间戳在时序库里是幂等的，不会把数据算错。
type Spool struct {
	path       string
	offsetPath string
	// maxBytes 是未确认数据的上限；<=0 表示关闭缓冲（此时所有方法都是空操作）。
	maxBytes int64
	// serverBodyLimit 是 Agent 以为的 Server 上报体上限（字节），预算由它推出来。
	serverBodyLimit int64

	mu     sync.Mutex
	offset int64 // 数据文件里已被确认（可以丢弃）的前缀字节数
	size   int64 // 数据文件总字节数
	// learnedCeiling 是撞过 413 之后学到的小上限（0 = 还没撞过）。
	// 它只会下降，不会因为"估算看着够"而回升——自愈的结果不该被下一次估算顶回去。
	learnedCeiling int64
	// drainedBytes / drainedPoints 是累计补传量，供自监控指标回答"排到哪了"。
	drainedBytes  int64
	drainedPoints int64
}

// 补传预算的绝对上下限。
//
// **必须有限制**：Server 对上报体有上限（默认 16 MiB），一次把几 MB 的补传塞进去可能让
// **每一条**上报都超限而永远失败——那样缓冲就再也排不空了。这条约束没有变，
// 变的是"预算不再写死"：
//
//   - 上限 8 MiB：单轮请求体不至于过大（Server 默认上限的一半）；
//   - 下限 256 KiB：估算失准时宁可慢，也不能失败（比原先固定的 512 KiB 更保守）。
//
// 真正的预算按"Server 上限 − 上一轮实际载荷 − 余量"算出来（见 drainBudget），
// 所以正常环境下 8 MiB 的积压**一轮就能排空**；估算错了还有 413 自愈兜底（见 noteFailure）。
const (
	spoolDrainCeiling = 8 << 20
	spoolDrainFloor   = 256 << 10
)

// 估算本轮载荷里"非指标部分"（HostInfo / 实例清单 / 监听端口 / 回执）的字节数。
// 它们不参与补传，但同样占请求体，算预算时必须从可用空间里扣掉。
// 刻意取偏大值：估大了只是补传慢一点，估小了才会撞 413。
const spoolNonMetricAllowance = 256 << 10

// 单个指标点在 JSON 里的近似字节数（含标签）。同样刻意取偏大值（真实约 150-250 字节）。
const spoolMetricBytesEstimate = 256

// OpenSpool 打开（必要时创建）磁盘缓冲。path 为空或 maxBytes<=0 时返回 nil（表示关闭）。
//
// serverBodyLimit 是 Server 的上报体上限（字节，来自配置）。Agent 探测不到它，
// 所以由配置传入；<=0 时按 Server 的默认值 16 MiB 处理。
func OpenSpool(path string, maxBytes, serverBodyLimit int64) (*Spool, error) {
	if strings.TrimSpace(path) == "" || maxBytes <= 0 {
		return nil, nil
	}
	if serverBodyLimit <= 0 {
		serverBodyLimit = 16 << 20 // 与 Server 的默认上限一致（日志上限的 4 倍）
	}
	s := &Spool{path: path, offsetPath: path + ".offset", maxBytes: maxBytes, serverBodyLimit: serverBodyLimit}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("创建缓冲目录失败: %w", err)
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("打开缓冲文件失败: %w", err)
	}
	st, err := f.Stat()
	f.Close()
	if err != nil {
		return nil, fmt.Errorf("读取缓冲文件状态失败: %w", err)
	}
	s.size = st.Size()
	s.loadOffset()
	s.mu.Lock()
	defer s.mu.Unlock()
	// 启动时整理一次：把上次运行已确认的前缀真正删掉，并保证不超上限。
	// 放在启动时做而不是每次写入时做——重启是天然的整理时机，平时不必反复重写文件。
	if err := s.compactLocked(); err != nil {
		return nil, err
	}
	s.trimLocked()
	return s, nil
}

// Enabled 报告缓冲是否生效（未启用时返回 false，调用方据此决定要不要暴露自监控指标）。
func (s *Spool) Enabled() bool { return s != nil && s.maxBytes > 0 }

// Depth 返回尚未确认的字节数（即"还欠 Server 多少数据"）。
func (s *Spool) Depth() int64 {
	if !s.Enabled() {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.size - s.offset
}

// Append 把一轮采集到的指标落盘。
//
// 缓冲关闭、指标为空、序列化失败或磁盘写不进去时都只是记一条日志并放弃这一批——
// 上报路径不能因为缓冲出问题而中断（磁盘满了不该让 Agent 停止上报）。
func (s *Spool) Append(metrics []model.Metric) {
	if !s.Enabled() || len(metrics) == 0 {
		return
	}
	data, err := json.Marshal(metrics)
	if err != nil {
		slog.Warn("缓冲指标序列化失败，本批丢弃", "err", err)
		return
	}
	data = append(data, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()
	if int64(len(data)) > s.maxBytes {
		// 单批就超过上限：留着它也发不出去（补传只会一直超限），如实记一条并丢弃。
		slog.Warn("单批指标超过磁盘缓冲上限，已丢弃", "bytes", len(data), "limitMB", s.maxBytes>>20)
		return
	}
	f, err := os.OpenFile(s.path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		slog.Warn("写磁盘缓冲失败，本批丢弃", "err", err)
		return
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		slog.Warn("写磁盘缓冲失败，本批丢弃", "err", err)
		return
	}
	f.Close()
	s.size += int64(len(data))
	s.trimLocked()
}

// Drain 取出一批待补传的指标。**不删除**：成功送出后用 Ack 确认。
//
// 返回的 consumed 是本次读过的字节数（含被跳过的坏行），必须原样交给 Ack，
// 否则那段数据会被反复补传。
func (s *Spool) Drain(maxBytes int) (metrics []model.Metric, consumed int64) {
	if !s.Enabled() || maxBytes <= 0 {
		return nil, 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	off := s.offset
	for off < s.size {
		line, n, ok := s.lineAtLocked(off)
		if !ok {
			break
		}
		// 至少取一批：否则一行超大的记录会让补传永远取不出东西。
		if consumed > 0 && consumed+int64(n) > int64(maxBytes) {
			break
		}
		var batch []model.Metric
		if err := json.Unmarshal(line, &batch); err != nil {
			// 坏行跳过但计入 consumed：让 Ack 能把它一起丢掉，
			// 不然一行坏数据会把整条补传链路永久卡住。
			slog.Warn("磁盘缓冲中的一批指标无法解析，已跳过", "err", err)
			consumed += int64(n)
			off += int64(n)
			continue
		}
		metrics = append(metrics, batch...)
		consumed += int64(n)
		off += int64(n)
		if consumed >= int64(maxBytes) {
			break
		}
	}
	return metrics, consumed
}

// Ack 确认 consumed 字节已经成功送达（取上一次 Drain 的第二个返回值）。
func (s *Spool) Ack(consumed int64) {
	if !s.Enabled() || consumed <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.offset += consumed
	if s.offset > s.size {
		s.offset = s.size
	}
	if err := s.saveOffsetLocked(); err != nil {
		slog.Warn("记录缓冲确认偏移失败，下次启动可能重复补传一批", "err", err)
	}
	// 已确认的部分占了一半以上就顺手重写：否则数据文件会随着反复"追加 + 确认"无限变长。
	if s.offset > 0 && s.offset*2 >= s.size {
		if err := s.compactLocked(); err != nil {
			slog.Warn("整理磁盘缓冲失败，下次启动会重试", "err", err)
		}
	}
}

// Send 把缓冲里的指标并入本轮 payload 后发送，并维护缓冲：
//   - 送出成功 → 确认已并入的那一段（下次不再重复补传）；
//   - 送出失败 → 把**本轮自己的**指标落盘（已并入的那段没有确认，仍在缓冲里，不会重复）。
//
// 顺序是刻意的：先发送再确认。反过来一旦在"发送"与"落盘"之间崩溃，那批数据就彻底没了；
// 现在这样最坏只是重复，而重复的时序点在同 series + 同时间戳下是幂等的。
func (s *Spool) Send(p model.ReportPayload, send func(model.ReportPayload) (ReportResponse, error)) (ReportResponse, error) {
	if !s.Enabled() {
		return send(p)
	}
	live := p.Metrics
	backfill, consumed := s.Drain(s.drainBudget(len(live)))
	if len(backfill) > 0 {
		// 老点放前面只为读请求体时直观；时序库按时间戳索引，与顺序无关。
		merged := make([]model.Metric, 0, len(backfill)+len(live))
		merged = append(merged, backfill...)
		merged = append(merged, live...)
		p.Metrics = merged
	}
	resp, err := send(p)
	if err != nil {
		s.Append(live)
		s.noteFailure(err)
		return resp, err
	}
	s.Ack(consumed)
	s.noteDrained(consumed, len(backfill))
	return resp, nil
}

// drainBudget 算出本轮可以并入多少补传字节。
//
// 预算 = Server 上限的一半 − 本轮载荷估算（非指标部分 + 指标点数 × 单点估算），
// 夹在 [下限, 上限] 内，且不超过撞 413 后学到的小上限。
//
// 为什么留一半：请求体里还有 JSON 结构开销与标签重复，而且**本轮可能比上一轮更大**
// （新增实例、长标签）。估算刻意保守——估大了只是慢，估小了有 413 兜底。
func (s *Spool) drainBudget(livePoints int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	live := int64(spoolNonMetricAllowance) + int64(livePoints)*spoolMetricBytesEstimate
	budget := s.serverBodyLimit/2 - live
	if budget > spoolDrainCeiling {
		budget = spoolDrainCeiling
	}
	if budget < spoolDrainFloor {
		budget = spoolDrainFloor
	}
	if s.learnedCeiling > 0 && budget > s.learnedCeiling {
		budget = s.learnedCeiling
	}
	return int(budget)
}

// noteFailure 处理发送失败：只有 413 需要动预算（其它失败与"这一轮塞了多少"无关）。
//
// 这是安全网：动态预算一旦估错（Server 的实际上限比 Agent 以为的小），没有它就会重演
// 当初把上限钉死在 512 KiB 要防的那个坑——每一条都超限、缓冲永远排不空。
func (s *Spool) noteFailure(err error) {
	var se *HTTPStatusError
	if !errors.As(err, &se) || se.Code != http.StatusRequestEntityTooLarge {
		return
	}
	s.mu.Lock()
	next := s.learnedCeiling
	if next <= 0 {
		next = spoolDrainCeiling
	}
	next /= 2
	if next < spoolDrainFloor {
		next = spoolDrainFloor
	}
	s.learnedCeiling = next
	s.mu.Unlock()
	slog.Warn("Server 拒绝了过大的上报体（413），补传预算降到下一档——请核对两端的体积上限配置",
		"nextBudgetKB", next>>10)
}

// noteDrained 累计补传量，并记一行过程日志（"补传多少 / 还剩多少"）。
//
// 为什么值得记：断网恢复后，运维此前只能盯着 agent_spool_bytes 这条曲线猜"排到哪了"。
func (s *Spool) noteDrained(consumed int64, points int) {
	if consumed <= 0 {
		return
	}
	s.mu.Lock()
	s.drainedBytes += consumed
	s.drainedPoints += int64(points)
	remaining := s.size - s.offset
	s.mu.Unlock()
	if remaining > 0 {
		slog.Info("已补传缓冲数据",
			"drainedKB", consumed>>10, "points", points, "remainingKB", remaining>>10)
		return
	}
	slog.Info("磁盘缓冲已排空", "drainedKB", consumed>>10, "points", points)
}

// DrainedBytes / DrainedPoints 返回累计补传量（供自监控指标使用）。
func (s *Spool) DrainedBytes() int64 {
	if !s.Enabled() {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.drainedBytes
}

func (s *Spool) DrainedPoints() int64 {
	if !s.Enabled() {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.drainedPoints
}

/* ---- 文件读写与整理（下述 *Locked 方法要求调用方持有 s.mu）---- */

// lineAtLocked 读取 off 处的完整一行，返回（行内容, 含换行的字节数, 是否读到）。
func (s *Spool) lineAtLocked(off int64) ([]byte, int, bool) {
	if off >= s.size {
		return nil, 0, false
	}
	tail, err := s.readFrom(off, s.size-off)
	if err != nil {
		slog.Warn("读取磁盘缓冲失败", "err", err)
		return nil, 0, false
	}
	i := bytes.IndexByte(tail, '\n')
	if i < 0 {
		// 没有换行：多半是上次写入被中途打断（掉电）。把它当一整行读出来，
		// 解析失败会被 Drain 跳过并计入 consumed，不会永久卡住。
		return tail, len(tail), len(tail) > 0
	}
	return tail[:i], i + 1, true
}

// trimLocked 在上限内保留**最近的**数据：超出时从最老的一行开始丢，
// 并在丢弃量达到一定规模时把文件真正重写一次。
//
// 丢最老而不是丢最新：排障时人关心的是"最近发生了什么"，
// 而 Server 恢复后也是最近的一段数据更完整、更有对照价值。
//
// **为什么这里必须顺手整理**：只推进偏移等于"逻辑丢弃"，文件本身仍在变长。
// 断网期间不会有 Ack（没有东西送出去），于是只在 Ack 里整理的话，文件会随着
// 「追加 + 丢最老」无限增长——2026-10-04 真机实测：断网 7 小时后数据文件长到 **113 MB**，
// 而同时"未确认数据"一直稳在上限 8 MiB。机器磁盘小的场景下这会把盘写满。
// 现在按与 Ack 相同的阈值（已丢弃的超过一半）重写，文件被限制在约 2 倍上限以内。
func (s *Spool) trimLocked() {
	dropped := false
	for s.size-s.offset > s.maxBytes {
		_, n, ok := s.lineAtLocked(s.offset)
		if !ok || n == 0 {
			// 读不出来（文件被外部改坏等）：整体放弃，不要卡在一个坏文件上。
			s.offset = s.size
			break
		}
		s.offset += int64(n)
		dropped = true
		slog.Warn("磁盘缓冲超出上限，已丢弃最老的一批指标", "limitMB", s.maxBytes>>20)
	}
	// 已丢弃的部分超过一半就重写：既发生在断网期间（本函数），也发生在补传期间（Ack）。
	if dropped && s.offset > 0 && s.offset*2 >= s.size {
		if err := s.compactLocked(); err != nil {
			slog.Warn("整理磁盘缓冲失败，下次再试", "err", err)
		}
		return
	}
	if err := s.saveOffsetLocked(); err != nil {
		slog.Warn("记录缓冲确认偏移失败", "err", err)
	}
}

// compactLocked 把数据文件重写为"仅保留未确认的尾部"。
func (s *Spool) compactLocked() error {
	if s.offset <= 0 {
		return nil
	}
	tail, err := s.readFrom(s.offset, s.size-s.offset)
	if err != nil {
		return fmt.Errorf("读取缓冲尾部失败: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, tail, 0o600); err != nil {
		return fmt.Errorf("重写缓冲文件失败: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("替换缓冲文件失败: %w", err)
	}
	s.size = int64(len(tail))
	s.offset = 0
	return s.saveOffsetLocked()
}

func (s *Spool) readFrom(off, n int64) ([]byte, error) {
	if n <= 0 {
		return nil, nil
	}
	f, err := os.Open(s.path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return nil, err
	}
	buf := make([]byte, n)
	m, err := io.ReadFull(f, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	return buf[:m], nil
}

func (s *Spool) loadOffset() {
	data, err := os.ReadFile(s.offsetPath)
	if err != nil {
		return
	}
	var off int64
	if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &off); err != nil {
		return
	}
	if off < 0 {
		off = 0
	}
	if off > s.size {
		// 偏移比文件还大（文件被截断过）：以文件大小为准，避免读出空洞。
		off = s.size
	}
	s.offset = off
}

func (s *Spool) saveOffsetLocked() error {
	return os.WriteFile(s.offsetPath, []byte(fmt.Sprintf("%d\n", s.offset)), 0o600)
}
