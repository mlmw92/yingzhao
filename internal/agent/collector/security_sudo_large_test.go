package collector

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mlmw92/yingzhao/internal/agent/config"
)

// 大日志 + 偏移持久化的行为。
//
// 背景（2026-10-07 在 dev-server 上定位到的真缺陷）：偏移此前只在内存里，
// 每次重启都从字节 0 重读；而单周期最多解析 4 MiB、上报周期约 5 分钟，
// 线上 auth.log 有 **45 MB** —— 从 0 追到文件尾要约 60 分钟，期间任何一次重启
// 都会从头再来。后果是 **sudo 采集长期卡在文件最前面那几 MiB**：
// 库里 `sudo_audit` 只有 4 条、全是几天前的（09-26/09-28/10-06），而同一文件上的
// SSH 事件一直在产（SSH 的暴力破解是**聚合**事件，从文件前部就能产出，所以掩盖了症状）。
//
// 两条修复各由下面一条用例钉住：**冷启动跳到末尾窗口**、**偏移持久化后重启不重读**。

// 造一个超过单周期上限的日志，sudo 行放在末尾。
func writeLargeAuthLog(t *testing.T, dir string) (string, string) {
	t.Helper()
	path := filepath.Join(dir, "auth.log")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	filler := "Sep 26 10:00:00 host sshd[1]: Accepted password for alice from 10.0.0.9 port 22 ssh2\n"
	lines := securityMaxBytes/len(filler) + 200
	for i := 0; i < lines; i++ {
		if _, err := f.WriteString(filler); err != nil {
			t.Fatal(err)
		}
	}
	sudoLine := "Sep 26 10:00:01 host sudo:   alice : TTY=pts/0 ; PWD=/home/alice ; USER=root ; COMMAND=/bin/systemctl restart nginx\n"
	if _, err := f.WriteString(sudoLine); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path, sudoLine
}

func newCollectorWithState(t *testing.T, dir, path string) *SecurityCollector {
	t.Helper()
	// FIMBaselinePath 决定状态文件目录（偏移状态与它同目录），指到临时目录里避免污染仓库
	return NewSecurityCollector("n1", "10.0.0.1", config.SecurityConfig{
		SSHLogPaths:     []string{path},
		FIMBaselinePath: filepath.Join(dir, "fim_baseline.json"),
	})
}

// **决定性**：冷启动（没有可恢复的偏移）时必须直接跳到**末尾窗口**，
// 而不是从 0 开始——否则 45 MB 的日志要一小时才追得上，期间任何重启都前功尽弃。
func TestSecurityCollector_ColdStartReadsTail(t *testing.T) {
	dir := t.TempDir()
	path, _ := writeLargeAuthLog(t, dir)
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("日志 %d 字节（单周期上限 %d，冷启动窗口 %d）", st.Size(), securityMaxBytes, securityColdStartWindow)

	c := newCollectorWithState(t, dir, path)
	// **第一次**采集就必须读到末尾那条 sudo 行
	got := c.collectSudo()
	if len(got) != 1 {
		t.Fatalf("冷启动应跳到末尾窗口、第一轮就读到末尾的 sudo 行，got %d"+
			"（若为 0，线上表现为「sudo 审计永远只有文件最前面那几条，且不再增加」）", len(got))
	}
	if got[0].Detail["command"] != "/bin/systemctl restart nginx" {
		t.Fatalf("读到的命令不对（可能读到了半行）：%+v", got[0].Detail)
	}
	// 第二轮不该重复
	if again := c.collectSudo(); len(again) != 0 {
		t.Fatalf("偏移应已推进，不该重复上报，got %d", len(again))
	}
}

// 冷启动跳过的历史部分**不会**被当成新事件上报：它本来就在窗口之前。
func TestSecurityCollector_ColdStartSkipsHistory(t *testing.T) {
	dir := t.TempDir()
	path, _ := writeLargeAuthLog(t, dir)
	c := newCollectorWithState(t, dir, path)
	// 文件里只有末尾一条 sudo 行，冷启动窗口之前的填充都是 sshd 行
	if got := c.collectSudo(); len(got) != 1 {
		t.Fatalf("只应读到末尾那一条，got %d", len(got))
	}
}

// **决定性**：偏移要持久化——重启后接着上次的位置读，不从头再来。
func TestSecurityCollector_OffsetsSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	path, sudoLine := writeLargeAuthLog(t, dir)

	c1 := newCollectorWithState(t, dir, path)
	if got := c1.collectSudo(); len(got) != 1 {
		t.Fatalf("首次应读到末尾的 sudo 行，got %d", len(got))
	}

	// 重启：新建一个采集器（模拟 Agent 重启），状态文件还在
	c2 := newCollectorWithState(t, dir, path)
	if got := c2.collectSudo(); len(got) != 0 {
		t.Fatalf("重启后不该重读已处理过的内容，got %d（偏移没被持久化）", len(got))
	}

	// 重启后追加的新行必须能读到
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	newLine := "Sep 26 10:00:02 host sudo:   bob : TTY=pts/1 ; PWD=/root ; USER=root ; COMMAND=/usr/bin/apt update\n"
	if _, err := f.WriteString(newLine); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if got := c2.collectSudo(); len(got) != 1 {
		t.Fatalf("重启后追加的行应被读到，got %d", len(got))
	}
	_ = sudoLine
}

// 日志被轮转/重建（文件变小）时，偏移越界要走冷启动逻辑，而不是读到文件尾就卡住。
func TestSecurityCollector_RotationFallsBackToColdStart(t *testing.T) {
	dir := t.TempDir()
	path, _ := writeLargeAuthLog(t, dir)
	c := newCollectorWithState(t, dir, path)
	if got := c.collectSudo(); len(got) != 1 {
		t.Fatalf("首次应读到末尾的 sudo 行，got %d", len(got))
	}

	// 轮转：新文件小得多（小于冷启动窗口），里面有一条新 sudo 行
	small := "Sep 27 09:00:00 host sudo:   carol : TTY=pts/2 ; PWD=/tmp ; USER=root ; COMMAND=/bin/ls /root\n"
	if err := os.WriteFile(path, []byte(small), 0o600); err != nil {
		t.Fatal(err)
	}
	got := c.collectSudo()
	if len(got) != 1 || !strings.Contains(got[0].Detail["command"], "/bin/ls /root") {
		t.Fatalf("轮转后应从新文件里读到那条 sudo 行，got %+v", got)
	}
}
