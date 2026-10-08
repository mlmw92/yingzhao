package collector

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// 日志读取偏移的**持久化**与**冷启动落点**。
//
// 为什么需要（2026-10-07 实机定位到的真缺陷）：偏移此前只存在内存里，于是
// **每次 Agent 重启都从字节 0 重读**；而单周期最多解析 4 MiB（securityMaxBytes）、
// 上报周期约 5 分钟。dev-server 的 /var/log/auth.log 有 **45 MB** ——
// 从 0 追到文件尾需要约 **60 分钟**，而期间任何一次重启（升级、崩溃、手工重启）
// 都会让它从头再来。实际后果：**sudo 采集长期卡在文件最前面那几 MiB**，
// 表现为"sudo 审计事件只有很旧的几条、而且再也不增加"，且**不报任何错**
// （这与 collectSudo 注释里记的那个旧坑同源：都是"偏移不对导致永远读不到新内容"）。
//
// 两条一起用才有效：
//  1. **持久化偏移**：重启后接着上次的位置读，不从头再来；
//  2. **冷启动跳到末尾窗口**：没有状态可恢复时（首次部署 / 状态文件丢失 / 日志轮转），
//     不从 0 开始，而是从"文件末尾往前 securityColdStartWindow"开始——
//     历史日志没有必要每装一次就重扫一遍，而"看不到新日志"是要命的那一侧。

const (
	// securityOffsetDefaultState 偏移状态文件名（与 FIM 基线同目录）。
	securityOffsetDefaultState = "log_offsets.json"
	// securityColdStartWindow 冷启动时从文件末尾往前读这么多字节。
	//
	// 1 MiB 覆盖"几轮上报间隔内新增的日志"绰绰有余（被扫描的机器上 auth.log 每分钟也就几十 KB），
	// 又不至于让首次采集去啃几十 MB 的历史。
	securityColdStartWindow = 1 << 20
)

// offsetState 是偏移的持久化形态。ssh 与 sudo 各一份：两者读同一个文件、关注不同内容，
// 必须各推进各的（见 collectSudo 的注释，那里记着复用偏移导致的旧缺陷）。
type offsetState struct {
	SSH  map[string]int64 `json:"ssh"`
	Sudo map[string]int64 `json:"sudo"`
}

// offsetsPath 返回偏移状态文件路径：与 FIM 基线同目录（都是"采集器的本地状态"）。
func (c *SecurityCollector) offsetsPath() string {
	dir := filepath.Dir(c.cfg.FIMBaselinePath)
	if c.cfg.FIMBaselinePath == "" {
		dir = "."
	}
	return filepath.Join(dir, securityOffsetDefaultState)
}

// loadOffsets 加载上次的读取偏移（不存在则留空，由 seekStart 走冷启动逻辑）。
func (c *SecurityCollector) loadOffsets() {
	path := c.offsetsPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("读取日志偏移状态失败（按冷启动处理）", "path", path, "err", err)
		}
		return
	}
	var st offsetState
	if err := json.Unmarshal(data, &st); err != nil {
		slog.Warn("日志偏移状态解析失败（按冷启动处理）", "path", path, "err", err)
		return
	}
	c.mu.Lock()
	for k, v := range st.SSH {
		c.sshOff[k] = v
	}
	for k, v := range st.Sudo {
		c.sudoOff[k] = v
	}
	c.mu.Unlock()
	slog.Info("已加载日志读取偏移", "path", path, "ssh", len(st.SSH), "sudo", len(st.Sudo))
}

// saveOffsets 持久化当前偏移。每轮采集后调用一次——文件很小（几个路径），
// 而"丢了就要重扫几十 MB"的代价远大于这一次写。
func (c *SecurityCollector) saveOffsets() {
	c.mu.Lock()
	st := offsetState{SSH: map[string]int64{}, Sudo: map[string]int64{}}
	for k, v := range c.sshOff {
		st.SSH[k] = v
	}
	for k, v := range c.sudoOff {
		st.Sudo[k] = v
	}
	c.mu.Unlock()

	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return
	}
	path := c.offsetsPath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		slog.Warn("日志偏移状态写入失败", "path", path, "err", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		slog.Warn("日志偏移状态落盘失败", "path", path, "err", err)
	}
}

// seekStart 把文件定位到本次应读的起点，返回该起点（调用方读完后按"起点 + 实际读取字节数"写回）。
//
// off 传入该日志对应的偏移表（sshOff 或 sudoOff）。**冷启动**（该路径没有偏移，
// 或文件被轮转/重建导致偏移越界）时不从 0 开始，而是跳到末尾往前一个窗口，
// 并对齐到**下一行行首**——落点很可能落在半行中间，丢掉那半行才不会解析出半条命令。
func seekStart(f *os.File, size int64, off map[string]int64, path string) int64 {
	offset, known := off[path]
	if known && size >= offset {
		if offset <= 0 {
			_, _ = f.Seek(0, io.SeekStart)
			return 0
		}
		// 已持久化的偏移总是落在行首（写入时就对齐过），直接定位
		if _, err := f.Seek(offset, io.SeekStart); err == nil {
			return offset
		}
		_, _ = f.Seek(0, io.SeekStart)
		return 0
	}

	// 冷启动 / 文件被轮转重建
	if size <= securityColdStartWindow {
		_, _ = f.Seek(0, io.SeekStart)
		return 0
	}
	offset = size - securityColdStartWindow
	if _, err := f.Seek(offset-1, io.SeekStart); err != nil {
		_, _ = f.Seek(offset, io.SeekStart)
		return offset
	}
	// 逐字节读到换行：刻意不用 bufio（它会把后续内容预读进缓冲，文件位置就对不上了）
	buf := make([]byte, 1)
	for {
		if _, err := f.Read(buf); err != nil {
			// 读到文件尾都没有换行：退回原偏移，交给正常读取路径处理
			_, _ = f.Seek(offset, io.SeekStart)
			return offset
		}
		if buf[0] == '\n' {
			break
		}
	}
	pos, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		return offset
	}
	return pos
}
