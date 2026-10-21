package receiver

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/mlmw92/yingzhao/internal/model"
	"github.com/mlmw92/yingzhao/internal/server/asset"
	"github.com/mlmw92/yingzhao/internal/server/config"
	"github.com/mlmw92/yingzhao/internal/server/node"
)

// 中间件集群归属：实例 → 集群的 member_of。
//
// 覆盖四个必须做对、且都不报错的点：
//  1. **只有权威集群名才建集群**：ES 的 cluster_name / MongoDB 的 setName / MySQL GR 的组名
//     来自被采集方；其余类型（以及这三类里没报名的实例）**不建集群**——
//     拿 Agent 本地别名（各采集器填的 cfg.Name）冒充身份，等于把两批机器并成一个集群；
//  2. **归属节点落空串**（集群跨主机、没有单一归属节点），因此它对受限用户不可见——
//     所以实例上必须留一份 `cluster` 属性，否则受限用户什么都看不到；
//  3. **幂等**：同一份清单连报两轮，集群资产与边都不增加；
//  4. **同一集群的多个成员只落一份集群资产**。
func TestHandleReportWritesMiddlewareClusterMembership(t *testing.T) {
	dir := t.TempDir()
	store, err := asset.Open(filepath.Join(dir, "assets.db"))
	if err != nil {
		t.Fatalf("打开资产库失败: %v", err)
	}
	defer store.Close()
	svc := asset.NewService(store)

	mgr := node.New(filepath.Join(dir, "nodes.json"), time.Minute)
	rec := New(&assetTestStorage{}, mgr, config.AgentAuthConfig{}, nil, nil, nil, nil)
	rec.SetAssetService(svc)

	const grName = "aaaaaaaa-1111-2222-3333-444444444444"
	payload := model.ReportPayload{
		Node:  "web-01",
		Group: "g1",
		MySQLInstances: []model.MySQLInstance{
			{Instance: "10.0.0.10:3306", Name: "mysql-1", Node: "web-01", Role: "PRIMARY",
				Topology: "cluster", Group: "mygr", ClusterName: grName, Up: true},
			{Instance: "10.0.0.11:3306", Name: "mysql-2", Node: "web-02", Role: "SECONDARY",
				Topology: "cluster", Group: "mygr", ClusterName: grName, Up: true},
		},
		MongoDBInstances: []model.MongoDBInstance{
			{Instance: "10.0.0.12:27017", Name: "mongo-1", Node: "web-01", Role: "PRIMARY",
				Topology: "replicaset", Group: "rs0", ClusterName: "rs0", Up: true},
			// 单机 Mongo：hello 里没有 setName → ClusterName 为空 → 不该建集群
			{Instance: "10.0.0.13:27017", Name: "mongo-standalone", Node: "web-01", Role: "STANDALONE",
				Topology: "standalone", Group: "standalone-alias", Up: true},
		},
		RedisInstances: []model.RedisInstance{
			// redis 只有本地别名，没有权威集群名 → 不该建集群
			{Instance: "10.0.0.14:6379", Name: "redis-1", Node: "web-01", Group: "redis-alias", Up: true},
		},
	}
	postReport(t, rec, payload)
	postReport(t, rec, payload) // 第二轮：验证幂等

	// 1) MySQL GR：一份集群资产（两个成员共用一个组名）
	grRef := asset.Ref{TypeKey: asset.TypeMiddlewareCluster,
		NaturalKey: asset.MiddlewareClusterNaturalKey("mysql", grName)}
	gr, ok, err := svc.Get(grRef)
	if err != nil || !ok {
		t.Fatalf("组复制集群资产未写入: ok=%v err=%v", ok, err)
	}
	if gr.Node != "" {
		t.Fatalf("集群跨主机，归属节点应落空串（仅全局范围可见），实际 %q", gr.Node)
	}
	if v, _ := gr.ValueFrom("type", asset.SourceDiscovery); v != "mysql" {
		t.Fatalf("集群的 type 属性应为 mysql，实际 %q", v)
	}
	links, err := svc.Links(grRef)
	if err != nil {
		t.Fatalf("查询集群关联失败: %v", err)
	}
	if len(links) != 2 {
		t.Fatalf("两个成员应各有一条 member_of（幂等后仍为 2），实际 %d：%+v", len(links), links)
	}
	for _, l := range links {
		if l.Kind != asset.LinkMemberOf {
			t.Fatalf("关系种类应为 member_of，实际 %q", l.Kind)
		}
		if l.Source != asset.SourceDiscovery {
			t.Fatalf("自动建立的边来源应为 discovery，实际 %q", l.Source)
		}
		if l.To.NaturalKey != grRef.NaturalKey {
			t.Fatalf("边的方向应是「实例 → 集群」，实际对端 %q", l.To.NaturalKey)
		}
	}

	// 2) MongoDB 副本集：同样一份资产、一条边
	rsRef := asset.Ref{TypeKey: asset.TypeMiddlewareCluster,
		NaturalKey: asset.MiddlewareClusterNaturalKey("mongodb", "rs0")}
	if _, ok, err := svc.Get(rsRef); err != nil || !ok {
		t.Fatalf("副本集集群资产未写入: ok=%v err=%v", ok, err)
	}

	// 3) **不猜**：别名不是身份——这两条都不该出现集群资产
	for _, key := range []string{
		asset.MiddlewareClusterNaturalKey("redis", "redis-alias"),
		asset.MiddlewareClusterNaturalKey("mongodb", "standalone-alias"),
		asset.MiddlewareClusterNaturalKey("mysql", "mygr"), // 本地别名也不是身份
	} {
		if _, ok, err := svc.Get(asset.Ref{TypeKey: asset.TypeMiddlewareCluster, NaturalKey: key}); err != nil || ok {
			t.Fatalf("拿 Agent 本地别名建了集群（%s）: ok=%v err=%v", key, ok, err)
		}
	}

	// 4) 实例上的 cluster 属性：集群资产对受限用户不可见，这条属性是补偿
	instRef := asset.Ref{TypeKey: asset.TypeMiddlewareInst, NaturalKey: "mysql:10.0.0.10:3306"}
	inst, ok, err := svc.Get(instRef)
	if err != nil || !ok {
		t.Fatalf("实例资产未写入: ok=%v err=%v", ok, err)
	}
	if v, _ := inst.ValueFrom("cluster", asset.SourceDiscovery); v != grName {
		t.Fatalf("实例上应带 cluster 属性（值为权威集群名），实际 %q", v)
	}
	if v, _ := inst.ValueFrom("group", asset.SourceDiscovery); v != "mygr" {
		t.Fatalf("group 属性应保持 Agent 本地别名 mygr（两者不是一个东西），实际 %q", v)
	}
}

// 集群类型是稳定对象，**不进** EphemeralTypes（成员全消失就是真失联）。
func TestMiddlewareClusterTypeIsNotEphemeral(t *testing.T) {
	for _, k := range asset.EphemeralTypes() {
		if k == asset.TypeMiddlewareCluster {
			t.Fatalf("中间件集群是稳定对象，不应被当作短命对象排除在台账与健康度之外")
		}
	}
	if asset.MiddlewareClusterNaturalKey(" elasticsearch ", " prod ") != "elasticsearch:prod" {
		t.Fatalf("自然键应做首尾去空白：%q", asset.MiddlewareClusterNaturalKey(" elasticsearch ", " prod "))
	}
}
