package model

import "testing"

func rulesOf(fs []DangerFinding) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Rule)
	}
	return out
}

func TestDangerFindings_PushIntoPersistenceDir(t *testing.T) {
	for _, path := range []string{
		"/etc/systemd/system/backdoor.service",
		"/etc/cron.d/x",
		"/etc/profile.d/evil.sh",
		"/root/.ssh/authorized_keys", // 同时也是敏感文件，下面单独验"多条"
	} {
		fs := DangerFindings(OpsKindFilePush, map[string]string{"path": path})
		if len(fs) == 0 {
			t.Fatalf("%s 应被判为危险（写进去会被系统自己执行）", path)
		}
	}
}

func TestDangerFindings_PushSensitiveFile(t *testing.T) {
	fs := DangerFindings(OpsKindFilePush, map[string]string{"path": "/etc/shadow"})
	if len(fs) != 1 || fs[0].Rule != "push-sensitive-file" {
		t.Fatalf("/etc/shadow 应命中敏感文件规则，实际 %v", rulesOf(fs))
	}
	if fs[0].Reason == "" {
		t.Fatal("命中必须带人话理由——说不清理由的告警没人会看")
	}
}

// 一条动作可能同时命中多条：界面要把理由**都**列出来。
func TestDangerFindings_MultipleRules(t *testing.T) {
	fs := DangerFindings(OpsKindFilePush, map[string]string{"path": "/root/.ssh/authorized_keys"})
	if len(fs) != 2 {
		t.Fatalf("/root/.ssh/authorized_keys 应同时命中「目录」与「敏感文件」两条，实际 %v", rulesOf(fs))
	}
}

// **决定性负例**：普通路径不能被判为危险——误报会让人绕过审批。
func TestDangerFindings_NoFalsePositiveOnNormalPath(t *testing.T) {
	for _, path := range []string{
		"/opt/app/conf/app.conf",
		"/tmp/yingzhao-filepush/e2e.txt",
		"/etc/nginx/nginx.conf",         // 在 /etc 下但不是系统会自执行的目录
		"/etc/systemd/systemd-foo/x",    // 前缀相近但不是那个目录（边界必须按目录段比）
		"/etc/systemd/system-backup/x",  // 同上
		"/etc/cron.daily-backup/x",      // 同上
		"/home/deploy/.ssh/authorized_keys", // 别人的 authorized_keys 不是"这台机器的入口"
	} {
		if fs := DangerFindings(OpsKindFilePush, map[string]string{"path": path}); len(fs) > 0 {
			t.Fatalf("%s 不该被判为危险，实际命中 %v（理由：%s）", path, rulesOf(fs), fs[0].Reason)
		}
	}
}

func TestDangerFindings_RestartCriticalUnit(t *testing.T) {
	for _, unit := range []string{"sshd.service", "systemd-networkd.service", "monitor-agent.service"} {
		fs := DangerFindings(OpsKindSvcRestart, map[string]string{"unit": unit})
		if len(fs) != 1 || fs[0].Rule != "restart-critical-unit" {
			t.Fatalf("%s 应被判为关键单元，实际 %v", unit, rulesOf(fs))
		}
	}
}

// **决定性负例**：普通业务服务重启必须放行——把它们也列为危险，
// 结果是每次重启都弹警告，最后没人看警告。
func TestDangerFindings_NoFalsePositiveOnNormalUnit(t *testing.T) {
	for _, unit := range []string{"nginx.service", "mysql.service", "yingzhao-app.service", "redis@6379.service"} {
		if fs := DangerFindings(OpsKindSvcRestart, map[string]string{"unit": unit}); len(fs) > 0 {
			t.Fatalf("普通服务 %s 不该被判为危险（否则警告会被无视）", unit)
		}
	}
}

func TestDangerFindings_OtherKindsAndEmpty(t *testing.T) {
	if fs := DangerFindings(OpsKindNodeDiagnostics, nil); fs != nil {
		t.Fatalf("无参数的动作不该有判定，实际 %v", fs)
	}
	if fs := DangerFindings(OpsKindSvcRestart, map[string]string{}); fs != nil {
		t.Fatalf("空参数不该有判定，实际 %v", fs)
	}
	// 只读查询即使参数看着敏感也不判：它们不改机器
	if fs := DangerFindings(OpsKindSvcStatus, map[string]string{"unit": "sshd.service"}); fs != nil {
		t.Fatalf("只读查询不该被判为危险，实际 %v", fs)
	}
}

// 清单自洽：每条规则都要有理由、有取值，且理由不能是空话。
func TestDangerRules_AreSelfConsistent(t *testing.T) {
	if len(dangerRules) == 0 {
		t.Fatal("规则清单不该为空")
	}
	for _, r := range dangerRules {
		if r.rule == "" || r.kind == "" || r.param == "" {
			t.Fatalf("规则缺少标识：%+v", r)
		}
		if len(r.values) == 0 {
			t.Fatalf("规则 %s 没有取值清单", r.rule)
		}
		if len(r.reason) < 8 {
			t.Fatalf("规则 %s 的理由太短（等于没说）：%q", r.rule, r.reason)
		}
	}
	// 界面提示用的形态也要能拿到
	if len(DangerRules()) != len(dangerRules) {
		t.Fatalf("DangerRules() 应返回全部规则")
	}
}

// 目录清单必须与 Agent 侧 guardcheck 用的是同一份（本文件就是那份的唯一定义处）。
func TestPersistenceDirs_NotEmptyAndAbsolute(t *testing.T) {
	if len(PersistenceDirs) < 10 {
		t.Fatalf("目录清单看着被截断了：%v", PersistenceDirs)
	}
	for _, d := range PersistenceDirs {
		if len(d) < 2 || d[0] != '/' {
			t.Fatalf("必须是绝对路径：%q", d)
		}
	}
}
