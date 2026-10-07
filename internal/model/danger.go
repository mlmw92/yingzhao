package model

import "strings"

// 高危参数判定：**危险不等于禁止**。
//
// 平台不提供任意命令执行，目标机器是否放行由它自己的 `guards.ops` 决定
// （"机器自身的同意优先于中心的授权"）。因此这里的判定**不是授权**，而是**代价**：
//   - 审批开启时：命中的动作**无条件**进审批（不看它是否在审批范围清单里）；
//   - 审批关闭时：**不硬拦**（单管理员部署下硬拦等于把功能锁死），由界面强提示 + 审计留痕。
//
// 为什么硬拦是错的：把危险参数直接拒掉，运维会改用别的手段（登机器手改），风险反而看不见。
// 与 §3.4「组合护栏只告警不拒绝」是同一取向。
//
// 清单放在 model 而不是某一端：**两端都要用同一份**（Agent 侧的 guardcheck 已经有一份
// "系统会自执行的目录"，两端各写一份必然漂移——正则在 ops.go 已经是这个先例）。

// PersistenceDirs 是「系统自己会执行其内容」的目录。
//
// 只列目录不列单个文件：护栏本身是**目录前缀**语义，用单个文件（如 /etc/ld.so.preload）
// 去比对前缀会得到毫无意义的结论。因此这里选的都是"往里放文件就会被执行"的目录。
//
// 由 `internal/agent/ops/guardcheck.go` 与本文件共用（此前只在 Agent 侧，Server 侧无法引用）。
var PersistenceDirs = []string{
	"/etc/systemd/system", "/usr/lib/systemd/system", "/lib/systemd/system",
	"/etc/init.d", "/etc/cron.d", "/etc/cron.hourly", "/etc/cron.daily",
	"/etc/cron.weekly", "/etc/cron.monthly", "/var/spool/cron",
	"/etc/profile.d", "/etc/ld.so.conf.d", "/etc/sudoers.d",
	"/root/.ssh", // authorized_keys：加一把钥匙等于给自己开一个免密入口
}

// SensitiveFiles 是**精确命中**即为危险的账户与凭据文件。
//
// 与 PersistenceDirs 分开：那些是"目录前缀"语义（写进去的任何文件都会被执行），
// 这些是"就这一个文件"语义（覆盖它就等于改掉谁能登进这台机器）。
var SensitiveFiles = []string{
	"/etc/passwd",
	"/etc/shadow",
	"/etc/group",
	"/etc/sudoers",
	"/etc/ssh/sshd_config",
	"/root/.ssh/authorized_keys",
	"/root/.ssh/config",
	"/etc/pam.d/sshd",
}

// CriticalUnits 是**精确命中**即为危险的 systemd 单元：重启它们可能让这台机器失联，
// 或者让平台自己掉线。
//
// 刻意**不含**普通业务服务（nginx / mysql / 应用自身的单元）：那些正是 ops 通道存在的意义，
// 把它们也列为"危险"会让每次重启都弹警告，最后没人看警告。
var CriticalUnits = []string{
	"sshd.service", "ssh.service", // 断掉远程登录
	"systemd-logind.service", "dbus.service", // 影响整机会话与大量服务
	"systemd-networkd.service", "NetworkManager.service", "networking.service", // 断网 = 失联
	"firewalld.service", "ufw.service", "iptables.service", // 防火墙
	"docker.service", "containerd.service", "kubelet.service", // 容器/集群底座
	"cron.service", "crond.service", "rsyslog.service", "systemd-journald.service", // 调度与日志
	"monitor-agent.service", // 平台自己的 Agent：重启它自己会掉线
}

// DangerFinding 是一条命中的危险判定。
type DangerFinding struct {
	// Rule 是规则标识（审计与前端展示用；同一条动作可能命中多条）。
	Rule string `json:"rule"`
	// Param / Value 是命中的参数名与取值。
	Param string `json:"param"`
	Value string `json:"value"`
	// Reason 是**给人看的**理由。必须写：说不清理由的告警没人会看，误报会让人绕过审批。
	Reason string `json:"reason"`
}

// dangerRule 是一条判定规则。
type dangerRule struct {
	rule   string
	kind   string
	param  string
	values []string
	// exact 为 true 时按**精确**匹配（单元名、单个文件）；false 时按**路径前缀**匹配（目录）。
	exact  bool
	reason string
}

var dangerRules = []dangerRule{
	{
		rule: "push-persistence-dir", kind: OpsKindFilePush, param: "path",
		values: PersistenceDirs, exact: false,
		reason: "该目录里的文件会被系统自己执行：写进去即持久化，**不需要重启任何服务**",
	},
	{
		rule: "push-sensitive-file", kind: OpsKindFilePush, param: "path",
		values: SensitiveFiles, exact: true,
		reason: "覆盖它会直接改变谁能登进这台机器（账户或凭据文件）",
	},
	{
		rule: "restart-critical-unit", kind: OpsKindSvcRestart, param: "unit",
		values: CriticalUnits, exact: true,
		reason: "重启它可能让这台机器失联，或者让平台自己掉线",
	},
}

// DangerFindings 判定一次参数化动作是否命中危险参数，返回**全部**命中的规则。
//
// 返回列表而不是布尔：一条动作可能同时命中多条（例如写 /root/.ssh/authorized_keys
// 既在"系统会自执行的目录"里、又是"账户凭据文件"），界面要把理由都列出来。
func DangerFindings(kind string, params map[string]string) []DangerFinding {
	if len(params) == 0 {
		return nil
	}
	var out []DangerFinding
	for _, r := range dangerRules {
		if r.kind != kind {
			continue
		}
		v := strings.TrimSpace(params[r.param])
		if v == "" {
			continue
		}
		for _, want := range r.values {
			hit := v == want
			if !hit && !r.exact {
				hit = strings.HasPrefix(v, strings.TrimSuffix(want, "/")+"/")
			}
			if hit {
				out = append(out, DangerFinding{Rule: r.rule, Param: r.param, Value: v, Reason: r.reason})
				break // 同一规则只报一次（同一个值可能同时是某目录的子路径与其本身）
			}
		}
	}
	return out
}

// DangerRuleView 是一条规则对外的形态（下发给界面）。
//
// **必须带 Values**：界面要在**提交前**把危险理由列出来，没有清单就没法匹配。
// 这份清单不是秘密（它是策略，不是凭据），带出去换来的是"清单只有一份"——
// 两端各写一份名单必然漂移，而漂移的表现是"界面没提醒、服务端却拦了"这种难查的不一致。
type DangerRuleView struct {
	Rule   string   `json:"rule"`
	Kind   string   `json:"kind"`
	Param  string   `json:"param"`
	Values []string `json:"values"`
	// Exact 为 true 时按精确匹配（单元名、单个文件），false 时按路径前缀匹配（目录）。
	Exact  bool   `json:"exact"`
	Reason string `json:"reason"`
}

// DangerRules 返回全部规则的对外形态（供界面在提交前提示"哪些参数会被判为危险"）。
func DangerRules() []DangerRuleView {
	out := make([]DangerRuleView, 0, len(dangerRules))
	for _, r := range dangerRules {
		out = append(out, DangerRuleView{
			Rule: r.rule, Kind: r.kind, Param: r.param,
			Values: r.values, Exact: r.exact, Reason: r.reason,
		})
	}
	return out
}
