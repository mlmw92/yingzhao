package main

import (
	"strings"
	"testing"
)

const sampleOutput = `[2026-10-19 15:19:35] DEBUG - base_path=/var/local/fdfs/storage, connect_timeout=30, network_timeout=60, tracker_server_count=1
server_count=1, server_index=0
tracker server is 10.0.0.10:22122
group count: 1

Group 1:
group name = group1
disk total space = 60358 MB
disk free space = 24671 MB
trunk free space = 0 MB
storage server count = 1
active server count = 1
storage server port = 23000

	Storage 1:
		id = 10.0.0.10
		ip_addr = 10.0.0.10 (VM-0-10-ubuntu.local)  ACTIVE
		total storage = 60358 MB
		free storage = 24671 MB
`

func TestParseSingleGroup(t *testing.T) {
	s, err := parse(sampleOutput)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if s.Groups != 1 || s.Storages != 1 || s.Active != 1 {
		t.Errorf("groups/storages/active = %d/%d/%d，want 1/1/1", s.Groups, s.Storages, s.Active)
	}
	if s.TotalMB != 60358 || s.FreeMB != 24671 || s.TrunkFreeMB != 0 {
		t.Errorf("space = total:%d free:%d trunk:%d，want 60358/24671/0", s.TotalMB, s.FreeMB, s.TrunkFreeMB)
	}
}

func TestParseMultipleGroupsSums(t *testing.T) {
	out := sampleOutput + `
Group 2:
group name = group2
disk total space = 100 MB
disk free space = 40 MB
trunk free space = 5 MB
storage server count = 2
active server count = 1
`
	s, err := parse(out)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if s.Groups != 1 { // group count: 只出现一次，是总数
		t.Errorf("groups = %d，want 1", s.Groups)
	}
	if s.TotalMB != 60458 || s.FreeMB != 24711 || s.TrunkFreeMB != 5 {
		t.Errorf("space 未按 group 累加: total:%d free:%d trunk:%d", s.TotalMB, s.FreeMB, s.TrunkFreeMB)
	}
	if s.Storages != 3 || s.Active != 2 {
		t.Errorf("storages/active = %d/%d，want 3/2", s.Storages, s.Active)
	}
}

func TestParseUnrecognizedOutputFails(t *testing.T) {
	for _, out := range []string{"", "connection refused", "some unrelated log"} {
		if _, err := parse(out); err == nil {
			t.Errorf("parse(%q) 应返回错误：无法识别的输出不能被当成健康", out)
		}
	}
}

func TestRenderContract(t *testing.T) {
	body := render(stats{Groups: 1, Storages: 2, Active: 1, TotalMB: 100, FreeMB: 40, TrunkFreeMB: 5})
	wants := []string{
		"fastdfs_up 1",
		"fastdfs_group_count 1",
		"fastdfs_storage_count 2",
		"fastdfs_storage_online_count 1",
		"fastdfs_storage_offline_count 1", // 2-1
		"fastdfs_total_space 104857600",   // 100 MB
		"fastdfs_free_space 41943040",     // 40 MB
		"fastdfs_used_space 62914560",     // 60 MB（总-空闲）
		"fastdfs_trunk_free_space 5242880",
	}
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("渲染结果缺少 %q\n%s", want, body)
		}
	}
	// 磁盘 IO 与网络累计值 fdfs_monitor 不提供，不能上报 0 假装有数据
	for _, absent := range []string{"fastdfs_disk_read_bytes", "fastdfs_net_recv_bytes"} {
		if strings.Contains(body, absent) {
			t.Errorf("不应输出 %s：fdfs_monitor 不提供该指标", absent)
		}
	}
}

func TestCommandModes(t *testing.T) {
	bin, args := command(config{mode: "local", monitorBin: "fdfs_monitor", clientConf: "/etc/fdfs/client.conf"})
	if bin != "fdfs_monitor" || len(args) != 1 || args[0] != "/etc/fdfs/client.conf" {
		t.Errorf("local 模式命令 = %s %v", bin, args)
	}

	bin, args = command(config{mode: "docker", container: "mw-fastdfs", monitorBin: "fdfs_monitor", clientConf: "/etc/fdfs/client.conf"})
	if bin != "docker" || strings.Join(args, " ") != "exec mw-fastdfs fdfs_monitor /etc/fdfs/client.conf" {
		t.Errorf("docker 模式命令 = %s %v", bin, args)
	}
}

func TestCollectFailureReportsOffline(t *testing.T) {
	body := collect(config{mode: "local", monitorBin: "/nonexistent/fdfs_monitor", clientConf: "/etc/fdfs/client.conf"})
	if !strings.Contains(body, "fastdfs_up 0") {
		t.Errorf("采集失败应上报 fastdfs_up 0，实际：%s", body)
	}
}
