package security

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"github.com/nebula/monitor/internal/model"
)

// 高危命令的**事后发现**（能力全景表 §3.7 行 185 落点②）。
//
// 为什么是"事后"而不是"事前阻断"：平台不提供任意命令执行，所以不存在"即将执行的任意命令"
// 可以拦；而 SSH 认证日志里**本来就没有命令**（只有登录成败），要事前拿到命令就得引入
// auditd / bash_history / PTY 会话采集——那正是行 185 明确不需要的会话代理方向。
//
// 现成可用的只有 **sudo**：`collectSudo` 已经把命令全文放在 `SecurityEvent.Detail["command"]` 里。
// 本文件做的事就是把它扫一遍，命中的派生一条 `high_risk_cmd` 事件。
//
// 判定只收"**必然危险**"的形态，不做启发式：误报会让人把这一类整体无视，
// 而这类能力的全部价值就在于"出现时值得马上看一眼"。

// CommandFinding 是一条命中的高危命令特征。
type CommandFinding struct {
	// Rule 规则标识（去重与界面展示用）。
	Rule string
	// Reason 人话理由。
	Reason string
}

// commandRule 是一条特征规则。
type commandRule struct {
	rule    string
	re      *regexp.Regexp
	reason  string
}

// commandRules 是全部规则。
//
// 每条都要能一眼看出"为什么这条一定危险"；写不出理由的形态（例如单独一个 passwd）
// 一律不收——那是误报的主要来源。
var commandRules = []commandRule{
	{
		rule:   "rm-rf-root",
		re:     regexp.MustCompile(`(^|[\s;&|])rm\s+(-[a-zA-Z]+\s+)*-[a-zA-Z]*r[a-zA-Z]*f[a-zA-Z]*\s+/(\s|$|\*)`),
		reason: "递归强制删除根目录",
	},
	{
		rule:   "rm-rf-root-flags-split",
		re:     regexp.MustCompile(`(^|[\s;&|])rm\s+.*-[a-zA-Z]*r[a-zA-Z]*\s+.*-[a-zA-Z]*f[a-zA-Z]*\s+/(\s|$|\*)`),
		reason: "递归强制删除根目录（参数分开写）",
	},
	{
		rule:   "dd-of-device",
		re:     regexp.MustCompile(`\bdd\b[^|;&]*\bof=/dev/`),
		reason: "直接向块设备写入（可能毁掉磁盘或分区表）",
	},
	{
		rule:   "mkfs",
		re:     regexp.MustCompile(`(^|[\s;&|])mkfs(\.[a-z0-9]+)?\s`),
		reason: "格式化文件系统",
	},
	{
		rule:   "chmod-777-root",
		re:     regexp.MustCompile(`\bchmod\b[^|;&]*\s(-R\s+)?0?777\s+/(\s|$)`),
		reason: "把根目录权限改成所有人可写",
	},
	{
		rule:   "chown-recursive-root",
		re:     regexp.MustCompile(`\bchown\b[^|;&]*\s-R\b[^|;&]*\s/(\s|$)`),
		reason: "递归改掉根目录的属主",
	},
	{
		rule:   "pipe-download-to-shell",
		re:     regexp.MustCompile(`\b(curl|wget)\b[^|]*\|\s*(sudo\s+)?(ba|z|da)?sh\b`),
		reason: "把下载来的内容直接交给 shell 执行（内容不可审计）",
	},
	{
		rule:   "overwrite-account-file",
		re:     regexp.MustCompile(`>>?\s*/etc/(passwd|shadow|sudoers|gshadow)\b`),
		reason: "重定向覆盖账户或 sudo 配置",
	},
	{
		rule:   "flush-firewall",
		re:     regexp.MustCompile(`\biptables\s+-F\b|\bnft\s+flush\s+ruleset\b`),
		reason: "清空防火墙规则（等于临时摘掉所有防护）",
	},
	{
		rule:   "clear-history",
		re:     regexp.MustCompile(`(^|[\s;&|])history\s+-c\b|>\s*~?/?\.bash_history\b`),
		reason: "清除命令历史（抹掉痕迹）",
	},
	{
		rule:   "stop-remote-access",
		re:     regexp.MustCompile(`\bsystemctl\s+(stop|disable|mask)\s+(sshd|ssh|monitor-agent)\.service\b`),
		reason: "停掉远程登录或平台 Agent（断掉自己的后路）",
	},
	{
		rule:   "kill-init",
		re:     regexp.MustCompile(`(^|[\s;&|])kill\s+-9\s+1(\s|$)`),
		reason: "杀死 PID 1（会拖垮整个系统）",
	},
	{
		rule:   "account-change",
		// 只收"改别人"的形态：裸 `passwd`（改自己口令）太常见，收进来会把这一类淹掉。
		re:     regexp.MustCompile(`(^|[\s;&|])(useradd|userdel|usermod|chpasswd|visudo)\s|(^|[\s;&|])passwd\s+\S+`),
		reason: "新增/删除账户或改他人口令",
	},
}

// ScanCommand 扫描一条命令文本，返回命中的规则（空 = 没命中）。
//
// 入参是 sudo 日志里的原文，可能带参数与引号；这里只做形态匹配，不做 shell 解析
// （解析命令是另一个量级的复杂度，而误判一个"看起来危险"的命令比漏掉一条更糟）。
func ScanCommand(cmd string) []CommandFinding {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return nil
	}
	var out []CommandFinding
	for _, r := range commandRules {
		if r.re.MatchString(cmd) {
			out = append(out, CommandFinding{Rule: r.rule, Reason: r.reason})
		}
	}
	return out
}

// HighRiskWindowMs 是同类高危命令的**去重窗口**。
//
// 不去重则一条 `while true; do sudo rm -rf /; done` 会把事件列表刷满，
// 而这一类事件的全部价值就是"出现时值得马上看一眼"——刷屏等于失效。
// 窗口内同一节点 + 同一规则 + 同一命令只报一次；出窗后再出现会重新报（那说明还在发生）。
const HighRiskWindowMs = 10 * 60 * 1000

// highRiskEventID 生成派生事件的稳定 ID（存储层按 ID 去重）。
func highRiskEventID(node, rule, cmd string, ts int64) string {
	sum := sha256.Sum256([]byte(cmd))
	return fmt.Sprintf("%s|%s|%s|%s|%d", node, model.SecurityCatHighRiskCmd, rule,
		hex.EncodeToString(sum[:8]), ts/HighRiskWindowMs)
}

// deriveHighRisk 从一条 sudo 审计事件派生出高危命令事件（没有命中则返回 nil）。
//
// 派生事件的 `Detail` 带**命令原文**：事后排查必须能看到到底执行了什么，
// 只写"检测到高危命令"等于把线索丢掉。原文本身来自 sudo 日志，属既有的采集口径。
func deriveHighRisk(e model.SecurityEvent) []model.SecurityEvent {
	if e.Category != model.SecurityCatSudoAudit {
		return nil
	}
	cmd := strings.TrimSpace(e.Detail["command"])
	if cmd == "" {
		return nil
	}
	findings := ScanCommand(cmd)
	if len(findings) == 0 {
		return nil
	}
	out := make([]model.SecurityEvent, 0, len(findings))
	for _, f := range findings {
		d := map[string]string{"rule": f.Rule, "command": cmd}
		if v := e.Detail["targetUser"]; v != "" {
			d["targetUser"] = v
		}
		out = append(out, model.SecurityEvent{
			ID:       highRiskEventID(e.Node, f.Rule, cmd, e.Timestamp),
			Node:     e.Node,
			NodeIP:   e.NodeIP,
			Category: model.SecurityCatHighRiskCmd,
			// 用 critical 而不是 warning：这一类的定位就是"出现时值得马上看一眼"，
			// 落在 warning 里会和常规 sudo 流水混在一起，等于没有信号。
			Severity: model.SeverityCritical,
			Message:  "高危命令：" + f.Reason,
			Detail:   d,
			SourceIP: e.SourceIP,
			User:     e.User,
			Timestamp: e.Timestamp,
		})
	}
	return out
}
