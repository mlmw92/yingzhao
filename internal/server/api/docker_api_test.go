package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mlmw92/yingzhao/internal/model"
	"github.com/mlmw92/yingzhao/internal/server/instancereg"
)

// Docker 管理面（ADR-0006）：全部同步直连 daemon，端点清单来自 Agent 上报。
//
// 与改动前的差别是本文件的核心：原先只有 /container/docker/hosts 是同步接口，
// 其余全走 ops 下行通道（延迟一个上报周期）。现在读写都是直连，因此这里测的是
// 「路由 + 权限 + 直连真的把请求打到了 daemon 上 + 地址不外泄」。

// setDockerEndpoints 注册端点并在用例结束后清掉。
//
// instancereg.Default 是包级单例：不清理会污染同包其它用例（它们共用这套夹具）。
func setDockerEndpoints(t *testing.T, node string, eps []model.DockerEndpoint) {
	t.Helper()
	instancereg.Default.SetDockerEndpoints(node, eps)
	t.Cleanup(func() { instancereg.Default.SetDockerEndpoints(node, nil) })
}

// GET /api/v1/container/docker/endpoints 的四条底线：
//  1. 权限点是 docker:read，**不能**被 container:read / middleware:read 顶替（Docker 与 K8s 是两个授权面）；
//  2. 按资源范围过滤——范围外的端点连名字都不返回；
//  3. 端点来自上报的**端点级**清单（一个 daemon 一条），不是容器级清单去重出来的；
//  4. 返回体里**不含 daemon 地址**——直连之后地址确实到了 Server 手上，
//     但它不该再往浏览器走一步（这是这次改动唯一新增的暴露面）。
func TestRoutes_DockerEndpoints(t *testing.T) {
	const (
		nodeA = "web-01"
		nodeB = "db-01"
	)
	setDockerEndpoints(t, nodeA, []model.DockerEndpoint{
		{Node: nodeA, Name: "local-docker", Addr: "tcp://10.9.9.9:2375", Reachable: true},
	})
	setDockerEndpoints(t, nodeB, []model.DockerEndpoint{
		{Node: nodeB, Name: "db-docker", Addr: "unix:///var/run/docker.sock", Reachable: false},
	})

	a := scopeTestAPI(t)
	mux := newRoutesMux(a)

	// 1) 别的只读权限点不得顶替 docker:read。
	for _, perm := range []string{"container:read", "middleware:read"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, reqWith(globalPrincipal(perm), http.MethodGet, "/api/v1/container/docker/endpoints", ""))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s 不得顶替 docker:read，code = %d，body = %s", perm, rec.Code, rec.Body.String())
		}
	}

	// 2) 有 docker:read 但范围只有 g1 → 只返回 web-01 上的端点。
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, reqWith(restrictedPrincipal([]string{"docker:read"}, "g1"), http.MethodGet, "/api/v1/container/docker/endpoints", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("范围内应放行，code = %d，body = %s", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	eps, _ := body["endpoints"].([]any)
	if len(eps) != 1 {
		t.Fatalf("范围外端点不得返回，实际 %v", body["endpoints"])
	}
	first, _ := eps[0].(map[string]any)
	if first["node"] != nodeA || first["name"] != "local-docker" {
		t.Fatalf("端点标识不符，实际 %v", first)
	}
	if first["reachable"] != true {
		t.Fatalf("可达性应取自上报，实际 %v", first["reachable"])
	}
	if first["unixSocket"] != false {
		t.Fatalf("tcp 地址不应被判为 unix socket，实际 %v", first["unixSocket"])
	}

	// 3) 全局身份看到两个端点；unix socket 要标出来（Server 与 daemon 不同机时连不上，
	//    那是部署形态问题，得在界面上先说清楚，而不是等人点进去才报错）。
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, reqWith(globalPrincipal("docker:read"), http.MethodGet, "/api/v1/container/docker/endpoints", ""))
	eps, _ = decodeBody(t, rec)["endpoints"].([]any)
	if len(eps) != 2 {
		t.Fatalf("全局身份应看到 2 个端点，实际 %d：%s", len(eps), rec.Body.String())
	}
	byName := map[string]map[string]any{}
	for _, h := range eps {
		m := h.(map[string]any)
		byName[m["name"].(string)] = m
	}
	if byName["db-docker"]["unixSocket"] != true {
		t.Fatalf("unix socket 端点应被标出，实际 %v", byName["db-docker"])
	}
	// 稳定排序：先按节点名（db-01 在 web-01 之前）。
	if eps[0].(map[string]any)["node"] != nodeB {
		t.Fatalf("应按节点名稳定排序（db-01 在前），实际 %v", eps[0])
	}

	// 4) 地址边界：两个端点各自的上报地址都不得出现在响应里。
	raw := rec.Body.String()
	for _, banned := range []string{"10.9.9.9", "2375", "/var/run/docker.sock", "docker.sock", "addr"} {
		if strings.Contains(strings.ToLower(raw), strings.ToLower(banned)) {
			t.Fatalf("端点清单不得含 %q：%s", banned, raw)
		}
	}
}

// 空结果必须是 `[]` 而不是 `null`：否则前端每个用到它的地方都要多写一层判空。
func TestRoutes_DockerEndpoints_EmptyIsArray(t *testing.T) {
	a := scopeTestAPI(t)
	mux := newRoutesMux(a)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, reqWith(globalPrincipal("docker:read"), http.MethodGet, "/api/v1/container/docker/endpoints", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("无端点配置也应 200，code = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"endpoints":[]`) {
		t.Fatalf("空结果应是 [] 而非 null，实际 %s", rec.Body.String())
	}
}

// 直连真的把请求打到了 daemon 上，并且把结果翻译成界面用的表格。
//
// 用一个假 daemon（httptest.Server）而不是打桩：这条用例要证明的正是"Server 会去连 daemon"，
// 打桩就把要验的那一步跳过去了。
func TestDockerContainers_DirectQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/containers/json" {
			http.NotFound(w, r)
			return
		}
		// 只读查询默认带 all=true（管理面默认要能看到已停止的容器）。
		if r.URL.Query().Get("all") != "true" {
			t.Errorf("默认应带 all=true，实际 %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"Id": "aaaaaaaaaaaa1111222233334444", "Names": []string{"/web-1"},
			"Image": "nginx:1.27", "State": "running", "Status": "Up 2 hours",
			"Created": 1700000000, "Ports": []any{},
		}})
	}))
	defer srv.Close()

	setDockerEndpoints(t, "web-01", []model.DockerEndpoint{
		{Node: "web-01", Name: "local-docker", Addr: srv.URL, Reachable: true},
	})

	a := scopeTestAPI(t)
	mux := newRoutesMux(a)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, reqWith(globalPrincipal("docker:read"), http.MethodGet,
		"/api/v1/container/docker/containers?node=web-01&name=local-docker", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("直连查询应成功，code = %d，body = %s", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	rows, _ := body["rows"].([]any)
	if len(rows) != 1 {
		t.Fatalf("应返回 1 个容器，实际 %v", body["rows"])
	}
	row, _ := rows[0].([]any)
	if len(row) == 0 || row[0] != "web-1" {
		t.Fatalf("首列应是容器名（去掉前导斜杠），实际 %v", row)
	}
	// 短 ID：与 `docker ps` 的显示口径一致（12 位）。
	if row[len(row)-1] != "aaaaaaaaaaaa" {
		t.Fatalf("ID 应截成 12 位，实际 %v", row[len(row)-1])
	}
}

// daemon 连不上时：502 + 一句能指导动作的话，且**不复述地址**。
//
// 用 unix socket 触发最常见的那种失败（Server 与 daemon 不同机）。
// 原始错误是 "connect: no such file or directory"，看起来像 daemon 挂了，
// 而实际是 Server 根本够不着那台机器的 socket——这句话必须由平台翻译出来。
func TestDockerContainers_UnreachableUnixSocketExplainsItself(t *testing.T) {
	setDockerEndpoints(t, "web-01", []model.DockerEndpoint{
		{Node: "web-01", Name: "local-docker", Addr: "unix:///definitely/not/here.sock", Reachable: true},
	})

	a := scopeTestAPI(t)
	mux := newRoutesMux(a)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, reqWith(globalPrincipal("docker:read"), http.MethodGet,
		"/api/v1/container/docker/containers?node=web-01&name=local-docker", ""))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("连不上应 502，code = %d，body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "unix socket") || !strings.Contains(body, "tcp://") {
		t.Fatalf("应说明是 unix socket 且给出可执行的建议，实际 %s", body)
	}
	if strings.Contains(body, "/definitely/not/here.sock") {
		t.Fatalf("错误信息不得复述 daemon 地址，实际 %s", body)
	}
}

// 端点定位是 (node, name) 二元组：只给 name 不够，也不该被含糊放过。
func TestDockerEndpoints_UnknownOrIncompleteIsRejected(t *testing.T) {
	setDockerEndpoints(t, "web-01", []model.DockerEndpoint{
		{Node: "web-01", Name: "local-docker", Addr: "tcp://127.0.0.1:2375"},
	})
	setDockerEndpoints(t, "db-01", []model.DockerEndpoint{
		{Node: "db-01", Name: "local-docker", Addr: "tcp://127.0.0.1:2376"},
	})

	a := scopeTestAPI(t)
	mux := newRoutesMux(a)

	cases := []struct {
		name string
		url  string
		code int
	}{
		// 同名端点存在于另一台机器上：必须按 node 区分，不能命中错的那台。
		{"同名不同机", "/api/v1/container/docker/containers?node=other-01&name=local-docker", http.StatusNotFound},
		{"不存在的端点", "/api/v1/container/docker/containers?node=web-01&name=nope", http.StatusNotFound},
		{"缺 name", "/api/v1/container/docker/containers?node=web-01", http.StatusBadRequest},
		{"缺 node", "/api/v1/container/docker/containers?name=local-docker", http.StatusBadRequest},
		// 范围外（web-01 属 g1，登录者只有 g2）→ 与"不存在"同一句话，不套出"这台机器上有这么个端点"。
		{"范围外", "/api/v1/container/docker/containers?node=web-01&name=local-docker", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 前四条用全局身份；最后一条用 g2 范围。
			p := globalPrincipal("docker:read")
			if tc.name == "范围外" {
				p = restrictedPrincipal([]string{"docker:read"}, "g2")
			}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, reqWith(p, http.MethodGet, tc.url, ""))
			if rec.Code != tc.code {
				t.Fatalf("期望 %d，实际 %d：%s", tc.code, rec.Code, rec.Body.String())
			}
		})
	}
}

// 读写是两个权限点，且各自独立：只读的顶替不了写，写的也顶替不了只读。
//
// 写操作（docker:write）是**超管专属**（用户 2026-10-10 拍板）；这条用例只验路由层的
// 权限门本身，超管专属那层在 auth 的权限目录里（见 catalog_test.go）。
func TestRoutes_DockerReadWritePermissionsAreSeparate(t *testing.T) {
	a := scopeTestAPI(t)
	mux := newRoutesMux(a)

	// 1) 只读接口：docker:write 不能顶替 docker:read。
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, reqWith(globalPrincipal("docker:write"), http.MethodGet,
		"/api/v1/container/docker/endpoints", ""))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("docker:write 不得顶替 docker:read，code = %d，body = %s", rec.Code, rec.Body.String())
	}

	// 2) 写接口：docker:read 不能顶替 docker:write。
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, reqWith(globalPrincipal("docker:read"), http.MethodPost,
		"/api/v1/container/docker/containers/web-1/stop?node=web-01&name=local-docker", ""))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("docker:read 不得顶替 docker:write，code = %d，body = %s", rec.Code, rec.Body.String())
	}

	// 3) 有 docker:write 时权限门放行——这里没有这个端点，因此落到 404（而不是 403）。
	//    两者混起来就分不清"没权限"和"没这个端点"。
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, reqWith(globalPrincipal("docker:write"), http.MethodPost,
		"/api/v1/container/docker/containers/web-1/stop?node=web-01&name=local-docker", ""))
	if rec.Code == http.StatusForbidden {
		t.Fatalf("有 docker:write 不应被权限门拦下，body = %s", rec.Body.String())
	}
}

// 容器标识会被拼进 daemon 的 URL 路径，因此路径里的值必须过白名单——
// 放斜杠进去等于开了一条改变请求路径的口子。
func TestDockerContainerID_RejectsPathSeparators(t *testing.T) {
	setDockerEndpoints(t, "web-01", []model.DockerEndpoint{
		{Node: "web-01", Name: "local-docker", Addr: "tcp://127.0.0.1:2375"},
	})

	a := scopeTestAPI(t)
	mux := newRoutesMux(a)

	// 用 %2F 让斜杠真的进到路径参数里（否则会被 ServeMux 当成段分隔符）。
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, reqWith(globalPrincipal("docker:read"), http.MethodGet,
		"/api/v1/container/docker/containers/a%2Fb?node=web-01&name=local-docker", ""))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("含斜杠的容器标识应 400，code = %d，body = %s", rec.Code, rec.Body.String())
	}
}

// 写操作的宽限期上限必须真的生效：越界报错而不是静默夹到上限
// （静默夹值会让人以为"我设了 300 秒"，而实际只等了 30 秒）。
func TestDockerStop_TimeoutOutOfRangeIsRejected(t *testing.T) {
	setDockerEndpoints(t, "web-01", []model.DockerEndpoint{
		{Node: "web-01", Name: "local-docker", Addr: "tcp://127.0.0.1:2375"},
	})

	a := scopeTestAPI(t)
	mux := newRoutesMux(a)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, reqWith(globalPrincipal("docker:write"), http.MethodPost,
		"/api/v1/container/docker/containers/web-1/stop?node=web-01&name=local-docker&timeout=9999", ""))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("超上限的宽限期应 400，code = %d，body = %s", rec.Code, rec.Body.String())
	}

	// start 不接受 timeout：参数被静默忽略比报错更坏。
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, reqWith(globalPrincipal("docker:write"), http.MethodPost,
		"/api/v1/container/docker/containers/web-1/start?node=web-01&name=local-docker&timeout=5", ""))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("start 带 timeout 应 400，code = %d，body = %s", rec.Code, rec.Body.String())
	}
}

// 不传 timeout 时不能透传 0：Docker API 把缺省的 t 当作 0（立即 SIGKILL），
// 而 CLI 默认是 10 秒——平台跟随 CLI，否则"停止容器"会变成"强杀"。
func TestDockerStopTimeoutDefaultIsGraceful(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/?node=web-01&name=local-docker", nil)
	rec := httptest.NewRecorder()
	got, ok := dockerIntParam(rec, req, "timeout", dockerDefaultStopTimeout, 0, dockerMaxStopTimeout)
	if !ok {
		t.Fatalf("默认值应被接受，code = %d", rec.Code)
	}
	if got != 10 {
		t.Fatalf("默认宽限期 = %d，want 10（0 会立即 SIGKILL）", got)
	}

	// 显式 0 仍然允许：那是一次有意的立即强杀。
	req = httptest.NewRequest(http.MethodPost, "/?timeout=0", nil)
	rec = httptest.NewRecorder()
	got, ok = dockerIntParam(rec, req, "timeout", dockerDefaultStopTimeout, 0, dockerMaxStopTimeout)
	if !ok || got != 0 {
		t.Fatalf("显式 0 应被接受，got=%d ok=%v", got, ok)
	}
}
