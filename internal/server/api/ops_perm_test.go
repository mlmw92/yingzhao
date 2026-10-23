package api

import (
	"testing"

	"github.com/mlmw92/yingzhao/internal/server/auth"
	"github.com/mlmw92/yingzhao/internal/server/ops"
)

// 动作目录自带的域权限点（Action.Perm）必须真的存在于权限目录里。
//
// 这是决策点 D4（权限点写在动作目录里、提交时统一校验）的配套守卫。
// 把权限点写进目录之后，**拼错一个字母的症状是"该动作永远 403 且没人知道为什么"**
// ——`checkPerm` 只比对 Principal 有没有这个键，不会报"这个权限点不存在"。
// 因此必须有一条用例把动作目录与权限目录两侧对起来。
func TestOpsCatalog_PermKeysExistInPermissionCatalog(t *testing.T) {
	known := map[string]bool{}
	for _, d := range auth.PermissionCatalog() {
		for _, it := range d.Items {
			known[it.Key] = true
		}
	}
	checked := 0
	for _, a := range ops.Catalog() {
		if a.Perm == "" {
			continue
		}
		checked++
		if !known[a.Perm] {
			t.Errorf("动作 %s 要求权限点 %q，但它不在权限目录里（拼错了？）", a.Kind, a.Perm)
		}
	}
	// 2026-10-23：容器写动作移出下行目录后（改由 Server 直连执行，权限在路由层校验），
	// 目录里暂时没有带 Perm 的动作。检查循环保留：将来再有动作声明 Perm 时，
	// 拼错依然会被这条用例挡下——只是今天它没有可检查的对象。
	if checked == 0 {
		t.Log("动作目录当前没有带 Perm 的动作（域权限点已改由路由层校验）")
	}
}
