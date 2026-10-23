package ops

import (
	"testing"

	"github.com/mlmw92/yingzhao/internal/model"
)

// 2026-10-23 决定：Agent 只负责采集，K8s 写操作改由 Server 直连 apiserver。
// 因此这三个动作**必须从下行动作目录里消失**——留在目录里意味着还能创建一条
// 注定没人执行的下行任务（Agent 已经不再实现它们）。
func TestCatalog_ContainerWritesAreNotDownlinkActions(t *testing.T) {
	for _, kind := range []string{
		model.OpsKindContainerRestart,
		model.OpsKindContainerScale,
		model.OpsKindContainerDelete,
	} {
		if _, ok := Lookup(kind); ok {
			t.Fatalf("%s 不应再登记为下行动作（改由 Server 直连执行）", kind)
		}
	}
	for _, a := range Catalog() {
		if a.Group == "容器操作" {
			t.Fatalf("「容器操作」分组应已移除，仍存在: %s", a.Kind)
		}
	}
}
