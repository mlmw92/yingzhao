package ops

import (
	"os/exec"
	"sort"
	"strings"

	"github.com/nebula/monitor/internal/agent/config"
	"github.com/nebula/monitor/internal/model"
)

// 本文件处理一件事：**把「两个护栏配在一起就等于任意代码执行」这个隐性依赖显式化**。
//
// 为什么需要它：file.push 的护栏是「允许写的目录前缀」，svc.restart 的护栏是「允许重启的单元清单」，
// 各自看都正常、各自都有测试。但若同一台机器同时允许「写某个可重启服务的可执行文件所在目录」
// 与「重启该服务」，那么"推一个文件 + 重启"就等于任意代码执行 —— **不需要任何"任意命令"能力**。
// 配置的人不会意识到自己配出了一个 RCE，界面上也没有任何提示（这条组合此前没有任何检查）。
//
// 还有一类更短的路：系统**自己会执行**的目录（systemd 单元目录、cron 目录、profile.d、
// ld.so.conf.d、sudoers.d、root 的 authorized_keys）。往这些地方写文件**连重启都不需要**，
// 等下一次登录 / 下一次 cron / 下一次动态链接即可生效，所以它们与 units 清单无关，单独检查。

// GuardRisk 已搬到 `model.GuardRisk`：它要随上报体序列化送出去，而界面要用同一句话描述，
// 因此放在两端都引用的包里（`Describe()` 的注释本来就写着"日志与界面共用"）。
// 这里保留别名只是为了少改调用点，类型定义只有 `model` 那一份。
type GuardRisk = model.GuardRisk

// persistenceDirs 是「系统自己会执行其内容」的目录，已**收敛到** `model.PersistenceDirs`。
//
// 收敛的理由：Server 侧的"危险参数"判定（`model.DangerFindings`）要用**同一份**清单——
// 两端各写一份，迟早会出现"Agent 认为是持久化目录、Server 不认为"这种自相矛盾。
// 正则在 `model/ops.go` 已经是这个先例（两端共用一份）。
var persistenceDirs = model.PersistenceDirs

// CheckGuardRisks 检查本机护栏的组合是否等价于任意代码执行，返回风险清单（空 = 没命中）。
//
// 刻意**只告警、不拒绝**：这两项都是用户明确写下的同意（他同意写那个目录、也同意重启那个单元），
// 系统替他悄悄禁掉其中一个，比让风险可见更难排查，也违背「机器自身的同意优先于中心授权」。
// 把组合说出来，由他决定收掉哪一个 —— 本函数因此只做判定，不做任何拦截。
//
// executableOf 由调用方注入（真实实现是 SystemdExecutables），便于测试；
// 返回空表示"这个单元的可执行文件在哪我们不知道"，此时只跳过 exec 类检查——
// **不命中不等于安全**，所以 persistence 类的检查与它无关地照常执行。
func CheckGuardRisks(g config.OpsGuards, executableOf func(unit string) []string) []GuardRisk {
	if !g.File.OpsFileEnabled() {
		return nil
	}
	dirs := g.File.OpsAllowedDirs()
	forms := allowedDirForms(dirs)
	var out []GuardRisk

	// ① 系统自己会执行的目录：与"能不能重启服务"无关
	for _, dir := range dirs {
		for _, exposed := range persistenceDirs {
			if inAllowedDirOrSelf(exposed, []string{dir}) {
				out = append(out, GuardRisk{Kind: model.GuardRiskPersistence, AllowedDir: dir, Exposed: exposed})
			}
		}
	}

	// ② 可重启服务的可执行文件，落在允许写入的目录里
	if g.Write {
		// 单元集合是 map，遍历顺序随机；先排序再处理，让日志与用例的输出稳定可比
		// （每次启动打印顺序都不一样的话，diff 日志时会以为是配置变了）。
		units := make([]string, 0, len(g.Units))
		for unit := range g.OpsAllowedUnits() {
			units = append(units, unit)
		}
		sort.Strings(units)
		for _, unit := range units {
			for _, exe := range executableOf(unit) {
				if dir, ok := coveringDir(exe, forms); ok {
					out = append(out, GuardRisk{Kind: model.GuardRiskExec, AllowedDir: dir, Exposed: exe, Unit: unit})
				}
			}
		}
	}
	return out
}

// coveringDir 返回覆盖该路径的允许目录（取第一个命中的形态，含软链解析后的形态）。
func coveringDir(target string, dirs []string) (string, bool) {
	for _, d := range dirs {
		if strings.HasPrefix(target, d+"/") {
			return d, true
		}
	}
	return "", false
}

// SystemdExecutables 返回某 systemd 单元实际执行的可执行文件/脚本路径（取 ExecStart 与 ExecReload）。
//
// 读不到就返回 nil（非 systemd 机器、单元不存在、systemctl 不可用）：
// 这时"可执行文件在哪"无从得知，于是 ② 类检查不命中——**宁可少报，也不要按单元名猜路径**，
// 猜错的结果是没人会去核这条告警。
func SystemdExecutables(unit string) []string {
	out, err := exec.Command("systemctl", "show", "-p", "ExecStart", "-p", "ExecReload", unit).Output()
	if err != nil {
		return nil
	}
	return parseExecPaths(string(out))
}

// parseExecPaths 从 systemctl show 的输出里取 path= 的值。
//
// 形态：ExecStart={ path=/usr/sbin/nginx ; argv[]=/usr/sbin/nginx -g "daemon off;" ; ignore_errors=no ; ... }
// 一个单元可能有多个 ExecStart/ExecReload 行，逐个取。
func parseExecPaths(out string) []string {
	var res []string
	for _, line := range strings.Split(out, "\n") {
		i := strings.Index(line, "path=")
		if i < 0 {
			continue
		}
		rest := line[i+len("path="):]
		if j := strings.Index(rest, " ;"); j >= 0 {
			rest = rest[:j]
		}
		if rest = strings.TrimSpace(rest); rest != "" {
			res = append(res, rest)
		}
	}
	return res
}
