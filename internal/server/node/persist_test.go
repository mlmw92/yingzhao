package node

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/mlmw92/yingzhao/internal/model"
)

// fakePersist 记录写入次数，用来验证「注册信息变了才写、心跳走单独通道」。
type fakePersist struct {
	coalesce  bool
	metaSaves int
	flushes   [][]heartbeat
	lastMeta  metaFile
}

func (f *fakePersist) Load() (metaFile, error)    { return metaFile{}, nil }
func (f *fakePersist) SaveMeta(mf metaFile) error { f.metaSaves++; f.lastMeta = mf; return nil }
func (f *fakePersist) FlushHeartbeat(states []heartbeat) error {
	f.flushes = append(f.flushes, states)
	return nil
}
func (f *fakePersist) CoalesceHeartbeat() bool { return f.coalesce }
func (f *fakePersist) Name() string            { return "fake" }

func newTestManager(p Persist) *Manager {
	m := newManager(p, 60*time.Second)
	m.ensureDefaultGroup()
	return m
}

func report(node, group, ip string) *model.ReportPayload {
	return &model.ReportPayload{Node: node, Group: group, IP: ip, Mode: model.ModeCollect}
}

// 心跳不触发整份写：同一份注册信息重复上报只写一次（首次注册）。
func TestRegisterOnlyPersistsMetaOnRegistrationChange(t *testing.T) {
	p := &fakePersist{coalesce: true}
	m := newTestManager(p)
	p.metaSaves = 0 // 建默认分组那一次是初始化噪音

	m.Register(report("web-01", "g1", "10.0.0.1"))
	if p.metaSaves != 1 {
		t.Fatalf("首次注册应写一次 meta，实际 %d", p.metaSaves)
	}
	m.Register(report("web-01", "g1", "10.0.0.1"))
	m.Register(report("web-01", "g1", "10.0.0.1"))
	if p.metaSaves != 1 {
		t.Fatalf("注册信息未变不应再写，实际 %d", p.metaSaves)
	}

	// 分组由 Server 端管理：Agent 上报的 group 只在节点尚无分组时兜底，
	// 后续上报不会覆盖用户改过的分组（因此也不该触发写）。
	m.Register(report("web-01", "g2", "10.0.0.1"))
	if p.metaSaves != 1 {
		t.Fatalf("Agent 上报不应覆盖已设分组，实际写 %d 次", p.metaSaves)
	}
	// 界面改分组 → 注册信息变化 → 必须写
	if err := m.SetNodeGroup("web-01", "g2"); err != nil {
		t.Fatal(err)
	}
	if p.metaSaves != 2 {
		t.Fatalf("改分组应写 meta，实际 %d", p.metaSaves)
	}
}

// 文件后端不做心跳合并：每次上报都整份写（既有行为不变）。
func TestRegisterWritesEveryTimeForFilePersist(t *testing.T) {
	p := &fakePersist{coalesce: false}
	m := newTestManager(p)
	p.metaSaves = 0
	m.Register(report("web-01", "g1", "10.0.0.1"))
	m.Register(report("web-01", "g1", "10.0.0.1"))
	if p.metaSaves != 2 {
		t.Fatalf("文件后端应每次写，实际 %d", p.metaSaves)
	}
}

// FlushHeartbeats 把内存里的 last_seen/status 交给持久化层。
func TestFlushHeartbeats(t *testing.T) {
	p := &fakePersist{coalesce: true}
	m := newTestManager(p)
	m.Register(report("web-01", "g1", "10.0.0.1"))

	m.FlushHeartbeats()
	if len(p.flushes) != 1 || len(p.flushes[0]) != 1 {
		t.Fatalf("flushes = %+v", p.flushes)
	}
	got := p.flushes[0][0]
	if got.Hostname != "web-01" || got.Status != "online" || got.LastSeen == 0 {
		t.Fatalf("心跳内容 = %+v", got)
	}
}

// 文件后端：注册信息与心跳都能读回。
func TestFilePersistRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nodes.json")
	m := New(path, time.Minute)
	m.Register(report("web-01", "g1", "10.0.0.1"))
	m.AddGroup("g1", "web")

	reloaded := New(path, time.Minute)
	n, ok := reloaded.GetNode("web-01")
	if !ok || n.Group != "g1" || n.IP != "10.0.0.1" {
		t.Fatalf("重新加载 = %+v, %v", n, ok)
	}
	found := false
	for _, g := range reloaded.ListGroups() {
		if g.Name == "g1" {
			found = true
		}
	}
	if !found {
		t.Fatal("分组未持久化")
	}
}

// 删除节点后整份同步必须把它从持久化层删掉（否则重启后它又回来了）。
func TestRemoveNodeDeletesFromMeta(t *testing.T) {
	p := &fakePersist{coalesce: true}
	m := newTestManager(p)
	m.Register(report("web-01", "g1", "10.0.0.1"))
	m.RemoveNode("web-01")

	for _, n := range p.lastMeta.Nodes {
		if n.Hostname == "web-01" {
			t.Fatalf("已删除的节点仍在 meta 里: %+v", p.lastMeta.Nodes)
		}
	}
}

// 主机快照里有些字段每轮上报都在变（磁盘已用、在线用户）。它们不是注册信息，
// 不能让 Register 每次都触发整份 SaveMeta——那会把节点表打成写热点，
// 还会在写锁里同步做 N 条 upsert 的事务。
func TestRegisterIgnoresVolatileHostInfo(t *testing.T) {
	p := &fakePersist{coalesce: true}
	m := newTestManager(p)
	p.metaSaves = 0

	withInfo := func(diskUsed uint64, users []model.OnlineUser) *model.ReportPayload {
		r := report("web-01", "g1", "10.0.0.1")
		r.HostInfo = model.HostInfo{
			CPUModel: "x86", CPUCores: 8, MemoryTotal: 16 << 30, DiskTotal: 100 << 30,
			DiskUsed: diskUsed, BootTime: 1700000000, OnlineUsers: users,
			Disks: []model.DiskStat{{
				Mountpoint: "/", Device: "/dev/vda1", Fstype: "ext4",
				Total: 100 << 30, Used: diskUsed, Free: 100<<30 - diskUsed, UsedPercent: 40,
			}},
		}
		return r
	}

	m.Register(withInfo(40<<30, nil))
	if p.metaSaves != 1 {
		t.Fatalf("首次注册应写一次 meta，实际 %d", p.metaSaves)
	}
	// 只有磁盘已用与在线用户变了：不该再写。
	m.Register(withInfo(41<<30, []model.OnlineUser{{User: "alice", Terminal: "pts/0"}}))
	m.Register(withInfo(42<<30, []model.OnlineUser{{User: "bob", Terminal: "pts/1"}}))
	if p.metaSaves != 1 {
		t.Fatalf("只有磁盘/在线用户变化不应写 meta，实际 %d", p.metaSaves)
	}
	// 内存变了：这是注册级信息，必须写。
	r := withInfo(42<<30, nil)
	r.HostInfo.MemoryTotal = 32 << 30
	m.Register(r)
	if p.metaSaves != 2 {
		t.Fatalf("内存变化应写 meta，实际 %d", p.metaSaves)
	}
	// 内存中的快照仍应更新为最新值（读接口用的是内存，不是库）。
	got, ok := m.GetNode("web-01")
	if !ok || got.HostInfo.DiskUsed != 42<<30 {
		t.Fatalf("内存里的磁盘已用未更新: %+v", got)
	}
}
