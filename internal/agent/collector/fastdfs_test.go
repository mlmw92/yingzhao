package collector

import (
	"context"
	"testing"

	"github.com/mlmw92/yingzhao/internal/model"
)

// TestFastDFSExporterCarriesInstanceLabels 锁定 exporter 模式下实例配置里的
// role / group 会写进指标 labels。
//
// 为什么重要：receiver 只会给**缺失 group** 的指标补节点分组（payload.Group）。
// exporter 模式此前不打 role/group，于是实例配置里的 group1 被静默改成节点分组
// default——界面上看到的分组不是用户配的那个。
func TestFastDFSExporterCarriesInstanceLabels(t *testing.T) {
	url := expositionServer(t, "fastdfs_up 1\nfastdfs_storage_count 1\n").URL
	metrics, _ := NewFastDFSCollector("node-1", []model.FastDFSInstanceConfig{{
		Name: "fs", Addr: "10.0.0.10:23000", Role: "storage", Group: "group1", ExporterURL: url,
	}}).CollectCtx(context.Background())

	if len(metrics) == 0 {
		t.Fatal("exporter 模式没有产出指标")
	}
	for _, m := range metrics {
		if got := m.Labels["group"]; got != "group1" {
			t.Errorf("%s group = %q，want group1（配置里的实例分组丢了）", m.Name, got)
		}
		if got := m.Labels["role"]; got != "storage" {
			t.Errorf("%s role = %q，want storage", m.Name, got)
		}
		if got := m.Labels["addr"]; got != "10.0.0.10:23000" {
			t.Errorf("%s addr = %q，want 10.0.0.10:23000", m.Name, got)
		}
	}
}
