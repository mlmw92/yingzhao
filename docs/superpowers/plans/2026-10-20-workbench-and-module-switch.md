# 值班台与模块开关（阶段 1）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 用「模块开关 + 值班台」把 22 个平铺菜单收成按需开启的模块，并把「我该做什么」提到首页。

**Architecture:** 开关复用 `uicfg`（`ui.yaml` 的 `disabled` 关闭清单），前端用一份模块目录驱动侧栏、命令面板、路由守卫与设置页；后端在 `ui` 接口做不可关闭项校验，并让周期任务在模块关闭时跳过。值班台新增聚合接口 `GET /api/v1/workbench/summary`，首页把「待我处理」固定置顶。

**Tech Stack:** Go（标准库 + 现有 API 框架）、Vue 3 + Element Plus、Vitest。

**Spec:** `docs/superpowers/specs/2026-10-19-workbench-and-module-switch-design.md`

## Global Constraints

- 开关语义是**关闭清单**：未配置 = 全开；升级新增页面自动可见。
- 不可关闭清单（硬编码）：`overview`、`alerts`、`alerting`（整域）、`system`。
- 关闭只隐藏入口并停止对应定时任务，**不注销后端路由、不删除数据**。
- 定时任务的跳过按正常结果记日志（`slog.Info("模块已关闭，跳过执行", "module", key)`），不打 error。
- 侧栏分组默认全部展开，**不得回退成按当前路由折叠**。
- 中文注释；注释只写「为什么」。

## Review Focus

- **旧配置文件兼容**：`ui.yaml` 没有 `disabled` 字段时必须是全开，升级后不能出现「新功能不可见」。
- **部分更新不能清空其它字段**：只保存模块开关时，名称/Logo/副标题必须保持原值。
- **深链不能绕过开关**：已关闭页面的路由必须重定向首页并给出原因。
- **单段失败不能伪装成 0**：工作台某段查询失败要进 `errors`，界面不能显示「0 条待办」。
- **手动触发不受开关影响**：开关只停自动调度；用户手动执行巡检/报告仍应成功。

---

## Task 1: uicfg 开关字段、不可关闭校验与 PUT 部分更新

**Files:**
- Modify: `internal/server/uicfg/uiconfig.go`
- Modify: `internal/server/api/ui.go`
- Create: `internal/server/uicfg/uiconfig_test.go`
- Create: `internal/server/api/ui_test.go`

**Interfaces:**
- Produces: `UIConfig.Disabled []string`、`uicfg.LockedModuleKeys`、`(*Manager).Disabled(key string) bool`。
- Consumes: 无。

- [ ] **Step 1: 写失败测试（uicfg）**

```go
package uicfg

import (
	"path/filepath"
	"testing"
)

func TestSaveRejectsLockedModules(t *testing.T) {
	m, err := New(filepath.Join(t.TempDir(), "ui.yaml"), DefaultUIConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, key := range []string{"overview", "alerts", "alerting", "system"} {
		cfg := m.Get()
		cfg.Disabled = []string{key}
		if err := m.Save(cfg); err == nil {
			t.Errorf("%q 不应允许关闭", key)
		}
	}
}

func TestSaveAllowsClosableModules(t *testing.T) {
	m, err := New(filepath.Join(t.TempDir(), "ui.yaml"), DefaultUIConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cfg := m.Get()
	cfg.Disabled = []string{"assets", "logs"}
	if err := m.Save(cfg); err != nil {
		t.Fatalf("可关闭模块不应被拒: %v", err)
	}
	if !m.Disabled("assets") || m.Disabled("hosts") {
		t.Fatalf("Disabled 判定错误: %+v", m.Get().Disabled)
	}
}

func TestOldConfigWithoutDisabledIsAllEnabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ui.yaml")
	// 旧版本文件没有 disabled 字段
	if err := writeFile(path, "name: 英招\nsubtitle: 运维平台\n"); err != nil {
		t.Fatal(err)
	}
	m, err := New(path, DefaultUIConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if len(m.Get().Disabled) != 0 {
		t.Fatalf("缺字段应解析为空（=全开），实际 %v", m.Get().Disabled)
	}
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
```

（`writeFile` 需要 `os` 导入，补上。）

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/server/uicfg/ -v`
Expected: FAIL（`Disabled` 未定义）。

- [ ] **Step 3: 实现**

`uiconfig.go` 的 `UIConfig` 增加：

```go
	// Disabled 是已关闭的模块或页面 key（见前端 modules/catalog.js）。
	//
	// **用关闭清单而不是开启清单**：全量开启时不需要任何配置，
	// 且升级新增的页面会自动出现；开启清单会让存量部署升级后静默看不到新功能。
	Disabled []string `yaml:"disabled" json:"disabled,omitempty"`
```

新增：

```go
// LockedModuleKeys 是不可关闭的模块与页面：
// overview/alerts 是「发现与处置」主线的两端，system 是唯一能改回配置的入口，
// alerting 整域不可关（组内单页仍可关）
var LockedModuleKeys = map[string]bool{
	"overview": true,
	"alerts":   true,
	"alerting": true,
	"system":   true,
}

// Disabled 判断某个模块/页面是否被关闭。
func (m *Manager) Disabled(key string) bool {
	if key == "" {
		return false
	}
	for _, k := range m.cfg.Disabled {
		if k == key {
			return true
		}
	}
	return false
}

// normalizeDisabled 去空白、去重，并拒绝不可关闭项。
func normalizeDisabled(list []string) ([]string, error) {
	out := make([]string, 0, len(list))
	seen := map[string]bool{}
	for _, raw := range list {
		k := strings.TrimSpace(raw)
		if k == "" || seen[k] {
			continue
		}
		if LockedModuleKeys[k] {
			return nil, fmt.Errorf("模块 %q 不允许关闭", k)
		}
		seen[k] = true
		out = append(out, k)
	}
	return out, nil
}
```

`Save` 在写盘前调用：

```go
	normalized, err := normalizeDisabled(c.Disabled)
	if err != nil {
		return err
	}
	c.Disabled = normalized
```

- [ ] **Step 4: 运行 uicfg 测试**

Run: `go test ./internal/server/uicfg/ -v`
Expected: PASS。

- [ ] **Step 5: 写 API 部分更新的失败测试**

```go
package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/nebula/monitor/internal/server/uicfg"
)

func uiTestAPI(t *testing.T) *API {
	t.Helper()
	a := scopeTestAPI(t)
	m, err := uicfg.New(filepath.Join(t.TempDir(), "ui.yaml"),
		uicfg.UIConfig{Name: "英招", Subtitle: "运维平台"})
	if err != nil {
		t.Fatalf("uicfg.New: %v", err)
	}
	a.uiMgr = m
	return a
}

func TestUIPutPartialUpdateKeepsBrand(t *testing.T) {
	a := uiTestAPI(t)
	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("system:config"), http.MethodPut,
		"/api/v1/ui/settings", `{"disabled":["assets"]}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d（body=%s）", rec.Code, rec.Body.String())
	}
	cfg := a.uiMgr.Get()
	if cfg.Name != "英招" || cfg.Subtitle != "运维平台" {
		t.Fatalf("部分更新不应清空品牌字段: %+v", cfg)
	}
	if len(cfg.Disabled) != 1 || cfg.Disabled[0] != "assets" {
		t.Fatalf("disabled 未保存: %+v", cfg.Disabled)
	}
}

func TestUIPutRejectsLockedModule(t *testing.T) {
	a := uiTestAPI(t)
	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(globalPrincipal("system:config"), http.MethodPut,
		"/api/v1/ui/settings", `{"disabled":["alerts"]}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d，want 400（body=%s）", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 6: 运行确认失败**

Run: `go test ./internal/server/api/ -run TestUIPut -v`
Expected: FAIL（部分更新后 Name 变成默认值/空）。

- [ ] **Step 7: 实现 PUT 部分更新**

`internal/server/api/ui.go` 的 `handleUIPut` 改为指针合并：

```go
func (a *API) handleUIPut(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     *string   `json:"name"`
		Logo     *string   `json:"logo"`
		Footer   *string   `json:"footer"`
		Subtitle *string   `json:"subtitle"`
		Disabled *[]string `json:"disabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	// 部分更新：未出现的字段保持原值。
	// 此前整体替换导致「只保存模块开关」把名称/Logo/副标题一起清空，
	// 以及保存站点设置会丢副标题（前端发了、后端没接）。
	cfg := a.uiMgr.Get()
	if body.Name != nil {
		cfg.Name = *body.Name
	}
	if body.Logo != nil {
		cfg.Logo = *body.Logo
	}
	if body.Footer != nil {
		cfg.Footer = *body.Footer
	}
	if body.Subtitle != nil {
		cfg.Subtitle = *body.Subtitle
	}
	if body.Disabled != nil {
		cfg.Disabled = *body.Disabled
	}
	if err := a.uiMgr.Save(cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "config": a.uiMgr.Get()})
}
```

- [ ] **Step 8: 运行测试**

Run: `go test ./internal/server/api/ -run TestUIPut -v && go test ./internal/server/uicfg/ -count=1`
Expected: PASS。

- [ ] **Step 9: 提交**

```bash
git add internal/server/uicfg internal/server/api/ui.go internal/server/api/ui_test.go
git commit -m "feat: uicfg 支持模块关闭清单与部分更新"
```

---

## Task 2: 前端模块目录、开关 store、侧栏与路由守卫

**Files:**
- Create: `web/src/modules/catalog.js`
- Create: `web/src/composables/useModules.js`
- Modify: `web/src/components/Sidebar.vue:162-241`
- Modify: `web/src/router/index.js`
- Modify: `web/src/components/CommandPalette.vue`
- Create: `web/src/modules/modules.test.js`

**Interfaces:**
- Produces: `moduleGroups`、`pageDomain`、`MODULE_LOCKED`（catalog）；`useModules()`（store）。
- Consumes: `GET /api/v1/ui/settings` 返回的 `disabled`。

- [ ] **Step 1: 写失败测试**

```js
import { describe, it, expect, vi, beforeEach } from 'vitest'
import http from '../api/http'

vi.mock('../api/http', () => ({
  default: { get: vi.fn() },
}))

import { moduleGroups, pageDomain, isLocked } from './catalog'
import { useModules } from '../composables/useModules'

describe('模块目录与开关 store', () => {
  it('目录包含六个域，页面能映射回所属域', () => {
    expect(moduleGroups.map((g) => g.key)).toEqual([
      'monitoring', 'assets', 'ops', 'alerting', 'security', 'system',
    ])
    expect(pageDomain['logs']).toBe('monitoring')
    expect(pageDomain['assets']).toBe('assets')
  })

  it('不可关闭项与设计稿一致', () => {
    expect(isLocked('overview')).toBe(true)
    expect(isLocked('alerts')).toBe(true)
    expect(isLocked('alerting')).toBe(true)
    expect(isLocked('system')).toBe(true)
    expect(isLocked('assets')).toBe(false)
  })

  it('关闭清单同时按页面与域生效', async () => {
    http.get.mockResolvedValue({ disabled: ['assets', 'logs'] })
    const m = useModules()
    await m.load()
    expect(m.isVisible('assets', 'assets')).toBe(false)
    expect(m.isVisible('logs', 'monitoring')).toBe(false)
    expect(m.isVisible('hosts', 'monitoring')).toBe(true)
  })
})
```

- [ ] **Step 2: 运行确认失败**

Run: `cd web && ./node_modules/.bin/vitest run src/modules/modules.test.js`
Expected: FAIL（模块不存在）。

- [ ] **Step 3: 建目录文件**

`web/src/modules/catalog.js`：把 `Sidebar.vue` 里的 `groups` 原样搬过来（含图标导入），并追加：

```js
export const moduleGroups = groups

// pageDomain 把页面 key 映射到所属域，路由守卫与命令面板共用。
export const pageDomain = Object.fromEntries(
  groups.flatMap((g) => g.items.map((i) => [i.key, g.key])),
)

// 不可关闭项：值班台、告警中心、告警运维整域、系统设置。
// 与后端 uicfg.LockedModuleKeys 保持一致。
const LOCKED = new Set(['overview', 'alerts', 'alerting', 'system'])
export function isLocked(key) {
  return LOCKED.has(key)
}

// 首页不在 groups 里，单独补上域映射
pageDomain['overview'] = ''
```

`Sidebar.vue` 删除内联 `groups` 与本文件不再使用的图标导入，改为：

```js
import { moduleGroups as groups } from '../modules/catalog'
```

- [ ] **Step 4: 建 `useModules.js`**

```js
import { ref } from 'vue'
import http from '../api/http'

// 模块级单例：侧栏、路由守卫、命令面板、设置页共用同一份关闭清单，
// 避免各自拉一次接口后出现「菜单隐藏了但深链还能进」。
const disabled = ref([])
const loaded = ref(false)

let inflight = null

export function useModules() {
  async function load(force = false) {
    if (loaded.value && !force) return
    if (inflight) return inflight
    inflight = http
      .get('/api/v1/ui/settings')
      .then((cfg) => {
        disabled.value = cfg.disabled || []
        loaded.value = true
      })
      .catch(() => {
        // 拉取失败按全开处理：宁可多显示，也不要把功能藏起来
        disabled.value = []
      })
      .finally(() => {
        inflight = null
      })
    return inflight
  }

  function isDisabled(key) {
    return !!key && disabled.value.includes(key)
  }

  // 页面与所在域任一被关即不可见
  function isVisible(pageKey, domainKey) {
    return !isDisabled(pageKey) && !isDisabled(domainKey)
  }

  return { disabled, loaded, load, isDisabled, isVisible }
}
```

- [ ] **Step 5: 侧栏按开关过滤**

`Sidebar.vue` 的 `isItemVisible` 增加域参数与开关判定：

```js
const modules = useModules()
onMounted(() => modules.load())

function isItemVisible(group, sub) {
  if (!modules.isVisible(sub.key, group.key)) return false
  if (!sub.perm) return true
  return auth.can(sub.perm)
}

const visibleGroups = computed(() =>
  groups
    .filter((g) => !modules.isDisabled(g.key))
    .map((g) => ({ ...g, items: g.items.filter((sub) => isItemVisible(g, sub)) }))
    .filter((g) => g.items.length > 0),
)
```

- [ ] **Step 6: 路由守卫**

`router/index.js`：给每条子路由加 `meta.module`（页面 key）与 `meta.domain`（域 key），例如：

```js
{ path: 'hosts', name: 'hosts', component: ..., meta: { perm: 'nodes:read', module: 'hosts', domain: 'monitoring' } },
{ path: 'assets', name: 'assets', component: ..., meta: { perm: 'assets:read', module: 'assets', domain: 'assets' } },
{ path: 'node/:name', name: 'node', props: true, component: ..., meta: { perm: 'nodes:read', module: 'hosts', domain: 'monitoring' } },
```

`overview` 的 `meta` 为 `{ module: 'overview', domain: '' }`；其余按 `catalog.js` 的 `pageDomain` 逐一补齐（27 条子路由）。

守卫增加：

```js
import { useModules } from '../composables/useModules'

const modules = useModules()

router.beforeEach(async (to) => {
  if (to.meta.public) return true
  await modules.load()
  if (to.meta.module && !modules.isVisible(to.meta.module, to.meta.domain)) {
    // 深链/收藏夹也要拦住，否则用户以为开关没生效
    ElMessage.info('该模块已关闭，可在「系统设置 → 功能开关」中开启')
    return { path: '/' }
  }
  if (to.meta.perm) {
    if (auth.principal.loaded && !auth.can(to.meta.perm)) return { path: '/' }
  }
  return true
})
```

（`ElMessage` 从 `element-plus` 导入。）

- [ ] **Step 7: 命令面板过滤**

`CommandPalette.vue` 的 `routeItems` 计算属性（约 230 行）在权限判定之后加一行：

```js
const modules = useModules()
// 与侧栏同一份判定：关闭的模块不出现在搜索结果里
if (c.meta && c.meta.module && !modules.isVisible(c.meta.module, c.meta.domain)) continue
```

并在 `<script setup>` 的导入区加：

```js
import { useModules } from '../composables/useModules'
```

- [ ] **Step 8: 运行测试**

Run: `cd web && ./node_modules/.bin/vitest run src/modules/modules.test.js src/components/Sidebar.test.js src/components/CommandPalette.test.js`
Expected: PASS（Sidebar / CommandPalette 既有用例不得回归）。

- [ ] **Step 9: 提交**

```bash
git add web/src/modules web/src/composables/useModules.js web/src/components/Sidebar.vue web/src/router/index.js web/src/components/CommandPalette.vue
git commit -m "feat(web): 模块目录与开关联动侧栏、路由与命令面板"
```

---

## Task 3: 设置页「功能开关」

**Files:**
- Create: `web/src/components/settings/ModuleSettingsSubView.vue`
- Create: `web/src/components/settings/ModuleSettingsSubView.test.js`
- Modify: `web/src/components/settings/SettingsView.vue:51-62`

**Interfaces:**
- Consumes: `GET/PUT /api/v1/ui/settings`、`moduleGroups`、`isLocked`、`useModules().load(true)`。
- Produces: 设置页 `功能开关` 分区。

- [ ] **Step 1: 写失败测试**

```js
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import http from '../../api/http'
import ModuleSettingsSubView from './ModuleSettingsSubView.vue'

vi.mock('../../api/http', () => ({
  default: { get: vi.fn(), put: vi.fn() },
}))

beforeEach(() => {
  vi.clearAllMocks()
  http.get.mockResolvedValue({ name: '英招', subtitle: '运维平台', disabled: ['assets'] })
  http.put.mockResolvedValue({ ok: true })
})

describe('ModuleSettingsSubView', () => {
  it('列出各域与页面，不可关闭项置灰并说明原因', async () => {
    const w = mount(ModuleSettingsSubView, { global: { plugins: [ElementPlus] } })
    await flushPromises()
    expect(w.text()).toContain('观测监控')
    expect(w.text()).toContain('资产与配置')
    expect(w.text()).toContain('告警中心')
    expect(w.text()).toContain('不可关闭')
  })

  it('保存只提交 disabled，不覆盖其它字段', async () => {
    const w = mount(ModuleSettingsSubView, { global: { plugins: [ElementPlus] } })
    await flushPromises()
    const btn = w.findAll('button').find((b) => b.text().includes('保存'))
    await btn.trigger('click')
    await flushPromises()
    expect(http.put).toHaveBeenCalledWith('/api/v1/ui/settings', { disabled: ['assets'] })
  })
})
```

- [ ] **Step 2: 运行确认失败**

Run: `cd web && ./node_modules/.bin/vitest run src/components/settings/ModuleSettingsSubView.test.js`
Expected: FAIL（组件不存在）。

- [ ] **Step 3: 实现组件**

```vue
<template>
  <div class="module-settings">
    <el-alert
      type="info"
      :closable="false"
      show-icon
      title="关闭仅隐藏入口并停止对应定时任务，不影响已产生的数据，也不改变接口可达性。"
    />
    <div v-for="g in moduleGroups" :key="g.key" class="module-group">
      <div class="group-head">
        <strong>{{ g.label }}</strong>
        <el-switch
          :model-value="!disabled.includes(g.key)"
          :disabled="isLocked(g.key)"
          @update:model-value="(v) => toggleDomain(g.key, v)"
        />
        <span v-if="isLocked(g.key)" class="locked-reason">不可关闭</span>
      </div>
      <div class="items">
        <el-checkbox
          v-for="item in g.items"
          :key="item.key"
          :model-value="!disabled.includes(item.key)"
          :disabled="isLocked(item.key) || disabled.includes(g.key)"
          @update:model-value="(v) => togglePage(item.key, v)"
        >{{ item.label }}</el-checkbox>
      </div>
    </div>
    <el-button type="primary" :loading="saving" @click="save">保存</el-button>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import http from '../../api/http'
import { moduleGroups, isLocked } from '../../modules/catalog'
import { useModules } from '../../composables/useModules'

const disabled = ref([])
const saving = ref(false)
const modules = useModules()

async function loadConfig() {
  const cfg = await http.get('/api/v1/ui/settings')
  disabled.value = [...(cfg.disabled || [])]
}

onMounted(loadConfig)

function toggleDomain(key, on) {
  if (isLocked(key)) return
  const set = new Set(disabled.value)
  on ? set.delete(key) : set.add(key)
  disabled.value = [...set]
}

function togglePage(key, on) {
  if (isLocked(key)) return
  const set = new Set(disabled.value)
  on ? set.delete(key) : set.add(key)
  disabled.value = [...set]
}

async function save() {
  saving.value = true
  try {
    await http.put('/api/v1/ui/settings', { disabled: disabled.value })
    await modules.load(true)
    ElMessage.success('功能开关已保存')
  } catch (e) {
    ElMessage.error('保存失败' + (e?.message ? '：' + e.message : ''))
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.module-group { margin: 16px 0; padding: 12px 14px; background: var(--el-fill-color-lighter); border-radius: 8px; }
.group-head { display: flex; align-items: center; gap: 12px; margin-bottom: 8px; }
.locked-reason { color: var(--t3); font-size: var(--fs-sm); }
.items { display: flex; flex-wrap: wrap; gap: 8px 16px; }
</style>
```

- [ ] **Step 4: 注册分区**

`SettingsView.vue`：导入组件并加入 `panes`（放在 `brand` 之后）：

```js
import ModuleSettingsSubView from './ModuleSettingsSubView.vue'
// panes 中：
{ name: 'modules', label: '功能开关', comp: ModuleSettingsSubView },
```

- [ ] **Step 5: 运行测试**

Run: `cd web && ./node_modules/.bin/vitest run src/components/settings/ModuleSettingsSubView.test.js src/components/settings/SettingsView.test.js`
Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add web/src/components/settings
git commit -m "feat(web): 设置页新增功能开关"
```

---

## Task 4: 周期任务跟随模块开关

**Files:**
- Modify: `internal/server/asset/inspect_schedule.go:437-447`
- Modify: `internal/server/report/schedule.go:315-327`
- Modify: `internal/server/dialtest/scheduler.go:89`
- Modify: `cmd/server/main.go`（接线）
- Create: `internal/server/asset/module_gate_test.go`
- Create: `internal/server/report/module_gate_test.go`

**Interfaces:**
- Consumes: `uicfg.Manager.Disabled(key string) bool`。
- Produces: 三个调度器的 `SetModuleGate(func(string) bool)`。

- [ ] **Step 1: 写失败测试（以巡检为例）**

```go
package asset

import "testing"

func TestInspectTickSkipsWhenModuleDisabled(t *testing.T) {
	s := &InspectScheduler{}
	s.SetModuleGate(func(key string) bool { return key == "inspect" })
	if !s.moduleDisabled() {
		t.Fatal("inspect 模块关闭时应跳过自动巡检")
	}
}
```

report 同理：

```go
package report

import "testing"

func TestReportTickSkipsWhenModuleDisabled(t *testing.T) {
	s := &Scheduler{}
	s.SetModuleGate(func(key string) bool { return key == "report" })
	if !s.moduleDisabled() {
		t.Fatal("report 模块关闭时应跳过自动报告")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/server/asset/ ./internal/server/report/ -run ModuleDisabled -v`
Expected: FAIL（`SetModuleGate` / `moduleDisabled` 未定义）。

- [ ] **Step 3: 实现三个调度器的闸门**

每个调度器结构体加字段与两个方法（以巡检为例）：

```go
	// moduleGate 查询「该模块是否被关闭」；nil 表示不限制（测试与旧调用方）。
	moduleGate func(string) bool

func (s *InspectScheduler) SetModuleGate(g func(string) bool) { s.moduleGate = g }

func (s *InspectScheduler) moduleDisabled() bool {
	return s.moduleGate != nil && s.moduleGate("inspect")
}
```

`tick()` 开头：

```go
	if s.moduleDisabled() {
		slog.Info("模块已关闭，跳过执行", "module", "inspect")
		return
	}
```

report 的 `tick()` 开头同样插入，key 为 `"report"`；dialtest 在 `runOnce()` 开头插入，key 为 `"dialtest"`：

```go
	if s.moduleDisabled() {
		slog.Info("模块已关闭，跳过执行", "module", "dialtest")
		return
	}
```

**手动触发不受影响**：闸门只加在 `tick()` / `runOnce()` 这类自动路径，`RunNow` 原样保留。

- [ ] **Step 4: 接线**

`cmd/server/main.go` 在 `uiMgr` 构造之后（三处调度器创建之后）加：

```go
	// 模块开关：关闭的模块跳过自动调度（手动触发仍可执行）
	if uiMgr != nil {
		gate := uiMgr.Disabled
		if inspectSched != nil {
			inspectSched.SetModuleGate(gate)
		}
		if reportSched != nil {
			reportSched.SetModuleGate(gate)
		}
		dialtestSched.SetModuleGate(gate)
	}
```

- [ ] **Step 5: 运行测试**

Run: `go test ./internal/server/asset/ ./internal/server/report/ ./internal/server/dialtest/ -count=1 && go build ./...`
Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add internal/server/asset internal/server/report internal/server/dialtest cmd/server/main.go
git commit -m "feat: 关闭的模块跳过自动巡检、报告与拨测"
```

---

## Task 5: 值班台聚合接口

**Files:**
- Create: `internal/server/api/workbench_api.go`
- Create: `internal/server/api/workbench_api_test.go`
- Modify: `internal/server/api/query.go:330`（注册路由）

**Interfaces:**
- Consumes: `a.alerts`、`a.approval`、`a.approvalInScope`、`a.unacknowledgedAlerts`、`filterByNodeScope`、`Principal(r)`。
- Produces: `GET /api/v1/workbench/summary`，响应：

```json
{"todos":[{"kind":"alert","available":true,"count":7,"items":[...]}],
 "errors":[{"kind":"approval","message":"查询审批单失败"}]}
```

- [ ] **Step 1: 写失败测试**

```go
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nebula/monitor/internal/model"
)

func TestWorkbenchSummary_SegmentsByPermission(t *testing.T) {
	a := scopeTestAPI(t)
	a.alerts = &alertStoreStub{active: []model.AlertEvent{{
		ID: "e1", RuleID: "r1", RuleName: "cpu", Node: "web-01",
		Severity: model.SeverityWarning, State: model.AlertStateFiring,
	}}}
	// 没有 approval:read 的身份：审批段必须是 available=false，而不是 count=0
	p := globalPrincipal("alerts:read")
	rec := httptest.NewRecorder()
	newRoutesMux(a).ServeHTTP(rec, reqWith(p, http.MethodGet, "/api/v1/workbench/summary", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d（body=%s）", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	todos := body["todos"].([]any)
	if len(todos) != 2 {
		t.Fatalf("应返回两段待办，实际 %d", len(todos))
	}
	approval := todos[1].(map[string]any)
	if approval["available"] != false {
		t.Fatalf("无权限时 approved 段必须 available=false，实际 %v", approval)
	}
	alert := todos[0].(map[string]any)
	if alert["count"].(float64) != 1 {
		t.Fatalf("告警段 count = %v，want 1", alert["count"])
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/server/api/ -run TestWorkbenchSummary -v`
Expected: FAIL（404）。

- [ ] **Step 3: 实现**

```go
package api

import (
	"net/http"

	"github.com/nebula/monitor/internal/model"
)

type workbenchTodoItem struct {
	ID        string         `json:"id"`
	Title     string         `json:"title"`
	Node      string         `json:"node,omitempty"`
	Instance  string         `json:"instance,omitempty"`
	Severity  model.Severity `json:"severity,omitempty"`
	Message   string         `json:"message,omitempty"`
	CreatedAt int64          `json:"createdAt,omitempty"`
}

type workbenchTodo struct {
	Kind      string               `json:"kind"`
	Available bool                 `json:"available"`
	Count     int                  `json:"count"`
	Items     []workbenchTodoItem  `json:"items"`
}

type workbenchError struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

// handleWorkbenchSummary 聚合「需要人做决定才能推进」的事项。
//
// 三条硬要求：
//   - available 与 count=0 分开：没权限/未启用审批不等于「没人提交」；
//   - 单段失败不拖垮整体，但失败要进 errors，界面不能显示成 0 条；
//   - 范围收窄在服务端做，不依赖前端过滤。
func (a *API) handleWorkbenchSummary(w http.ResponseWriter, r *http.Request) {
	p := Principal(r)
	resp := struct {
		Todos  []workbenchTodo  `json:"todos"`
		Errors []workbenchError `json:"errors"`
	}{Todos: make([]workbenchTodo, 0, 2)}

	// 段 1：待确认告警
	alertSeg := workbenchTodo{Kind: "alert", Items: []workbenchTodoItem{}}
	if !p.HasPermission("alerts:read") {
		alertSeg.Available = false
	} else {
		alertSeg.Available = true
		events := filterByNodeScope(a, p, a.unacknowledgedAlerts(a.alerts.Active()),
			func(e model.AlertEvent) string { return e.Node })
		alertSeg.Count = len(events)
		for i, e := range events {
			if i >= 10 {
				break
			}
			alertSeg.Items = append(alertSeg.Items, workbenchTodoItem{
				ID: e.ID, Title: e.RuleName, Node: e.Node, Instance: e.Instance,
				Severity: e.Severity, Message: e.Message, CreatedAt: e.StartsAt,
			})
		}
	}
	resp.Todos = append(resp.Todos, alertSeg)

	// 段 2：待我审批
	approvalSeg := workbenchTodo{Kind: "approval", Items: []workbenchTodoItem{}}
	if !p.HasPermission("approval:read") || !a.approvalReadyQuiet() {
		approvalSeg.Available = false
	} else {
		approvalSeg.Available = true
		a.approval.ExpireOverdue()
		me := AuthenticatedUser(r)
		list := a.approval.List(approvalFilterPending(me))
		for _, it := range list {
			if !a.approvalInScope(p, it) {
				continue
			}
			if approvalSeg.Count < 10 {
				approvalSeg.Items = append(approvalSeg.Items, workbenchTodoItem{
					ID: it.ID, Title: it.Title, Node: it.Node, CreatedAt: it.CreatedAt,
				})
			}
			approvalSeg.Count++
		}
	}
	resp.Todos = append(resp.Todos, approvalSeg)

	writeJSON(w, http.StatusOK, resp)
}
```

需要补两个小辅助（放在同文件）：

```go
// approvalReadyQuiet 只回答「审批功能是否可用」，不写响应体（聚合接口自行决定如何降级）。
func (a *API) approvalReadyQuiet() bool {
	return a.approval != nil
}

// approvalFilterPending 与 handleApprovalList 的默认口径一致：待我审批。
func approvalFilterPending(me string) approval.Filter {
	return approval.Filter{State: approval.StatePending, ExcludeRequester: me}
}
```

（需导入 `internal/server/approval`。）

路由注册（`query.go` 告警路由旁）：

```go
	mux.HandleFunc("GET /api/v1/workbench/summary", a.permit(a.handleWorkbenchSummary, "alerts:read"))
```

> 权限点挂 `alerts:read` 作为入口；审批段在段内再按 `approval:read` 收窄。

- [ ] **Step 4: 运行测试**

Run: `go test ./internal/server/api/ -run TestWorkbenchSummary -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/server/api/workbench_api.go internal/server/api/workbench_api_test.go internal/server/api/query.go
git commit -m "feat(api): 值班台聚合接口"
```

---

## Task 6: 首页「待我处理」与区块重排

**Files:**
- Create: `web/src/components/overview/WorkbenchTodos.vue`
- Create: `web/src/components/overview/WorkbenchTodos.test.js`
- Modify: `web/src/components/overview/useOverviewLayout.js:7-18`、`:20`
- Modify: `web/src/components/OverviewView.vue:30-75`、`:91` 附近

**Interfaces:**
- Consumes: `GET /api/v1/workbench/summary`。
- Produces: 首页固定置顶区块 `todos`。

- [ ] **Step 1: 写失败测试**

```js
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import http from '../../api/http'
import WorkbenchTodos from './WorkbenchTodos.vue'

vi.mock('../../api/http', () => ({
  default: { get: vi.fn() },
}))

beforeEach(() => {
  vi.clearAllMocks()
  http.get.mockResolvedValue({
    todos: [
      { kind: 'alert', available: true, count: 7, items: [{ id: 'e1', title: 'CPU 高', node: 'web-01' }] },
      { kind: 'approval', available: false, count: 0, items: [] },
    ],
    errors: [],
  })
})

describe('WorkbenchTodos', () => {
  it('展示待确认告警数量，无权限的段显示无权限而不是 0', async () => {
    const w = mount(WorkbenchTodos, { global: { plugins: [ElementPlus] } })
    await flushPromises()
    expect(w.text()).toContain('待确认告警')
    expect(w.text()).toContain('7')
    expect(w.text()).toContain('无权限')
  })

  it('没有待办时显示明确空态', async () => {
    http.get.mockResolvedValue({
      todos: [{ kind: 'alert', available: true, count: 0, items: [] },
              { kind: 'approval', available: true, count: 0, items: [] }],
      errors: [],
    })
    const w = mount(WorkbenchTodos, { global: { plugins: [ElementPlus] } })
    await flushPromises()
    expect(w.text()).toContain('当前没有需要你处理的事项')
  })
})
```

- [ ] **Step 2: 运行确认失败**

Run: `cd web && ./node_modules/.bin/vitest run src/components/overview/WorkbenchTodos.test.js`
Expected: FAIL。

- [ ] **Step 3: 实现组件**

```vue
<template>
  <div class="wb-todos">
    <div v-for="t in todos" :key="t.kind" class="wb-row">
      <span class="wb-label">{{ labelOf(t.kind) }}</span>
      <span v-if="!t.available" class="wb-muted">无权限或未启用</span>
      <span v-else-if="t.count === 0" class="wb-muted">无</span>
      <router-link v-else :to="linkOf(t.kind)" class="wb-count">{{ t.count }} 条 →</router-link>
    </div>
    <div v-if="allClear" class="wb-empty">当前没有需要你处理的事项</div>
    <div v-if="errors.length" class="wb-errors">
      <span v-for="e in errors" :key="e.kind">{{ e.message }}</span>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import http from '../../api/http'

const todos = ref([])
const errors = ref([])
const loaded = ref(false)

const allClear = computed(
  () => loaded.value && errors.value.length === 0 &&
    todos.value.every((t) => !t.available || t.count === 0),
)

function labelOf(kind) {
  return kind === 'alert' ? '待确认告警' : '待我审批'
}
function linkOf(kind) {
  return kind === 'alert' ? '/alerts' : '/approvals'
}

onMounted(async () => {
  try {
    const data = await http.get('/api/v1/workbench/summary')
    todos.value = data.todos || []
    errors.value = data.errors || []
  } catch (e) {
    errors.value = [{ kind: 'summary', message: '待办加载失败' }]
  } finally {
    loaded.value = true
  }
})
</script>

<style scoped>
.wb-row { display: flex; align-items: center; gap: 12px; padding: 6px 0; }
.wb-label { color: var(--t2); }
.wb-count { color: var(--accent); font-weight: 600; }
.wb-muted { color: var(--t3); }
.wb-empty { color: var(--t3); padding: 6px 0; }
.wb-errors { color: var(--danger); font-size: var(--fs-sm); }
</style>
```

- [ ] **Step 4: 布局调整**

`useOverviewLayout.js`：

```js
const defaultBlocks = [
  { key: 'todos', title: '待我处理', span: 12, visible: true },
  { key: 'health', title: '系统健康度', span: 4, visible: true },
  { key: 'kpi', title: '关键指标', span: 4, visible: true },
  { key: 'criticalAlerts', title: '紧急告警', span: 4, visible: true },
  { key: 'hostOverview', title: '主机概览', span: 12, visible: true },
  { key: 'middleware', title: '中间件概览', span: 12, visible: true },
  { key: 'intelligence', title: '智能运维', span: 12, visible: true },
  { key: 'recentAlerts', title: '最近告警', span: 12, visible: true },
]
const PINNED_KEYS = ['todos']
```

`OverviewView.vue`：
- `compMap` 增加 `todos: markRaw(WorkbenchTodos)`；
- 删除 `b.key === 'intelligence'` 的固定置顶注释与逻辑（现由 `todos` 置顶）；
- 智能运维的「默认收起」**已经实现**（`intelCollapsed` 初值就是 `true`），本任务不需要改；只需把它从 `PINNED_KEYS` 移出，让其按默认顺序排在「中间件概览」之后。

- [ ] **Step 5: 运行测试**

Run: `cd web && ./node_modules/.bin/vitest run src/components/overview src/components/OverviewView.test.js`
Expected: PASS（`overview.test.js` 若断言旧顺序需同步改为新顺序）。

- [ ] **Step 6: 提交**

```bash
git add web/src/components/overview web/src/components/OverviewView.vue
git commit -m "feat(web): 首页新增待我处理并调整区块顺序"
```

---

## Task 7: 常用置顶、文档与版本

**Files:**
- Create: `web/src/composables/usePinnedPages.js`
- Create: `web/src/composables/usePinnedPages.test.js`
- Modify: `web/src/components/Sidebar.vue`（置顶区）
- Modify: `docs/guides/configuration-reference.md`（ui.yaml 说明）
- Modify: `CONTEXT.md`（术语）
- Modify: `VERSION`

**Interfaces:**
- Produces: `usePinnedPages()`：`pinned`、`toggle(key)`、`isPinned(key)`，存 `localStorage`。

- [ ] **Step 1: 写失败测试**

```js
import { describe, it, expect, beforeEach } from 'vitest'
import { usePinnedPages } from './usePinnedPages'

beforeEach(() => localStorage.clear())

describe('usePinnedPages', () => {
  it('最多保留 5 个，重复点击取消置顶', () => {
    const p = usePinnedPages()
    for (const k of ['a', 'b', 'c', 'd', 'e', 'f']) p.toggle(k)
    expect(p.pinned.value).toHaveLength(5)
    p.toggle('a')
    expect(p.isPinned('a')).toBe(false)
  })

  it('关闭的模块即使置顶也不显示（由调用方判定）', () => {
    const p = usePinnedPages()
    p.toggle('logs')
    expect(p.pinned.value).toContain('logs')
  })
})
```

- [ ] **Step 2: 运行确认失败**

Run: `cd web && ./node_modules/.bin/vitest run src/composables/usePinnedPages.test.js`
Expected: FAIL。

- [ ] **Step 3: 实现**

```js
import { ref } from 'vue'

const KEY = 'nebula-pinned-pages'
const MAX = 5

function load() {
  try {
    const raw = JSON.parse(localStorage.getItem(KEY) || '[]')
    return Array.isArray(raw) ? raw.slice(0, MAX) : []
  } catch {
    return []
  }
}

export function usePinnedPages() {
  const pinned = ref(load())

  function persist() {
    try {
      localStorage.setItem(KEY, JSON.stringify(pinned.value))
    } catch {
      /* ignore */
    }
  }

  function isPinned(key) {
    return pinned.value.includes(key)
  }

  function toggle(key) {
    if (!key) return
    pinned.value = isPinned(key)
      ? pinned.value.filter((k) => k !== key)
      : [...pinned.value, key].slice(-MAX)
    persist()
  }

  return { pinned, isPinned, toggle }
}
```

- [ ] **Step 4: 侧栏置顶区**

`Sidebar.vue` 在「值班台」下方渲染最多 5 个置顶页：

```vue
<div v-if="pinnedItems.length" class="pinned-pages">
  <router-link
    v-for="p in pinnedItems"
    :key="p.key"
    :to="p.to"
    class="pinned-item"
    :class="{ active: isActiveItem(p) }"
  >{{ p.label }}</router-link>
</div>
```

```js
const pinned = usePinnedPages()
// 开关优先级高于置顶：已关闭模块即使被置顶也不显示
const pinnedItems = computed(() =>
  pinned.pinned.value
    .map((k) => allItems.find((i) => i.key === k))
    .filter((i) => i && modules.isVisible(i.key, pageDomain[i.key])),
)
```

（`allItems` 为 `groups.flatMap(g => g.items)`。）

- [ ] **Step 5: 文档**

`configuration-reference.md` 增加 `ui.yaml` 段落：

```markdown
## UI 配置（`ui.yaml`）

`name` / `logo` / `footer` / `subtitle` 为品牌配置；`disabled` 是已关闭的模块与页面 key。

- `disabled` 是**关闭清单**：缺省或空 = 全开，升级新增页面自动可见。
- 不可关闭：`overview`（值班台）、`alerts`（告警中心）、`alerting`（整域）、`system`（系统设置）。
- 关闭只隐藏入口并停止自动巡检/报告/拨测，不注销接口、不删除数据。
- 接口采用部分更新：只提交 `disabled` 不会覆盖品牌字段。
```

`CONTEXT.md` 术语表增加：

```markdown
**模块开关**
`ui.yaml` 的 `disabled` 关闭清单。关闭 = 隐藏入口 + 停止对应定时任务，不改变接口可达性与已有数据。

**值班台**
首页。固定置顶「待我处理」，只放需要人做决定才能推进的事项（待确认告警、待我审批）。
```

- [ ] **Step 6: 版本与全量验证**

`VERSION` 递增次版本（如 `1.34.0`）。

Run: `go test ./... && cd web && ./node_modules/.bin/vitest run`
Expected: 全部通过。

- [ ] **Step 7: 提交**

```bash
git add web/src/composables/usePinnedPages.js web/src/composables/usePinnedPages.test.js web/src/components/Sidebar.vue docs/guides/configuration-reference.md CONTEXT.md VERSION
git commit -m "feat: 常用置顶与模块开关文档"
```

---

## 本批不做（明确延后）

- **菜单分组内排序**：设计稿列为 P1，但需要对每组的条目顺序做持久化与交互设计；本批只实现「常用置顶」（固定 5 个），组内排序留到下一批。
- **卸载模块**：关闭只隐藏入口与停调度，不注销路由、不删数据。
- **巡检差异进待办**：差异项没有处置状态机，见设计稿 D5。

---

## 验证清单

- [ ] `go test ./...` 全绿；`cd web && ./node_modules/.bin/vitest run` 全绿。
- [ ] 实机：关闭「资产与配置」→ 侧栏消失、深链重定向首页、vault 数据仍在。
- [ ] 实机：只保存模块开关 → 名称/副标题不变。
- [ ] 实机：关闭「服务拨测」→ 日志出现「模块已关闭，跳过执行」，手动触发仍可执行。
- [ ] 实机：值班台显示待确认告警数量；无审批权限时显示「无权限或未启用」。
- [ ] 真实浏览器走查侧栏、设置页与首页。
