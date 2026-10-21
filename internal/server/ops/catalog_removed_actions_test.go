package ops

import (
	"strings"
	"testing"

	"github.com/mlmw92/yingzhao/internal/model"
)

// 2026-10-10 重规划（ADR-0006）删除的动作**不得**回到目录里。
//
// 为什么值得一条守卫：这些动作被删的原因不是"没人用"，而是"它们与 Server 直连通路
// 语义重复、且慢一个上报周期"。如果将来有人顺手把某个 docker.* 动作加回来，
// 同一个页面上就会同时存在两条数据通路——一条实时、一条延迟，而使用者无从分辨
// 自己看到的是哪条。这种"多一条路"的退化不会报错，只会让人越来越不信界面上的数字。
func TestCatalog_RemovedActionsStayRemoved(t *testing.T) {
	removed := []string{
		"container.workloads", "container.pods", "container.describe",
		"container.events", "container.logs",
		"docker.info", "docker.containers", "docker.inspect", "docker.logs", "docker.images",
		"docker.start", "docker.stop", "docker.restart", "docker.rm", "docker.image.rm",
	}
	for _, kind := range removed {
		if a, ok := Lookup(kind); ok {
			t.Errorf("动作 %s 已被 Server 直连取代，不应回到动作目录（当前分组 %q）", kind, a.Group)
		}
	}
	// 分组也随之清空：目录里不该留下一个没有任何动作的分组——
	// 那会让界面上出现一个点不开的标签页。
	for _, a := range Catalog() {
		switch a.Group {
		case "容器", "Docker", "Docker 操作":
			t.Errorf("分组 %q 内不应再有动作（该能力已改由 Server 直连）：%s", a.Group, a.Kind)
		}
	}
}

// 容器 / 镜像标识的字符集：这个值会被拼进 Docker Engine API 的 URL 路径，
// 放进斜杠、问号或空格就等于给了一条改变请求语义的口子。
//
// 直连之后（ADR-0006）这条约束由 api/docker_api.go 的 dockerIDFromPath 承担，
// 但正则本身仍定义在 model 里、由两侧共用，因此这里继续钉住它的字符集。
func TestCatalog_DockerIDPattern(t *testing.T) {
	for _, v := range []string{"", "a b", "--host", "a/b", "a?b", "a#b", "../etc"} {
		if model.OpsDockerIDPattern.MatchString(v) {
			t.Errorf("容器标识不应接受 %q", v)
		}
	}
	for _, v := range []string{"local-docker", "docker_1", "web-1", strings.Repeat("a", 64)} {
		if !model.OpsDockerIDPattern.MatchString(v) {
			t.Errorf("容器标识应接受 %q", v)
		}
	}
}

// Perm 只有两种形态：空（只走入口 ops:exec）或 `域:动作`。写成别的样子说明拼错了。
func TestCatalog_PermShape(t *testing.T) {
	for _, a := range Catalog() {
		if a.Perm == "" {
			continue
		}
		parts := strings.Split(a.Perm, ":")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			t.Errorf("动作 %s 的 Perm 形态不对：%q（应为 域:动作）", a.Kind, a.Perm)
		}
	}
}
