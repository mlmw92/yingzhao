# UI-SPEC · NebulaEye 运维平台逐页设计方案

> **性质**：界面规格文档（UI-SPEC）。面向"改什么、为什么、改成什么样"，不含实现代码。  
> **基线**：Vue 3 + Vite + Element Plus + ECharts，hash 路由，全站深色，三套主题（`b` 极光蓝 / `a` 星云青绿 / `c` 星河紫）。  
> **关联文档**：`web/design/PROMPT-运维平台前端重构.md`（批次 0–5 已落地，本文件承接 **批次 6 之后**）。  
> **结论标注规则**：✅ 已核实（指到行号/class）· 🟡 推测（需实测确认）· ⚠️ 存疑（需产品/后端确认）

---

## 0. 结论先行

### 0.1 覆盖度

| 项                        | 数量                                              |
| ------------------------ | ----------------------------------------------- |
| 通读的前端文件                  | 60+ 个 `.vue`，合计约 25,000 行                       |
| 覆盖路由页面                   | **28 个**（`/screen` 数据大屏按既有约定**不在本轮范围**）         |
| 页面规格编号                   | PS-00（令牌层）～ PS-29（全局件），其中页面规格 **PS-01 ～ PS-28** |
| 识别出的**正确性缺陷**（bug，非体验问题） | **12 项**（见 0.2）                                 |
| 识别出的**系统性视觉债务**          | **12 类**（见 0.3）                                 |

### 0.2 必须先修的 12 项正确性缺陷（P0）

这些不是"不好看"，是"不工作"或"数据错"。建议在任何视觉改造前单独一个批次修掉。

| #   | 页面               | 缺陷                                                                                         | 证据                                                                                           | 影响                                                                |
| --- | ---------------- | ------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------- | ----------------------------------------------------------------- |
| B1  | 角色与权限 PS-27      | 权限目录字段名契约不匹配：后端 `permDomain` **无 json tag**，序列化为 `Domain`/`Items`；前端读 `d.domain`/`d.items` | `internal/server/auth/model.go:332-335` vs `RolesView.vue:137/139/140/142/144/256` ✅         | 47 个权限点的矩阵**整块不可用**；`d.items.length`（L140）抛 TypeError。配错角色 = 配错权限 |
| B2  | 中间件监控 PS-06      | 「展示类型」配置**不生效于渲染**：`visibleBuiltinTabs`/`validTabs` 只用于深链校验，模板 25-113 无任何 `v-if`           | `MiddlewareView.vue:206/209/241/248/254` ✅                                                   | 用户隐藏了类型，Tab 依然全显示——功能假象                                           |
| B3  | 中间件（轻采集）PS-06    | **双定时器**：`RefreshBar` 内部已按 interval 轮询，组件又 `setInterval(load, 30000)`                      | `LightCollectorTab.vue:227` + `RefreshBar.vue:74` ✅                                          | 5 个轻采集 Tab 每 30 秒发**两次**请求                                        |
| B4  | 安全中心 PS-24       | 共享 `loadError`：`Promise.all` 并发下任一模块成功即在成功分支清空错误                                           | `SecurityView.vue:430/562/572/582/602` ✅                                                     | 部分失败被静默吞掉，用户看到"一切正常"但区块是空的                                        |
| B5  | 安全中心 PS-24       | `defenseLoading` 声明后**从未置 true**，却绑在 `v-loading`                                           | `SecurityView.vue:391` + `:46` ✅                                                             | 入侵防御卡永远没有加载态                                                      |
| B6  | 自定义仪表盘 PS-22     | 非编辑态点「添加面板」→ `savePanel` 写 `draft.value.panels`，此时 `draft === null` → **空指针崩溃**            | `DashboardView.vue:50` → `:190` 🟡                                                           | 可复现崩溃（空看板首屏的默认路径）                                                 |
| B7  | 自定义仪表盘 PS-22     | `PanelChart` 恒定 `type:'line'`，而配置下拉提供 4 种类型                                                | `PanelChart.vue:68` vs `DashboardView.vue:63-68` ✅                                           | 选了 area/bar/gauge 不生效，配置与呈现矛盾                                     |
| B8  | 配置巡检 PS-10       | 模板中一个**游离的 `>`** 字符会渲染成多余文本节点                                                              | `InspectView.vue:126` ✅                                                                      | 表格内多出一个 ">"                                                       |
| B9  | 中间件 PS-06        | `loading` 初值 `false`，首帧 `!loading && instances.length===0` 成立 → **空态引导闪现**                 | `RedisTab.vue:618`、`LightCollectorTab.vue:118`（其余 Tab 均为 `ref(true)`，如 `MySQLTab.vue:293`） ✅ | 6 个 Tab 首帧闪烁                                                      |
| B10 | 主机详情 PS-04       | ✅ **已修**（2026-10-09 实测）：`setMonitorRef` 在每个面板 ref 挂载时都触发一次全量 `loadMonitor`；**实测首次进 Tab2 发 96 次** range 请求（原估 35 次偏低），修复后 **12 次**         | `NodeView.vue:630-644` + `:1168`（串行 await） 🟢                | 已修：ref 回调改为只补刷「本次新建的图」；复测全 5 Tab 合计 111 → **27 条**                                                |
| B11 | 容器 Tab PS-06     | 「在线主机」统计对每个 host **无条件 `up++`**，未判断 `h.up` → 恒等于「主机总数」                                     | `DockerTab.vue:175-181` ✅                                                                    | KPI 是错误数据；`.is-down` 永不生效                                         |
| B12 | 值格式化 PS-06/PS-10 | `row.x ? format(x) : '-'` 把**真值 0 吞成 '-'**                                                 | `MongoTab.vue:52/56/82`、`FastDFSTab.vue:52-59` ✅                                             | 「0 MB」显示成「-」，被读成"未采集"                                             |

### 0.3 十二类系统性视觉债务（跨页面）

| 编号  | 债务             | 现状                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     | 收敛目标                                                                                               |
| --- | -------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------- |
| D1  | **状态表达**       | 6+ 套自绘并存：`.dot/.st-*`、`.op-pill`、`.sev-tag`、`.def-badge`、`.score-pill`、`.tag.*`、`.status-led`、`.pill`、手写 `.status-pill`。`StatusPill.vue` 已实现 `ok/warn/danger/info/muted` + `dot`，但仅在 4 个页面使用                                                                                                                                                                                                                                                                                                                                                           | 全站状态一律 `StatusPill`；**状态必须有颜色之外的第二编码**（图标/形状/前缀）                                                   |
| D2  | **容器语言**       | `.glass` / `.card` / `.panel` / `el-card` 四套并存；`SectionCard.vue` 已提供 `title/subtitle/dense/actions/footer`，但仅 HostsView / SecurityView / InspectView / AssetModelView / UpgradeView / ContainerView 使用                                                                                                                                                                                                                                                                                                                                                 | 一律 `SectionCard`；`.glass` 淘汰                                                                       |
| D3  | **令牌空转**       | `--sp-*` 4px 栅格在页面组件层**几乎 0 落地**；`--brand-grad`/`--sh-inset`/`--sp-8`/`--dur-3` **0 引用**；旧别名仍是主力（`--text-dim` 208 处、`--text-muted` 162 处 vs `--t1` 仅 20 处）                                                                                                                                                                                                                                                                                                                                                                                             | 旧别名 → 新令牌；间距/字号 100% 走 `--sp-*`/`--fs-*`                                                           |
| D4  | **规范件空转**      | `style.css:978` 的 `.save-bar`（sticky 保存条）与 `:995` 的 `.danger-zone`（危险区隔离）**已完整实现，全站 0 个 `.vue` 引用**                                                                                                                                                                                                                                                                                                                                                                                                                                                    | 设置类页面全部接入；危险操作全部进 `.danger-zone`                                                                   |
| D5  | **四态缺失**       | 空态/加载/错误/**部分失败** 四态各页各写；大量 `catch(e) { /* ignore */ }` 静默吞错（AlertsView 至少 5 处、UpgradeView 5 处、OverviewView 的 `get()` 把失败吞成 `{}`）                                                                                                                                                                                                                                                                                                                                                                                                                      | 统一：空态 `EmptyState` · 加载骨架 `.sk` · 错误带重试 · **部分失败用 `.partial-bar` 顶部提示条，已成功数据照常渲染**                 |
| D6  | **可访问性**       | 大量 `<div @click>` 作为主交互路径（主机方块、中间件卡、告警行、KPI 下钻卡、日志行展开、资产标签）；状态仅靠颜色；全仓库 `prefers-reduced-motion` **0 处**（style.css 有 2 个无限动画）                                                                                                                                                                                                                                                                                                                                                                                                                           | 主交互元素语义化（`<button>` 或 `role`+`tabindex`+键盘）；状态双编码；无限动画加降级                                          |
| D7  | **响应式断点散落**    | 全局注释声明 4 档（1500/1400/1100/720），实际只实现 **2 档**（1400、1100）；各页面私有断点 850/900/1200/560/640/760/2400 等十余个；`AssetListView`/`AssetModelView`/`InspectView`/`AssetTopologyView`/`LogsView`/`AuditView`/`DialTestView`/`MetricsExploreView`/`DashboardView` **0 处 `@media`**                                                                                                                                                                                                                                                                                      | 全局立 3 档（1280 / 1024 / 768），页面私有断点收编                                                                |
| D8  | **信息密度失控**     | 表格 9–11 列 + 写死列宽：HostsView 约 1254px、AssetListView 约 1262px、OpsView 约 1148px、AuditView 约 1520px、NodeView 进程表 8 列 → 1440px 以下必横向滚动；AuditView 自己在注释里承认（L108-110）                                                                                                                                                                                                                                                                                                                                                                                          | **列宽重排 + 数值列右对齐等宽 + 窄屏响应式降级**；**"默认收起低频列"不適用於 HostsView**——其磁盘读写/负载/流量是日常巡检判读依据，须常驻（详见 **5.1**）    |
| D9  | **图表与令牌脱节**    | 15 个中间件 Tab 的抽屉趋势图**只设了 legend 文字色**，`axisLabel.color`/`tooltip` 全落 ECharts 默认；`BaselineTrendChart` 换肤不换色；`IntelligenceView` 15 rgba + 12 hex；`RedisTab` 74 处硬编码 rgba                                                                                                                                                                                                                                                                                                                                                                                  | 图表配色统一由 `cssVar()` 读 `--chart-*`/语义色，并监听 `nebula:theme-changed`                                    |
| D10 | **字号失序（可读性）**  | 实测全站 `font-size` 字面量 **557 处**：`13px` 337 / `12px` 51 / `14px` 46 / `12.5px` 15 / `11px` 17 / `11.5px` 6 / **10.5px 2 / 10px 2**。`--fs-*` 令牌虽已定义（`--fs-xs:12 / sm:13 / base:14`），但页面层大量绕过令牌写裸值，且出现 `.5px` 这类**非栅格值**；`html,body` 根字号 16px（L207）与正文 13px 并存，EP 组件 base 被抬到 16px（L673）→ **组件比正文大 3 级，层级倒置**                                                                                                                                                                                                                                              | 立 5 档正文基线（14/13/12 三级 + 标题两档），禁用 <12px；`.5px` 全部归整；EP base 降到 14px 与正文同档（详见 **1.4**）               |
| D11 | **筛选区尺寸不统一**   | 同一类筛选控件有**三处不统一**：（a）`size` 标志混乱——`AssetListView`/`HostsView` 主工具栏用默认 size（EP 控件 40px 高），`LogsView`/`InspectView`/`AuditView`/`AlertsView` 用 `size="small"`（32px 高），**同页应用高差 8px**；（b）`width` 各页定义——`LogsView` 有 96/180/200/210/240/320/400px 共 7 档，`InspectView` 有 130/140/150/170/240px 共 5 档，`HostsView` 100/120/200px、`AssetListView` 130/150/260px、`AlertsView` 110/150/220px，**同类控件（如下拉）宽 100→400 跨度 4×**；（c）筛选容器形态不统一——`AssetListView`/`.filter-bar`（带边框+底色）、`LogsView`/`.toolbar`（裸 flex，无边框）、`AlertsView`/`.rule-filter-bar`（内联 style 裸 flex） | **统一一种筛选容器**（见 **1.5**），控件 size 全站统一为 `small`（32px），按角色定 5 档宽度令牌（XS/S/M/L/XL = 96/130/180/240/320） |
| D12 | **描述段与上方元素贴脸** | 描述/说明/小注（`.note` `.muted` `.schedule-hint` `.verify-note` `.field-hint` `.alert-gap`）与上方元素距离**散落**：6/8/10/14/26px 各种值都有。典型：`AuditView` `.verify-note{margin-top:6px}`（L557）、`AssetListView` `.note{margin-top:14px}`（L2211）、`InspectView` `.schedule-hint{margin-top:10px}`（L509）、`HostsView` `.refresh-text` 内联 `font-size:13px` + 紧邻 32px 高的刷新控件、LogsView 的 `.toolbar` 与下方 `.field-row` 只有 `margin-bottom:10px`。同一应用内不同页"小字说明贴控件"或"小字说明独立成段"两种风格并存                                                                                                   | 立**描述段三档距离**令牌（紧贴 6 / 标准 10 / 分块 14），并统一 `.muted` 字号 13px / 行高 1.5 / 颜色 `--t3`；详见 **1.6**          |


### 0.4 七条判断（如与既有认知冲突，以此为准）

1. **首页"智能运维"区块固定置顶是错的**。它 `min-height:260px` + 4 信号卡 + 就绪度 + 最高优先级，把 KPI / 健康度 / 紧急告警全压到第二屏，而首页的第一职责是"现在有没有事情要处理"。建议**改为可折叠**（默认收起，有 critical 时自动展开）。**注**：放开"隐藏"本轮不做（2026-10-09 确认，见 §33 #6），`PINNED_KEYS` 维持现状。
2. **告警详情必须是"列表 + 详情"可对照形态**。当前 `el-drawer` 默认带遮罩，打开详情后列表被盖住；且 30 秒轮询会**主动关闭正在查看的抽屉**（`AlertsView.vue:844-846`），读到一半被打断。这是处置主路径，每天被走几十次。
3. **"配置类"页面的核心缺失不是美观，是"未保存改动"**。`.save-bar` 样式已写好两年却零引用，13 个设置类页面没有一个提示脏数据。改完表单切走即静默丢失。
4. **把 Element Plus 字号"放大"到 16px 是反效果的**。`style.css:673` 的注释明确写着"放大 Element Plus 组件整体字号"，结果是按钮/输入框/下拉（16px）比它所处的业务正文（13px）还大 3px，**层级倒置**，同屏看着"控件抢戏、内容退后"。正确做法是让组件回到 14px 与正文同档，需要强调的地方靠**字重与颜色**而非字号。这一条与既有注释的意图相反，是本规格中唯一一处建议"回退"的决定（详见 1.4）。
5. **"搜索区域大小不一"不是单页问题，是筛选容器 + 控件 size + 宽度三处都不统一**。`AssetListView`/`HostsView` 用默认 40px 控件，`LogsView`/`InspectView`/`AuditView`/`AlertsView` 用 32px small；容器有 4 种（`.filter-bar` / `.toolbar` / `.rule-filter-bar` / `.filters`）；同类下拉宽度 100/120/130/150/180/200/220/240/320/400 横跨 4 倍。规范做法：size 全站 `small`、容器统一 `.filter-bar`、宽度走 5 档令牌 `--w-xs..xl`（详见 **1.5**）。
6. **"描述段跟其他元素贴脸"也是多页通病，不是某页调一下**。`AuditView` 的 `.verify-note{margin-top:6px}`（最贴脸）与 `AssetListView` 的 `.note{margin-top:14px}`（独立成段）同应用内相差 2.3×。规范做法：`.muted` 在 `style.css` 仅一处定义（删 8 处页内覆写），三档距离 `--desc-tight/base/block`（6/10/14px）覆盖"行内/标准/分块"三种语境（详见 **1.6**）。
7. **主机列表的「磁盘读写 / 负载 / 流量」必须保持默认展示**（**本条为方案修正**）。初稿曾建议"默认收起这三项低频列"，该建议**已撤回**：这三项是容量与排障的判读依据（磁盘读写看 IO 瓶颈、负载看饱和、流量看异常外联），日常巡检几乎每次都要扫一眼，收进列设置等于把每天的固定动作变成"先点开列设置再勾选"。空间不够应当靠**列宽重排 + 磁盘读写改两行堆叠 + 操作列瘦身 + 窄屏响应式降级**去换，而不是牺牲常驻信息（详见 **5.1**）。

---

## 1. PS-00 · 设计令牌层与全局规范件（先于任何页面改动）

**优化目标**：让 28 个页面的改动都有唯一事实来源，避免"改 100 个组件、每个都自己发明一次"。

### 1.1 令牌补充（追加，不删除任何现有变量）

| 新增                                | 值                                                                 | 用途                                                         |
| --------------------------------- | ----------------------------------------------------------------- | ---------------------------------------------------------- |
| `--sp-7`                          | 28px                                                              | 补 4px 栅格断点（当前 `--sp-1..6/8` 缺 7）                           |
| `--z-*`                           | `--z-sticky:40 / --z-overlay:100 / --z-modal:200 / --z-toast:300` | 当前 `z-index` 散落在 `.save-bar`(20)、`CommandPalette`(312) 等裸值 |
| `--lh-tight:1.25 / --lh-base:1.5` | —                                                                 | 行高无令牌，全站裸 1.4/1.5/1.6/1.7                                  |
| `--chart-*` 明暗双档                  | 现有 8 色各增 `-dim`                                                   | 面积图填充、阈值区                                                  |

**移除或补用量**（0 引用）：`--brand-grad`、`--sh-inset`、`--sp-8`、`--dur-3`、`--chart-indigo`、`--brand-ink`。

### 1.2 表格密度统一（写进 `style.css` 的 `html.dark .el-table` 覆盖块，一次生效）

| 项       | Element 默认 | 现状实测                                                                | **规范值**                                                 |
| ------- | ---------- | ------------------------------------------------------------------- | ------------------------------------------------------- |
| 表格正文字号  | 14px       | 13px（L775/779）                                                      | **14px（`--fs-base`）** ← 详见 1.4；与行高 38px 同时改，净效果更疏朗      |
| EP 基础字号 | 16px       | 16px（L673，注释写着"放大 EP 组件整体字号"）                                       | **14px** ← 现状是"组件 16px + 正文 13px"的**层级倒置**，必须拉齐（详见 1.4） |
| 行高      | ≈48px      | ≈34.5px（7+7 padding + 13×1.5 + 1px）                                 | **38px**（`padding: 8px 0`）← 规格原写 `9px` 是按 13px 正文推算的；正文已升 14px，取 8px 才命中 38px（实测：单行 = 8+8 + 14×1.5 + 1 = 38） |
| 表头      | 14px 加粗    | 12px/600/`--t3`/字距 .04em ≈32px                                      | 保持，字距降到 **.02em**（中文 ≥0.04em 尚可，但 0.08em 会散）            |
| 斑马纹     | 开          | **已强制关闭**（L798-800）✅                                                | 保持关闭                                                    |
| 行 hover | —          | **两处冲突**：L733 `--accent-dim` 与 L776 `--fill-hover`，后写生效 → L733 是死声明 | **统一 `--fill-hover`**，删死声明                              |
| 数值列     | —          | `.num-cell` 已定义（L802-811）但需页面手动加 class，**多数页面未用**                   | 推广到全部数值列：右对齐 + `font-variant-numeric: tabular-nums`     |
| 空态高度    | 60px       | 160px（L813-815）✅                                                    | 保持                                                      |

### 1.3 规范件增强（现有 4 件 + 新增 4 件）

| 组件                      | 现状                                                   | 增强                                                                                 |
| ----------------------- | ---------------------------------------------------- | ---------------------------------------------------------------------------------- |
| `SectionCard`           | `title/subtitle/dense` + `default/actions/footer` 插槽 | 加 `loading` / `error` / `errorRetry` 三个 prop，内置骨架与错误条                              |
| `EmptyState`            | `icon/title/hints/actionText`，仅 1 个动作                | 加 `size`（table-inline / block）、`secondaryActionText`                               |
| `StatusPill`            | `tone/dot/size`                                      | 加 `icon`（给状态第二编码）、`text` prop（免插槽）                                                 |
| `PageHeader`            | `title/desc` + `actions`                             | 保持；补 `breadcrumb` 可选（与顶栏联动）                                                        |
| **新增 `FilterChips`**    | —                                                    | 筛选条件回显条：逐条可撤销 + 清空全部（LogsView 只有 2/6 条件可见、AuditView 无、AssetListView 下钻态有盲区，三处同需求）  |
| **新增 `PartialBar`**     | —                                                    | 部分失败提示条（`--warn` 底 + 「重试失败项」），替代各页 `catch(e){}`                                    |
| **新增 `MwDetailHeader`** | —                                                    | 中间件详情对象头：状态 + 标题 + 元信息 + 时间范围（只有 RedisTab 有，其余 14 个 Tab 抽屉顶部是裸 label-value）        |
| **新增 `TrendChart`**     | —                                                    | 趋势图工厂：统一 `axisLabel.color`/`tooltip`/`splitLine` 取令牌 + `ResizeObserver` + 空/错/加载三态 |

#### 1.3.1 实施记录（批次 8 · 2026-10-09）

**已落地（现有 3 件增强 + 新增 2 件）**

| 组件 | 增强内容 | 文件 |
| --- | --- | --- |
| `SectionCard` | 加 `loading`（内置 `.sec-skeleton` 骨架）/ `error`（内置 `.sec-error` 错误条）/ `errorRetry`（显示重试按钮，`emit('retry')`）三个 prop。**四态优先级：错误 > 加载 > 内容**（错误/加载是"本次这块没拿到数据"，优先于可能过期的内容） | `common/SectionCard.vue` |
| `EmptyState` | 加 `size`（`block` 默认 / `table-inline` 紧凑，供 `el-table #empty` 槽）+ `secondaryActionText`（`emit('secondary-action')`）；`.es-action` → `.es-actions` 包裹层 | `common/EmptyState.vue` |
| `StatusPill` | 加 `icon`（颜色之外的第二编码，色盲友好）+ `text`（免插槽）；插槽优先于 `text` | `common/StatusPill.vue` |
| **新增 `FilterChips`** | 筛选回显条：`chips=[{key,label,value}]` 逐条 × 撤销（`emit('remove',key)`）+ 清空（`emit('clear')`）；值为空时不出冒号段 | `common/FilterChips.vue` |
| **新增 `PartialBar`** | 部分失败条（`--warn` 底 + 「重试失败项」）：`failed` 支持字符串数组或 `[{label,message}]`；`emit('retry')`；超 `maxShow` 折叠为「等 N 项」 | `common/PartialBar.vue` |

**已落地（`style.css`）**

- **`prefers-reduced-motion: reduce` 全局降级**：`animation-duration` / `transition-duration → 0.01ms`、`animation-iteration-count → 1`，并显式停掉 `.sk` / `.sk-pulse` 无限动画。用 `0.01ms` 而非 `none`，以保留 `transitionend` / `animationend` 事件语义（依赖这些事件收尾的代码在降级模式下不会卡住）。
- 令牌补充：`--sp-7: 28px`（补 4px 栅格缺档）、`--lh-tight: 1.25`。

**实测复核（无头 Chrome，1440×900）**

- 12 个既有页面（hosts / alerts / audit / security / assets / logs / ops / container / dashboard / dialtest / assets-model / approvals）全部正常渲染、**无 JS 报错**、`scrollWidth == innerWidth == 1440` → 共享件改动**零回归**。
- `prefers-reduced-motion` 实测：`.sk` 在 `no-preference` 下 `animation-name=sk-pulse / 1.4s / infinite`，在 `reduce` 下 **`none / 0s / 1`** ✓。
- 单测：新增 `common/common.test.js`（**18 用例**，覆盖四态优先级 / 事件 / size / 折叠）；全量 **33 files / 214 tests** 全绿；构建 `✓ built`。

**遗留（挂账）**

1. **规范件尚未在各页接入**：本批次只做"造件 + 单测"。`SectionCard.loading/error`、`EmptyState.size`、`FilterChips`、`PartialBar` 的实际接入按 §31 批次 11–17 逐页推进（PS-00 的定位是"先于任何页面改动"的唯一事实来源）。
2. **§1.1 的 `--z-*` / `--chart-*-dim` 未做**：`--z-*` 若按规格值（sticky40 / overlay100 / modal200 / toast300）会**低于 EP 的 popper / dialog（2000+）**，直接改会把自绘层叠压到 EP 弹层之下 → 需先做一次"EP 基准 vs 自绘"的层叠审计，故挂账。`--chart-*-dim` 依赖 `TrendChart` 图表工厂（批次 14），一并挂账。
3. **`PageHeader.breadcrumb`**（§1.3 列为"可选"）：需与顶栏联动，留待 PS-29 全局收口。

#### 1.3.2 实施记录（批次 9 · 2026-10-09）

**已落地（新增 3 件 + 扩展 1 件 + 图表工厂 1 项）**

| 组件 | 内容 | 文件 |
| --- | --- | --- |
| `MwStatusDot`（扩展） | **两态 → 三态**：`normal`(绿 `--ok`) / `warning`(琥珀 `--warn`) / `abnormal`(**红 `--danger`**)。此前 `abnormal` 用的是琥珀色，与"重启中"同级，**离线/失败被系统性低估**；本次按 §6.3 把"离线/失败/已停止/崩溃"统一改红。另加 `dotOnly`（只画点不显示文案，供主机卡/列表首列）、别名归一（`online/up/running`→normal，`paused/restarting/pending/created`→warning，`offline/down/failed/dead/exited/CrashLoopBackOff`→abnormal），**未知取值兜底 abnormal**（宁可高估不可漏报） | `mw/MwStatusDot.vue` |
| **新增 `TrendChart`** | 趋势图工厂组件：封装 ECharts 生命周期（init / setOption / dispose，`ResizeObserver` 由 `initChart` 内部挂载并在 dispose 时断开），内置 **空 / 加载 / 错误** 三态覆盖层（错误可 `emit('retry')`）。`empty` 未显式传时按 `option.series` 是否全空自动判定 | `mw/TrendChart.vue` |
| **新增 `MwHostCard`** | 主机卡：状态点（`dotOnly`）+ 名称/副名（等宽字体）+ 右侧计数 + 默认插槽（容器 chip 列表）+ `dim`/`selected` 态。统一 Docker/K8s/Redis 三套各自手写的主机块 | `mw/MwHostCard.vue` |
| **新增 `MwDetailHeader`** | 抽屉对象头：状态点 + 标题 + 副标题 + 元信息字段网格 + **时间范围切换（默认 5 档）**（`v-model:range`，`emit('update:range')`） | `mw/MwDetailHeader.vue` |
| **新增 `mwRange.js`** | 时间范围工具：`MW_RANGE_OPTIONS`（5m/15m/1h/6h/24h）、`rangeToWindow(range, now)` → `{start,end}`、`rangeToStep(range)`（长窗口用更大步长，避免 24h 拉回上千点） | `mw/mwRange.js` |
| **`echarts.js` 新增 `mwTrendOption`** | 中间件趋势图统一 option 工厂：`axisLabel.color` / `tooltip` / `splitLine` 全部取令牌；单序列不显示图例（一个名字的图例是噪音），多序列自动显示 | `charts/echarts.js` |

**试点接入（2 个 Tab）**

| Tab | 改动 |
| --- | --- |
| `DockerTab` | `.glass` → `SectionCard`（实例拓扑 / 容器列表）；空态 → `EmptyState`（含可执行提示）；实例拓扑主机块 → `MwHostCard` **并补回容器 chip 列表**（原区块只剩主机头，是"占一屏的空壳"，按 §6.3「要么补全要么删掉」选择补全）；容器 6 态 → `MwStatusDot` 三态（running→normal，paused/restarting/created→warning，exited/dead→abnormal）；抽屉 → `MwDetailHeader` + `TrendChart`（含 5 档时间范围）；顶部 `el-alert` 承载刷新失败（不再只 `console.error` 后静默留白）；**修 B11**：`在线主机` 此前对每台主机无条件 `up++`，恒等于主机总数 → 改为 `if (h.up) up++`（已 grep 后端确认 `dockerHostInfo.Up` 是真实字段） |
| `LightCollectorTab`（5 个轻采集 Tab 的基座） | `.glass` → `SectionCard`；空态 → `EmptyState`；自绘 `.dot` → `MwStatusDot`；抽屉 → `MwDetailHeader` + `TrendChart`（5 档时间范围）；**修 B3**（删组件内 `setInterval(load,30000)`，自动刷新统一交给 `RefreshBar`，其带页面隐藏暂停）；**修 B9**（`loading` 初值 `false`→`true`，消除首屏"先闪空态再出数据"）；趋势图改 **`Promise.all` 并行**（原为串行 await）；图表实例随 `destroy-on-close` 卸载即 dispose |

**实测复核（无头 Chrome，1440×900）**

- **三态色实测**：Docker 页 9 个状态点 → `normal=rgb(34,197,94)` / `warning=rgb(255,180,84)` / `abnormal=rgb(255,93,108)`；明细：运行中→normal、已暂停→warning、已停止→abnormal、重启中→warning、异常→abnormal ✓。容器 chip 左边框色同步三态 ✓。
- **抽屉实测**：Docker `head="web-nginx"` 时间范围档=5 TrendChart=1 画布=1；RabbitMQ `head=true` 档=5 TrendChart=2 ✓。
- **空态/错误态实测**：docker empty→`EmptyState`（标题「尚未配置 Docker 监控」+ 2 hints）；docker error→`el-alert`（文案 + 重试）；rabbitmq 同理 ✓；四种情形横向溢出均 **0px**。
- **15 Tab 冒烟**：全部 `溢出=0px`、`Tab数=15`、**无 JS 报错**（仅 mock 无后端的 WS 噪音，已排除）。
- **横向溢出**：Docker/RabbitMQ 两页 `docSW == innerWidth == 1440`，零横向滚动 ✓。
- **残留 `.glass`**：中间件内容区 **0 处**；页面仅剩外壳 `aside.sidebar.glass` / `header.topbar.glass` 两处（属全局壳层，不在本批范围）。
- **单测**：新增 `mw/mw-skeleton.test.js`（**29 用例**：三态与别名归一 / `dotOnly` / TrendChart 三态与 retry 与卸载销毁 / MwHostCard / MwDetailHeader / mwRange）；全量 **34 files / 243 tests** 全绿；构建 `✓ built in 12.88s`。

**跨页副作用（有意为之）**

- `MwStatusDot.abnormal` 由琥珀改红，**波及所有使用它的 Tab**（Redis/MySQL/Postgres/Nginx/Kafka/RocketMQ/K8s/Mongo/FastDFS）。已核对各 Tab 传入值仅为 `normal|abnormal` 或其等价别名，语义不受影响；颜色变化即 §6.3 的既定意图（同一共享件，全站一致）。

**遗留（挂账）**

1. **其余 8 个手写 Tab 未接入**（Redis/MySQL/Postgres/Nginx/Kafka/RocketMQ/K8s/Mongo/FastDFS）：仍用 `.glass`/`.mw-list`/自绘 `.dot`，按"先立骨架 → 再逐 Tab 接入"两批推进，属批次 10+。
2. **`MwDetailHeader` 的时间范围尚未真正驱动全部趋势查询**：Docker / LightCollector 已接；Redis 原有 5 档自绘控件未替换（其抽屉结构最重，随 RedisTab 拆分一并做）。
3. **K8s 仍是 0 图表**，节点/Pod 表无分页——属批次 10。

### 1.4 字号与可读性基线（D10 · 独立议题）

**优化目标**：解决"页面文字偏小、且小得没有章法"。这不是审美问题——运维场景里告警等级、IP、阈值、风险等级大量落在 10–11.5px，是**要被读准**的信息，不是装饰。

#### 1.4.1 实测：字号字面量分布（排除 `screen/**`）

| 字号             | 出现次数      | 判定                               |
| -------------- | --------- | -------------------------------- |
| 13px           | **337**   | 事实上的正文，但比令牌 `--fs-base:14px` 低一档 |
| 12px           | 51        | 允许（辅助/元信息下限）                     |
| 14px           | 46        | 与 `--fs-base` 一致，但只占 8%          |
| 18px           | 23        | 数字强调，OK                          |
| **11px**       | **17**    | ❌ 越界，须清                          |
| **12.5px**     | **15**    | ❌ 非栅格值，须归整到 12 或 13              |
| 16px / 15px    | 9 / 9     | OK                               |
| **11.5px**     | **6**     | ❌ 越界                             |
| 20 / 22 / 26px | 5 / 4 / 3 | 标题档，OK                           |
| **10.5px**     | **2**     | ❌ 严重越界                           |
| **10px**       | **2**     | ❌ 严重越界                           |

合计 **557 处**字面量。`<12px` 与 `.5px` 两类共 **42 处**，是本次要清零的对象。

#### 1.4.2 三处结构性错位（比"字小"更严重）

| #  | 错位                                                          | 证据                                                                            | 后果                                                    |
| -- | ----------------------------------------------------------- | ----------------------------------------------------------------------------- | ----------------------------------------------------- |
| S1 | **组件字号 > 正文字号**：Element Plus base 被抬到 `16px`，业务正文是 `13px`   | `style.css:673`（`--el-font-size-base:16px`）vs `:775/779`（表格 13px）             | 同屏内按钮/输入框比它所处的内容还大，**视觉层级倒置**；表单与表格并排时尤其明显            |
| S2 | **根字号与正文脱钩**：`html,body{font-size:16px}`，但 `--fs-base:14px` | `style.css:207` vs `:188`                                                     | 任何走 `rem`/`em` 的第三方样式都会按 16px 算，与 13/14px 的正文不同源      |
| S3 | **令牌被绕过**：`--fs-*` 已定义 8 档，页面层却写裸值，且出现 `.5px`               | `style.css:188-189`；`OpsView.vue:1512/1577`（10.5px）、`:1472/1600/1744`（11.5px） | 字号无法一次调整；`.5px` 在不同 DPI 下渲染不一致（Chrome 取整、Firefox 不取整） |

#### 1.4.3 规范基线（写入 `style.css`，一次生效）

| 层级         | 令牌          | 值             | 适用                                |
| ---------- | ----------- | ------------- | --------------------------------- |
| 页面标题       | `--fs-2xl`  | 24px          | `PageHeader` title                |
| 区块标题       | `--fs-lg`   | 17px          | `SectionCard` title / 抽屉 h3       |
| KPI 数值     | —           | 24–30px       | 大数字，保持                            |
| **正文**     | `--fs-base` | **14px**      | 表格正文、列表项、正文段落 ← **从 13px 上调**     |
| **次要**     | `--fs-sm`   | **13px**      | 副标题、说明、标签值                        |
| **辅助（下限）** | `--fs-xs`   | **12px**      | 元信息、时间戳、脚注                        |
| 微型标签       | —           | **禁止 < 12px** | 全大写 + 字距的小标签也统一 12px，字距 ≤ `.08em` |

**两条硬规则**：

1. **全站不得出现 < 12px 的字号**（`10px`/`10.5px`/`11px`/`11.5px` 共 27 处全部上调）。
2. **不得出现 `.5px` 小数**（`12.5px` 15 处 → 归整为 13px）；`11.5px` → 12px。

#### 1.4.4 越界清单（按文件，优先修）

| 文件                                                                                                                                                                                                                      | ≤12px 处数 | 典型越界值                                                        | 备注                                       |
| ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------- | ------------------------------------------------------------ | ---------------------------------------- |
| `OpsView.vue`                                                                                                                                                                                                           | **19**   | 10.5px（L1512/1577）、11.5px（L1472/1600/1744）、12.5px ×6         | 最严重；节点操作页含高风险动作说明，小字 = 读漏风险              |
| `IntelligenceView.vue`                                                                                                                                                                                                  | **12**   | **10px**（L54 `.hero p` / `.context-card>p`，字距 .15em）、11px ×4 | 10px + .15em 字距是全站最小文字                   |
| `overview/IntelligenceBlock.vue`                                                                                                                                                                                        | 6        | 11px（`.risk-label` / `.severity` / `.host-ip`）               | **风险等级标签只有 11px**——这是要被读准的信息             |
| `LogsView.vue`                                                                                                                                                                                                          | 6        | 12.5px（L849/857）                                             | 日志正文建议直接 13px                            |
| `AlertsView.vue`                                                                                                                                                                                                        | 6        | 11.5px（L1594）、12.5px ×2                                      |                                          |
| `asset/AssetListView.vue`                                                                                                                                                                                               | 5        | 11.5px（L2047）、11px ×2（L2059/2180）                            | 表格内 11px                                 |
| `settings/AlertPipelineSubView.vue`                                                                                                                                                                                     | 4        | 12.5px（L806/820）                                             |                                          |
| `settings/SelfMonitorSubView.vue`                                                                                                                                                                                       | 3        | —                                                            |                                          |
| `intelligence/AnalysisCoverageCard.vue`                                                                                                                                                                                 | 3        | **10px**（L17 `.coverage-head p`，字距 .14em）                    | 与 IntelligenceView 同源                    |
| `ai/AiDiagnosePanel.vue`                                                                                                                                                                                                | 3        | —                                                            | AI 结论文本，属阅读型内容，不宜小                       |
| `Sidebar.vue`                                                                                                                                                                                                           | 3        | 11.5px（L430）、11px（L544）                                      | 导航项**与对照组不同级**：主项 13px、子项 11.5px、角标 11px |
| `AppTopBar.vue`                                                                                                                                                                                                         | 2        | 11px（L231）                                                   |                                          |
| `CommandPalette.vue`                                                                                                                                                                                                    | 2        | 11px（L352/366）                                               | 快捷键提示                                    |
| `mw/LightCollectorTab.vue`                                                                                                                                                                                              | 2        | —                                                            |                                          |
| `UpgradeView.vue`                                                                                                                                                                                                       | 2        | —                                                            | 升级页含版本号等关键信息                             |
| `overview/TriageBar.vue`                                                                                                                                                                                                | 1        | 11px（L58）                                                    | **首页分诊条**——首屏第一视线，11px 最不该出现在这里          |
| `NodeView.vue`                                                                                                                                                                                                          | 1        | 11px（L1344 `.obj-kpi-value small`）                           | KPI 单位后缀                                 |
| `ReportView.vue` / `StatusView.vue` / `RuleModal.vue` / `AuditView.vue` / `ApprovalView.vue` / `KpiCard.vue` / `InspectView.vue` / `AssetTopologyView.vue` / `UsersView.vue` / `RolesView.vue` / `RetentionSubView.vue` | 各 1      | 11–12.5px                                                    | 顺手清零                                     |

#### 1.4.5 展示改进方向

- **数字与单位分离**：`NodeView` 的 `.obj-kpi-value small`（11px 单位）是当前唯一做法，推广为统一模式——数值 24–30px、单位 12px、`--t3`，不再单独调小。
- **小标签换编码**：`IntelligenceView` / `IntelligenceBlock` 用「10px + 全大写 + .15em 字距」做 eyebrow 标签。中文界面下这套（英文排版手法）**既小又散**。改为 12px + 中文短标签 + 左侧 3px 色条，不再靠字距撑识别度。
- **表格正文 13 → 14px**：与 `--fs-base` 对齐，行高由 34.5px → 38px（见 1.2），净效果是**更疏朗而不是更挤**。
- **行高兜底**：正文 14px 配 `--lh-base:1.5`，标题配 `--lh-tight:1.25`（1.1 已提令牌，此处强制落地）。

#### 1.4.6 交互改进方向

- **EP base 16px → 14px**（`style.css:673`）：按钮/输入框/下拉与正文同档，消除层级倒置。这是**一行改动影响全站**的杠杆点，建议放在批次 8 一并做。
- **根字号 16px → 14px**（`style.css:207`）：与 `--fs-base` 同源，避免第三方 `rem` 样式失配。
- **对话/抽屉内正文不低于 14px**：抽屉是阅读型容器（告警详情、风险分析、日志上下文），当前多处 12–13px。

> **注意**：本轮只做"字号归位"，**不改布局结构**。13px→14px 后若某处出现换行抖动，属于 1.2 表格密度与 D8 列分级的范畴，在对应批次处理。

#### 1.4.7 实施记录（批次 8 · 2026-10-09）

**已落地（令牌层，一次生效）**

| 位置 | 改动 | 说明 |
| --- | --- | --- |
| `style.css:209` | `html,body` 根字号 `16px → 14px` | 与 `--fs-base` 同源（§1.4.2 S2） |
| `style.css:677-678` | `--el-font-size-base 16→14`、`--el-font-size-small 14→13` | EP 回到默认档（medium 16 / large 18 本就是默认值）；消除"组件 > 正文"层级倒置 |
| `style.css:781/785` | 表格 `.el-table` 与 `.cell` 字号 `--fs-sm(13) → --fs-base(14)` | 表格正文与全站正文同档 |
| `style.css:800` | `td.el-table__cell` padding `7px 0 → 8px 0` | 行高命中 38px（见下） |
| `style.css:794` | 表头 `letter-spacing .04em → .02em` | §1.2 表头规范 |
| `style.css:734` | 删死声明 `--el-table-row-hover-bg-color: var(--accent-dim)` | 被 6.0.1 块的 `--fill-hover` 覆盖、永不生效 |

**已落地（越界清零，12 个 `.vue` 文件）**

- **`<12px` 全清**：`OpsView`(11/11.5/10.5×5)、`IntelligenceView`(10×1 + 11×4)、`overview/IntelligenceBlock`(11×3)、`intelligence/AnalysisCoverageCard`(10×1)、`AlertsView`(11.5)、`asset/AssetListView`(11.5 + 11×2)、`AppTopBar`(11)、`CommandPalette`(11×2)、`Sidebar`(11.5 + 11)、`NodeView`(11)、`overview/TriageBar`(11)。一律上调到 **12px**（辅助下限）。
- **`.5px` 全清**：`12.5px → 13px`（`OpsView`×7、`AlertsView`×2、`LogsView`×2、`settings/AlertPipelineSubView`×2、`ReportView`×1、`IntelligenceView`×1）；`11.5px → 12px`。
- **微型标签字距**：`IntelligenceView .hero p/.context-card>p`（`.15em`）、`AnalysisCoverageCard .coverage-head p`（`.14em`）、`Sidebar .nav-group-title`（`.1em`）统一压到 **`.06em`**（§1.4.3「字距 ≤ `.08em`」）。

**实测复核（无头 Chrome + mock API，1440×900，`ui-layout-regression` skill）**

| 页面 | 最小渲染字号 | `<12px` 元素 | 表格溢出 | 表头裁切 |
| --- | --- | --- | --- | --- |
| hosts / assets / audit / ops / alerts / intelligence / logs / report / settings / status | **12px** | **0** | 0（alerts 规则表 68px，见下） | **无** |
| 全部页面 `document.scrollWidth == innerWidth == 1440` | — | — | 页面级无横向滚动 | — |

- **行高**：`th` = 38px；单行 `td` = **38px**（实测 HostsView 两行单元格 59px，减去一行 21px 即单行值）。
- **`<12px` 清零在渲染层得到验证**（不只是源码 grep）：10 个页面全部 `最小字号 = 12px`。
- **D8 成果未回退**：hosts/assets/audit/ops 四页横向溢出仍为 **0**（字号不影响 `table-layout: fixed` 列宽，与 §33 #11 结论一致）。

**遗留（本批次不做，已挂账）**

1. **`AlertsView` 规则表 68px 横向溢出**（新发现，非批次 8 引入）：规则表列宽 `类型120 + 级别90 + 持续80 + 启用80 + 静默80 + 操作130 = 580` + 4 个 `min-width`（名称140/触发条件200/通知渠道150/应用范围140 = 630）= **1210px > 可用 1142px**。属 **D8 列宽**类问题，AlertsView 不在 D8 已修 4 页内，留待 D8 续做。
2. **ECharts canvas 字号**：`charts/echarts.js`、`RedisTab.vue`、`TopologyGraph.vue`、`NetworkTab.vue` 仍有 `fontSize: 10/11`（图表轴标签/图例）。这是 canvas 渲染、非 CSS `font-size`，归 **D9（图表与令牌脱节）** 统一处理，不在本批次。
3. **13px 的逐处归类**：全站 337 处 `13px` 中，"正文（应 14px）"与"次要（保持 13px）"需逐处判断，本批次只做了**表格正文**这一处结构性提升（覆盖绝大多数正文场景），页面级零散 13px 的归类留待后续按页复核。
4. `web/design/*.html`（设计原型，不进构建）与 `assets/print.css`（`pt` 打印单位）不在范围内。

### 1.5 筛选区尺寸规范（D11 · 独立议题）


**优化目标**：消除"搜索区域大小不一"。统一一种筛选容器、一个控件高度档、五档宽度令牌。

#### 1.5.1 实测：现状三处不统一

| 维度           | 当前状况                                                                                                                                                                                    | 证据                                                                                            |
| ------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| **控件 size**  | `AssetListView` / `HostsView` 主工具栏用默认 size（40px 高）；`LogsView` / `InspectView` / `AuditView` / `AlertsView` 用 `size="small"`（32px 高）                                                     | `AssetListView.vue:46/55/63/79/90`（无 size）vs `InspectView.vue:17/23/25`（small）                |
| **控件 width** | `LogsView` 7 档（96/180/200/210/240/320/400px）；`InspectView` 5 档（130/140/150/170/240px）；`HostsView` 3 档（100/120/200px）；`AssetListView` 3 档（130/150/260px）；`AlertsView` 3 档（110/150/220px） | `grep style="width:"` 各页汇总                                                                    |
| **容器形态**     | `.filter-bar`（带 1px 边框 + `var(--fill-1)` 底色 + 圆角） / `.toolbar`（裸 flex，无边框） / `.rule-filter-bar`（内联 style 裸 flex） / `.filters`（裸 flex）四种                                                 | `AssetListView.vue:1943` / `LogsView.vue:822` / `AlertsView.vue:202` / `SecurityView.vue:677` |

#### 1.5.2 规范基线（写入 `style.css` 一次生效）

| 维度      | 规范                                                                                 | 备注                                                       |
| ------- | ---------------------------------------------------------------------------------- | -------------------------------------------------------- |
| 控件 size | **`size="small"` 全站统一**（**实测渲染 24px 高**；原写 32px 有误，见 §1.5.7 偏差说明） | 24px 是用户截图里"看起来正常"的那一档；与正文 14px + 行高 1.5 的视觉比例最协调（EP 档位 large=40 / default=32 / small=24） |
| 筛选容器    | 统一 **`.filter-bar`**（带边框、`var(--fill-1)` 底色、`padding: 10px 12px`、圆角 `var(--r-md)`） | 删除裸 flex 写法；`<div style="display: flex; gap: 8px">` 全部替换 |
| 控件宽度    | 5 档令牌：`--w-xs:96 / --w-s:130 / --w-m:180 / --w-l:240 / --w-xl:320`                 | 见下表"按角色定档"                                               |
| 容器间距    | `display:flex; gap:10px; flex-wrap:wrap`                                           | 与现有 `.filter-bar` 一致                                     |
| 高度对齐    | 容器内若同时出现 `el-button` 与 `el-input`，靠 `align-items: center` 保证基线对齐（已是现状）             | —                                                        |

#### 1.5.3 宽度档位映射（按"角色"，按"最长候选项字数"反推）

| 角色   | 档位 | 宽度    | 适用                                    | 取证（最长候选项）                                                        |
| ---- | -- | ----- | ------------------------------------- | ---------------------------------------------------------------- |
| 紧凑枚举 | XS | 96px  | 状态（正常/异常）、模式（关键词/正则）、单字角色             | `LogsView` 模式选择 96px ✅；`HostsView` 刷新间隔 100px                    |
| 默认下拉 | S  | 130px | 短枚举（类型 6-8 项、来源 3 项、分组）               | `InspectView` 类型 150px / 节点 150px；`HostsView` 分组 120px           |
| 长下拉  | M  | 180px | 中等枚举（节点列表 10-30 项、来源、归类 6-10 项）       | `AuditView` 分类 150px / `LogsView` 节点 200px                       |
| 关键搜索 | L  | 240px | 关键词搜索（带前缀图标）、IP/主机名、容器字段              | `InspectView` 关键词 240px ✅；`AssetListView` 关键词 260px（**接近**，下移一档） |
| 时间范围 | XL | 320px | `el-date-picker type="datetimerange"` | `LogsView` 400px（**过大**，下移一档）                                    |

> **两处需要收缩**：`LogsView` 的 `el-date-picker` 从 400px 改 320px（XL 档上限），`AssetListView` 的关键词 260px 改 240px（L 档）。其它一律按档就近归整。

#### 1.5.4 落地方式（建议两阶段，避免一次改动面过大）

- **阶段 A · 容器统一**（不伤各页布局）：把 4 类裸 flex 容器全部替换为 `SectionCard` 内的 `<div class="filter-bar">`，或新增 `<FilterBar>` 规范件；`style="display: flex; gap: 8px"` 全部删掉
- **阶段 B · 控件统一**（影响面大，按页推进）：所有 `el-select` / `el-input` / `el-date-picker` 加 `size="small"`（AssetListView / HostsView），`style="width: Npx"` 改为 `class="w-s"` / `w-m` / `w-l` 之类

#### 1.5.5 展示与交互改进

- **展示**：用同一档 size + 同一容器形态后，用户在"主机列表"和"集中日志"看到的搜索区**形状完全一致**，只有"有几列、关键词占多宽"的不同——这才是合理的差异
- **交互**：宽度档固定后，超长候选项会被截断（EP `el-select` 默认行为）+ tooltip 显示全名，**不会再出现"宽 200 装得下、宽 100 装不下"的随机性**
- **可访问性**：24px 高 + `--fs-sm` 13px 字号（详见 1.4）满足 WCAG 2.5.8（Target Size Minimum，24px）；13px 满足 WCAG 1.4.12（Text Spacing）

#### 1.5.6 已知受影响页面

| 页面              | 当前 size   | 容器形态                       | 改动                                       |
| --------------- | --------- | -------------------------- | ---------------------------------------- |
| `AssetListView` | 默认 40px   | `.filter-bar`（有边框）         | 加 `size="small"`，width 改档                |
| `HostsView`     | small 已统一 | `.toolbar`（裸 flex）         | 改用 `filter-bar` 容器                       |
| `LogsView`      | small 已统一 | `.toolbar`（裸 flex）         | 改用 `filter-bar` 容器；时间选择器 400→320         |
| `InspectView`   | small 已统一 | `.filter-bar`（有边框）         | 130/140/150/170/240 → 归到 130/150/180/240 |
| `AuditView`     | small 已统一 | `.card .filter-bar`（双层）    | 去 `.card` 包裹（修双层边框 B 同源）                 |
| `AlertsView`    | small 已统一 | `.rule-filter-bar`（裸 flex） | 改用 `filter-bar` 容器                       |
| `SecurityView`  | small 已统一 | `.filters`（裸 flex）         | 改用 `filter-bar` 容器                       |

#### 1.5.7 实施记录（批次 8 · 2026-10-09）

**已落地（令牌层 `style.css`，一次生效）**

| 位置 | 改动 | 说明 |
| --- | --- | --- |
| `style.css` 令牌区 | 新增 `--w-xs:96 / --w-s:130 / --w-m:180 / --w-l:240 / --w-xl:320` | §1.5.3 五档 |
| `style.css` 规范层 | 新增全局 `.filter-bar`（`flex/gap:10px/wrap` + `padding:10px 12px` + `--fill-1` + `1px` 边框 + `--r-md`） | 全站唯一筛选容器 |
| `style.css` 规范层 | 新增 `.w-xs … .w-xl` 宽度工具类（含 `.el-input.w-*` / `.el-select.w-*` 变体） | 控件加 class 即落档，替代 `style="width:Npx"` |
| `style.css` 规范层 | 新增 `.el-date-editor.el-date-editor.w-*` 双类名规则（`width` + `flex:none`） | 日期选择器特例，见"偏差说明 2" |

**已落地（阶段 A · 容器统一）**

- `hosts / assets / topology / inspect / alerts / logs / audit / security` 8 页的 `.toolbar` / `.rule-filter-bar` / `.filters` / 内联 `display:flex` 全部收敛为 `.filter-bar`；`AuditView` 同时去掉 `.card` 双层包裹（修 B 类双层边框同源）。
- 实测 8 页 `.filter-bar` 形态**完全一致**：`padding=10px 12px / gap=10px / border=1px / bg=rgba(148,178,224,0.04) / radius=8px`。

**已落地（阶段 B · 控件统一）**

- 8 页筛选控件一律 `size="small"` + `class="w-*"`（替代全部 `style="width:Npx"`）。
- 实测宽度落档（1440×900，`el-input/el-select/el-date-editor` 实测宽×高）：

| 页面 | 控件实测宽度 | 命中档位 |
| --- | --- | --- |
| `assets` | `[130,130,130,130,240,130]` | S/S/S/S/L/S |
| `logs` | `[320,96,240,180,180,180]` | XL/XS/L/M/M/M |
| `topology` | `[240,130,240,240]` | L/S/L/L |
| `alerts` | `[240,130]` | L/S |
| `audit` | `[130,180,180,320,130]` | S/M/M/XL/S |
| `inspect` | `[130,130,240]` + `[130,130,180]` | S/S/L + S/S/M |
| `security` | `[130,130]`（`SectionCard #actions` 槽） | S/S |
| `hosts` | `[130,240]` | S/L |
| `RefreshBar` | 刷新间隔下拉 `96×24` | XS |

- **`RefreshBar` 刷新间隔下拉** `style="width:92px"` → `class="w-xs"`；实测 `el-select.el-select--small.w-xs = 96×24`（§1.5.3 点名的"刷新间隔属 XS"案例）。

**偏差说明（实测 vs 规格）**

1. **`size="small"` 渲染高 = 24px，不是规格 §1.5.2 原写的 32px**。EP 组件档位为 `large=40 / default=32 / small=24`，规格把 `default` 的 32px 误记为 `small`。已在 §1.5.2 / §1.5.5 就地更正；24px 满足 WCAG 2.5.8（Target Size Minimum 24px）。
2. **日期选择器宽度需双类名特例**：EP `.el-input__wrapper{flex-grow:1}` 会把 `datetimerange` 根节点（**根自带 `.el-input__wrapper` 类**）作为 flex 子项拉满；且 EP `.el-date-editor.el-input__wrapper{width:var(--el-date-editor-width)}`（`datetimerange` 默认 **400px**）与 `.w-xl` 同特异度 (0,2,0) 且 EP CSS 后置加载 → 源序压不过。故用 `.el-date-editor.el-date-editor.w-xl{width:var(--w-xl);flex:none}`（**提到 (0,3,0)** + 置 `flex:none`）。修前实测 `logs` 570px / `audit` 424px，修后均 **320px**。

**实测复核（无头 Chrome + mock API，1440×900）**

- 15 个路由 `document.scrollWidth == innerWidth == 1440` → **页面级零横向滚动**。
- 8 个 `.filter-bar` 页形态一致（见上）；`.muted` 计算色 = `rgb(111,131,166)`（`--t3`）、字号 13px ✓。
- 构建 `✓ built`；测试 `32 files / 196 tests` 全绿。

**遗留（本批次不做，已挂账）**

1. **`OpsView` `.op-toolbar` 筛选行**（节点/动作/状态/范围 4 个下拉 `168/168/140/136px` + 搜索 `250px`）与 **`ReportView` 表单行**（`200/140px`）仍是 magic width。二者**不在 §1.5.6 页面清单内**；`OpsView` 若要归整需连同搜索框 + 按钮簇整体转 `size="small"`（当前整条工具条为 `default` 32px），属更大的改动，留待后续按页推进。
2. `InspectView` 定时巡检行的 `el-input-number`（"每 N 小时"）保留 EP 默认宽（实测内层 `.el-input` 120px）——数字 + 步进器不宜压到 96px。

---

### 1.6 描述段与上方元素距离规范（D12 · 独立议题）

**优化目标**：解决"有些页面描述性文字跟其他元素挨得太近"。让"小字说明"在视觉上**可识别为独立信息层**，同时不再因 margin-top 6/8/10/14/26 散落而不可预测。

#### 1.6.1 实测：描述类样式散落

| 选择器              | 位置                       | margin-top                | 行高   | 字号              |
| ---------------- | ------------------------ | ------------------------- | ---- | --------------- |
| `.verify-note`   | `AuditView.vue:557`      | **6px**（最贴脸）              | —    | 13px            |
| `.verify-extra`  | `AuditView.vue:557`      | 6px                       | —    | 13px            |
| `.verify-caveat` | `AuditView.vue:559`      | 8px                       | —    | 12px            |
| `.synonyms`      | `AssetModelView.vue:512` | 10px                      | 22px | —               |
| `.schedule-hint` | `InspectView.vue:509`    | 10px                      | —    | —               |
| `.tip`           | `InspectView.vue:510`    | 10px                      | —    | —               |
| `.foot`          | `LogsView.vue:839`       | 10px                      | —    | —               |
| `.field-hint`    | `LogsView.vue:827`       | 0（行内）                     | —    | 12px            |
| `.note`          | `AssetListView.vue:2211` | **14px**                  | 20px | 13px（继承 .muted） |
| `.note`          | `InspectView.vue:507`    | 10px                      | 20px | 13px            |
| `.note`          | `AssetListView.vue:2100` | 14px                      | —    | —               |
| `.alert-gap`     | `LogsView.vue:823`       | 0 / `margin-bottom: 10px` | —    | —               |
| `.alert-gap`     | `InspectView.vue:511`    | 0 / `margin-bottom: 12px` | —    | —               |

**关键观察**：

- `AuditView` 的 `.verify-note{margin-top:6px}` 是**全站最贴脸**的描述段——它紧跟"完整性校验结果"行，但视觉上 6px 不足以把它分成另一层
- `AssetListView` 的 `.note{margin-top:14px}` 是**全站最远**的——14px + 上面 `.filter-bar` 12px padding 实际净空 26px
- 同一应用内 6px ↔ 14px，**差距 2.3×**，用户感觉"有的页面贴脸、有的页面独立成段"完全正常

#### 1.6.2 规范基线

> 一个原则：**描述段是"上方主体的延伸说明"，不是独立段落**。所以它不能太远（远了像另起一段），也不能太近（近了读着像主控件的注释）。

| 场景                             | 令牌               | 值    | 适用                                                       |
| ------------------------------ | ---------------- | ---- | -------------------------------------------------------- |
| **行内提示**（与控件同行的解释文字）           | `--desc-tight`   | 6px  | 仅在控件**右侧**用 `<span class="muted">` 表达"占位符 / 输入提示 / 权限说明" |
| **标准距离**（描述段紧接上方内容块）           | `--desc-base`    | 10px | `.note` / `.schedule-hint` / `.tip` / `.synonyms`        |
| **分块距离**（描述段在独立容器内，前面是另一个独立容器） | `--desc-block`   | 14px | `AssetListView` 那种"filter-bar 块 → 独立说明段"                 |
| **段落内行距**                      | `--lh-base: 1.5` | —    | 与正文同档（1.1 已提令牌，1.4 已落档，1.6 强校验）                          |

#### 1.6.3 配套：`.muted` 全站统一（写在 `style.css` 一次生效）

```css
/* 描述段：全站仅此一处定义 .muted；其它 8 处页内覆写全部删除 */
.muted {
  color: var(--t3);
  font-size: 13px;
  line-height: var(--lh-base);
}
.muted-tight { margin-top: var(--desc-tight); }
.muted-base  { margin-top: var(--desc-base); }
.muted-block { margin-top: var(--desc-block); }
```

**页内 `.muted` 覆写全部删除**（实测**共 15 处**，含下文所列 9 处之外的 `AssetListView` / `AssetModelView` / `AssetTopologyView` / `ProfileView` / `RolesView` / `UsersView` / `UpgradeView` / `IntelligenceView`；逐处见 §1.6.7）。原挂账清单：`AuditView:576` / `LogsView:819` / `InspectView:490` / `AlertsView:1516` / `OpsView:1274` / `SecurityView:716` `:948` / `StatusView:121` / `DialTestView:250`。

#### 1.6.4 展示与交互改进

- **展示**：用户看到"小字说明"时，**第一眼能感知到它是解释**——`--t3`（第三级文字色）比 `--t2` 弱、比 `--t1` 强；行高 1.5 让密集文字有呼吸感
- **交互**：当描述段是"权限受限"或"输入提示"时，**可以加上 `:hover` 显示完整说明或更多上下文**（细节由各页决定，本规格不强制）
- **可访问性**：13px 已是 1.4 规范的"次要"档（最低 12px），行高 1.5 满足 WCAG 1.4.12（Text Spacing）

#### 1.6.5 改动清单（按页）

| 页面               | 现状类                                  | 改后                       |
| ---------------- | ------------------------------------ | ------------------------ |
| `AuditView`      | `.verify-note{margin-top:6px}`       | 改 `.muted-base`（10px）    |
| `AuditView`      | `.verify-extra{margin-top:6px}`      | 改 `.muted-base`          |
| `AuditView`      | `.verify-caveat{margin-top:8px}`     | 改 `.muted-base`（统一 10px） |
| `InspectView`    | `.schedule-hint{margin-top:10px}`    | 改 `.muted-base`          |
| `InspectView`    | `.tip{margin-top:10px}`              | 改 `.muted-base`          |
| `InspectView`    | `.note{margin-top:10px}`             | 改 `.muted-base`          |
| `LogsView`       | `.foot{margin-top:10px}`             | 改 `.muted-base`          |
| `AssetListView`  | `.note{margin-top:14px}`（L2211）      | 保留 14px（这是分块场景的"分块距离"档）  |
| `AssetModelView` | `.synonyms{margin-top:10px}`         | 改 `.muted-base`          |
| `OpsView`        | `.head-actions{margin-top:10px}`     | 改 `.muted-base`          |
| `AlertsView`     | `.muted` 内联 `margin-top: 10px`（L319） | 改 `.muted-base`          |

#### 1.6.6 已知遗留：内容为"嵌套在控件旁的提示"

- `LogsView` 的 `.field-hint`（行内）+ `.field-row`（`margin-bottom: 10px`）：这是"输入框 + 候选项"的复合控件，**不算描述段**，保留现状
- `AlertsView` 的 `.event-toolbar-hint`（行内）：同上，保留
- `AlertsView` 的 `tpl-desc`（行内）：同上，保留

> 这三条**不进入 1.6 的改动**，避免误改行内控件的间距。

#### 1.6.7 实施记录（批次 8 · 2026-10-09）

**已落地（令牌层 `style.css`，一次生效）**

| 位置 | 改动 | 说明 |
| --- | --- | --- |
| `style.css` 令牌区 | 新增 `--lh-base:1.5` | 描述段行距（§1.6.2 行距档） |
| `style.css` 令牌区 | 新增 `--desc-tight:6 / --desc-base:10 / --desc-block:14` | 三档距离 |
| `style.css` 规范层 | 全局 `.muted{color:var(--t3);font-size:13px;line-height:var(--lh-base)}` + `.muted-tight/-base/-block{margin-top}` | 全站**唯一** `.muted` 定义 |

**已落地（删页内 `.muted` 覆写，共 15 处）**

`AuditView` / `LogsView` / `InspectView` / `AlertsView` / `OpsView` / `SecurityView`(×2) / `StatusView` / `DialTestView` / `AssetListView` / `AssetModelView` / `AssetTopologyView` / `ProfileView` / `RolesView` / `UsersView` / `UpgradeView` / `IntelligenceView`（压缩单行内）。

保留的**上下文选择器**（非裸覆写，不动）：`.sec .muted` / `.panel-title .muted` / `.ex-row .muted` / `.discover-count.muted`。

**已落地（描述段距离归一到 `.muted-base` / `--desc-base`）**

| 页面 | 原值 | 改后 |
| --- | --- | --- |
| `AuditView` `.verify-note` / `.verify-extra` | `margin-top:6px`（全站最贴脸） | `var(--desc-base)` 10px |
| `AuditView` `.verify-caveat` | `margin-top:8px` | `var(--desc-base)` 10px |
| `InspectView` `.note` / `.schedule-hint` / `.tip` | `10px` | `var(--desc-base)` 令牌化 |
| `LogsView` `.foot` | `10px` | `var(--desc-base)` |
| `AssetModelView` `.synonyms` | `10px` | `var(--desc-base)` |
| `OpsView` `.head-actions` | `10px` | `var(--desc-base)` |
| `AssetListView` `.note` | `14px`（L2211） | **保留**（§1.6.2 分块场景，`--desc-block`） |

**偏差说明（颜色层级）**

§1.6.3 定的是 `.muted{color:var(--t3)}`（较暗的 `--text-muted`）。历史上部分页 `.muted` 用的是 `--t2`（较亮的 `--text-dim`）。本批次**统一到 `--t3`**——即"描述段比正文更弱"，与 §1.6.4「第一眼能感知到它是解释」一致。实测计算色 = `rgb(111,131,166)` = `--t3` ✓。

**实测复核**

- `.muted` 计算色 = `rgb(111,131,166)`、字号 13px（渲染层验证，非源码 grep）✓。
- 源码 grep：base `.muted` 覆写清零，仅剩 4 处上下文选择器 ✓。
- 构建 `✓ built`；测试 `32 files / 196 tests` 全绿。

---

## 2. PS-01 · 登录页 `/login`

**功能点**：用户名 / 密码 / 登录 / 品牌展示 / 错误提示 / 页脚。

**现状问题（✅ 已核实）**

- [可访问性] 两个输入框**都无 `autocomplete`**（L18-39），密码管理器无法填充——对比 `ChangePasswordSubView` 三个输入都有（L19/29/38）
- [可访问性] 错误 alert 无 `role="alert"`，且位于密码框与按钮之间，出现时把按钮顶下去 → **布局跳动**
- [状态缺失] 无失败计数/锁定提示；网络错误与凭证错误措辞不分（统一 `e.message || '登录失败'`，L117）
- [交互成本] `router.replace('/')` 硬编码（L111/114），深链接被登出后回来要重新导航
- [视觉一致性] `.login-card` 仍带 `glass` class；旧令牌 6 处；硬编码 px 19 处；`.bg-glow` 用 `filter: blur(40px)` + 600px 径向渐变，低端设备合成开销

**优化目标**：3 秒内完成登录，且失败时知道为什么。

**展示改进**

- 错误区改为**表单上方固定预留位**（高度锁定），出现时不推动布局；加 `role="alert"`
- 品牌区保留（已有 Logo + 名称 + 副标题 ✅），副标题在窄屏不换行
- 光晕从 `blur(40px)` 改为静态径向渐变或缩到 300px，去掉 `filter`

**交互改进**

- `autocomplete="username"` / `"current-password"`；登录按钮 `native-type="submit"`（当前回车靠 input 的 `@keyup.enter`，语义错位）
- 连续失败 3 次后显示节流/锁定提示；网络错误与凭证错误分开措辞
- 支持 `?redirect=` 回跳

**优先级**：价值 ★★☆ · 成本 低

---

## 3. PS-02 · 对外状态页 `/status`

**功能点**：整体状态徽标 / 组件列表（状态·延迟·24h 可用率·最近检查）/ 自动刷新 / 品牌名。

**现状问题（✅ 已核实）**

- [状态缺失] **无历史事件 / 90 天可用率条 / 订阅入口** —— 对外状态页的两块标准配置全缺，访客只能看"此刻"
- [视觉一致性] 手写 `.pill`（L116-120）而未用 `StatusPill`；scoped 内 9 处硬编码色（`#15803d`/`#b45309`/`#b91c1c`…）与非 scoped 令牌块（L188-196）**重复定义**，改主题要改两处
- [状态缺失] `loading` 变量模板未使用（L50）；首次加载失败文案"显示的是上一次结果"在首次即错误
- [分享] 无定制 `<title>` / og 标签（分享到 IM 无预览卡片）；hash 路由 `#/status` 不够干净；无品牌 Logo（登录页有）
- [可访问性] 状态仅颜色 + 文字；`.type` 用 `text-transform: uppercase`（中文无影响但英文域名会变形）

**优化目标**：让外部访客**不需要解释**就能看懂"服务是否正常、过去是否稳定、出问题怎么知道"。

**展示改进**

- 顶部增加 **90 天可用率条**（每日一格，绿/黄/红）+ 历史事件时间轴
- 加**颜色图例**与一行"延迟 / 24h 可用率是怎么测出来的"（拨测含义说明）
- 补品牌 Logo（与登录页同源）
- 手写 `.pill` → `StatusPill`；9 处硬编码色改令牌

**交互改进**

- 补订阅入口（邮件 / Webhook）
- `<title>` 改为「品牌名 · 服务状态」，补 meta description / og 标签
- 30 秒轮询加 `visibilitychange` 暂停
- ~~给 `/status` 提供 history 路由或短链~~ → **已决定不做**（2026-10-09 确认，见 §33 #1）：维持 hash 路由 `#/status`，不引入服务端 rewrite。后续若确需干净短链，作为独立部署侧工单另行评估

**优先级**：价值 ★★★★（唯一对外页面，代表产品门面）· 成本 中（历史/订阅需后端接口，其余零依赖可先做）

---

## 4. PS-03 · 首页概览 `/`

**功能点**：页头 / AI 研判条 / 7 个可拖拽 block（健康度、4×KPI、紧急告警、智能运维、主机概览、中间件概览、最近告警）/ 自定义布局 / 30 秒轮询。

**现状问题（✅ 已核实）**

- [状态缺失] `get()` 把失败吞成 `{}`（L91），**后端全挂与"真的没数据"长得一模一样**；除中间件外 6 个区块无骨架（L269-280 只有 middleware 传 loading）
- [性能] 30 秒轮询**不判断页面可见性**（L287-290），而 `RefreshBar`(L84-92) 与 `HostsView`(L1104-1113) 都做了暂停
- [信息架构] **智能运维区块被 `PINNED_KEYS` 固定置顶且不可隐藏**（`useOverviewLayout.js:17/32-42`），260px 重区块把 KPI/健康度/紧急告警压到第二屏
- [交互成本] 区块被隐藏后**只能在编辑模式找回**，非编辑态无任何"已隐藏 N 个"提示
- [视觉一致性] 自绘 `.ov-card-head/.ov-card-bar`，与 `SectionCard` 是同结构的第二份实现（L28-41）
- [硬编码样式] `IntelligenceBlock` **15 处硬编码色**（`#fb923c`/`#fde68a`/`#a7f3d0`/`#22d3ee`…），整份 `<style>` 压成单行 1900 字符（L58）
- [可访问性] KPI 卡、主机方块、告警行、中间件卡均为 `<div @click>`，无键盘入口
- [信息密度] `HostOverview` 短名截到 6 字符（`web-01` 与 `web-011` 显示相同）；阈值口径两套（方块 85/95/100 vs popover 70/80/85），同一台机器两处颜色可能矛盾
- [性能] 每台主机一个 `el-popover` 且 hover 触发，500 台 = 500 个常驻实例（L13-21）

**优化目标**：打开首页 3 秒内回答三个问题——**有没有事要处理 / 多少台机器 / 哪些中间件**。

**展示改进**

- `.ov-card` 换 `SectionCard`，编辑工具进 `#actions` 插槽
- 智能运维区块**改为可折叠**（默认收起，有 critical 时自动展开）
- KPI 卡与 `KpiCard.vue` **收敛为同一组件**（现两套：84px vs 70px 高、26px vs 20px 数值、`--r-md` vs `--r-lg`）
- `IntelligenceBlock` 15 处硬编码色令牌化，单行 style 展开
- 主机方块补非颜色编码（离线角标），短名改 8 字符 + popover 显示全名


**交互改进**

- 加页级 `loadError` + 首屏骨架；轮询加 `visibilitychange` 暂停
- 隐藏区块加「已隐藏 N 个 · 恢复」入口（非编辑态可见）
- 研判条「查看详情」带 node/rule 过滤参数跳 `/alerts`（当前跳全量，过去还要自己找）；加 `role="alert"`
- `RecentAlerts` 与 `CriticalAlerts` 合并为参数化 `AlertList`（`timeAgo`/`goAlerts`/`.dot.sev-*` 逐行重复）
- **告警去重（默认做）**：`recentAlerts` 过滤掉已在 `criticalAlerts` 出现的条目，消除同屏重复（§33 #7）
- ~~放开智能运维的隐藏限制~~ → **本轮不做**（2026-10-09 确认，§33 #6）：`PINNED_KEYS` 维持现状，隐藏仍不可用。上方展示改进中的"可折叠"照做（折叠 ≠ 隐藏）
- popover 改点击触发或虚拟滚动（大集群性能）
- 全部 `<div @click>` 语义化

**优先级**：价值 ★★★★★（全站入口）· 成本 中

---

## 5. PS-04 · 主机列表 `/hosts`

**功能点**：页头（分组管理/批量升级/添加主机/打印）/ 普通主机·网闸代理双页签 / 刷新控制 / 状态筛选 / 分组筛选 / 搜索 / 列设置 / 多选批量 / 10 列排序 / 分页 / 行下钻 / 行内编辑显示名 / 行内改分组 / 升级·删除 / 添加主机抽屉 / 深链 `?node=`。

**现状问题（✅ 已核实）**

- [信息密度] 12 列最小宽合计约 **1254px**（可用宽仅 1168px），1440px 以下必横向滚动；「操作」列 `fixed="right"` 遮挡内容（L131-254）。**10 列当前全部默认展示**（`L675` 全勾选），其中「磁盘读写」120px 挤一行装"读 x / 写 y"是**全表最紧的单元格**（L244-251）
- [信息架构] 批量状态藏在页头按钮文案里（`批量升级 (3)`），站内已有 `BatchBar` 却未用（AssetListView/OpsView 在用）
- [交互成本] 添加主机抽屉极长：网闸场景约 10 个 section、6 段命令块（每段 `max-height:200px` 独立滚动）+ 20+ 表单项，**无内部目录/锚点/步骤条**
- [状态缺失] `/api/v1/install-info` 失败只 toast 且置空，命令区显示"等待加载…"但**无重试入口**（L932-941）
- [视觉一致性] 行高亮三档三种写法：`var(--danger-dim)` / `rgba(244,63,94,0.08) !important` / `var(--warn-dim)`，中间那档既硬编码又 `!important`（L1370-1378）
- [响应式] **0 处 `@media`**（样式段 1153-1568）
- [性能] 默认 20 秒一次 `Promise.all` 拉 4 个接口；代理页签下仍全量拉主机数据（L865-874）
- [硬编码样式] 6 处硬编码色（L1251/1374/1388/1481/1518/1531）
- 亮点：**四个规范件全部套用**，是全站最完整的文件——应作为其它页面的参照基准

**优化目标**：一屏内看到更多机器，且常用操作不用翻页/不用进抽屉找。

**展示改进**

- 列宽重排 + 操作列瘦身（**10 列全默认展示，不靠收起换空间**）→ **1440px 下无需横向滚动**；窄屏（≤1280px）响应式降级才收起低频列
- **「磁盘读写」改两行堆叠**（与「流量↓/↑」同形态）：120px 挤一行装"读 x / 写 y"是本页最紧的单元格，改两行后反而更清晰
- 行高亮三档统一 `--danger-dim`/`--warn-dim`/`--info-dim`，去 `!important`
- 次级导航（自绘 `.subnav-item`）改为带 `role="tab"` 的语义控件，或统一 `el-radio-group`
- **筛选条按 §1.5 收敛**：当前 `.toolbar` 是裸 flex（虽然 `size="small"` 已统一），**改 `.filter-bar` 容器**；`refreshInterval` 100px → XS、`groupFilter` 120px → S、`keyword` 200px → M（接近 L 240，但 200 更合适，因为是带前缀图标的搜索）

**交互改进**

- 批量操作改挂 `BatchBar`（选中即出现在表格上方，与 AssetListView/OpsView 一致）
- 抽屉加**步骤条 / 锚点导航**，命令块默认折叠；`install-info` 失败加重试 + 骨架
- 补 1200 / 900 两档断点
- 代理页签表格补分页、排序、"共 N 个"计数
- 改分组下拉里"当前分组"加 ✓ 标记（现仅 disabled，看不出当前值）

**优先级**：价值 ★★★★★（最高频列表页）· 成本 中

#### 5.1 列宽重排测算（磁盘读写 / 负载 / 流量**保持默认展示**）

> **方案修正**：初稿曾建议"默认收起磁盘读写 / 负载 / 流量"，**该建议已撤回**。这三项是容量与排障的判读依据（磁盘读写看 IO 瓶颈、负载看饱和、流量看异常外联），日常巡检几乎每次都要扫一眼；把它们收进列设置等于**把每天的固定动作变成"先点开列设置再勾选"**。维持现状的全默认展示（`HostsView.vue:675` `selectedCols = ref(colOptions.map(c => c.key))` 全开）。

**现状实测**（`HostsView.vue:131-260`）

| 列         | min-width  | 内容形态                        |
| --------- | ---------- | --------------------------- |
| selection | 44         | 复选框                         |
| 主机名称 / IP | 160        | 两行（名称 + IP）                 |
| 状态        | 80         | tag                         |
| 分组        | 100        | 文字                          |
| Agent 版本  | 110        | 如 `v1.30.35`                |
| CPU       | 90         | 进度条                         |
| 内存        | 90         | 进度条                         |
| 磁盘使用      | 100        | 进度条                         |
| 流量↓/↑     | 120        | **两行堆叠**（↓ / ↑）             |
| 负载        | 70         | 单值 `0.52`                   |
| 磁盘读写      | 120        | **一行**"读 x / 写 y" ← 最紧      |
| 操作        | 170        | 3 个 link 按钮，`fixed="right"` |
| **合计**    | **1254px** |                             |

**可用宽度**：`1440 − 232（sidebar）− 40（content padding 20×2）= 1168px` → **缺口 86px**。

**重排方案**（不删列、不改默认勾选，纯靠压缩与形态调整）

| 列         | 现状       | 调整后      | 增量      | 依据                                 |
| --------- | -------- | -------- | ------- | ---------------------------------- |
| 主机名称 / IP | 160      | 150      | −10     | 两行布局，150 仍容得下长主机名                  |
| 状态        | 80       | 76       | −4      | tag 文字仅 2 字                        |
| 分组        | 100      | 92       | −8      | 分组名通常 2–6 字                        |
| Agent 版本  | 110      | 96       | −14     | `v1.30.35` 共 8 字符，等宽字体下 96px 足够    |
| CPU       | 90       | 84       | −6      | 进度条 + 百分比                          |
| 内存        | 90       | 84       | −6      | 同上                                 |
| 磁盘使用      | 100      | 92       | −8      | 同上                                 |
| 流量↓/↑     | 120      | 100      | −20     | 两行堆叠，宽度由较长一行决定（约 80px）             |
| 负载        | 70       | 60       | −10     | 单值 4 字符                            |
| **磁盘读写**  | 120      | **140**  | **+20** | 改**两行堆叠**（读 / 写各一行），与流量列同形态        |
| 操作        | 170      | 150      | −20     | 3 个 link 按钮收 padding；或改「详情」+「更多」下拉 |
| selection | 44       | 44       | 0       | —                                  |
| **合计**    | **1254** | **1168** | **−86** | **恰好等于可用宽度**                       |

**三层落地策略**

1. **基础（必做）**：上表列宽重排 + 磁盘读写改两行 → 1440px 下 10 列全开无横向滚动
2. **兜底（推荐）**：补 `≤1280px` 断点，此时才收起 `version` 与 `group`（低频），其余 8 列保留 → 1280px 下约 980px，可容纳
3. **可选（给 1366px 机器留余量）**：操作列改「详情 + 更多下拉」（再省约 40px），或对 `diskRW`/`netIn` 用 `show-overflow-tooltip`

> **注意**：1168px 是**恰好等于**可用宽度的临界值，无余量。实施时需在 1440px 窗口实测确认；若实测仍溢出（EP 表格有默认 cell padding 会吃掉额外空间），优先走第 2 层的响应式降级，而不是回头收起磁盘读写 / 负载 / 流量。

**实施记录（2026-10-09，已落地并实测）**

本轮把第 1 层（基础列宽重排）**同时应用到 4 个溢出页面**，不只主机列表——因为 `#11` 实测发现 `AuditView`(234px) / `OpsView`(222px) / `AssetListView`(136px) 的溢出比主机列表更严重。实测可用宽度是 **1142px**（1440 − 232 sidebar − 40 padding − 滚动条），比规格里估的 1168px 更紧，所以各页都按 1142 重算。

| 页面              | 原合计   | 重排后   | 主要调整                                                                                             |
| --------------- | ----- | ----- | ------------------------------------------------------------------------------------------------ |
| `HostsView`     | 1254  | 1116  | 名称 160→140、状态 80→72、分组 100→76、CPU/内存/磁盘使用保持 90/90/100、流量 120→100、负载 70→72、磁盘读写 120→116（**改两行堆叠**）、操作 170→126（+wrap）、**表头「Agent 版本」→「版本」** |
| `AssetListView` | 1262  | 1062  | 名称 260→220、类型 140→110、归属节点 140→120、上报状态 120→100、最近上报 130→110、来源 100→84、责任人 110→96、操作 220→180 |
| `OpsView`       | 1348  | 1100  | 下发时间 152→140、节点 200→150、动作 200→170、批次 112→96、状态 106→92、触发人 176→140、耗时 146→120、操作 214→150（+wrap；`.dur-bar` 40→36、数值列 58→52） |
| `AuditView`     | 1400  | 1150  | 时间 180→160（**改两行：日期 / 时分秒**）、分类 110→92、操作者 140→120、操作 240→190、变更摘要 240→190、来源 200→150、状态码 90→80、结果 90→76、资产改动 110→92 |

实测结果（1440×900，Chrome headless + 列宽诊断脚本）：**4 个页面横向溢出全部归零**（112/136/222/234 → 0），**表头无裁切**。

两条实施经验（都已写进代码注释）：

1. **`min-width` 列的表头裁切是隐蔽回归**：`el-table` 表头是 `nowrap`，列宽压过头时表头文字会被裁掉而不报错。判定方法是 `th .cell` 的 `scrollWidth > clientWidth`——诊断脚本已内置该检查，`HostsView` 的「Agent 版本」就是靠它发现的（该列**改动前就裁**：110px 装不下 6 字符 + 排序图标），最终以短表头「版本」根治。
2. **进度条列有宽度下限**：`HostsView` 的 CPU / 内存 / 磁盘使用列内含 `mini-bar`（`flex:1; min-width:40px`）+ 数值（约 34px），内容区低于约 80px 就会把进度条压到 min-width 之下 → 这三列**不再压缩**（90 / 90 / 100 保持原值）。

**第 2 层（≤1280px 断点降级）本轮未做**：自动取消勾选 `version`/`group` 会让用户"列不见了但不知道为什么"，且 1280 及以下仍有原生横向滚动可用。触发条件：若实际部署中存在 ≤1280px 的运维终端，再补断点逻辑（`HostsView` 已有 `selectedCols` 列设置，改造成本低）。

---

## 6. PS-05 · 主机详情 `/node/:name`

**功能点**：对象头（主机名·IP·Agent 版本 + CPU/内存/磁盘/负载 4 个实时 KPI）/ 主机切换 / 5 个 Tab（概览·基础监控·进程·端口·防火墙）/ WebSocket 实时 / 7 个面板独立时间范围 / 复制主机名·IP。

**现状问题（✅ 已核实）**

- [性能] ~~**最严重**：`setMonitorRef` 在每个面板 ref 挂载时都调一次全量 `loadMonitor`（L630-644）~~ **✅ 已修（2026-10-09）**：实测首次进 Tab2 是 **96 次** range 请求（12 metric × 8 次全量，原估 35 次偏低），修复后 **12 次**。修法：ref 回调改为只补刷「本次才建出来的图」（`if (!existed) loadMonitor(name, key)`）。仍待办：`loadMonitor` 内多指标**串行 await**（L1168）🟡——12 个请求串行发出，可并行化到 1 个批次（见 §33.2b）
- [状态缺失] Tab1 的进程 Top10 与告警事件表**静默 catch**（L1001-1013），`loadErrors` 只有 4 项，无加载态
- [状态缺失] 收到 WS 首帧前，4 个环形图 + 4 张趋势图渲染**全 0**，用户会误读为"这台机器空闲"
- [交互成本] `autoRefresh` 有 ref 且参与 WS 重连判断，但**界面上无开关**（L527）
- [信息架构] 5 个 Tab 的小标题全靠自绘 `.section-title`，用 EP 的 `--el-color-primary` 而非 `--accent`（L1371-1378），切主题与卡片不同色；`<div>` 无标题层级
- [信息密度] Tab1 纵向约 3-4 屏，无分区锚点；Tab2 顶部 1 段总说明 + 7 个面板各自 desc = 同一屏 8 段解释文字
- [视觉一致性] 死代码：`.port-item` 定义两次互相冲突（L1350-1352 vs L1361-1363）、自绘 `.status-pill` 模板无引用（L1407-1410）、media query 引用不存在的 `.device-grid`（L1485/1489）
- [硬编码样式] 7 处，防火墙状态卡整块用旧式硬编码 + fallback（L1409-1516）
- [性能] 15s/30s 两个轮询不判断页面可见性（L1310-1311）

**优化目标**：打开一台机器，不切 Tab 就知道它什么状态；切 Tab 不卡。

**展示改进**

- 对象头保持（已是全站标杆 ✅），补充：端口状态改紧凑 `StatusPill` chips、设备信息两列定义列表（已改 ✅）
- `.section-title` → `SectionCard`（或至少 `--el-color-primary` → `--accent`、`<div>` → `<h3>`）
- 环形图加 **80% / 90% 阈值刻度线**；副标题给有意义信息（「剩余 178 GB」而非「已用/总量」）
- 7 个面板的 8 段解释文字压缩为 **1 段总说明 + 每个面板 tooltip**

**交互改进**

- **修 `setMonitorRef`**（nextTick 后统一 loadMonitor 一次）；`loadMonitor` 内多指标改 `Promise.all`
- 环形/趋势图在收到首帧前显示**骨架而非 0**
- 给 `autoRefresh` 补 UI 开关；轮询加 visibility 暂停
- 7 个面板加**全局时间范围一键切换**（现在逐个改）
- 清理三处死代码

**优先级**：价值 ★★★★★ · 成本 中（性能修复集中在两个函数，回归面小，建议单独一个 commit 先做）

---

## 7. PS-06 · 中间件监控 `/middleware` + 15 个 Tab

**功能点**：展示类型配置 / 15 个 Tab（10 内置 + 5 轻采集）/ `?tab=` 深链 / 各 Tab 通用的 KPI +（部分有）拓扑 + 实例列表 + 详情抽屉。

### 6.1 跨 Tab 一致性（这是本页最大的价值点）

**15 个 Tab 之间的一致性实测** ✅：

| 维度    | 是否统一     | 证据                                                                                                                        |
| ----- | -------- | ------------------------------------------------------------------------------------------------------------------------- |
| 容器类名  | **否**    | Redis 用 `.card`，其余 9 个手写 Tab 用 `.glass`，Light 用 `.glass`                                                                  |
| 区块顺序  | 半统一      | 「KPI→拓扑→列表」是 8 个 Tab 的共识；Mongo/FastDFS/Light **缺拓扑**；K8s 是「KPI→集群卡→节点表→Pod 表」；Redis 多出告警摘要 + 2 个图表区                       |
| 标题层级  | **否**    | 有 `::before` 强调条：Redis/Docker/Nginx/K8s；无：MySQL/Postgres/Kafka/RocketMQ；无 `.section-title` 类：Mongo/FastDFS/Light          |
| 图表配色  | **否**    | Redis 令牌代理 + 4 种写死 tooltip 底色；**其余 9 个 Tab 抽屉图只设 legend 文字色**，`axisLabel.color`/`tooltip` 全落默认                            |
| 图表数量  | 1–12     | Redis 3 概览 + 8~9 抽屉；K8s **0**；其余 1                                                                                        |
| 图表高度  | 三档       | Redis 抽屉 160px / 手写 Tab 300px / Light 180px                                                                               |
| 状态展示  | **三套并存** | `MwStatusDot` + 自绘 `.dot`（7 处）+ 自绘 `.status-dot`（5 处）+ 自绘 `.badge`（2 处）                                                   |
| 「在线」色 | **四种取值** | `--accent`（5 处）/ `--ok`（6 处）/ `--ok`+辉光（3 处）/ `--danger` 对离线（Redis）                                                       |
| 空态    | 10 份手写   | 每个 Tab 复制一份 `.empty-guide`，**无一用 EmptyState**；只有 Redis 提供可执行命令                                                            |
| 图标来源  | **四种**   | 内联 svgIcon 函数 / `@element-plus/icons-vue` / 模板内联 SVG 字符串 / Tab 的 `<img>` logo；Kafka/RocketMQ/Postgres 用了**同一段"四宫格" path** |
| 网格基准  | 五个值      | `minmax(220/200/260/280/160px)`                                                                                           |
| 抽屉形态  | 三种       | 只有 Redis 有对象头 + 时间范围                                                                                                      |
| 分页默认值 | 两种       | 10（手写 Tab） vs 20（Light）                                                                                                   |

### 6.2 各 Tab 关键问题

| Tab                                      | 核心问题                                                                                                                                                                                                                                                                                                               |
| ---------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **RedisTab**（1852 行，全站最重）                | 抽屉趋势图**串行**请求 8~9 个指标且失败静默（L1067-1093）；图表**关闭时不销毁**（L1187 只在卸载时 dispose）→ 反复开关累积 ECharts 实例与 ResizeObserver；`.ms-slave-card` 逐字重复 2 次、`rel-node-meta` 重复 6 次；约 **80 行死代码**（`slotRangeStart/Count/Total`、`clusterSlotView`、`.slot-*` 等）；**74 处硬编码 rgba**；⚠️ 无危险写操作（已核查：无 flush / 无命令执行 / 无 key 删除，只有空态的 agent 安装命令） |
| **MySQLTab**                             | 集群故障结论（脑裂/无主）**只藏在 title 悬浮**（L54/373-384）；`.section-title` 无强调条；徽标 4 个自绘 badge 写死 rgba（L510-513）                                                                                                                                                                                                                  |
| **DockerTab**                            | 「实例拓扑」区块**只有主机头**（容器列表被移除）→ 空壳区块占一屏；**在线主机统计逻辑错误**（B11）；容器 6 态被压成 `up ? normal : abnormal` 两态，paused/restarting/dead 与 running 无法区分                                                                                                                                                                                |
| **K8sTab**                               | 集群卡片**不可点击**且无下钻；节点/Pod 表**无分页**（上千 Pod 全量渲染）；「无异常 Pod」无正向反馈（L101 直接 v-if 隐藏）；**0 图表**                                                                                                                                                                                                                             |
| **NginxTab**                             | 「实例拓扑」硬编码"独立实例"，Nginx 本无集群概念 → 与列表**完全同源同信息**，纯冗余；reading/writing/waiting 三列互斥却横排；`.dot.up` 用 `--accent` 而 `.status-dot.up` 用 `--ok`（同文件两色）                                                                                                                                                                        |
| **MongoTab**                             | 缺拓扑区块，但 Mongo **有副本集概念**（列表已有 topology / 复制延迟列）；`topology` 列直接吐英文原始值（Redis 有 `topoLabel` 中文映射）；**0 值被吞**（B12）                                                                                                                                                                                                      |
| **PostgresTab / KafkaTab / RocketMQTab** | 拓扑硬编码"独立实例"，但角色/复制延迟/`group` 字段已存在 → 有主从/集群数据却不呈现；Kafka/RocketMQ/Postgres **共用同一段"四宫格"图标**，无类型区分；RocketMQ 抽屉「消费延迟」与「消息积压」**绑同一个 `consumerLag`** → 同值两名                                                                                                                                                             |
| **FastDFSTab**                           | 缺 tracker→group→storage 三层拓扑；已用/空闲/总空间三列**未呈现使用率**，用户要自己做除法；`.metric-good/warn/bad` **未定义**（其它 Tab 有）→ 着色类残缺                                                                                                                                                                                                       |
| **LightCollectorTab + 5 个 spec**         | **双定时器**（B3）；`loading` 初值 false（B9）；指标列**无单位/无格式化**（ES 集群状态显示裸 0/1/2；RabbitMQ 内存裸字节）；`.mw-list` 与 `mw.css` 重复定义；pageSize 20 vs 10                                                                                                                                                                                  |


### 6.3 优化目标与方案

**优化目标**：15 个 Tab 从"15 个各自发明的页面"收敛为"一个骨架 + 15 份数据"。

**展示改进**

1. **容器与区块骨架归一**：`.glass`/`.card` → `SectionCard`；空态 → `EmptyState`（Redis 的可执行命令用 `actionText` 或插槽保留）
2. **抽公共组件**：`MwHostCard`（Docker/K8s/Redis 三套主机卡）、`MwSlaveNode`/`MwMasterNode`（Redis 重复 6 处）、`MwDetailHeader`（对象头 + 时间范围，14 个 Tab 抽屉补上）、`TrendChart`（图表工厂）
3. **`MwStatusDot` 两态扩为三态**（normal / warning / abnormal），并把"离线/失败"从黄色改**红色**——当前 Docker 的 paused/restarting/dead、K8s 的 Pending/CrashLoopBackOff 全被压成一态，严重度被系统性低估
4. 拓扑区块：Nginx/Postgres/Kafka/RocketMQ 的"独立实例"硬编码 → 按 `group`/角色**真实聚合**；Mongo 补副本集、FastDFS 补三层、Docker 的拓扑要么补全要么删掉
5. 「在线」色统一为 `--ok`；删除 5 套自绘 `.dot`/`.status-dot`；替换三 Tab 共用的"四宫格"图标

**交互改进**

1. **修 B2**（展示类型配置接到渲染）、**B3**（删双定时器）、**B9**（loading 初值）、**B11**（在线主机统计）、**B12**（0 值）
2. 抽屉趋势图改 `Promise.all`；**关闭即 dispose**；失败在对应卡片上显示错误态而非空白
3. 列表列分级：Redis 11 列默认收起低频列（碎片率/从节点/运行时长）
4. 「实例选择」与「时间范围」目前各 Tab 各自缺失——统一补齐（只有 Redis 有 5 档范围，其余 10 个固定近 1 小时且无控件）
5. RedisTab 拆分 + 清理 80 行死代码（目标降到 ~1200 行）

**优先级**：价值 ★★★★★（覆盖面最大）· 成本 高（15 个 Tab，建议按"先立骨架组件 → 再逐 Tab 接入"两批推进）

#### 6.3.1 实施记录 · 批次 9「先立骨架」（2026-10-09）

按 §6.3 的两批推进顺序，本批只做**上半批：骨架组件 + 2 个 Tab 试点**（组件契约见 §1.3.2）。

**骨架组件（已落地）**

- `MwStatusDot` 三态（normal/warning/abnormal）+ `dotOnly` + 别名归一 + 未知兜底 abnormal。
- 新增 `TrendChart`（三态图表工厂）、`MwHostCard`、`MwDetailHeader`、`mwRange.js`；`echarts.js` 新增 `mwTrendOption` 统一 option 工厂。

**试点 Tab（已落地）**

- `DockerTab`：`.glass`→`SectionCard`、空态→`EmptyState`、主机块→`MwHostCard`（**补回容器 chip 列表**，原区块是"只有主机头的空壳"）、容器 6 态→三态、抽屉→`MwDetailHeader`+`TrendChart`；顺带修 **B11**（在线主机统计恒等于总数）。
- `LightCollectorTab`（5 个轻采集 Tab 基座）：`.glass`→`SectionCard`、空态→`EmptyState`、自绘 `.dot`→`MwStatusDot`、抽屉→`MwDetailHeader`+`TrendChart`；顺带修 **B3**（双定时器）、**B9**（loading 初值）、趋势图改 `Promise.all`。

**本批未做（按"再逐 Tab 接入"留到批次 10+）**

- §6.2 表中其余 8 个手写 Tab 的 `.glass`/自绘 `.dot` 归一；RedisTab 拆分与 80 行死代码清理；拓扑真实聚合（Nginx/Postgres/Kafka/RocketMQ 的"独立实例"硬编码）；列表列分级；K8s 图表与分页。

**验收（实测证据见 §1.3.2）**：三态色正确、抽屉趋势图渲染、空/错态正确、15 Tab 零溢出零报错、单测 34 files/243 tests、构建通过。

---

## 8. PS-07 · 容器与工作负载 `/container`

**功能点**：集群选择 / 命名空间输入 / 三 Tab（工作负载·Pod·事件）/ 异步任务状态条 / 结果表（列由后端动态生成）/ 详情抽屉 / 日志抽屉 / 与台账·集中日志的跨页联动。

**现状问题（✅ 已核实）**

- [信息架构] **无面包屑**：四层导航（集群在页头、命名空间在工具栏、Tab 在中间、行/抽屉）分散在三个区域，用户无法一眼确认"我在看哪个集群的哪个命名空间"
- [信息架构] 命名空间是**自由文本输入**，无候选/无补全/无校验 → 输错得到空结果
- [信息架构] **工作负载与 Pod 之间无下钻**：点 Deployment 只能看详情，无法筛出它的 Pod；真实心智模型是父子，界面是平级
- [交互成本] Job 类型的「台账」按钮**可见但点击后被拒**（L743 弹 `ElMessage.info`），与该文件注释自定的"点了只会撞 403 的入口不该摆在那里"原则自相矛盾
- [交互成本] 日志抽屉改容器名后必须点「重新拉取」，无"已改未应用"提示
- [可访问性] `.state-dot` 纯色块无文字；任务状态条变化无 `aria-live`
- [响应式] 三个写死宽度（240/220/200px）；`max-height: calc(100vh - 220px)`
- 亮点：**令牌使用全站最规范**（`--r-*`/`--fs-*`/`--t1-3`/`--*-dim/-bd` 全部到位）；异步透明度做得最好（任务状态条、上次刷新时间、条件变更警告、截断警告、超时结论、失败分类文案）；进入自动拉取**只在无结果时发一次**且先尝试认领上次回执 ✅

**优化目标**：让"我在看什么范围"始终可见，让父子层级可下钻。

**展示改进**

- 把 集群 + 命名空间 + Tab 合成**一条可见的层级指示条**（面包屑或"当前范围"条）
- Tab 形态从 `type="card"` 改为与 MiddlewareView 一致的**下划线型**
- 结果截断警告旁补「缩小范围」快捷操作

**交互改进**

- 命名空间改**下拉 + 自动补全**（从服务端取该集群的命名空间列表）
- 工作负载 → Pod 下钻（点 Deployment 提供「查看其 Pod」，自动填入筛选并切 Tab）
- Job 的「台账」按钮按类型禁用
- 日志容器名变更加「已修改，点重新拉取生效」
- 任务状态条加 `aria-live`

**优先级**：价值 ★★★☆ · 成本 中

---

## 9. PS-08 · 资产台账 `/assets`

**功能点**：6 张健康度 KPI（前 5 张可下钻）/ 六维筛选 / 含已忽略开关 / 新建·编辑 / 行内操作（详情·编辑·忽略·恢复·彻底删除）/ 批量（转派·打标签·忽略·恢复）/ 导出 CSV / 详情抽屉三 Tab（属性对比·变更历史·关联关系）/ 关系人工维护 / 期望值（标杆）/ AI 诊断 / 跨页深链。

**现状问题（✅ 已核实）**

- [信息架构] **2227 行单文件**，18 个功能点 + 1 抽屉 + 7 弹窗强耦合
- [信息架构] **下钻态有盲区**：只有 3 个 tag（L105-107），而 `drillStatus('missing')`（L1304-1308）与 `drillIgnored`（L1320-1325）**没有对应 tag** → 点了"失联"卡片后列表变少但无可见出口撤销
- [视觉一致性] 状态表达**四套并存**：`.dot + .st-*`、`.tag.missing/.archived`、`.tag.host/.tag.mw`、`.src-*`；`StatusPill` **0 处使用**；5 处写死色（L1984/1989/2045/2104/2107）
- [状态缺失] 详情抽屉**无加载态**：先置 `detail = row` 再 `await loadDetail`（L1357-1369）；`Promise.all`（L1344）任一失败整体抛错 → "历史接口挂了"表现为"整个资产详情打不开"，**无部分失败降级**
- [状态缺失] 批量面板**全部成功时无明细**（只有汇总 alert），部分失败反而可查（L832-850）
- [交互成本] 筛选改值不自动查询（需点查询），但 checkbox 与 KPI 下钻是即时的 → 同一行两种触发语义
- [交互成本] 「彻底删除」（唯一不可逆）与「忽略」同处"更多"下拉，仅靠 `divided` 分隔
- [交互成本] 新建的「其它属性」是自由 key/value，`attrDict` 已加载却不提供属性名候选 → 写错键静默入库
- [响应式] **0 处 `@media`**；9 列固定宽约 1262px；25 处内联 `style="width:…px"`
- [可访问性] KPI 下钻 `div + @click` 无 role/tabindex；状态仅颜色；大量解释只放 `title`（触屏不可达）
- [性能] 翻页时摘要被重复请求；开抽屉为判定一个布尔而拉全量 `listInspectBaselines()`

**优化目标**：从"能管的列表"升级为"可信的单一事实来源"。

**展示改进**

- 状态表达统一到 `StatusPill`，删 4 套自绘 + 5 处硬编码
- **筛选摘要条**：6 个筛选条件汇总成一条可整体清除的 `FilterChips`，并把 `drillStatus`/`drillIgnored` 纳入可见可撤销
- 「彻底删除」移出下拉，进 `.danger-zone` 并说明影响范围

**交互改进**

- 抽屉三 Tab 各自 loading / 错误 / 重试，`Promise.all` 改 `allSettled` → 属性对比（最高频）永远可用
- 筛选区折叠（默认只留关键词 + 类型 + 状态）；属性名改为可从 `attrDict` 选择
- 批量结果补"全部成功"明细
- 抽屉内显示"当前标杆是哪台"并可与 InspectView 互跳
- **筛选条按 §1.5 收敛**：6 个 select + 1 个 input + 1 个 checkbox 当前 130/150/150/150/260px 五种宽度；改 L/S/M/XS/L 归整；主工具栏加 `size="small"`（当前用默认 40px）

**优先级**：价值 ★★★★★ · 成本 中高（建议先拆子组件再改视觉）

## 10. PS-09 · 关系视图 `/assets/topology`（补断档规格）

> **说明**：本文档原从 §9 PS-08 直接跳到 §11 PS-10，**PS-09 无独立章节**（编号断档）。此处按批次 12 已落地的 `AssetTopologyView.vue`（333 行）补齐规格，行文与其余 PS 页一致。

**功能点**：关系总览聚合（按「来源类型 × 关系种类 × 目标类型 × 边来源」数条数 + 关系总数 + 无边资产数）/ 关系图（`TopologyGraph` 图四件套：图例·缩放·全屏·导出）/ 四维筛选（关系种类·边来源·资产类型·归属节点）/ 点总览某行把下图切到该筛选（下钻动线）/ 点图节点 → 台账 `?id=` 深链 / 刷新。

**现状问题（✅ 已核实）**

- [信息架构] **两段互不相干**：总览与图各写各的，用户看完"库里有哪些关系"后无法把它变成"看这一类的连法"，只能手改筛选
- [可读性] 全库规模下**把每条边都画出来不可读**（力导向图散成一团），且无聚合入口回答"整体形态"
- [信息架构] **无边资产不可见**：纳管了但没有任何关系的资产是排查"关系为什么没建起来"的起点，页面不显示数量
- [状态缺失] 两段**共用一个 loading**：总览接口挂了把图一起清空（反之亦然），无部分失败降级
- [视觉一致性] 图例/缩放/全屏/导出四件套**散落各入口**（台账详情抽屉、告警影响面、本页各写一套），同一张图在三处读起来像三种
- [硬编码样式] 标签底色写死 `rgba(74,157,240,.14)`（主题「极光蓝」主色），切青绿/紫色主题后不跟随；`.muted`/`.mono` 未走全局令牌
- [交互成本] 图内节点点了没有出口（无法从图跳到该资产详情）

**优化目标**：让"关系"这件事先**读得懂整体形态**，再**看得见具体连法**，两段是一条动线而非两块并列内容。

**展示改进**

- **一页两段**：上段「关系总览」聚合（行数与台账规模无关），下段「关系图」回答"某个子集长什么样"
- 顶部 summary-line 显示**关系总数**与**无边资产数**（>0 时标 `--warn`）
- 硬编码 `rgba` 改 `--accent-dim`；`.muted` 走全局唯一定义、`.mono` 走 `--mono` 令牌
- 图四件套与截断提示**收进 `TopologyGraph` 组件**，三入口自动共用同一套

**交互改进**

- 点总览某行 → **把下图切到该筛选**（`kind`+`source`+`type` 三维一起带，避免点"主机→容器"顺带把"中间件实例→容器"带进来）
- **两段各自 loading/error**（`overviewError`/`graphError` 独立），刷新走 `Promise.allSettled`，一段挂了不牵连另一段
- 点图节点 → `router.push({ path:'/assets', query:{ id } })` 打开该资产详情（抽屉里还有它自己的邻域图）
- 筛选条走全局 `.filter-bar`（`.inline` 去自带边框/底色，避免卡片内双层边框）；类型下拉候选**取自总览出现过的类型**（不引新接口，且候选项按定义相关）
- 筛选非空时显示「清除筛选」

**优先级**：价值 ★★★★ · 成本 中

---

## 11. PS-10 · 配置项模型 `/assets/model`

**功能点**：类型切换（chip 行）/ 字段表 9 列行内编辑（中文名·单位·备注·类型·枚举·比对档位）/ 运行态字段只读展示 / 清空比对设置 / 保存 / 添加建议属性（6 个预置）/ 覆盖度进度条 / 疑似同义键提示。

**现状问题（✅ 已核实）**

- [信息架构] **字段"增"入口缺失**：表格无"新增字段"按钮，唯一入口是「添加建议属性」弹窗且只能从 6 个硬编码预置勾选 → **自定义字段键无法创建**
- [信息架构] **字段"删"入口缺失**：表格无操作列，只能把配置清空（清空后不写入，但行仍留在表里）
- [交互成本] **无"未保存改动"提示**：`selectType` 直接 `buildDraft` 覆盖 draft（L367-371），**改了半天切个类型全没了**
- [交互成本] 改 N 行后要回顶部找「保存」，表格长时已滚出视口且无吸底
- [信息密度] 单行含 7 个控件；「来源」+「覆盖度」两列只读占 310px，把可编辑列挤到右侧，横向滚动时**决策主列「比对」滚出视口**
- [状态缺失] 表格无 `#empty`、无 `v-loading`
- [视觉一致性] 自绘 `.type-chip`（`<button>` 无 `type` 属性）、自绘覆盖度进度条；圆角写死 10px/3px
- [信息架构] 「排除（运行态）」改了之后，顶部只读的运行态字段列表**不同步**（L47-50 vs L345-348）

**优化目标**：让模型成为真正的 CRUD，且编辑过程不会丢数据。

**展示改进**

- 只读统计列（来源/覆盖度）**移出编辑表**（改"点字段行展开详情"或右侧固定），9 列降到 7 列
- `.type-chip` 换带 `aria-pressed` 的语义控件；覆盖度条用 `el-progress` 或令牌化

**交互改进**

- **补"新增字段" / "删除字段"入口**（表格加操作列 + 顶部"+ 新建字段"），「添加建议属性」降级为"从模板新建"
- **保存栏吸底**（复用 `.save-bar`）+ 未保存改动计数 + 切类型前拦截
- 字段表加 `EmptyState` + `v-loading`
- 「运行态字段」顶部展示与「比对」列**双向联动**
- 枚举弹窗补"从现有值导入" / 批量粘贴

**优先级**：价值 ★★★★（功能缺口而非体验瑕疵）· 成本 中

---

## 12. PS-11 · 配置巡检 `/inspect`

**功能点**：手动执行巡检 / 周期化巡检配置 / 上次执行信息 / 巡检记录列表 / 差异项列表 / 期望值（标杆）列表 / 打印。

**现状问题（✅ 已核实）**

- [信息架构] **差异比对不是 diff**：只是"期望值/实际值"两列并列，无字符级高亮、无 `before → after` 箭头；缺失时写"（字段已缺失）"、新增时写"—"，**靠占位符而非视觉编码**；且与 `AssetListView` 的 `<span class="struck">old</span> → <b>new</b>`（L439）**两套视觉**
- [信息架构] "执行一次"与"周期化巡检"两组范围控件**形态几乎相同**（L14-31 vs L39-85），边界只写在代码注释里
- [信息架构] 差异项是选中记录的结果，却与记录列表**平级并排**，无从属视觉表达
- [信息交互] **未选中记录时整块不渲染**（`v-if="selectedRun"` L168）→ 首屏看不到这块，用户不知道点行会发生什么
- [视觉一致性] **游离的 `>` 字符会渲染**（B8，L126）；执行区用 `.card .panel` 而其余 4 块用 `SectionCard`，且 `.card` 内套 `.filter-bar` → **双层边框**
- [状态缺失] 差异项加载失败只 toast；巡检执行中只有按钮 loading，**无进度/耗时**
- [交互成本] 「设为标杆」**本页无入口**（只能去台账），页面只说"去台账里设"却无跳转链接；「清除」倒是有
- [交互成本] 差异项无筛选/搜索/分页；无操作列（差异只能看不能处理）

**优化目标**：巡检的价值全在"差异是否一眼可读、是否可处理"。

**展示改进**

- 差异项升级为**真正的 diff**：期望 `--struck--` → 实际（复用 AssetListView 的 `.struck` 语义）+ 四类（新增/变更/缺失/合规偏差）视觉编码
- 修 B8；执行区换 `SectionCard`
- "现在跑一次" / "以后自动跑" 加明确小标题 + 副标题分层
- **筛选条按 §1.5 收敛**：执行区 130/150/240 三种宽度归到 S/M/L；周期化行 `.schedule-hint{margin-top:10px}` 改 `.muted-base`（详见 §1.6）

**交互改进**

- 差异项区块**常驻**（未选中时显示引导空态，选中后显示"记录 #N · 时间"标题条 + 高亮记录行）
- 差异项加筛选（级别/类型/资产关键词）+ 按资产分组 + 分页
- 「设为标杆」给跳转入口，与「清除」成对
- 打印按钮行为随选中状态变化，需在按钮上说明（当前不透明）

**优先级**：价值 ★★★★ · 成本 中

---

## 13. PS-12 · 节点操作 `/ops`

**功能点**：4 张 KPI / 关键词搜索 / 服务端筛选（节点·状态·批次）+ 客户端筛选（动作·时间）/ 筛选 chips / 列显隐 / 紧凑·宽松行高 / 导出 / 批量（导出·重执行·删除）/ 排序 / 分页 / 行操作（查看结果·复制批次·取消·删除）/ **下发弹窗**（目标多选 + 按分组整选 + 动作 + 动态参数 + 文件上传 + 原因）/ 能力校验（"n/m 台放行"）/ **危险参数识别 + 二次确认** / 审批态区分 / 批量结果弹窗 / 结果抽屉（基本信息 + 时间线 + 分节输出）。

**现状问题（✅ 已核实）**

- [信息架构] **动作目录没有独立呈现形态**：只作为下发弹窗内的 `el-option-group` 下拉存在，且**打开弹窗时才拉取**（L673-674）→ **用户在打开弹窗前无法浏览/检索动作目录**
- [交互成本] 危险动作护栏**只落在二次确认弹窗的文本里**（L731-751）：表单区无红色区块、无警示图标，写操作仅一枚 12px `.tag-write` 小标签；`file.push`（唯一会覆盖目标机文件的动作）只有一行 field-hint
- [信息密度] 结果抽屉输出块固定 `max-height:320px`，诊断包 7 个分节要 7 次局部滚动，无分节折叠/锚点
- [状态缺失] 下发弹窗无自身加载态与错误区（`loadCatalog` 失败只 toast，L684）
- [状态缺失] 空态用 `empty-text` 字符串（L111）而非 `EmptyState`；错误态无重试（对比 SecurityView/AuditView 有）
- [视觉一致性] 状态配色**三套并存实现且两套是死代码**：`STATE_CLASSES`/`stateClass`（L483-487）模板从未调用；`.dot.*`（L1309-1320）与 `.st-*`（L1321-1332）无引用；实际用的是自绘 `.op-pill`
- [交互成本] 工具条 6 控件 + 3 按钮平铺，且行为不一致（节点/状态 change 即查，动作/时间不触发）
- [响应式] 9 列约 1148px，横向滚动时"下发时间"与"节点"（排序锚点）滑出视野
- [硬编码样式] 6 处硬编码色 + 20+ 处宽度/字号字面量

**优化目标**：高风险操作页面——把护栏从"提交那一刻"前移到"决策那一刻"。

**展示改进**

- 动作目录**显性化**：在"下发操作"之外提供动作目录面板（分组卡片或可检索列表），展示动作名 / 只读·写标记 / 说明 / 覆盖范围 / "n/m 台放行"
- 下发表单加**危险视觉**：写操作与 `file.push` 用 `.danger-zone` 红色区块 + 警示图标
- `.op-pill` → `StatusPill`，删 3 组死配色（约 60 行）

**交互改进**

- 下发弹窗补加载态、目录拉取失败的卡内错误区
- 工具条收起为"更多筛选"；统一触发语义（全部即时或全部点查询）
- 结果抽屉输出改**分节折叠 + 目录锚点**
- 空态/错误态改 `EmptyState` + 带重试的错误条
- 补左右固定列策略（保住"下发时间"与"节点"）

**优先级**：价值 ★★★★★（高风险页面）· 成本 中高

---

## 14. PS-13 · 审批中心 `/approvals`

**功能点**：三 Tab（待我审批 / 我的申请 / 设置）/ 审批单列表 7 列 / 批准（二次确认写明"批准后**立即执行**"）/ 驳回（必填理由）/ 撤回 / 详情抽屉（descriptions + 审批链 timeline）/ 设置（启用 + 需审批动作 + 有效期）。

**现状问题（✅ 已核实）**

- [信息架构] **四个规范件全部未使用**（全批文件里唯一全未用的整页组件）：无 `PageHeader`，标题在 card header 自绘 `.head`（L5-10）
- [信息架构] 「设置」（配置）与两个「队列」（工作流）混在同一个 `el-tabs`（L24-26），且设置 Tab 的可见性由权限决定 → 不同角色看到不同 Tab 数
- [可访问性] **状态同色歧义**：`approved` 与 `executed` 同为 `success`（L179-180）；`rejected`/`cancelled`/`expired` 同为 `info`（L182-184）→ 红绿色盲或黑白打印下**完全无法区分**
- [状态缺失] 表格**无 `v-loading`**（loading 只绑刷新按钮）；空态是 `empty-text` 纯文本；失败只 toast
- [交互成本] Tab 上**无待办数量角标** → 必须切到"待我审批"才知道有没有活；无批量批准/驳回
- [交互成本] 操作列 200px 塞 3 个按钮；无筛选、无分页，全量渲染
- [交互成本] 抽屉内「参数」是 `k=v，k=v` 一行拼接（L203-205），在 540px 抽屉里被截断——**而参数正是批准决策的依据**

**优化目标**：审批人要在 10 秒内做出"批 / 不批"的判断，且这个判断有据可依。

**展示改进**

- 换 `PageHeader` + `SectionCard`；「设置」从 Tab 拆出（页头齿轮入口或独立路由）
- 状态用 `StatusPill` + **图标区分**：`approved`（已批准·待执行）vs `executed`（已执行）；`rejected`/`cancelled`/`expired` 分别用 danger / muted / muted+时钟图标
- 抽屉内参数改**可读渲染**（键/值分行），高风险参数高亮

**交互改进**

- 「待我审批」Tab 加**待办数徽标**
- 表格补 `v-loading`；空态换 `EmptyState`（"没有待办 = 当前没有待审批的高风险动作"）；失败给页面级错误 + 重试
- 加筛选（状态/动作/申请人）+ 分页；操作列改下拉菜单
- 设置加载失败时保留上次值不静默覆盖（L228）→ 改显式提示

**优先级**：价值 ★★★☆ · 成本 中低

---

## 15. PS-14 · 告警中心 `/alerts`

**功能点**：6 张 KPI / 打印 / 维护窗口 / 告警事件列表（三态筛选 + 批量确认 + 测试事件 + 单条认领 + 详情抽屉）/ 详情内处置（认领·指派·关闭·重开·评论·跳节点·跳日志·趋势图 + 阈值线·资产与影响面拓扑·AI 诊断）/ 告警规则（搜索·类型筛选·分页·新建·编辑·删除·启停·静默·导入导出）/ 告警分组配置 / 风暴收敛 / 抑制规则 / 30 秒自动刷新。

**现状问题（✅ 已核实）**

- [交互成本] **详情抽屉带遮罩**，`el-drawer size="480px"` 未设 `:modal="false"` → 打开详情后**列表被盖住，无法对照**；误点遮罩即关闭，未提交的评论随之中断
- [交互成本] **30 秒轮询会主动关闭正在查看的抽屉**（L844-846）→ 读到一半被打断；`commentText` 跨告警残留（`onDrawerClosed` L1327-1330 不清空）
- [交互成本] **双击行打开详情**，但无任何"可双击"的视觉提示（无 cursor、无 hover）；操作列又有单击"详情"，两条路径并存且都不明显
- [视觉一致性] 三列 `el-tag` 共用一套色板表达三种维度 → **"级别=警告"与"处置=已认领"同为橙**；"级别=信息"/"处置=已关闭"/"抑制=已抑制"同为灰蓝。唯一冗余编码是级别列的 3px 色条
- [信息架构] 一个页面纵向堆叠 **5 个 SectionCard + 2 dialog + 1 drawer**（1782 行），"监控/配置/高级"三种性质在同一条滚动轴，无分层导航
- [信息架构] 「关闭告警」（强动作）、「查看日志」（中性）、「AI 诊断」（只读）并列在同一个 `.ev-actions`（L555-591），注释声称"分开摆"但实际未分区
- [状态缺失] 事件表与规则表**均无 `v-loading`**；失败态三套（自绘红字 / `el-alert` / 静默 ignore）；趋势图失败静默 → 抽屉里留一个 240px 空白框
- [交互成本] 同一动作两种叫法：列表"确认"、抽屉"认领"、后端 `/alerts/ack`、提示"已认领"
- [响应式] **0 处 `@media`**
- [硬编码样式] `.maintenance-hint` 写死 `rgba(230,162,60,0.08)`（与 `--warn-dim` 不同源）；间距含非 4px 栅格值
- 亮点：**KPI 颜色纪律是全站最好的**——只有「活跃告警」与「紧急」上红，「警告」仅数字琥珀，「信息/已抑制/24h」中性；代码注释明确写了这条约束 ✅
- 部分正确：只有**维护窗口**"关着也占整块"；分组配置与风暴收敛已做条件展开 ✅

**优化目标**：处置一条告警时，能一边看趋势/影响面，一边对照列表里相邻的同规则告警判断是不是同一次故障。


**展示改进**

- 详情改"列表 + 详情"可对照形态：`size` 提到 560-680px 或 `:modal="false"` + `:close-on-click-modal="false"`
- 动作分区：强动作（关闭告警）一行，只读辅助（AI 诊断、查看日志）另起一个只读区块
- 状态三重编码：级别用**色条**、处置用 **StatusPill**、抑制用**带删除线的灰标签**——三列三种视觉语言

**交互改进**

- 抽屉打开时**暂停 30 秒轮询的自动关闭**；`onDrawerClosed` 清空评论草稿
- "双击打开"改整行 `cursor:pointer` + hover 高亮，操作列"详情"保留
- 动作命名统一为"认领"
- 三张表补 `v-loading`；部分失败（抑制/分组/模板/主机/通知渠道）统一 `PartialBar` 替代 5 处 `/* ignore */`
- 「高级设置 · 抑制与分组」折叠为默认收起（概览之后最有价值的是事件表，但配置区块把事件表压到第三屏以下）

**优先级**：价值 ★★★★★（处置主路径，每天几十次）· 成本 中

---

## 16. PS-15 · 告警规则弹窗 `RuleModal.vue`

**功能点**：基本信息（名称/启用/类型/级别）+ 阈值规则（指标/运算符/阈值/持续）+ 主机离线 / 安全事件 / 服务离线·主从切换·集群损坏 + 应用范围（全部/指定主机/分组）+ 通知渠道 + 临时静默 / 周期静默时段 / 升级策略（3 个折叠块）。

**现状问题（✅ 已核实）**

- [信息架构] **618 行 / 约 24 个字段压在一个 760px 对话框**，仅靠 3 个折叠块做减法；**无分步、无进度指示**
- [状态缺失] **无字段级校验**：无 `:rules`、无 `prop`，5 条 `if` + `ElMessage.warning` 顶部 toast（L507-511），不定位、不标红、不滚动
- [状态缺失] **无保存中态**：`saving` ref 不存在，footer 按钮无 `:loading`（L363）→ **可重复点击创建多条同名规则**
- [交互成本] 未设 `:close-on-click-modal="false"`（L2）→ **618 行表单被一次遮罩误触清空**，无脏数据确认、无草稿
- [交互成本] 切换规则类型时已填字段**静默保留在 form 里**（L397-404 未重置）→ 可能提交异类残留字段
- [可访问性] 升级策略 4 个字段用原生 `<label>` 无 `for`/`id`（L323/330/340/347）
- [视觉一致性] 分区标题用 EP 的 `--el-color-primary`（L548），与 `SectionCard` 的 `--accent` 条不同源，切主题即露馅
- [响应式] 全部 `el-col` 用固定 `:span`，**无任何断点**；注释已自认窄屏"选项会被裁掉后半截"（L92-93）

**优化目标**：一次填对，且误触不会毁掉半天工作。

**展示改进**

- 分区标题改用 `--accent`，与 SectionCard 同源
- 折叠区已有「已配置 N 项」摘要（L277/291/314）——**把这个模式复制到"触发条件"区**

**交互改进**

- **加保存中态**（按钮 `:loading` + 禁用重复提交）
- **加字段级校验**（`el-form :rules` + `prop`），失败时错误落到字段并滚动定位
- `:close-on-click-modal="false"` + 脏数据关闭确认（`before-close`）
- 切类型时清空异类字段
- 补 `:xs/:sm/:md` 断点；7/24 列的运算符列改整行或按钮组
- 未启用渠道旁加"去通知配置启用"的跳转链接
- 4 个原生 `<label>` 改 `el-form-item` 或加 `for`/`id`

**优先级**：价值 ★★★★★（配错直接落到线上告警配置）· 成本 中低

---

## 17. PS-16 · 通知配置 `/notify`

**功能点**：5 个渠道启停（邮件 / Webhook / 钉钉 / 飞书 / 企微）+ 各自参数 + 发送测试邮件 + 保存热生效。

**现状问题（✅ 已核实）**

- [信息架构] 5 张卡等价平铺，**无聚合视图**：不知道"当前 2 个渠道可用、1 个启用但未填地址"
- [交互成本] **「发送测试邮件」只测邮件**（`/api/v1/notify/test` L202），但页面管 5 个渠道 → Webhook/钉钉/飞书/企微**配完无法验证连通性**。**注**：后端其实已有 `POST /api/v1/alerts/test?channel=`（`api/test.go:22-34`）支持指定渠道，是前端没接，非后端缺失（§33 #2）
- [状态缺失] 加载失败只 toast，页面停留在全空表单 → 用户看到"好像没配过"，可能重复配置
- [状态缺失] 无"上次保存时间 / 上次测试结果 / 最近发送失败"
- [交互成本] 5 条校验只查"启用的渠道必须有地址"，不校验 URL 格式 / 发件人格式；错误是顶部 toast，**不定位到卡片**
- [视觉一致性] 用裸 `el-card`（未用 `SectionCard`），`.ch-title` 15px 与 SectionCard 的 14px 不一致；卡片 header 无状态指示
- [可访问性] `el-switch` 无文字标签；密码字段无"已保存过密码"的状态标识
- 亮点：**硬编码色 0 处，是全批最干净的文件**；保存中态与加载时禁用按钮做对了 ✅

**优化目标**：从"配置页面"升级为"可用性页面"——一进来就知道通知发不发得出去。

**展示改进**

- 5 张卡换 `SectionCard`，header 补 `StatusPill`（已启用 / 未启用 / **配置不完整**）
- 页面顶部加**渠道健康度摘要条**（N 个已启用 / M 个缺地址）

**交互改进**

- 测试入口改**可选渠道**（下拉选渠道 → 测试），调用 `POST /api/v1/alerts/test?channel=<key>`（**已存在**，`api/test.go:22-34`，无需后端新增）；⚠️ 该接口会写入一条测试告警并广播 WS，按钮文案须写明"会产生一条测试事件"（详见 §33 #2）
- 加载失败给页面级错误态 + 重试
- 5 条校验改**卡片内字段级提示**（未填地址的启用渠道描边转 warn + 字段下红字）
- `el-switch` 加 `aria-label`；密码补"已保存"标识

**优先级**：价值 ★★★☆ · 成本 低中（⚠️ 多渠道测试需后端确认）

---

## 18. PS-17 · 智能分析 `/intelligence`

**功能点**：分析窗口切换（24h / 7d / 30d）/ 4 张信号卡 / 数据就绪度 / 风险优先级队列（等级 + 类型筛选）/ 详情抽屉（综合评分 + 证据 + 可能根因 + 基线趋势 + 容量预测 + AI 诊断）。

**现状问题（✅ 已核实）**

- [硬编码样式] **27 处硬编码**（`IntelligenceView` 15 rgba + 12 hex、`AnalysisCoverageCard` 7+3、`BaselineTrendChart` 5+6）：`#111c2e`/`#cbd5e1`/`#22d3ee`/`#fb923c`/`#a5b4fc`… 在主题 b 下"看起来对"，**切到 a/c 立刻崩**
- [硬编码样式] `BaselineTrendChart` **不监听 `nebula:theme-changed`**，是全站唯一"换肤不换色"的图
- [状态缺失] 空态用 `el-empty` 未用 `EmptyState`；失败只 toast；**窗口切换失败时标题已变但数据还是旧窗口**（L14/L49）
- [信息架构] 「样本不足」只用一行 12px 小字，与"数据已就绪"视觉权重几乎等同 → **容易被当作稳定**
- [信息密度] 64 行承载 12 个功能点，模板/脚本**极度压缩**（单行数百至上千字符），无法维护
- [可访问性] 风险等级与置信度**共用一套色板**（high=danger / medium=warning / low=info），语义不同却同色；`.pulse` 无限动画无降级
- [性能] 抽屉内 `BaselineTrendChart` 用 `v-for` 为每条基线建一个 ECharts 实例 + 一个 `window.resize` 监听
- [视觉一致性] 死 CSS `.hero*` 仍在（L54）

**优化目标**：这一页卖的是"数据可信度"，所以**结论的可信度本身必须可见**。

**展示改进**

- 27 处硬编码令牌化（`#111c2e`→`--s1`、`#22d3ee`→`--chart-cyan`、`#fb923c`→`--chart-orange`）；删 `.hero*` 死 CSS
- 模板/脚本**展开为常规换行**（可维护性）
- 「样本不足」加明显标记（行左侧灰条 + 标签，而不是小字）
- 风险等级与置信度**分成两套编码**（等级用色 + 文案，置信度用点阵/进度 + 文案）

**交互改进**

- `BaselineTrendChart` 改 `cssVar()` 取色 + 监听 `nebula:theme-changed`（对齐 AlertsView L1319-1322/1456 既有做法）
- `points` 为空或 `upper/lower` 为 0 时渲染"暂无基线数据（需 N 小时样本）"占位，**而不是空图 + 0 值 markLine**
- `initChart` 前校验容器宽高 > 0（抽屉动画期间 0 宽初始化是真实风险）；改 `ResizeObserver`
- 空态换 `EmptyState`（区分"筛选无结果"与"样本不足"）；失败加页面级错误 + 重试；窗口切换失败保留旧标签
- 基线趋势合并为单图多 series 或按需展开（默认前 2 条）
- `AnalysisCoverageCard` 的 eyebrow 从英文 `DATA CONFIDENCE` 改中文；进度条补 `role="progressbar"`

**优先级**：价值 ★★★★（全批最大一致性债 + 真实渲染风险）· 成本 低（纯替换为主）

---

## 19. PS-18 · 服务拨测 `/dialtest`

**功能点**：任务列表 10 列（名称/类型/目标/间隔/超时/启用开关/告警级别/通知/对外/操作）/ 列表内直接启停（乐观更新 + 失败回滚）/ 新建·编辑弹窗（11 项，https 额外 2 项证书阈值）/ 删除 / 最近拨测结果表 7 列。

**现状问题（✅ 已核实）**

- [信息架构] **任务表没有"当前状态/最近延迟/可用率"列** → 要看下方另一张表才知道任务是否健康，两表靠"任务名"人肉对应
- [信息架构] **结果表缺"检测时间"列**（L58-79）→ "最近"无从判断
- [信息架构] **全页 0 图表**：无拓扑、无时延趋势、无可用率时间线
- [状态缺失] `Promise.all` 任一失败只 `console.error`（L163）→ 两表皆空且无提示、无重试；`toggleEnabled` 失败只回滚 + console，**无 `ElMessage`**（用户只看到开关弹回）
- [视觉一致性] `.metric-good` 用 `--accent`（蓝），证书健康显示成蓝色，与 `.status-led.on` 的 `--chart-green` 不一致 → **蓝绿错位**
- [视觉一致性] 自写 `.status-led` 与 `StatusPill` 完全重叠；`.dot/.dot.up/.dot.down`（L238-240）是死规则
- [硬编码样式] 死规则 5 条（`.page-header/.page-title/.page-desc/.chart-section/.section-title`，含 22px 渐变文字）
- [信息密度] 10 列约 1110px，低于 1100 断点即横向滚动；「通知」列多个 `el-tag` 平铺，渠道多时行高失控
- [响应式] **0 处 `@media`**

**优化目标**：一眼看出哪个拨测任务不健康，以及它是从什么时候开始不健康的。

**展示改进**

- 任务表&#x52A0;**「当前状态」列**（`StatusPill` + 最近延迟 + 可用率），与结果表同源
- 结果表&#x8865;**「检测时间」列 + 延迟 sparkline**（复用 `areaOption`）
- 状态表达统一到 `StatusPill`，删 `.status-led`/`.dot` 死规则；`.metric-good` 改 `--ok`

**交互改进**

- 加载失败改错误条 + 重试；toggle 失败给 `ElMessage`
- 补 1100/720 断点（窄屏隐藏次要列）+ 数值列加 `num-cell`
- 任务表加搜索/类型筛选；结果表加分页

**优先级**：价值 ★★★☆ · 成本 低中

**✅ 已实施（2026-10-09，批次 14）**

- 任务表加**「当前状态」列**（`StatusPill` tone=ok/danger/muted + dot + `cur-meta`「12.3ms · 可用率 99.5%」），与结果表同源（`curResult()`）；实测行文本「官网 … 正常 12.3ms · 可用率 99.5%」/「后台 … 已停用」/「数据库 … 异常 0ms · 可用率 42%」
- 结果表补**「检测时间」列**（`fmtTime` → `2026-10-09 19:47`）+ **「延迟趋势」列**（新建 `components/common/Sparkline.vue`，复用 `areaOption`，`points` 兼容 `[[ts,v]]` 与 `[{timestamp,value}]`）；后端 `handleDialtestLatest` 的 `dialtestResult` 补 `checkedAt`/`uptime`/`latencyHistory`（24h `QueryRange` 双查询 `dial_test_latency` + `dial_test_up`，step 300000）
- 状态统一 `StatusPill`，删 `.status-led`/`.dot`/`.status-text` 死规则；`.metric-good` 由 `--accent` 改 `var(--ok)`（修蓝绿错位）
- 加载失败改 `SectionCard :error` + 重试；`toggleEnabled` 失败 `ElMessage.error` + 回滚；`saveTask` 失败提示
- 任务表加搜索 + 类型筛选（`filteredTasks`）；结果表加**分页**（`pagedResults`/`resultPage`，每页 10）
- `class-name="num-cell"` 数值列；补 `@media (max-width:1100px/720px)`
- **列宽收敛（实测修 68px 溢出）**：任务表「目标」180→150、「通知」120→90、「操作」130→120（合计 1200→1130）；结果表「目标」180→150、「原因」200→160、「检测时间」170→160（合计 1210→1130），均 ≤ 1142 预算
- 新增 `DialTestView.test.js` 3 例（当前状态列文案 / 开关失败回滚提示 / 关键词·类型筛选）

---

## 20. PS-19 · 巡检报告 `/report`

**功能点**：生成报告（日报/周报/月报 + 7 章节多选）+ 周期化生成（开关/间隔/类型/立即生成）+ 历史报告列表 + 预览（iframe）+ 新标签页 / 打印（新窗口）/ 下载 HTML。

**现状问题（✅ 已核实）**

- [信息架构] 预览区在**页面最底部**，生成后靠 `scrollIntoView` 滚过去（L275-279）→ "生成 → 预览 → 回历史"来回滚动距离长
- [信息架构] 报告以 **iframe 内联白底独立 HTML**（L141/L342）→ "白板嵌黑页"，且 iframe 内部再滚动 = **双滚动条**
- [交互成本] 只能下载 HTML，**无 Markdown / 邮件**；**PDF 能力其实已有**（`printReport()` L287 + 后端模板自带 `@page A4`，见 §33 #3），但按钮只写"打印"，用户不知道它等于"存 PDF" → 已改文案
- [交互成本] 打印必须新开窗口，被拦截时只有一句提示
- [交互成本] 参数只有"类型 + 章节"：无时间范围、无节点/分组范围
- [交互成本] 历史报告无删除/搜索/分页，全量渲染
- [状态缺失] **iframe 无加载态**（点击到出现之间是空白或上一份报告）；预览失败（403/已删除）无提示
- [视觉一致性] 类型列用 `el-tag`，跳过章节用 `el-tag warning`，未用 `StatusPill`
- [硬编码样式] `border: 1px solid var(--border, #ebeef5)` 回退值是**浅色灰**（L340）；死规则 `.frame-head/.frame-actions`
- [响应式] **0 处 `@media`**；周期化那一行 5 个控件横排，窄屏必溢出

**优化目标**：报告是给领导和客户看的，出报告的路径要短、产物要可直接分发。

**展示改进**

- 预览改**右侧抽屉或全屏弹层**，或把预览提到历史之上
- iframe 边框回退值改 `--bd`；死规则清理

**交互改进**

- **iframe 加加载态与失败兜底提示**
- 历史报告加分页/搜索/删除；类型列换 `StatusPill`
- 生成参数：**本轮不补时间/节点范围**。范围是按生成者权限折算的，加参数会与权限模型冲突（§33 #3b）；改为在页面写明"报告是当前状态快照"
- 周期化那一行改 `.form-grid` 或补 720 断点换行
- ~~**导出补 PDF**~~ → **已有，本轮只补文案**：按钮改「打印 / 存为 PDF」并加 title 说明（§33 #3，2026-10-09 已落地）

**优先级**：价值 ★★★☆ · 成本 中

**✅ 已实施（2026-10-09，批次 14）**

- 预览由页底内联 iframe 改**右侧 `el-drawer`**（`size="min(880px, 92vw)"`），消双滚动条；iframe 加加载态（`previewLoading`）/ 超时兜底（`previewTimer`）/ 失败兜底（`previewError` + `onIframeError`）
- 历史报告加**分页**（`pagedHistory`/`historyPage`，每页 10）+ **搜索**（`historyKw`/`filteredHistory`）+ **删除**（`deleteReport` + `ElMessageBox.confirm`）
- 类型列 `el-tag` → `StatusPill`（`typeTone`）
- iframe 边框回退值改 `var(--bd)`；删死规则 `.frame-head/.frame-actions`
- 生成参数区写明"**当前状态快照**"（时间/节点范围本轮不做，与权限模型冲突，§33 #3b）
- `.generate-row` 补 720 断点换行
- **后端新增删除能力**：`report.Generator.Delete(id)`（删 HTML + 历史条目，ID 校验防路径穿越）+ `ReportProvider.Delete` + 路由 `DELETE /api/v1/report/history/{id}`（`report:export`）+ `handleReportDelete`
- `ReportView.test.js` 9 → 13 例（历史搜索 / 分页 / 删除 DELETE / 预览抽屉 + iframe 失败兜底）

---

## 21. PS-20 · 集中日志 `/logs`

**功能点**：后端能力行 + 探测失败告警 / 时间范围（datetimerange + 4 快捷项）/ 匹配模式（关键词·正则）/ 关键词 / 节点 / 来源 / 字段过滤 `key:value` / 容器（Pod）过滤 / 查询·加载更多 / 截断提示 / 扫描诊断 / 日志行（结构化 chips + 点击展开）/ 资产标签 / **日志 → 建告警规则** / 深链 / Agent 配置示例弹窗。

**现状问题（✅ 已核实）**

- [信息架构] **生效中的筛选条件只有 2/6 可见**：仅字段过滤与容器过滤有可删 chips，时间范围/模式/关键词/节点/来源改完即隐形（对比 OpsView 有 `.op-chips`）
- [信息密度] **无关键词高亮**：正文是纯 `{{ row.text }}`（L224），且默认单行省略（L837）→ **命中词很可能落在省略号之后**
- [状态缺失] **完全没有导出能力**（grep 确认 0 命中 `download`/`csv`/`Blob`）——OpsView/SecurityView/AuditView 三页都有，**日志页是唯一缺口**，排障留证要手抄
- [状态缺失] 无命中总数与"当前第几批"；`loadMore` 失败无法判断"追加失败还是全部失败"
- [交互成本] 字段过滤必须手输 `key:value`，候选只填名字、无值候选
- [可访问性] 日志行展开是 `<div @click>`（L218-225），无 `tabindex`/`aria-expanded`；资产标签 `<span @click>` 无 `role="link"`
- [硬编码样式] 2 处（L852/858）；字号/间距字面量约 10 处
- 亮点：**空态做了"还没查 / 查过没命中"二态区分** ✅；深链 + 客户端形态校验完整 ✅

**优化目标**：命中即高亮，查到即能带走。

**展示改进**

- **关键词/正则命中高亮** + 行内命中计数
- 6 个筛选条件收敛为**统一的 `FilterChips` 条**（与 OpsView 一致）
- **筛选条按 §1.5 收敛**：当前 `.toolbar` 是裸 flex 无边框，**改 `.filter-bar` 容器**；控件宽度 96/180/200/210/240/320/400px 七档 → 归整为 XS(96)/S(130)/M(180)/L(240)/XL(320) 五档；其中**时间选择器从 400px 降到 320px**（XL 上限）

**交互改进**

- **补导出**（CSV / 复制选中行）——可复用 OpsView 的 CSV 逻辑
- 日志行展开改可聚焦控件 + `aria-expanded`
- 后端扫描预算可视化（撞边界前可预判）
- 追加失败的分批提示

**优先级**：价值 ★★★★（高频日常动作）· 成本 中

---

## 22. PS-21 · 指标浏览 `/metrics/explore`

**功能点**：左侧指标目录树（分类分组 + 搜索 + 有数据/无数据自动发现）/ 右侧趋势图 / 导出 CSV / 加入仪表盘。

**现状问题（✅ 已核实）**

- [信息架构] **无 `PageHeader`**：整页无标题，页头只有"指标目录"四字
- [交互成本] **没有时间范围控件**：`rangeBounds('1h')` 在 L122 与 L158 **两处硬编码** → 只能看近 1 小时（DashboardView 却有 4 档）
- [交互成本] 查询完全由"点一个指标"决定：**无 label 过滤**（后端已支持 `labels.<name>=`，前端没接）、**无聚合**（后端确无通用能力，建议砍掉）、无图例开关、无对比 → 无法构造多序列分析
- [交互成本] 「加入仪表盘」硬编码看板名 `'我的看板'`（L165），不能选已有看板，同名即追加
- [状态缺失] 图表查询期间**无任何 loading/骨架**；series 为空时画空坐标系，与查询失败不可区分；空态是自写一行文字
- [性能] `el-tree default-expand-all` + 每次搜索对全量 filter，指标上千时卡顿；只监听 `window resize`（侧栏折叠不同步）
- [响应式] **0 处 `@media`**；`.left` 固定 320px
- 亮点：**硬编码色 0 处**，令牌层面已收敛 ✅

**优化目标**：从"能点出一张图"升级为"能做一次对比分析"。

**展示改进**

- 补 `PageHeader` + **时间范围切换（1h/6h/24h/7d）**（`rangeBounds` 函数已支持 4 档，只需接上）
- 右侧图表区补 loading / 空 / 错误三态（骨架 `.sk` + `EmptyState`）
- 树节点状态（有数据/无数据）改 `StatusPill`（ok/muted + dot）
- `.chart` 外观收敛进 `SectionCard`（与仪表盘面板同语言）

**交互改进**

- 接 **label 过滤**：后端已支持 `?labels.<name>=<value>`（`parseLabelQuery` `query.go:1230-1235`），**纯前端改造**（§33 #4）
- 图例开关；「加入仪表盘」改弹出选择已有看板或新建（复用 `useDashboards`）
- resize 改 `ResizeObserver`
- **聚合（by/函数）本轮砍掉**：后端无通用聚合能力，且本页低频，造查询语言性价比弱（§33 #4b）

**优先级**：价值 ★★★★ · 成本 中

**✅ 已实施（2026-10-09，批次 14）**

- 补 `PageHeader`（标题「指标浏览」），导出/加入仪表盘移入 `#actions`，`:disabled="!selected"`
- 图表区补三态：`chartLoading` / `chartError`（含重试）/ `chartEmpty`（`EmptyState size="table-inline"`）；`.chart` 收敛进 `SectionCard class="chart-card" dense`
- 树节点状态改 `StatusPill :tone="active?'ok':'muted'" size="sm" dot`（实测树内 2 个 pill）
- 图例开关 `showLegend` + `el-checkbox`（关时 `opt.legend = undefined`）
- 「加入仪表盘」改 `el-dialog`（选已有看板或新建，复用 `useDashboards`），替代硬编码 `'我的看板'`
- resize 收敛：删 `window.addEventListener('resize')`，靠 `initChart` 内置 `ResizeObserver`
- 补 `@media (max-width:900px)` 纵向堆叠
- `MetricsExploreView.test.js` 3 → 5 例（加入仪表盘 update / 图例开关 legend undefined）

---

## 23. PS-22 · 自定义仪表盘 `/system/dashboards`

**功能点**：看板切换 / 新建 / 删除 / 编辑模式 / 添加·编辑·删除面板 / 面板网格渲染 / 两种空态（无看板 / 无面板）。

**现状问题（✅ 已核实）**

- [信息架构] **无 `PageHeader`**：导航条同时承载"切换看板 + 新建 + 编辑 + 删除"四类操作（L5-24），看板多时右侧按钮被挤出
- [信息架构] **面板顺序不可调**：无拖拽、无上移/下移 → 想换顺序只能删除重建
- [信息架构] **无栅格尺寸概念**：`repeat(auto-fill, minmax(360px,1fr))`，所有面板等宽
- **[B6] 非编辑态点「添加面板」→ 空指针崩溃**（L50 → L190）
- **[B7] `PanelChart` 恒定 `type:'line'`**，配置下拉 4 种类型选了不生效
- [状态缺失] 首屏无加载态：`dashboards` 为空时先渲染"暂无看板"空态，数据回来整块翻成网格；列表/保存失败静默
- [交互成本] **删除面板无二次确认**（与删除看板有 popconfirm 标准不一致）；**看板无法重命名**；**当前看板不记忆**（每次回到第一个）
- [性能] `v-for :key="i"`（L37）→ 删除/插入导致 ECharts 实例与面板数据**错配**；N 个面板同时发 N 个 range 查询
- [硬编码样式] `.dash { height: calc(100vh - 132px) }`；**死规则 6 条**（含 2 处硬编码 rgba，L225）
- [性能] `PanelChart` 的 `watch(props.panel, {deep:true})` → 编辑任一面板会让**所有**面板重新 load
- [正确性] 导出 `step` 恒为 60000（L88），与 `rangeBounds` 的 step（7d→1800s）不一致 → **7d 导出采样错误**

**优化目标**：仪表盘是本平台门面页，必须"能建、能排、能选类型、点了不崩"。

**展示改进**

- 页头换 `PageHeader`（导航与操作分离）
- `PanelChart` 容器改 `SectionCard`（dense + `#actions`），删 3 处硬编码色
- 序列配色走 `--chart-*` 轮转 + `cachedGradient`（当前所有序列固定 `COLORS.cyan`，多序列无法区分）

**交互改进**

1. **修 B6**（空指针）与 **B7**（chartType 生效）
2. 面板支持**拖拽排序 + 尺寸档位**（半宽/全宽），key 改稳定 id
3. 首屏加骨架；`.dash` 高度统一
4. 补看板改名、删除面板二次确认、当前看板记忆
5. `PanelChart` 的 watch 改为按 `metric/range/node/instance/labels` 精确触发；导出 step 复用 `rangeBounds`
6. 面板补 loading / 空 / 错误三态 + 重试

**优先级**：价值 ★★★★★（三处可复现缺陷）· 成本 中

**✅ 已实施（2026-10-09，批次 14）**

- **修 B6**：`addPanel()` 非编辑态先 `enterEdit()`（原 `draft.value.panels` 空指针崩溃）；实测非编辑态点「添加面板」弹窗正常出现
- **修 B7**：`PanelChart` 改 `monitorOption({ chartType: props.panel.chartType, unit, series })`（原恒 `type:'line'`）；实测 gauge 面板渲染仪表、line 面板渲染折线，共 2 canvas
- 页头换 `PageHeader`，看板切换独立 `.subnav`（实测 `[运维总览 | 数据库看板]`）
- 面板稳定 id（`ensureIds()` → `_id`），`:key="p._id"` 替代 `:key="i"`；**拖拽排序**（编辑态 `draggable` + `onDrop` splice）+ **尺寸档位**（`.span-full` 全宽 / 半宽 radio；实测编辑态 2 个尺寸按钮 + 2 个可拖拽面板 + 1 个全宽面板）
- 首屏骨架 `SectionCard :loading`；错误条 `el-alert` + 重试；看板改名弹窗；删除面板 `el-popconfirm` 二次确认；当前看板记忆（`localStorage` `nebula_dash_active`）
- `PanelChart` 的 `watch(props.panel,{deep:true})` → **精确 watch**（`watchKey` = metric/range/node/instance/chartType/labels），编辑单面板不再触发全量 load
- 导出 step 复用 `rangeBounds`（修 7d 采样错误）；三态 + 重试；序列配色走 `--chart-*` 轮转（`PALETTE` + `colorAt(i)`）
- **后端**：`dashboard.Panel` 加 `Size PanelSize`（`half`/`full`，空=half）
- 新增 `DashboardView.test.js` 5 例（B6 空面按钮进编辑态 / B7 chartType 传参 / 导出 step=1800000 / 看板记忆）

---

## 24. PS-23 · 操作审计 `/audit`

**功能点**：刷新 / **完整性校验（防篡改链）** / 导出 CSV / 打印 / 筛选（分类·操作者·路径·时间·每页条数·关联 id）/ 审计表格 9 列 / 本次改动抽屉 / 服务端分页。

**现状问题（✅ 已核实）**

- [信息架构] **筛选维度少于展示字段**：表格有状态码、结果成功/失败、来源 IP、method 四个字段，但**都不可筛** → "只看失败的操作""只看某个来源 IP"做不到
- [信息架构] **9 列只有首列固定**：横向滚动时"状态码/结果/资产改动"三列滑出视野——L108-110 注释已承认（列宽约 1520px vs 内容区 1130px），但只做了收窄，没把排障最关心的右侧三列固定
- [状态缺失] **唯一的响应式断点是死代码**：`.toolbar`（L565）与 `@media (max-width:720px) .toolbar`（L595）在模板中**完全未引用**（模板只有 `.filters`）→ 窄屏下 6 控件 + 400px 时间选择器无任何断点处理
- [交互成本] 筛选改变**立即请求且无防抖**；无"重置筛选"按钮
- [状态缺失] 完整性校验结果**无"重新校验"入口、无时间戳**（只能"收起"）；失败只 toast，结果卡不显示错误
- [性能] `changeSummary(row.detail)` 在单个单元格被调用 **6 次**，每次 3 个 `new RegExp`（L131-137/L346-349），**每行每次渲染 18 次正则匹配**；无 `row-key`
- [硬编码样式] 3 处带 hex 回退：`#d9534f`（Bootstrap 红，与任一主题 `--danger` 都不同）、`#4a9df0`（写死极光蓝 accent）、`rgba(255,255,255,0.08)`
- [信息密度] 变更摘要依赖对 `detail` **字符串正则解析**——服务端措辞一变解析即失效，且无法表达"哪个字段从什么变到什么"
- 亮点：**导出走独立路由 + 独立权限 `audit:export` + 401 处理** ✅；**打印带完整筛选元信息，明确面向凭证场景** ✅；有请求竞态守卫 ✅

**优化目标**：审计是凭证，必须"查得到、导得出、说得清是哪个版本的数据"。

**展示改进**

- 用 `StatusPill` 替换分类/结果的 `el-tag`；`.card` 改 `SectionCard`（与 SecurityView 一致）
- 硬编码 3 处 hex 回退改令牌
- **筛选条按 §1.5 收敛**：当前 `.card .filter-bar` 是双层边框，去 `.card` 包裹；时间控件宽度 120/150 归到 S
- **完整性校验说明按 §1.6 收敛**：`.verify-note{margin-top:6px}` 是全站最贴脸，改 `.muted-base`（10px）；`.verify-extra` / `.verify-caveat` 同步


**交互改进**

- &#x8865;**「只看失败」与来源 IP / method 筛选**（把最常见的两个审计动作变成一次点击）
- 「状态码 / 结果 / 资产改动」三列**右侧固定**
- **修死断点**并补真实响应式
- `changeSummary` 改为一次性计算 + 记忆化
- 完整性校验补时间戳 + 重新校验入口；失败落进结果卡

**优先级**：价值 ★★★☆ · 成本 低中

> **✅ 已实施（批次 13，2026-10-09）**：上面"展示改进 / 交互改进"全部落地。补记两处**实测才暴露**的细节：① `/logs` 与 `/audit` 的横向溢出不是"列宽本来就宽"，而是**新增列 / 容器内边距变化**打破既有预算——`LogsView` 加多选列（40px）后合计 1180px > 可用 1142px；`AuditView` 换 `SectionCard` 后内容区由 1142 → **1134px**（卡片 body 内边距 16px，比旧 `.card` 多 8px），列合计 1150px 即溢出。② 固定列在 EP 2.14 用 **sticky class**（`.el-table-fixed-column--right`）实现，**不再生成** `.el-table__fixed-right` 独立层，验收断言要看 class 而非旧节点名。详见 §32 批次 13 条目。

---

## 25. PS-24 · 安全中心 `/security`

**功能点**：KPI（合规评分环 + 安全事件/风险主机/FIM 变化）/ **入侵防御**（fail2ban 专属 jail，启停）/ **护栏组合风险** / **安全事件**（类别·节点筛选 + 分页）/ **基线合规明细**（仅未通过开关 + 导出矩阵）/ **文件完整性变化** 时间线。

**现状问题（✅ 已核实）**

- **[B4] 共享 `loadError`**：`Promise.all` 并发下任一模块成功即清空错误 → **部分失败被静默吞掉**
- **[B5] `defenseLoading` 从未置 true**，却绑在 `v-loading`（L46/L391）
- [状态缺失] 二次及以后的刷新**无加载反馈**（`loading` 只在 `onMounted` 置位，L606-610）→ KPI/事件/基线/FIM 四块"静默变数据"
- [信息架构] **风险等级有四套并行视觉**：`.sev-tag`（事件）+ `.score-pill`（基线）+ `.def-badge`（防护）+ `el-tag`（护栏）——都用同一组语义色，但尺寸/圆角/有无圆点各异
- [信息架构] **修复建议只覆盖 1/5 子模块**：只有护栏风险给了消除方法（L152-156）；**基线未通过项（数量最大）只有名称 + "未通过"，无任何整改指引**；安全事件无处置建议；FIM 无"如何确认是否合法"
- [信息密度] 基线区与 FIM 区是**两条独立内滚动区**（460px / 420px），与安全事件卡的等高（L856）叠加 → **卡内滚动 + 页面滚动双层滚动**
- [硬编码样式] 3 处：`.geo-tag`（L726，与 `--accent-dim` 重复）、`.tl-dot.tl-deleted`（L837）、**`.jail-tag` 写死青绿主题的 `--ok`**（L941，切到极光蓝时与 `--ok:#22c55e` 是两个绿）
- [性能] `getSecurityEvents({limit:2000})` 一次拉 2000 条后前端切片分页；`sortedDefenses` 每次全量 IP 排序
- [视觉一致性] `.dim`/`.muted` 各定义两次；两个独立的 1100px 断点未合并；`.section`（L688-690）是死代码
- [响应式] 两张 6 列表格无横向滚动策略或固定列；SectionCard 的 `#actions` 里塞筛选控件，窄屏与标题抢空间

**优化目标**：安全页是"我是否安全"的凭据页，静默失败的成本最高。

**展示改进**

- **四套风险等级收敛到 `StatusPill`**（删约 40 行自定义样式）
- 消除双层滚动：基线/FIM 与分页对齐（去掉卡内 `max-height`，或整卡内滚但页面不滚）
- 5 个子模块做成**可定位的分区导航**（锚点 / 子标签）
- 替换 3 处硬编码色

**交互改进**

1. **修 B4 + B5**（拆 `loadError`、接上 `defenseLoading`、补刷新态）
2. **补"修复建议"到基线未通过项与安全事件**（从"知道有问题"推进到"知道怎么改"）
3. 合并重复断点、删死代码

**优先级**：价值 ★★★★★（唯一功能性缺陷集中的页面）· 成本 低（状态修复）+ 中（视觉收敛）

---

## 26. PS-25 · 系统升级 `/system/upgrade`

**功能点**：当前版本 / 上传升级包（拖拽 + 进度）/ 待升级（版本·架构·大小·SHA256 + 取消·立即升级）/ 升级历史（7 列 + 切换/删除 + 分页）/ IP 地理库（上传 .xdb + 归属地验证 + 样本表 + 恢复内置库）。

**现状问题（✅ 已核实）**

- [状态缺失] **应用阶段无进度、无日志**：只有上传进度；应用后进入**最长 40 秒静默轮询**（L450-468），期间页面零变化，用户不知道是卡住还是在重启
- [状态缺失] **上传失败无就地错误区**（`before-upload` 返回 false + `ElMessage`），且 `:show-file-list="false"`（L26）→ 失败后拖放区看不出任何原因
- [状态缺失] **5 处 `catch(e){ /* ignore */ }`**（L383/389/396/413/663）→ 加载失败显示 `'-'`，分不清"没配"和"接口挂了"
- [信息架构] 887 行、5 区块纵向平铺，**无锚点/无折叠**；「待升级」区块 `v-if="pending"` 导致页面长度跳变
- [交互成本] **升级流程无步骤指示**（实际是 3 步：上传 → 应用 → 校验版本）
- [交互成本] **危险操作无视觉隔离**：删除升级包 / 切换版本 / 恢复内置库都是普通 warning/danger 按钮，与"刷新"混排；`.danger-zone` 已定义但 0 引用
- [交互成本] 「切换」即**回滚**，但文案未说明是"回退到旧版本"
- [硬编码样式] 39 处硬编码 px；1 处写死主题绿 `rgba(0,200,150,0.3)`（L795）
- [响应式] 历史表 7 列宽约 1014px 写死
- 亮点：**版本校验逻辑扎实**——应用后轮询**实际运行版本**、超时后读历史判定真实结果、区分"重启断连"与真失败（L450-496）✅；**是 13 个设置类文件中唯一用了 SectionCard ×5 + EmptyState 的** ✅

**优化目标**：升级是最高风险操作，40 秒静默等待必须变成可见的三步。

**展示改进**

- **显式 3 步指示**（已提交 / 服务重启中 / 正在校验版本）+ 阶段性文案——已有 `waitForServerVersion` 的状态机骨架，**只需暴露给用户**
- `applyError` 提升为**页面级区域**（当前只显示在"待升级"卡片，pending 一清空就无处可见）
- 「IP 地理库」（116 行）折叠——它与系统升级是两件事
- 写死主题绿改 `--accent` 派生；39 处硬编码令牌化

**交互改进**

- 上传失败改**就地错误区**（文件名 + 原因 + 重试），去掉 `show-file-list=false` 或改受控列表
- 5 处 `catch { /* ignore */ }` 补轻提示或状态标记
- 「切换」改「回退到此版本」；删除/切换/恢复内置库进 `.danger-zone`
- 加区块锚点导航

**优先级**：价值 ★★★★（最高风险操作）· 成本 中

#### 实施记录（批次 15 · 2026-10-09）

`UpgradeView.vue` **全量重写**，8 条现状问题全部处置：

| 现状问题 | 处置 |
| --- | --- |
| 应用阶段无进度（40s 静默轮询） | **显式三步指示** `.steps/.step/.step-dot`：`applyPhase` 状态机（`''/submitted/restarting/verifying/done/failed/timeout`）+ `applyKind`（`upgrade/rollback`）+ `stepIndex`/`stepHint` computed，把原有 `waitForServerVersion` 的隐藏状态机暴露给用户；`doApply`/`rollbackTo`/`waitForServerVersion` 内驱动 `applyPhase` |
| 上传失败无就地错误区 | `.upload-error`（`uploadError={name,reason}` + `lastUploadFile`）+ `retryUpload()`/`clearUploadError()`，失败后拖放区下方显示文件名 + 原因 + 重试 |
| 5 处 `catch{ /* ignore */ }` | 全部补独立 ref：`versionErr`/`pendingErr`/`historyErr`/`archiveErr`/`geoipLoadErr`，分别渲染错误态与重试 |
| 887 行、无锚点/无折叠 | 顶部 `.anchor-nav`（4 链接 + `scrollToId`）；「IP 地理库」默认折叠 `geoipOpen=false` + 展开/收起按钮（与升级解耦） |
| 无步骤指示 | 见第 1 行 |
| 危险操作无视觉隔离 | 历史危险区 `.hist-danger`、地理库危险区 `.geo-danger` 进 `.danger-zone`（`.dz-title`/`.dz-desc`），与「刷新」等普通操作分开 |
| 「切换」即回滚但文案不明 | 「切换」→「**回退到此版本**」（历史表操作列 `width:180`） |
| 39 处硬编码 px + 1 处写死主题绿 | 全部令牌化；`rgba(0,200,150,0.3)` → `var(--bd-strong)` |

**附带**：`applyError` 提升为**页面级** `.page-error`（`role="alert"`，不再随 `pending` 清空而消失）+ 部分加载失败汇总 `.page-warn`（`loadErrors` computed + `reloadAll`）；import 加 `WarningFilled`/`Check`。

**实测**（1440×900，`.workbuddy/tmp/b15-settings.mjs`）：锚点导航 4 项、历史表头 7 列、「回退到此版本」10 个、`.danger-zone` 2 处、IP 地理库默认折叠 `true`（展开后 `.danger-zone` 1 + 样本表 1 行）、横向溢出 **0px**、表头零被裁。

---

## 27. PS-26 · 系统设置 `/system/settings`（5 个子页）

**功能点**：左侧纵向二级导航（站点与品牌 / 告警管道 / 系统自监控 / 数据保留 / AI 助手）+ `keep-alive` 保留子页状态 + `?tab=` 直达。

### 26.1 容器层 `SettingsView.vue`

- [信息架构] 导航项是**纯 `div`**（L11-19），无 `role="tab"`/`aria-selected`，无 `<button>`，**键盘无法聚焦切换**
- [信息架构] **切换分类不写回 URL**（`active` 只在初始化读一次 `route.query.tab`，L55）→ 刷新/分享/前进后退全部失效
- [状态缺失] **无"未保存改动"提示**（13 个设置类页面全都没有）；`.save-bar` 样式已就绪但 0 引用
- 亮点：**唯一完全令牌化的文件**（新令牌 ~17 处、旧令牌 0、硬编码色 0）✅；横向 `el-tabs` 已改为纵向二级导航 ✅

### 26.2 各子页

| 子页              | 核心问题                                                                                                                                                                                                                                                                                                                           |
| --------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **告警管道**（838 行） | **不是流水线编排**——无画布、无节点、无可视化连线，是"4 个纵向可编辑表格 + 预览 + YAML 折叠"；三段管道的顺序与依赖**完全不可见**（只有顶部一段文字说明）；**relabel 单行 6 个控件**（注释已自认"一窄就叠成一条、没法输入"）；**YAML 与表单不同步**（改表格不更新 YAML，改 YAML 忘了点"应用"就直接保存 → 静默丢弃，真实数据丢失陷阱）；**删除无二次确认**（一行模板正文一次误点即消失）；**无表单校验**（op=replace 但 pattern 为空、正则非法、when 格式非法**静默丢弃**）；3 个 `.section-title` 是 `<span>` 无标题层级 |
| **站点与品牌**       | 「恢复默认」（破坏性）与「保存」并排且无二次确认，点了只改本地不落库也不提示；FileReader **无 `onerror`**；Logo base64 内嵌 localStorage（大图挤配额 + SVG XSS 面）；`--bg-soft` 未定义靠 fallback                                                                                                                                                                                     |
| **系统自监控**       | 10 秒轮询 + 每次 `dispose()+initChart()` **全量销毁重建**；**被 keep-alive 切走时仍在轮询**；探针失败与快照失败**合并进同一个 error**；**趋势查询失败完全吞掉**（L224-226 catch 空实现）；16 项指标平铺 4 列无分组，3 个"坏值"分散                                                                                                                                                                 |
| **数据保留**        | **「立即清理」是破坏性操作却无二次确认**，与"保存"并排；策略表单 5 控件 + 2 按钮横排（gap 6px）；描述区无 loading，首屏 7 项全 `'—'`；**「0 = 不清理」语义反直觉**                                                                                                                                                                                                                       |
| **AI 助手**       | **「测试连接」测的是服务端已保存配置，不是当前表单**（`http.testAIConfig()` 无参，L178）→ 改完端点点测试得到旧结果，**强误导**；`loading` 定义了但模板从未使用；修改字段后 `lastTest` 不清空；危险开关「允许发送敏感内容」**无视觉隔离**（会记审计但 UI 无提示）；API Key 无 `autocomplete`                                                                                                                                     |

**优化目标**：设置页的核心不是美观，是"改了什么、有没有保存、改错了能不能回来"。

**展示改进**

- 导航项换 `<button>` + `role="tab"` + `aria-selected` + `:focus-visible`
- 导航项加图标，并在高风险分类旁加**状态点**（AI 是否已启用、管道是否已配置）
- **告警管道加"管道"可视化**：最小成本是给三个区块加 ①②③ 序号 + "上一步输出 = 下一步输入"说明；高配版是只读 3 节点示意图（节点显示规则条数，点击滚动定位）
- 三个 `.section-title` 改 `<h3>`；空态换 `EmptyState`

**交互改进**

1. 切换分类时 `router.replace({ query: { tab } })`
2. **定义子页 dirty 契约**（子页 `expose` 出 `isDirty()`/`save()`），容器层渲染 `.save-bar`（样式已就绪）
3. **AI「测试连接」改为先保存草稿，或明确提示"将测试已保存的配置"**；改字段即清空 `lastTest`
4. 「恢复默认」/「立即清理」加二次确认 + 说明"需点击保存才生效"
5. **告警管道**：YAML 区在表单变更后提示"点『从表单生成』同步"；"应用到表单"加确认；删除行加确认或 Undo；when 格式非法**显式报错**；补表单校验（op=replace 必须有 pattern、模板非空、正则可编译）
6. relabel 表单行改**紧凑/展开两档**（默认一行显示 op + 摘要，点击展开完整编辑行）
7. 自监控：图表改 `setOption(notMerge)` 增量更新 + 轮询拆成"快照 10s / 图表 60s"；keep-alive 切走/页面隐藏时暂停；探针与快照错误拆成两条 alert；16 项指标按"运行时/HTTP/TSDB/通知/告警/节点"分组
8. 敏感开关与删除/清理类操作进 `.danger-zone`

**优先级**：价值 ★★★★★ · 成本 中高（告警管道是难点）

### 26.3 实施记录（批次 15 · 2026-10-09）

**容器层 `SettingsView.vue`**

| 现状问题 | 处置 |
| --- | --- |
| 导航项是纯 `div`，键盘不可聚焦 | 导航项换 `<button role="tab">` + `aria-selected` + `:focus-visible`；选中项 `tabindex=0`、其余 `-1`（roving tabindex） |
| 切换分类不写回 URL | 切换时 `router.replace({ query: { tab } })`，刷新/分享/前进后退均生效 |
| 无"未保存改动"提示 | 定义**子页 dirty 契约**：子页 `defineExpose({ dirty, save, reset })`，容器层渲染 `.save-bar`（吸底）显示「有未保存的改动」+ 放弃改动/保存 |

**子页 `BrandSubView`（站点与品牌）**：「恢复默认」加二次确认 + 明示"需点击保存才生效"；FileReader 补 `onerror`；`--bg-soft` 改已定义令牌；硬编码令牌化。

**子页 `RetentionSubView`（数据保留）**：「立即清理」加二次确认 + `loading`；描述区补 loading（首屏不再全 `'—'`）；「0 = 不清理」语义就地写清。

**子页 `AISubView`（AI 助手）**：「测试连接」按钮文案明示**测的是服务端已保存配置**（不误导）；改字段即清空 `lastTest`；`loading` 真正绑到按钮；危险开关「允许发送敏感内容」进 `.danger-zone`；API Key 补 `autocomplete`。

**子页 `SelfMonitorSubView`（系统自监控）**：图表改 `setOption(notMerge)` **增量更新**（不再 `dispose()+initChart()` 全量重建）；轮询拆成"快照 10s / 图表 60s"；keep-alive 切走/页面隐藏时暂停；探针失败与快照失败**拆成两条 alert**；趋势查询失败不再吞掉；16 项指标按"运行时/HTTP/TSDB/通知/告警/节点"**分组**。

**子页 `AlertPipelineSubView`（告警管道）**：顶部加**只读 3 节点可视化**（节点显示规则条数 + 箭头连线，点击滚动定位）+ 区块 ①②③ 序号；YAML 区在表单变更后提示"点『从表单生成』同步"（`yamlStale`）；"应用到表单"加确认；删除行加二次确认；`op=replace` 必须有 `pattern`、模板非空、正则可编译**显式报错**；relabel 行改**紧凑/展开两档**（默认一行显示 op + 摘要，点击展开完整编辑行）；3 个 `.section-title` 由 `<span>` 改 `<h3>`。

**实测**：导航 5 项 `button[role=tab]`（选中 `tabindex=0`、其余 `-1`）；改字段吸底条出现、放弃改动后消失；切「告警管道」hash=`#/system/settings?tab=pipeline`；管道节点 3 + 箭头 2 + 区块 `h3` 六项；横向溢出 **0px**。

---

## 28. PS-27 · 用户与角色 `/system/users` · `/system/roles`

**功能点**（UsersView）：用户列表 7 列 / 角色多标签 / 数据范围双维度（节点 + 业务标签）/ 新建·编辑 / 重置密码 / 禁用·启用 / 删除。  
**功能点**（RolesView）：角色列表 5 列 / 新建·编辑 / **权限点矩阵**（按域折叠 + 域内网格勾选，后端 **47 个权限点 / 19 个域**）/ 高风险权限警示 / 删除。

**现状问题（✅ 已核实）**

- **[B1] 权限目录字段契约不匹配**（后端 `Domain`/`Items` 无 json tag，前端读 `d.domain`/`d.items`）→ 矩阵整块不可用，`d.items.length` 抛 TypeError
- **[B1 附带] `DOMAIN_LABELS` 的 key 是英文**（dashboard/nodes…，L193-208），而后端 `Domain` 是**中文**（"仪表盘"/"主机 / 节点"）→ 即使字段名修好也永远命中不到
- [信息架构] 47 个权限点平铺在 19 个折叠域内，**无搜索 / 无域内全选 / 无"仅看已选" / 无高风险筛选**
- [交互成本] 高风险权限只是**顶部一段纯文本列举 9 个 key**（L132），与下方勾选框**无任何视觉关联**——勾选 `system:upgrade` 时看不到红色标记
- [可访问性] 自绘 `.perm-check` 用 `<label>` + `@click.prevent`（L143-148），**不是 `<input type="checkbox">`** → **47 个勾选框键盘完全不可达**，读屏不识别
- [交互成本] **无角色对比、无影响用户数**（改角色前无法评估影响范围）
- [状态缺失] 权限目录加载失败**静默**（`.catch(() => ({domains:[]}))`，L246）→ 矩阵空白且无提示
- [硬编码样式] **RolesView 43 处硬编码 px + 7 处硬编码色**（13 个文件中最多）；旧令牌 12 处
- UsersView：[交互成本] 操作列 **5 个 link 按钮平铺**；删除 popconfirm **不显示是哪个用户**；[信息架构] **无搜索/筛选/分页**；[状态缺失] 表格无空态；[视觉一致性] 死样式 16 行（`.page-head/.page-title/.page-sub`）
- 跨文件：`ROLE_LABELS` 在 `MainLayout.vue:63-70`、`UsersView.vue:180-187`、`RolesView.vue:182-189` **三处重复定义**

**优化目标**：授权是安全边界，配错角色的代价最高——必须"看得全、选得准、知道影响谁"。

**展示改进**

- 权限矩阵换回 `el-checkbox-group`（原生 input）或给 `<label>` 补 `role="checkbox"`/`tabindex`/空格切换；域内加「全选 / 清空」
- **高风险标记从顶部文本列举改为逐个权限点的危险标记** + 「仅看高风险」筛选
- 补**「影响用户数」列** + 角色对比（选 2 个角色并排 diff 权限）
- 矩阵加**搜索框**；折叠态默认收起并保留已选域展开
- `ROLE_LABELS` 抽到 composables（三处合一）
- 删死样式 + 43 处硬编码 + 12 处旧令牌令牌化 + `el-card.glass` → SectionCard

**交互改进**

1. **修 B1**（后端加 json tag 或前端对齐 `Domain`/`Items`）+ 修 `DOMAIN_LABELS`；**补一条单元测试**（现有 `RolesView.test.js:62` mock `{domains:[]}`，空数组掩盖了这个问题）
2. UsersView：加搜索 + 分页；「数据范围」列拆成两列或改 popover；删除 popconfirm 带用户名；禁用/启用加确认或撤销；表格补 `EmptyState`

**优先级**：价值 ★★★★★（B1 是正确性缺陷，且是安全边界）· 成本 极低（B1）+ 中（矩阵重构）

#### 实施记录（批次 15 · 2026-10-09）

**B1 修复（后端 `internal/server/auth/model.go`）**

```go
type permDomain struct {
    Domain string       `json:"domain"`
    Items  []Permission `json:"items"`
}
```

原 `permDomain` 无 json tag → 序列化出 `Domain`/`Items`（大写），前端读 `d.domain`/`d.items` 恒 `undefined` → 矩阵整块不可用（`d.items.length` 抛 TypeError）。补 tag 后字段名与前端对齐。**新增单测** `TestPermissionCatalogJSONKeysAreLowercase`（`catalog_test.go`）序列化断言小写 `domain`/`items`；`RolesView.test.js` 的 mock 由 `{domains:[]}` 改为非空目录（空数组掩盖了该问题）。

**`RolesView.vue` 全量重写**

| 现状问题 | 处置 |
| --- | --- |
| `DOMAIN_LABELS` key 是英文、后端域是中文 | `domainLabel(d)` 直接返回中文域（`DOMAIN_LABELS[d] \|\| d`），`DOMAIN_LABELS` 仅对旧英文域兜底 |
| 47 权限点无搜索/无域内全选/无筛选 | `.perm-toolbar`：搜索框 `permSearch` + 「仅看高风险」`riskOnly` + 计数；`visibleDomains` computed 按搜索/风险过滤并剔空域；`watch([permSearch, riskOnly])` 自动展开命中域；域内「全选本域」`selectDomain`/「清空本域」`clearDomain` |
| 高风险只是顶部纯文本，与勾选框无视觉关联 | `RISK_KEYS` Set（10 个）+ `isRisk()` + 逐个权限点 `.risk-badge` 危险标记 |
| 自绘 `.perm-check`（`<label>` + `@click.prevent`）47 个勾选框键盘不可达 | 换 `el-checkbox-group`（原生 input，键盘可达） |
| 无影响用户数 | 新增**「影响用户数」列**（`userCounts`/`userCountOf`，`loadAll` 增 `http.listUsers()`） |
| 权限目录加载失败静默 | 补错误态与重试 |
| 43 处硬编码 px + 7 处硬编码色 + 12 处旧令牌 | 令牌化；`el-card.glass` → `SectionCard`（`dense`）；表格包 `EmptyState` |
| 折叠默认全展开 | 折叠默认收起；`openEdit` 展开"已选权限所在域" |
| `ROLE_LABELS` 三处重复 | 抽到 `web/src/composables/useRoleLabels.js`，`MainLayout`/`UsersView`/`RolesView` 统一引入 |

**`UsersView.vue` 全量重写**：加**搜索**（用户名/昵称/角色）+ **分页**（`page`/`pageSize`/`filteredUsers`/`pagedUsers`，`.u-pager`）；「数据范围」**拆两列**（节点范围 + 业务范围，各带 tag）；操作列**收敛**为 编辑/重置密码 + 「更多」`el-dropdown`（禁用/启用/删除）；删除 `ElMessageBox.confirm` **带用户名**；表格包 `SectionCard dense` + `EmptyState`；`roleLabel` 改从 composable 引入；密码输入补 `autocomplete="new-password"`。

**实测**：users 表头 8 列（含节点范围/业务范围）、首屏 10 行 + 分页 + 搜索框、「更多」10、搜「支付」→ 1 行命中；roles 表头含「影响用户数」、矩阵原生 checkbox 14、域 5、高风险标记 7、搜索框 + 仅看高风险、计数「已选 4 / 14 项」、勾「仅看高风险」→ 7 个高风险点；横向溢出 **0px**。

---

## 29. PS-28 · 个人中心 `/system/profile`

**功能点**：横向 `el-tabs` 两页签（基本信息 / 修改密码）/ 用户名（禁用）/ 昵称（可改）/ 我的角色 / 数据范围 / 保存 / 复用 `ChangePasswordSubView`。

**现状问题（✅ 已核实）**

- [视觉一致性] **死样式 20 行**（`.page-head`/`.page-title`/`.page-sub` L91-106、`.card-title` L107-110）
- [视觉一致性] **导航形态与 SettingsView 冲突**：SettingsView 注释明确说"横向 el-tabs 改纵向后一眼可分"，ProfileView 仍是**横向 el-tabs**
- [视觉一致性] 两个 tab 的容器形态不同（`el-card.glass.info-card` vs `el-card.settings-card`）
- [信息架构] 「我的角色」「数据范围」是**只读信息却用 `el-form-item` 包裹**（L18-31），与可编辑的昵称混在同一张表单，而按钮只有"保存昵称"→ 用户可能以为能改角色
- [状态缺失] 无 loading / 无错误态；`principal` 未加载时 `principal.scope.mode`、`principal.roles.map` 会报错
- [状态缺失] 昵称改动后切 tab 或离开**无提示**
- [硬编码样式] 13 处硬编码 px；**新令牌 0 处**（规范渗透最低之一）
- `ChangePasswordSubView` 附带问题：**前端校验（≥6 位）与后端（≥2 类字符，`model.go:325-327`）不一致** → 前端放行后端拒绝；placeholder 写"建议 8 位含字母数字"而校验只要 6 位，**文案与规则矛盾**；后端错误走 `ElMessage` 而前端错误走 `:error` 就地，**两种错误呈现位置不一致**

**优化目标**：不能改的和能改的，必须一眼分得开。

**展示改进**

- 删 20 行死样式；只读信息（用户名/角色/数据范围）改 **`el-descriptions`** 而非 `el-form-item`
- `el-card.glass` → SectionCard；13 处硬编码 + 2 处旧令牌令牌化

**交互改进**

- 昵称改动加 dirty 提示 + 离开确认；保存成功给就地反馈
- tab 形态与 SettingsView 统一（或把"修改密码"并入 SettingsView 形成单一设置入口）
- 补 `principal` 未加载时的骨架
- **密码校验对齐后端规则** + 实时强度条；后端错误也就地显示在"原密码"字段

**优先级**：价值 ★★☆ · 成本 低

#### 实施记录（批次 15 · 2026-10-09）

**`ProfileView.vue` 全量重写**

| 现状问题 | 处置 |
| --- | --- |
| 死样式 20 行 | 删除（`.page-head`/`.page-title`/`.page-sub`/`.card-title`） |
| 导航形态与 SettingsView 冲突（横向 el-tabs） | `el-tabs` 改 `tab-position="left"`，与 SettingsView 纵向一致 |
| 两个 tab 容器形态不同 | 统一 `SectionCard` |
| 只读信息用 `el-form-item` 包裹（与可编辑昵称混排） | 只读信息（用户名/我的角色/节点范围/业务范围）改 `el-descriptions :column="1" border`，与昵称表单分开 |
| 无 loading / 无错误态，`principal` 未加载会报错 | `onMounted` 仅在无缓存时 `auth.loadMe()` + `loadingPrincipal` 骨架 |
| 昵称改动离开无提示 | `savedNickname`/`nickname`/`dirty` + `.save-bar` 吸底（放弃改动/保存昵称）+ `onBeforeRouteLeave` 有脏改动时 `ElMessageBox.confirm` 拦截 |
| 13 处硬编码 px、新令牌 0 处 | 令牌化 |

**`ChangePasswordSubView.vue` 全量重写**（PS-28 复用件）

- **校验对齐后端**：`classCount(pw)`（小写/大写/数字/特殊）+ `validate()` ≥8 位 + ≥2 类字符（对齐 `auth.ValidatePassword`）；placeholder 文案同步规则（原前端只要 6 位、文案写"建议 8 位"，**前后矛盾**）
- 新增**强度条** `strength` computed（4 档弱/中/强/很强）+ `.strength-bar`
- **后端错误就地回填**：`msg.includes('原密码') ? errors.oldPassword : errors.newPassword`（原后端错误走 `ElMessage`、前端错误走 `:error`，两种位置不一致）

**实测**：`el-descriptions` 1（字段 用户名/我的角色/节点范围/业务范围）、tabs 方向 `left`、改昵称后吸底条出现（「昵称已修改，尚未保存」）；新增 `ProfileView.test.js`（4 例）+ `ChangePasswordSubView.test.js`（5 例）。

---

## 30. PS-29 · 全局件（命令面板 / 页脚 / 壳层）

### 30.1 `CommandPalette.vue`（402 行）

- [信息架构] **只有导航，没有"动作/命令"**（无"新建拨测任务""生成日报"）——它是跳转器不是命令台，与 `Ctrl+K` 的用户预期有落差（placeholder 也只写"页面、主机"）
- [交互成本] 匹配只有小写 `includes`：无拼音/首字母/模糊打分/相关度排序；**无最近访问/常用置顶**
- [状态缺失] 主机加载无 loading；**主机拉取失败后 `hostsLoaded` 已置 true（L187→L201），本次会话不再重试且无提示**
- [信息架构] 主机组**无数量上限**，几百台时列表极长
- [可访问性] 有 `role="dialog" aria-modal` 但**无 `aria-label`**；列表无 `role="listbox"/"option"`，高亮项无 `aria-activedescendant`；**无焦点陷阱**（Tab 跑到背景页）；关闭后焦点不还原

**改进**：列表语义化 + 焦点管理 · 加最近访问/常用置顶 + 模糊打分 · 主机组 loading + 失败可重试 + 结果上限 · 补 Home/End/PgUp/PgDn · **引入"命令"类型条目并独立分组**（从导航器升级为命令台）

### 30.2 `SiteFooter.vue`（104 行）

- [信息架构] `links`（L38-40）与 `icpText`/`icpUrl`（L35-36）是预留未实现的**死代码**（约 30 行永不生效）
- [信息架构] 品牌名在页脚**无信息增量**（Sidebar 已有），窄屏还独占一行
- [状态缺失] **无版本号 / 构建号**（`WEB_VERSION` 已在 UpgradeView 使用，`src/version.js` 存在）——运维截图问"你这版是哪版"时看不到
- [视觉一致性] `color-mix(in srgb, ... 84%, transparent)`（L50）在旧 WebView 不支持；84% 半透明叠加产生不可预期对比度；**新令牌 0 处**
- 渲染范围：仅 MainLayout 的 21 个子路由；`/login` `/status` `/screen` 无 ✅（合理）

**改进**：删死代码或真正接上配置源 · **加版本号/构建时间**（成本几乎为零）· 品牌名去重或改为"品牌名 · 版本" · `color-mix` 改不透明底色 + 1px 上边框

### 30.3 壳层（MainLayout / Sidebar / AppTopBar）

批次 2–4 已落地（侧栏 232px、分组默认全展开 + 路由 watch、折叠态 tooltip、用户卡、顶栏 ⌘K 入口、大屏入口降噪、命令面板）。**遗留问题**：

- 顶栏「数据大屏」按钮与侧栏「对外状态页」入口已降噪 ✅，但顶栏 4 个图标中"刷新"依赖 `view.value.reload()`，未接入 `RefreshBar` 的统一状态
- `keep-alive :include` 只有 3 个页面（OverviewView/HostsView/AlertsView）→ **NodeView 切 Tab 不缓存、来回切换重复请求**（PS-05 已提）
- 全站无 `prefers-reduced-motion`；`style.css` 有 2 个无限动画（`.sk-pulse` L823、`pulse` L572）——**批次 8 已加 `prefers-reduced-motion` 全局降级**，见 §32

#### 30.4 实施记录（批次 17 · 2026-10-09）

**PS-29 `CommandPalette.vue`（402 → ~520 行，从「导航器」升级为「命令台」）**

- **三类数据源**：① 页面导航（**只从 `router.getRoutes()` 派生**，带参路由如 `/node/:name` 自动剔除）② **命令条目**（`COMMANDS`：切主题 b/a/c、`刷新当前页数据`、`打开数据大屏`、`返回首页概览`）③ 主机直达（`HOST_LIMIT=8` 上限）。
- **语义化**：容器 `role="dialog"` + `aria-modal` + `aria-label="命令面板"`；输入 `role="combobox"` + `aria-expanded` + `aria-controls="cmdk-listbox"` + `aria-activedescendant`；列表 `role="listbox"`；行 `role="option"` + `:aria-selected` + `id="cmdk-opt-{idx}"`。
- **焦点管理**：`lastFocused` 记录触发元素，`close()` 后 `nextTick` 还原；`.cmdk` 加 `@keydown.tab.prevent` 做**焦点陷阱**（Tab 不再跑到背景页）。
- **匹配**：`score(text,q)` 子串命中优先（`1000-i`）否则子序列匹配 + `rank()` 排序，替代原纯小写 `includes`。
- **最近访问**：`RECENT_KEY='nebula-cmdk-recent'` / `RECENT_MAX=5`，空查询时置顶「最近访问」分组。
- **主机组状态**：`hostsLoading`/`hostsError` refs，**失败时不置 `hostsLoaded` 可重试**（原 L187→L201 失败即 `hostsLoaded=true` 死锁）；模板底部 `.cmdk-status`（`err` 态 + `.cmdk-retry` 按钮）。
- **键盘**：补 `Home`/`End`/`PageUp`/`PageDown`；`setActive(i)` 取模环绕。
- **`markRaw(Monitor)`**：修「Vue received a Component that was made a reactive object」告警（组件对象放进 `hostItems` 响应式数组被代理）。
- `defineExpose({ open, close, toggle })`；`MainLayout` 新增 `nebula:refresh` 监听 → 调 `refresh()`。

**PS-29 `SiteFooter.vue`（104 → ~60 行）**

- **删死代码**：`links`（L38-40）/`icpText`/`icpUrl`（L35-36）约 30 行永不生效，全删。
- **加版本号**：`import { WEB_VERSION } from '../version'` → `<span class="site-footer__ver">v{{ version }}</span>`（运维截图可辨识版本）。
- **移除 `v-if="visible"` 外层包裹**（原 footer 为空时整块不渲染）；`footerText` 有值才显示右侧 `.site-footer__meta`。
- **令牌化**：`--text-dim`→`--t3`、`--border`→`--bd`、`--bg-elev`+`color-mix`→`--s1`（**消 `color-mix` 旧 WebView 兼容风险**）、字号→`--fs-sm`；断点 `760px`→`768px`。

**断点体系收编（全站除 `screen/**`）**

- 统一为**三档**：`1280px`（窄桌面降档/收元数据列）、`1024px`（双列塌单列/换行堆叠）、`768px`（表单筛选单列）；`screen/**` 大屏域独立（1500/2400/3440/560）不受约束。
- `style.css` 断点注释重写为三档体系；`sed` 批量把散落的 `760/900/960/1100/1200/1260` 等旧断点归并（30 处 `@media`）。

**全站旧别名令牌清零**

- 删除三套主题各一份旧别名**定义**：`--bg-elev`/`--bg-card`/`--border`/`--border-strong`/`--text`/`--text-main`/`--text-dim`/`--text-muted`/`--label`/`--radius`；`print.css` 同步删旧别名定义 + 4 项死令牌（`--bg-primary`/`--bg-secondary`/`--panel-bg`/`--border-color`）。
- **全部引用替换为新令牌**（`--text`→`--t1`、`--text-dim`→`--t2`/`--t3`、`--text-muted`→`--t3`、`--border`→`--bd`、`--bg-card`/`--bg-elev`→`--s1`、`--radius`→`--r-lg`、`--label`→`--t2`）。
- **带 fallback 的漏网引用**（`var(--oldX, #hex)`，`sed` 不命中）共 6 处，本轮手工补齐：`NodeView.vue` 3 处、`StatusView.vue` 2 处、`screen/tabs/NetworkTab.vue` 1 处。
- **决策：`--chart-green` 保留不清零** —— `--chart-*`（cyan/blue/green/red/orange/yellow/purple）是 §1.1 定义的**合法图表色板族**（非旧别名）；§32 要清零的是 `--text-dim`/`--text-muted`/`--border`/`--bg-card` 四项。

**打印层增强（`print.css`）**

- 隐藏清单补批次 8~17 新增共享件：`.save-bar`/`.anchor-nav`/`.fc-clear`/`.fc-x`/`.el-alert__close-btn`/`.sk-pulse`/`.sec-skeleton`。
- 药丸描边处理补 `.pill`/`.fc-chip` + `.pill-dot { box-shadow: none }`；进度条补 `.mini-bar`/`.mini-bar-fill`。

**HostsView 窄屏列降级（§5.1 配套）**

- `AUTO_HIDE_NARROW = ['version','group']` + `narrow` ref + `onNarrowChange(e)` + `autoHiddenCols` computed；`colVisible(k)` 在窄屏对这两列返回 `false`。
- `window.matchMedia('(max-width: 1280px)')` 监听（兼容 `addEventListener`/`addListener`），`onUnmounted` 移除。
- 表格上方加 `<el-alert v-if="autoHiddenCols.length" class="narrow-cols-tip">` 说明条。
- **红线守住**：只自动收起**元数据列**（版本/分组），**不改 `selectedCols` 用户勾选**；CPU/内存/磁盘/流量/负载等**指标类列任何宽度下都保留**（§0.4）。

**顺带修复：`/screen` 大屏 TDZ 白屏（既有缺陷）**

- 现象：`/screen` 根元素不渲染，控制台 `ReferenceError: Cannot access 'placedNodes' before initialization` at `TopologyMap.vue:25`。
- 根因：`hasNodes = computed(() => placedNodes.value.length > 0)` 与 `watch(hasNodes, ..., {immediate:true})` 被写在 `placedNodes` **定义之前**，`immediate` 在 setup 期间读 `hasNodes.value` 命中 `placedNodes` 的**暂时性死区**。
- 修复：把 `hasNodes` + `watch` 移到 `placedNodes` **之后**并加注释说明 TDZ 风险。

**验证**

- 串行 vitest（`--pool=forks --poolOptions.forks.singleFork=true`）→ **48 files / 401 tests 全绿**（批次 17 新增 `CommandPalette.test.js` 6 例 + `SiteFooter.test.js` 3 例）。
- `vite build --emptyOutDir=false` 通过（`✓ built in 16.24s`）。
- **1440×900 / 1280 / 1024 浏览器实测**（`.workbuddy/tmp/b17-global.mjs`）：`/` 页脚品牌「NebulaEye」+ 版本「v1.30.66」+ 死代码链接 0；命令面板 `role=dialog aria-label=命令面板`、列表 `role=listbox`、选项 34 含命令分组、输入「切极」命中「切换主题：极光蓝」、↓↓ 后 `active=cmdk-opt-2 aria-selected=true`、选中后关闭 + `localStorage` 写入最近访问 + 重开含「最近访问」；`/hosts` @1440 11 列表头全含版本/分组 + 溢出 0px、@1280 收为 9 列 + 说明条出现 + 溢出 0px、@1024 同 1280（溢出 224px 为既有 D8 问题，原生滚动可用）；**`/screen` 恢复渲染（根元素命中、文本 480 字、无 TDZ 报错）**；`/status` 正常；打印层侧栏/顶栏/页脚/保存条 `display=none`、页面底色 `rgb(255,255,255)`；除 mock 环境 WS 握手外无 JS 报错。

---

## 31. 分批实施路线（承接批次 0–5，从批次 6 之后继续）

> 每批结束必须 `npm run build` 通过；每批可独立上线，不出现"改到一半破版"。

| 批次        | 主题                                 | 范围                                                                                                                                                                                                                                                                                                                                                                                                                                     | 预估    |
| --------- | ---------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----- |
| **批次 7**  | **P0 正确性修复**                       | B1–B12 全部（B1 含后端 2 行 + 单测）                                                                                                                                                                                                                                                                                                                                                                                                             | 1 天   |
| **批次 8**  | **规范件增强 + 表格密度 + 字号基线 + 筛选/描述段规范**<br>（**✅ 全部完成 2026-10-09**：§1.4 字号基线 + §1.2 表格密度 + §1.5 筛选区 + §1.6 描述段 + §1.3 规范件增强，见 §1.4.7 / §1.5.7 / §1.6.7 / §1.3.1；§1.1 的 `--z-*` / `--chart-*-dim` 因 EP 层叠审计与 `TrendChart` 依赖挂账） | PS-00：`SectionCard` 加 loading/error、`EmptyState` 加 size、新建 `FilterChips`/`PartialBar`、表格密度拉齐、hover 死声明清理、补 `prefers-reduced-motion`；**§1.4 字号基线**：EP base 16→14px（L673）、根字号 16→14px（L207）、表格正文 13→14px、`<12px` 27 处与 `.5px` 15 处清零（OpsView/IntelligenceView/IntelligenceBlock 优先）；**§1.5 筛选区尺寸**：size 全站统一 small、容器统一 `.filter-bar`、宽度 5 档令牌 `--w-xs..xl`；**§1.6 描述段距离**：8 处页内 `.muted` 覆写删除，三档 `--desc-tight/base/block`、`.muted` 全局化 | 2.5 天 |
| **批次 9**  | **中间件骨架**<br>（**✅ 骨架 + 试点完成 2026-10-09**：见 §1.3.2 / §6.3.1；其余 8 个手写 Tab 接入属批次 10+）                          | 新建 `MwHostCard`/`MwDetailHeader`/`TrendChart`/`mwRange.js` + `echarts.mwTrendOption`；`MwStatusDot` 两态扩三态（离线/失败改红）+ `dotOnly`；试点 `DockerTab`/`LightCollectorTab`：`.glass`→`SectionCard`、空态→`EmptyState`、抽屉→`MwDetailHeader`+`TrendChart`；顺带修 **B3**（双定时器）/ **B9**（loading 初值）/ **B11**（在线主机统计）                                                                                                                                                                                                                                                                                                                                  | 2 天   |
| **批次 10** | **中间件逐 Tab 接入**<br>（**✅ 全部完成 2026-10-09**：15 个 Tab 全部接入骨架件；顺带修 `rangeToStep` **秒→毫秒**回归，见 §32 批次 10 条目）                    | 按 Redis → MySQL/Docker/K8s → Nginx/Postgres/Kafka/RocketMQ → Mongo/FastDFS → Light+5 spec 顺序。**成果**：`Postgres`/`Nginx`/`Kafka`/`RocketMQ`/`Mongo`/`FastDFS`/`K8s`/`MySQL`/`Redis` 9 个手写 Tab 全部 `.glass`→`SectionCard`、空态→`EmptyState`、自绘 `.dot`→`MwStatusDot`、抽屉→`MwDetailHeader`+`TrendChart`；概览柱状图改由 `TrendChart` 托管（删手写 init/dispose）；抽屉趋势查询改 `Promise.all` 并行；`RedisTab` 由 1852 行收敛至约 1100 行（清约 80 行死代码）。**关键回归修复**：`mwRange.rangeToStep` 原返秒（60/120/300），后端 `query.go` 判 `step<1000` 直接 400 → **9 个已重写 Tab 抽屉趋势图恒显"加载失败"**；改为毫秒（5000/15000/60000/120000/300000），对照 `NodeView.vue:1162`（`3600000`）与 `querier.go:37`（`step/1000`）。**附带修复**：`EmptyState.icon` prop 类型加 `Function`（9 Tab 各报一次类型告警） | 3 天   |
| **批次 11** | **告警域**<br>（**✅ 全部完成 2026-10-09**：PS-14 + PS-15 + PS-16，见 §32 批次 11 条目）                            | **PS-14 `AlertsView`**：抽屉改「列表 + 详情」可对照形态（列表仍可见 1142px）、轮询不打断打开中的抽屉、评论草稿随关闭清理、动作分区 + 命名统一"认领"、整行可点（双击）+ 状态三重编码（色 + 文案 + 图标）、三表 `v-loading`、`PartialBar` 部分失败、高级区默认收起、硬编码令牌归整。**PS-15 `RuleModal`**：保存中态防重复提交、字段级校验（`validate()` 真拦）、脏数据关闭确认、切类型清异类字段、分区标题同源 `--accent`、触发条件常驻摘要、响应式断点、未启用渠道「去通知配置启用」跳转、`label`→`span` 语义修正。**PS-16 `NotifyView` 重写**：5 张 `el-card`→`SectionCard`（`#actions` 内 `StatusPill` + `el-switch`）、顶部健康度摘要条、加载失败页面级错误态 + 重试、5 条 toast 校验改为卡片内字段级 `:error`、保存拦下配置不完整渠道并滚动定位、开关 `aria-label`、密钥字段"不回显"说明 | 2 天   |
| **批次 12** | **资产域**<br>（**✅ 全部完成 2026-10-09**：PS-11 + PS-10 + PS-09 + PS-08，见 §32 批次 12 条目）                            | PS-11（diff + 常驻 + 筛选）→ PS-10（增删 + 吸底保存）→ PS-09（图四件套）→ PS-08（先拆子组件）。**成果**：`InspectView` 差异升级真 diff（`.struck → .diff-new`）+ 差异块常驻 + 执行区 `SectionCard`；`AssetModelView` 补「新增/删除字段」入口 + `.save-bar` 吸底 + `dirty`/`dirtyCount` + 切类型拦截 + `EmptyState`/`v-loading`；`AssetTopologyView` 一页两段（聚合 + 图）各持四态 + 点行下钻筛选 + 走 `TopologyGraph` 图四件套；`AssetListView` **2217 → 1117 行**（拆出 `assetFormat.js` + `AssetKpiRow`/`AssetFilterPanel`/`AssetTable`/`AssetDetailDrawer`/`AssetAttrEditor`/`AssetModals` 7 件），状态统一 `StatusPill`、筛选摘要条（下钻可撤销）、彻底删除进 `.danger-zone`、抽屉三 Tab 各自四态、KPI 下钻改真按钮 | 3 天   |
| **批次 13** | **列表页**<br>（**✅ 全部完成 2026-10-09**：PS-04 + PS-05 + PS-20 + PS-23，见 §32 批次 13 条目）                            | PS-04（**列宽重排 5.1 ✅ 已实施（2026-10-09，4 页实测溢出归零）** + BatchBar + 抽屉分段 + 命令块默认折叠）→ PS-05（并行化 + WS 首帧骨架 + h3 语义标题 + 自动/暂停开关）→ PS-20（命中高亮 + 行内计数 + 导出 CSV/复制选中 + FilterChips）→ PS-23（result/remoteIP/method 筛选 + 右侧固定列 + `changeSummary` 记忆化 + 校验时间戳/重校验）；实测修 `/logs` 38px 与 `/audit` 16px 两处横向溢出                                                                                                                                                                                                                                                                                                                         | 2.5 天 |
| **批次 14** | **图表工具页**<br>（**✅ 全部完成 2026-10-09**：PS-22 + PS-21 + PS-18 + PS-19，见 §32 批次 14 条目）                          | PS-22（修 B6/B7 + 排序 + 类型）→ PS-21（时间范围 + 三态）→ PS-18（状态列 + sparkline）→ PS-19（预览 + 加载态）                                                                                                                                                                                                                                                                                                                                                    | 2 天   |
| **批次 15** | **设置域**<br>（**✅ 全部完成 2026-10-09**：PS-26 + PS-25 + PS-27 + PS-28，见 §32 批次 15 条目）                            | PS-26（dirty 契约 + `.save-bar` + 导航 a11y + URL 同步 + AI 测试语义 + 管道可视化）→ PS-25（三步指示 + 上传失败就地 + 危险区 + 锚点）→ PS-27（**修 B1** + 矩阵重构 + 高风险标记 + 影响用户数）→ PS-28（`el-descriptions` + dirty + 密码校验对齐后端）                                                                                                                                                                                                                                                                                                                                                   | 3 天   |
| **批次 16** | **安全与首页**<br>（**✅ 全部完成 2026-10-09**：PS-24 + PS-03 + PS-13，见 §32 批次 16 条目）                          | PS-24（状态缺陷 + 四套收敛 + 修复建议）· PS-03（首页状态完整化 + 智能运维可折叠）· PS-13                                                                                                                                                                                                                                                                                                                                                                             | 2 天   |
| **批次 17** | **全局收口**<br>（**✅ 全部完成 2026-10-09**：PS-29 + 断点收编 + 旧别名清零 + 打印层 + HostsView 窄屏降级 + 顺带修 `/screen` TDZ，见 §30.4）                           | PS-29（命令面板 + 页脚）+ 断点体系收编（1280/1024/768）+ 全站硬编码清零 + 打印层（既有附录 A）                                                                                                                                                                                                                                                                                                                                                                         | 2 天   |
| **批次 18** | **UI 反馈批量修复（9 项）**<br>（**✅ 全部完成 2026-10-09**：拨测拆列 + 资产/审计/升级/用户列宽与操作列 + 安全中心 + 自监控 400 + 品牌提示迁移 + 个人中心还原，见 §32 批次 18 条目）                           | 用户 9 张截图逐条修复：**拨测**（`当前状态` 拆「状态/延迟/可用率」三列 + `Sparkline` 改无轴 `sparkOption` 修延迟趋势压扁）· **资产台账**（`上报状态` 100→116、`归属节点` 120→150 + `show-overflow-tooltip`）· **操作列换行**（`op-col` flex 方案，资产/拨测/升级/用户统一）· **安全中心**（删锚点 + 顶部空白 16→14 + 基线明细 `max-height:560px` 滚动 + 节点/IP 检索）· **审计**（`结果` 76→94 消尾部多余点，四列合计收敛归零）· **系统自监控**（趋势 `step=30`→`60000` 修 400 + 六卡片网格重排）· **品牌提示**（`SettingsView` 左栏 `.sn-tip` → `BrandSettingsSubView` 常驻 `el-alert`）· **个人中心**（还原 `HEAD` 原样）· **升级页**（删锚点 + 版本号前置/`当前运行` tag 后置 + 操作列 `op-col`）· **用户管理**（「更多」下拉 5 按钮平铺进操作列） | 1 天   |

**三条纪律**（沿用批次 6）：

1. 一次只改一个页面，改完即提交
2. **先套模板，再做特例**
3. 遇到硬编码顺手替换，不专门开批次

---

## 32. 验收清单

- [x] `npm run build` 通过；三套主题切换后卡片/表格/按钮/**全部图表**（含 `BaselineTrendChart`）颜色跟随（**批次 17 复核，2026-10-09**）：`vite build --emptyOutDir=false` 通过（`✓ built in 16.24s`）；旧别名令牌清零后三套主题（`b`/`a`/`c`）色值全部经 `--t*`/`--bd*`/`--s*` 令牌解析，切主题派发 `nebula:theme-changed` 事件、`CommandPalette` 切主题命令实测生效
- [x] B1–B12 全部修复，其中 B1 有单测覆盖（**批次 15 已修 B1**：后端 `permDomain` 补 json tag `domain`/`items` + `TestPermissionCatalogJSONKeysAreLowercase`；`RolesView.test.js` 的 mock 不再是 `{domains:[]}`，改非空目录）
- [x] 全站状态表达 100% 走 `StatusPill`，且每个状态有颜色之外的第二编码（**批次 11–17 逐域落地，2026-10-09**：资产域（12：`AssetTable` 上报状态/来源、`AssetDetailDrawer` 生效值；原 4 套自绘 `.dot/.tag/.src-*` + 5 处写死色已删）、告警域（11）、列表页（13：`AuditView` 结果带图标第二编码）、图表工具页（14：`MetricsExploreView`/`DialTestView`/`ReportView`）、设置域（15）、**安全中心 `SecurityView` / 首页 `OverviewView` / 审批中心 `ApprovalsView`（16）**。**例外**：中间件域走 `MwStatusDot`（面向实例健康度，定位不同），见下行）
- [ ] 全站容器 100% 走 `SectionCard`，`.glass` 零引用（**进行中**：中间件内容区 **15 个 Tab 全部接入、0 处 `.glass`**（批次 10）；**资产域 4 页全部 `SectionCard`**（批次 12）；**告警/通知/安全/首页/审批（11/16）** 与**设置域 5 子页（15）**已接入；**仅剩页面外壳** `aside.sidebar`/`header.topbar` 与 `screen/**` 大屏域仍在用，见 §31）
- [x] 设置类页面 100% 有"未保存改动"提示；危险操作 100% 进 `.danger-zone`（**批次 15/17 落地，2026-10-09**：**PS-10 `AssetModelView`** `.save-bar` 吸底 + `dirty`/`dirtyCount` + 切类型拦截；**PS-08 `AssetListView`**「彻底删除」移入 `.danger-zone`（批次 12）；**设置域**：`SettingsView` 容器层 `dirty` 契约 + `.save-bar`、`ProfileView` 昵称吸底条 + 路由守卫拦截、`UpgradeView` 历史/地理库危险区、AI 敏感开关进 `.danger-zone`（批次 15）；**PS-25 `UpgradeView`** 39 处硬编码令牌化 + 危险区 `.hist-danger`/`.geo-danger`）
- [ ] 全站四态齐备：空态 `EmptyState` / 加载骨架 / 错误带重试 / 部分失败 `PartialBar`（**规范件已备**：`SectionCard.loading/error`、`EmptyState`、`PartialBar`，见 §1.3.1；**批次 11–17 各页已接入**（告警/通知/资产/列表/图表/设置/安全/首页/审批/命令面板），页面外壳与 `screen/**` 除外）
- [x] 无限动画有 `prefers-reduced-motion` 降级（批次 8，2026-10-09）：全局媒体查询 + 显式停 `.sk`/`.sk-pulse`；实测 `sk-pulse / 1.4s / infinite` → **`none / 0s / 1`**
- [x] 全站主交互元素键盘可达（**批次 11–17 落地，2026-10-09**）：`FilterChips` × 支持 Enter 撤销（11）；`SettingsView` 导航 `role="tab"` roving tabindex（15）；`RolesView` 权限矩阵改 `el-checkbox-group`（15）；`LogsView` 日志行改可聚焦 `<button>`（13）；**`CommandPalette` 全量语义化 + 焦点陷阱 + Home/End/PgUp/PgDn（17）**
- [x] 表格：行高 38px、正文 14px（`--fs-base`）、数值列右对齐等宽、1440px 下无横向滚动（抽查 HostsView/AuditView/OpsView）（**批次 8 行高 34.5→38px + 批次 13/14 列宽收敛**：`/hosts`/`/logs`/`/audit`/`/dialtest` 等实测溢出 **0px**、表头零被裁；`OpsView` 属 §1.5.6 挂账清单，不在抽查范围）
- [x] **列表页**（批次 13，2026-10-09）：**PS-04 `HostsView`** 页头「批量升级」移入 `SectionCard` 内的 `BatchBar`（选中才出现，`clearSelection()` 随操作收尾）、次级导航改 `role="tablist"` + `<button role="tab">`、安装信息抽屉补**错误态 + 骨架 + `loadInstallInfo()` 重试**、代理表加「共 N 个」+ 排序列 + `row-key`、行高亮去 `!important`、抽屉加**锚点导航**（`proxyAnchors` + `scrollToSection`）、**命令块默认折叠**（网闸代理 6 段改 `<details>/<summary>`，零 JS + 原生可访问；直连场景「安装命令」与手动证书块保持展开）。**PS-05 `NodeView`** 7 处 `.section-title` 由 `div` 改 `<h3>`、页头加**自动/暂停 `el-switch`**、`loadPanel` 并行化 + `loadErrors` 扩展 `topProc`/`alerts`、WS 首帧前用 `.rt-block.is-pending` + `.rt-skel` 骨架占位、新增 `detachWS()`/`resumeLive()` + `visibilitychange` 暂停（`nodeTimer`/`alertTimer` 加 `pageVisible && autoRefresh` 守卫）、环形图阈值 80/60 → **70/90**（对齐全站 `rateClass`）并在 `gaugeOption` 画三档色带 + 刻度、清死代码 `.status-pill` / 重复 `.port-item`、`--el-color-primary` 残留改 `--accent`。**PS-20 `LogsView`** 6 个筛选维度统一 `FilterChips`（撤销即重查）、关键词/正则**命中高亮**（`<mark>`，先切分原文再逐段转义，`MAX_HL=300` 防回溯）+ 行内命中计数、日志行改可聚焦 `<button>` + `aria-expanded`、加多选列 + 「复制选中 N 行」+ **导出 CSV**（BOM + 7 列）。**PS-23 `AuditView`** 新增 **result / remoteIP / method 三个筛选**（配套后端 `audit.QueryFilter` + `auditWhere` + `queryMemory` + `auditFilter` 扩展，Go 侧 `TestQueryFilterByResultRemoteIPAndMethod` 覆盖 SQL 与内存两路）、筛选回显统一 `FilterChips`、分类/结果 `el-tag` → `StatusPill`（结果带图标第二编码）、`.card` → `SectionCard`（`dense`）、**「状态码 / 结果 / 资产改动」三列右侧固定**、`changeSummary` 由「单元格内 6 次调用 × 3 正则」改为**加载时按行预计算**（`decorate()`）+ `row-key`、完整性校验补**时间戳 + 重新校验入口 + 失败落进结果卡**、3 处 hex 回退（`#d9534f`/`#4a9df0`/`rgba(255,255,255,0.08)`）改令牌、修死断点并补 `≤900px` 响应式。**实测发现并修复 2 处横向溢出**：`/logs` 列合计 1180px（加多选列 40px 后）→ 日志列 `min-width` 420→380 归零；`/audit` 列合计 1150px 而 `SectionCard` 内容区 **1134px**（比旧 `.card` 少 8px）→ 操作/变更摘要 190→180 + 卡片 `dense` 归零。**验证**：串行 vitest（`--pool=forks --poolOptions.forks.singleFork=true`）→ **39 files / 325 tests 全绿**（批次 13 新增 AuditView 5 例 + LogsView 4 例）；`vite build --emptyOutDir=false` 通过；`go test ./internal/server/audit/` 通过。**1440×900 浏览器实测**（`.workbuddy/tmp/b13-lists.mjs`）：四页横向溢出 **0px**、表头零被裁；`/hosts` 网闸代理抽屉 `details.cmd-box=6 / 默认展开 0 / 锚点 6`；`/node/host-01` 7 个 `h3` 语义标题 + 自动暂停开关 + `.rt-block` 骨架；`/logs` `<mark>=3`、行内计数 `[2,1]`、撤销关键词 chip 后重查不含 `q=`；`/audit` `StatusPill=8` / 残留 `el-tag=0`、右侧固定列 **15 个 sticky 单元格**（EP 2.14 用 `.el-table-fixed-column--right`，不再有 `.el-table__fixed-right` 层）、选「只看失败」后查询串带 `result=fail` 且 chip 回显「结果：只看失败」、完整性校验卡带时间戳 + 重新校验入口
- [x] **图表工具页**（批次 14，2026-10-09）：**PS-22 `DashboardView`** 修 **B6**（`addPanel()` 非编辑态先 `enterEdit()`，原 `draft.value.panels` 空指针崩溃）+ **B7**（`PanelChart` 改 `monitorOption({chartType: props.panel.chartType})`，原恒 `type:'line'`）+ 页头 `PageHeader` + 面板稳定 id（`ensureIds`→`_id`，`:key` 去 `i`）+ **拖拽排序** + **半宽/全宽档位**（`.span-full`）+ 首屏骨架 + 错误重试 + 看板改名 + 删除面板二次确认 + 当前看板记忆（`localStorage`）+ `PanelChart` 精确 watch + 导出 step 复用 `rangeBounds` + 三态 + `--chart-*` 配色轮转；后端 `dashboard.Panel.Size`。**PS-21 `MetricsExploreView`** 补 `PageHeader` + 图表三态（`chartLoading/chartError/chartEmpty`）+ 树节点 `StatusPill` + `.chart` 进 `SectionCard` + 图例开关 + 「加入仪表盘」选择弹窗（复用 `useDashboards`）+ `ResizeObserver` 收敛。**PS-18 `DialTestView`** 任务表加「当前状态」列（`StatusPill` + 延迟 + 可用率）+ 结果表「检测时间」+ 延迟 sparkline（新建 `Sparkline.vue`）+ 状态统一 `StatusPill` + 加载失败错误条 + 开关失败提示 + 搜索/类型筛选 + 结果分页；后端 `dialtest/latest` 补 `checkedAt`/`uptime`/`latencyHistory`。**PS-19 `ReportView`** 预览改右侧抽屉 + iframe 加载/失败兜底 + 历史搜索/分页/删除（后端新增 `DELETE /api/v1/report/history/{id}`）+ 类型列 `StatusPill`。**验证**：串行 vitest（`--pool=forks --poolOptions.forks.singleFork=true`）→ **41 files / 339 tests 全绿**（批次 13 基线 39/325，本批新增 `DashboardView.test.js` 5 例 + `DialTestView.test.js` 3 例 + `MetricsExploreView` 2 例 + `ReportView` 4 例）；`vite build --emptyOutDir=false` 通过；`go build ./internal/...` EXIT=0。**1440×900 浏览器实测**（`.workbuddy/tmp/b14-charts.mjs`）：四页横向溢出 **0px**、表头零被裁（`/dialtest` 两表原 68px 溢出 → 列宽收敛归零）、PS-22 非编辑态点「添加面板」弹窗出现（B6）、gauge/line 双 canvas（B7）、编辑态 2 尺寸按钮 + 2 可拖拽面板 + 1 全宽面板、PS-21 树节点 2 `StatusPill` + 图例开关 1、PS-18 当前状态列文案正确 + sparkline 2 + 结果分页 1、PS-19 历史 10 行 + 搜索「2026-10-02」→ 1 行 + 预览抽屉 iframe src 含 `/report/download` + 打印按钮；控制台仅 mock 环境 WS 握手失败。
- [x] **设置域**（批次 15，2026-10-09）：**PS-26 `SettingsView`** 容器层导航项由纯 `div` 改 `<button role="tab">` + `aria-selected` + `:focus-visible`（roving tabindex：选中 `0`、其余 `-1`），切换分类 `router.replace({ query: { tab } })` 写回 URL，并落地**子页 dirty 契约**（子页 `defineExpose({ dirty, save, reset })`，容器层渲染 `.save-bar` 吸底「有未保存的改动」+ 放弃改动/保存）；5 个子页逐页处置：**站点与品牌**「恢复默认」二次确认 + FileReader `onerror` + `--bg-soft` 令牌化；**数据保留**「立即清理」二次确认 + `loading` + 描述区首屏 loading + 「0 = 不清理」语义就地写清；**AI 助手**「测试连接」文案明示"测的是服务端已保存配置" + 改字段清 `lastTest` + 敏感开关进 `.danger-zone` + API Key `autocomplete`；**系统自监控** `setOption(notMerge)` 增量更新 + 轮询拆"快照 10s/图表 60s" + keep-alive/页面隐藏暂停 + 探针与快照错误拆两条 alert + 16 项指标六组分组；**告警管道** 只读 3 节点可视化（节点显示规则条数 + 箭头 + 点击滚动定位）+ 区块 ①②③ 序号 + `yamlStale` 提示 + 「应用到表单」确认 + 删除行确认 + `op=replace` 必填 pattern/模板非空/正则编译校验 + relabel 紧凑/展开两档 + `.section-title` 改 `<h3>`。**PS-25 `UpgradeView`** 全量重写：显式**三步指示**（`applyPhase` 状态机 `submitted/restarting/verifying/done/failed/timeout` + `applyKind`，把原隐藏状态机暴露给用户）+ `.upload-error` 上传失败就地（文件名 + 原因 + 重试）+ 5 处 `catch{}` 补独立错误 ref（`versionErr`/`pendingErr`/`historyErr`/`archiveErr`/`geoipLoadErr`）+ 页面级 `.page-error`（`role="alert"`）+ 部分失败 `.page-warn` + `.anchor-nav` 锚点导航 + 「IP 地理库」默认折叠 + 危险区 `.hist-danger`/`.geo-danger` + 「切换」→「回退到此版本」 + 39 处硬编码令牌化（写死主题绿 `rgba(0,200,150,0.3)` → `var(--bd-strong)`）。**PS-27 `RolesView`** 修 **B1**（后端 `internal/server/auth/model.go` 的 `permDomain` 补 json tag `domain`/`items`，原无 tag 致前端读 `d.domain`/`d.items` 恒 undefined、矩阵整块不可用）+ 新增 `TestPermissionCatalogJSONKeysAreLowercase` 序列化断言 + `RolesView.test.js` mock 由 `{domains:[]}` 改非空（空数组掩盖 B1）+ 权限矩阵换 `el-checkbox-group`（原生 input 键盘可达）+ `.perm-toolbar`（搜索 + 仅看高风险 + 计数）+ `RISK_KEYS` 10 个逐个权限点 `.risk-badge` + 域内全选/清空 + 折叠默认收起 + 新增**「影响用户数」列** + `el-card.glass` → `SectionCard` + `DOMAIN_LABELS` 中文域直返 + `ROLE_LABELS` 三处合一到 `composables/useRoleLabels.js`。**PS-27 `UsersView`** 加搜索（用户名/昵称/角色）+ 分页 + 「数据范围」拆两列（节点/业务）+ 操作列收敛为 编辑/重置密码 + 「更多」下拉 + 删除带用户名 + `EmptyState` + `roleLabel` 走 composable + 密码 `autocomplete="new-password"`。**PS-28 `ProfileView`** 删 20 行死样式 + `el-tabs` 改 `tab-position="left"` + 只读信息改 `el-descriptions` + `savedNickname`/`dirty` + `.save-bar` 吸底 + `onBeforeRouteLeave` 脏改动拦截 + `loadingPrincipal` 骨架；**`ChangePasswordSubView`** 校验对齐后端（`classCount` ≥2 类字符 + ≥8 位）+ 强度条 + 后端错误就地回填到「原密码」字段。**验证**：串行 vitest（`--pool=forks --poolOptions.forks.singleFork=true`）→ **44 files / 354 tests 全绿**（批次 14 基线 41/339，本批新增 `SettingsView.test.js` 3 例 + `ProfileView.test.js` 4 例 + `ChangePasswordSubView.test.js` 5 例 + `RolesView` 权限矩阵 3 例 + `UpgradeView` 按钮改名）；`vite build --emptyOutDir=false` **✓ built in 15.03s**；`go build ./internal/...` EXIT=0、`go test ./internal/server/auth/` ok（含新 B1 测试）。**1440×900 浏览器实测**（`.workbuddy/tmp/b15-settings.mjs`）：5 页（PS-26 五子页 / PS-25 / PS-27 users+roles / PS-28）横向溢出 **0px**、表头零被裁、零 JS 报错（仅 mock 环境 WS 噪声）；关键交互（吸底条出现/消失、URL hash 同步、三步指示容器、锚点 4 项、危险区 2 处、地理库默认折叠、权限矩阵原生 checkbox 14 + 高风险标记 7 + 搜索 + 「仅看高风险」过滤、影响用户数列、`el-descriptions`、tabs 方向 left、昵称吸底条）全部生效。
- [x] **安全与首页**（批次 16，2026-10-09）：**PS-24 `SecurityView`** 状态缺陷补齐 + 四套自绘状态收敛为 `StatusPill` + 修复建议分档；**PS-03 `OverviewView`** 首页状态完整化 + 智能运维区块**可折叠**（折叠 ≠ 隐藏，不触碰 `PINNED_KEYS`）+ 两告警区块**去重**（`recentAlerts` 过滤掉 critical 已出现条目，区块被隐藏时自动还原）；**PS-13 `ApprovalsView`** 状态与动作收敛。**验证**：串行 vitest **46 files / 392 tests 全绿**；`vite build --emptyOutDir=false` 通过；1440×900 浏览器实测三页无 JS 报错（仅 mock WS 握手失败）
- [x] **全局收口**（批次 17，2026-10-09，见 §30.4）：**PS-29 `CommandPalette`** 从「导航器」升级为「命令台」（三类数据源 + 全量语义化 + 焦点陷阱/还原 + 模糊打分 + 最近访问 + 主机组 loading/可重试/上限 + Home/End/PgUp/PgDn）；**PS-29 `SiteFooter`** 删死代码 + 加版本号 + 令牌化；**断点收编**为 1280/1024/768 三档（`screen/**` 独立）；**旧别名令牌全站清零**；**打印层增强**；**HostsView 窄屏列降级**；顺带修 **`/screen` 大屏 TDZ 白屏**（既有缺陷）。**验证**：串行 vitest **48 files / 401 tests 全绿**（新增 `CommandPalette.test.js` 6 例 + `SiteFooter.test.js` 3 例）；`vite build --emptyOutDir=false` 通过（`✓ built in 16.24s`）；1440/1280/1024 浏览器实测全通过，`/screen` 恢复渲染无 TDZ 报错
- [x] **UI 反馈批量修复**（批次 18，2026-10-09，用户 9 张截图逐条）：**拨测 `DialTestView`**「当前状态」拆为 `当前状态`(102)/`延迟(ms)`(80)/`可用率`(78) 三列（原状态列把延迟+可用率挤在 `StatusPill` 后的 `.cur-meta` 里）、`uptimeText()` → `uptimePct()`、`对外` 68→92（放不下「已发布」药丸）、`任务名称`/`目标` 改 `min-width` + `show-overflow-tooltip`；**延迟趋势修复根因**：`Sparkline` 复用带坐标轴的 `areaOption`（`grid.left:42` + y 轴刻度）在 28px 高度里压成细缝 → 新建无轴无网格的 `sparkOption(color, unit)`（`echarts.js`），`normalize()` 改取数值走 category x 轴。**资产台账 `AssetTable`** `上报状态` 100→116、`归属节点` 120→150 + `show-overflow-tooltip`。**操作列换行修复（#3/#9 共性根因）**：`.el-button + .el-button { margin-left:12px }` 相邻选择器对 `el-dropdown`（`display:inline-flex`）不生效 → 三件套被挤到第二行；统一给操作列加 `class-name="op-col"` + `:deep(.op-col .cell){display:flex;justify-content:flex-end;gap:4px;white-space:nowrap}` + `:deep(.op-col .cell .el-button + .el-button){margin-left:0}`，覆盖 `AssetTable`/`DialTestView`/`UpgradeView`/`UsersView`。**安全中心 `SecurityView`** 删顶部锚点导航（`<nav class="anchor-nav">` + `anchors`/`scrollToId`）、顶部空白 16→14 + `:deep(.refresh-bar){margin-bottom:0}`（原 `RefreshBar` 自带 16px 下边距叠加成 30px）、基线合规明细加 `max-height:560px;overflow-y:auto` 滚动 + `el-input.bl-search`（180px）按 `displayName`/`node`/`nodeIp` 检索 + 空态按 `baselineKw` 分支。**审计 `AuditView`** `结果` 76→94（`已成功`药丸 + 图标需 ~94px，原 76px 静默裁出尾部"点"）+ `分类`/`操作者`/`资产改动` 收敛消 6px 溢出。**系统自监控 `SelfMonitorSubView`** 趋势 `step=30` → `step=60000`（后端 `query.go` 判 `step<1000` 直接 400）+ 整体重排为 `el-card.settings-card`（`.probe-grid` 两探针卡 + `.metric-grid` 六指标卡替换 6 组整宽 `el-descriptions`）。**品牌提示迁移**：`SettingsView` 左栏下方 `.sn-tip` 删除 → `BrandSettingsSubView` 卡片 header 下常驻 `el-alert`。**个人中心 `ProfileView`** 还原 `HEAD` 原样（单 `el-card.glass.info-card` + `el-form` + 「保存昵称」按钮，删 `el-descriptions`/`.save-bar`/`dirty`/`onBeforeRouteLeave`）。**系统升级 `UpgradeView`** 删锚点导航、版本列 150→188 且**版本号前置 / `当前运行` tag 后置**（原 tag 把版本号挤没）、操作列 180→210 + `op-col`。**用户管理 `UsersView`** 删「更多」`el-dropdown` → 编辑/重置密码/禁用/删除 4 按钮平铺进操作列（`min-width` 190→252 + `op-col`）。**验证**：串行 vitest（`--pool=forks --poolOptions.forks.singleFork=true`）→ **48 files / 400 tests 全绿**（`DialTestView.test.js` mock 改 `sparkOption` + 首例改三列断言；`UpgradeView.test.js` 断言改 `v1.27.1`/`当前运行`；`ProfileView.test.js` 重写 3 例）；`vite build --emptyOutDir=false` 通过（`✓ built in 16.21s`）；**1440×900 浏览器实测 8 路由**（`.workbuddy/tmp/b18-measure.mjs`）：`/dialtest`（两表溢出 0）、`/assets`（表宽 1126 溢出 0）、`/audit`、`/upgrade`、`/users`、`/security`、`/selfmon`、`/profile` 全部**零横向溢出 / 零表头裁切 / 操作列不换行 / StatusPill 不裁切**（首轮实测发现拨测药丸裁切 + 审计 6px 溢出，修正后复测通过）
- [x] **HostsView 10 列全部默认展示**（`selectedCols` 不改默认值）；1440px 窗口实测无横向滚动；`≤1280px` 断点只收起 `version`/`group`，磁盘读写/负载/流量**任何宽度下都保留**（**批次 13 列宽重排 + 批次 17 窄屏降级，2026-10-09**）：`AUTO_HIDE_NARROW=['version','group']` 仅窄屏 `colVisible()` 返回 `false`，**不写 `selectedCols`**；`window.matchMedia('(max-width:1280px)')` 监听 + 表格上方 `el-alert.narrow-cols-tip` 说明条。**实测**（`.workbuddy/tmp/b17-global.mjs`）：`/hosts` @1440 表头 11 列全含版本/分组 + 溢出 **0px**；@1280 表头收为 9 列（无版本/分组）+ 说明条出现 + 溢出 **0px**；@1024 同 1280（溢出 224px 为既有 D8 问题，原生滚动可用）
- [x] **字号**（批次 8，2026-10-09）：全站 `font-size` 无 `<12px`、无 `.5px` 小数；表格正文 14px；EP base 与根字号均 14px；实测 10 个页面**最小渲染字号 = 12px、`<12px` 元素数 = 0**（OpsView / IntelligenceView / IntelligenceBlock / TriageBar 均已清零）
- [x] **筛选区**（批次 8，2026-10-09）：8 页筛选控件 `size="small"`（**实测渲染 24px**，非规格原写的 32px，见 §1.5.7）；容器统一 `.filter-bar`（带边框，8 页形态实测完全一致）；宽度走 `--w-xs/s/m/l/xl` 5 档；实测 LogsView 时间选择器 = **320px**、AssetListView 关键词 = **240px**、RefreshBar 刷新间隔 = 96px；15 路由页面级零横向滚动（`OpsView`/`ReportView` 不在 §1.5.6 清单内，挂账）
- [x] **描述段**（批次 8，2026-10-09）：`.muted` 在 `style.css` 仅一处定义（渲染实测计算色 `rgb(111,131,166)`=`--t3`、13px）；15 处页内覆写全部删除（保留 4 处上下文选择器）；`--desc-tight/base/block` 三档令牌生效；`AuditView.verify-note` 距离由 6px 改为 10px
- [x] **资产域**（批次 12，2026-10-09）：**PS-11 `InspectView`** 差异比对升级为**真正的 diff**（期望值 `.struck` 划删 → 实际值 `.diff-new` 高亮，含 `新增/变更/缺失/合规偏差` 四类视觉编码；`InspectView.vue:278-294`）+ 差异块**常驻**（未选中记录时显示引导空态，不再 `v-if="selectedRun"` 整块消失）+ 修 B8 游离 `>` + 执行区 `.card/.panel` 换 `SectionCard`（消双层边框）+ 筛选条按 §1.5 收敛 + 「设为标杆」给跳转入口 + 标杆表 `goAssets()`（`router?.push('/assets')`）与台账互跳。**PS-10 `AssetModelView`** 补**「新增字段」/「删除字段」入口**（表格操作列 + 顶部「+ 新建字段」，原「添加建议属性」降级为从模板新建）+ `.save-bar` **吸底** + `dirty`/`dirtyCount` **未保存改动计数** + 切类型**拦截**（`confirm` 防丢草稿）+ `EmptyState` + `v-loading` + 「运行态字段」顶部展示与「比对」列双向联动 + `.type-chip` 加 `aria-pressed`。**PS-09 `AssetTopologyView`**（补 §10 断档规格）**一页两段**：上段「关系总览」按「来源类型 × 关系种类 × 目标类型 × 边来源」聚合（`getLinkStats`）+ 点行把下图切到该筛选；下段「关系图」走 `TopologyGraph`（`getTopologyGraph`），图四件套（图例/缩放/全屏/导出）与截断提示都在组件内、三入口（台账详情/告警影响面/本页）共用；**两段各自 loading/error**（`Promise.allSettled` 刷新，一段挂了不牵连另一段）；筛选条 `.filter-bar.inline` 去双层边框、`.tag` 硬编码 `rgba` 改 `--accent-dim`、`.mono` 走 `--mono` 令牌；点节点 → 台账 `?id=` 深链。**PS-08 `AssetListView`**（先拆子组件再改视觉）**2217 → 1117 行**，拆为 8 个文件：`assetFormat.js`（展示口径单一来源）+ `AssetKpiRow`（5 张真按钮 + 1 张静态）+ `AssetFilterPanel`（默认只留类型/状态/关键词，「更多筛选」折叠 + `FilterChips` 摘要条含下钻可撤销）+ `AssetTable`（状态/来源统一 `StatusPill`、更多下拉只剩忽略/恢复）+ `AssetDetailDrawer`（三 Tab 各自 `SectionCard` 四态 + 危险区）+ `AssetAttrEditor`（其它属性按字典换控件）+ `AssetModals`（6 个受控弹窗）；「彻底删除」移出下拉进 `.danger-zone`；抽屉三 Tab 独立取数（`loadAttr`/`loadHistory`/`loadLinks`）替代 `Promise.all` 整体失败，`afterWrite` 用 `allSettled`；KPI 下钻改语义 `<button>`（焦点环）。**验证**：串行 vitest（`--pool=forks --poolOptions.forks.singleFork=true`）→ **39 files / 315 tests 全绿**（基线 37/261，批次 12 新增 `asset-subcomponents.test.js` 23 例 + `AssetTopologyView.test.js`/`InspectView.test.js`/`AssetModelView.test.js` 扩展）；`vite build --emptyOutDir=false` 通过（`AssetListView-Ct7O3XqK.js 59.28 kB`、`AssetModelView-CjhPXJ63.js 19.87 kB`）。**测试环境修复**：子组件测试 `PassthroughStub` 需同时渲染默认槽 + `#footer` 槽（弹窗提交按钮在 footer），且 `el-table` 列注册需 `await flushPromises()`（新增 `mountReady` helper）否则同步断言拿到空文本。**1440×900 浏览器实测**（mock API，`.workbuddy/tmp/b12-assets.mjs` + `b12-topology.mjs`）：`/assets` 页面横向溢出 **0px**、表格溢出 0px 表头无被裁、KPI **5 张真按钮 + 1 张静态**（`.kpi-click`）、表格内 `StatusPill` **6 个**、摘要条初始 0 chip +「更多筛选」默认收起 → 点「失联」后 chip「状态：失联」1 条、撤销后归 0、详情抽屉宽 **780** + 三 Tab（属性对比 2/变更历史 0/关联关系 1）+ 危险区 1；`/assets/model` 零溢出、类型 chip 3 全带 `aria-pressed`、「+ 新建字段」入口在、`.save-bar` 存在且改一处后文案由「没有未保存的改动」→「有 1 处未保存的改动」；`/inspect` 零溢出、**5 个 `SectionCard`**（执行巡检/周期化巡检/巡检记录/差异项/期望值）、差异项**常驻空态**在、选中记录后真 diff（`.struck` 2 / `.diff-new` 2 / `.diff-arrow` 3 / `.diff-miss` 1）+ 标题条「记录 #11 · 时间 · 3 条 · 涉及 2 个资产」；`/assets/topology` 零溢出 + 图四件套可用（上一会话 `b12-topology.mjs` 实测）；三页控制台仅 mock 环境 WS 握手失败。
- [x] **告警域**（批次 11，2026-10-09）：**PS-14 `AlertsView`** 抽屉改「列表 + 详情」可对照形态（实测抽屉宽 560 且列表内容仍可见 1142px）、轮询刷新不打断已打开抽屉、评论草稿随关闭清理、动作分区 + 命名统一"认领"、整行可点（`@row-dblclick`）+ 状态三重编码（色 + 文案 + 图标）、三表 `v-loading`、`PartialBar` 部分失败、高级筛选区默认收起、硬编码令牌归整；顺带修规则表列宽 **68px 横向溢出**（名称 140→130、通知渠道 150→110 且表头改「通知」、应用范围 140→110 且表头改「范围」，固定 580 + 弹性 550 = 1130 ≤ 1142 预算）。**PS-15 `RuleModal`** 保存中态防重复提交、字段级校验（`validate()` 真拦，错误落 `.el-form-item__error`）、脏数据关闭确认（`before-close` + `isDirty`）、切类型清异类字段、分区标题同源 `--accent`、触发条件常驻摘要、`:xs/:sm/:md` 断点、未启用渠道「去通知配置启用」跳转、4 个原生 `label`→`span` + `aria-label`/`aria-labelledby` 语义修正。**PS-16 `NotifyView` 重写** 5 张 `el-card`→`SectionCard`（`#actions` 内 `StatusPill` + `el-switch`）、顶部健康度摘要条（`未启用 / N 个已启用 / 配置不完整` 三态）、加载失败页面级错误态 + 重试、5 条 toast 校验改为卡片内字段级 `:error`、保存拦下配置不完整渠道并 `scrollIntoView` 定位、5 个开关 `aria-label`、密钥字段"已保存值不回显、留空=沿用"说明。**环境级修复**：`vite.config.js` 加 `test.server.deps.inline: ['element-plus']` —— 修复 vitest 外置 EP 后 `import AsyncValidator from 'async-validator'` 拿到 CJS 命名空间、`new AsyncValidator(...)` 抛错被 EP `.catch` 吞掉 → **全站表单 `validate()` 恒 resolve(true)、所有 `:rules` 在测试中静默失效**（症状：界面能拦、测试拦不住）。**验证**：串行 vitest **37 files / 261 tests 全绿**（`--pool=forks --poolOptions.forks.singleFork=true`）；`vite build --emptyOutDir=false` 通过；**1440×900 浏览器实测** `/alerts` 两表横向溢出 **0px**、表头无被裁、抽屉宽 560 且列表仍可见 1142px、趋势请求 `step=60000`、无 JS 报错（仅 mock 环境 WS 握手失败）；`/notify` 5 卡片、药丸「已启用/未启用/配置不完整」、健康度「2 个渠道已启用 · 钉钉机器人 配置不完整，保存会被拦下」、字段级错误「已启用该渠道，请先填写机器人 Webhook」；新增 `RuleModal.test.js`（7 例）+ `NotifyView.test.js`（7 例）
- [x] **中间件逐 Tab 接入**（批次 10，2026-10-09）：**15 个 Tab 全部接入骨架件**（`DockerTab`/`LightCollectorTab` 批次 9 已完成，本批补齐 `Postgres`/`Nginx`/`Kafka`/`RocketMQ`/`Mongo`/`FastDFS`/`K8s`/`MySQL`/`Redis`）；统一 `.glass`→`SectionCard`（表格区 `dense` + `#actions` 槽）、空态→`EmptyState`（`icon` 传渲染函数）、自绘 `.dot`→`MwStatusDot`、抽屉→`MwDetailHeader`+`TrendChart`、每 Tab 新增 `error` ref + `el-alert type="error"`（带"重试"）。**关键回归修复**：`mwRange.rangeToStep` 秒→毫秒（原返 60/120/300 触发后端 `step<1000` 400，导致 9 个 Tab 抽屉趋势图恒显"加载失败"；改 5000/15000/60000/120000/300000，依据 `NodeView.vue:1162` 与 `querier.go:37`）。**验证**：串行 vitest **35 files / 247 tests 全绿**（`--pool=forks --poolOptions.forks.singleFork=true`）；`vite build --emptyOutDir=false` 通过；**1440×900 浏览器实测 RedisTab**：页面横向溢出 **0px**、区块标题 `实例拓扑 | 实例列表 | 性能排行 Top 10 | 缓存命中率`、`MwStatusDot` 14 个 / 遗留 `.dot` **0 个**、表格内容宽 1142/1142 **零溢出且表头无被裁**、抽屉打开触发 **8 条 `query/range` 且 step 全为 60000（≥1000）**、8 张趋势卡、控制台仅 mock 环境的 WS 握手失败；新增 `RedisTab.test.js` 4 测试（空态引导 + 拓扑聚合 + 抽屉 step 单位契约）兜住静默回归
- [x] **中间件骨架 · 骨架件 + 试点**（批次 9，2026-10-09）：`MwStatusDot` 三态（**实测 `normal=rgb(34,197,94)` / `warning=rgb(255,180,84)` / `abnormal=rgb(255,93,108)`**，离线/失败已由黄改红）+ `dotOnly` + 别名归一；新增 `TrendChart`（三态）/`MwHostCard`/`MwDetailHeader`/`mwRange.js` + `echarts.mwTrendOption`；`DockerTab`/`LightCollectorTab` 完成 `.glass`→`SectionCard`（**中间件内容区 `.glass` 实测 0 处**）、空态→`EmptyState`、抽屉→`MwDetailHeader`+`TrendChart`；**15 Tab 实测零横向溢出、零 JS 报错**；顺带修 B3/B9/B11（B11 实测：主机总数 2 / 在线主机 1，不再恒等）；其余 8 个手写 Tab 接入属批次 10+
- [x] 全站状态表达 100% 走 `StatusPill`（注：**中间件域**的状态表达走 `MwStatusDot`，二者定位不同——`StatusPill` 面向资产/告警等业务状态，`MwStatusDot` 面向中间件实例健康度；**资产域批次 12 已全量切换 `StatusPill`**；安全/首页/审批批次 16 已接入）
- [x] `--text-dim`/`--text-muted`/`--border`/`--bg-card` 旧别名引用数降到 0；`--sp-*`/`--fs-*` 在页面组件层落地（**批次 17 清零，2026-10-09**）：三套主题各一份旧别名**定义**（`--bg-elev`/`--bg-card`/`--border`/`--border-strong`/`--text`/`--text-main`/`--text-dim`/`--text-muted`/`--label`/`--radius`）与 `print.css` 死令牌（`--bg-primary`/`--bg-secondary`/`--panel-bg`/`--border-color`）全删；引用 `sed` 批量替换 + **6 处带 fallback 的漏网引用**（`var(--oldX, #hex)`）手工补齐（`NodeView` 3 / `StatusView` 2 / `screen/tabs/NetworkTab` 1）。**复核**：引用残留 `0`、定义残留 `0`。**决策**：`--chart-*`（cyan/blue/green/red/orange/yellow/purple）为 §1.1 合法图表色板族，**保留不清零**；`--sp-*`/`--fs-*` 已在 §1.4/§1.5 落地
- [ ] 无新增英文界面文案

---

## 33. 遗留问题与待确认项 · 处置建议

### 33.0 三条处理原则

1. **先分清"要后端新接口"还是"前端没接已有能力"** —— 12 项里有 **4 项属于后者**（#2、#4 的 label 部分、#5、#8），可以直接落地，不必等任何人确认。
2. **能靠前端自解决的，不开后端工单** —— PDF 导出、label 过滤、namespace 补全都属于这一类。后端接口的成本是前端的 3–5 倍（要改协议 + 权限 + 测试 + 发版）。
3. **每项建议都要能独立回滚** —— 被否决时不牵连其他批次，默认值写死在文档里，实施时照默认值走、被否决才改。

### 33.1 建议汇总（12 项）

| #  | 事项                             | 建议                                                        | 理由与证据                                                                                                                                                          | 未被否决时的默认动作                                                                                            | 归口          |
| -- | ------------------------------ | --------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------- | ----------- |
| ~~1~~ | ~~`/status` 是否改 history 路由~~ | **已决定：不做**（2026-10-09 确认） | 换 `createWebHashHistory` 会让所有外部书签与已分享深链失效，收益（链接少一个 `#`）不抵风险 | **从待办移除**。维持 hash 路由 `#/status`，服务端 rewrite 方案一并搁置，不进任何批次 | 已关闭 |
| 2 ✅ | 多渠道连通性测试                       | **已实施**（2026-10-09） | `internal/server/api/test.go:22-34` `handleAlertTest` 已支持 `?channel=`，`engine.TestAlert(channel)` 空则全渠道；`notify/test`（`test.go:8-18`）只调 `TestEmail()`          | 立即落地。⚠️ 副作用：该接口会**写入一条测试告警事件并广播到所有 WS 客户端**（注释明写）→ 按钮文案必须写明"会产生一条测试事件"，否则用户以为配错了                      | 前端          |
| 3 ✅ | 报告 PDF 导出                      | **能力早已存在**（核查后更正），本轮只补**文案**                                  | 上一版写"需补 PDF 导出"是**判断错误**：`ReportView.vue:287` `printReport()` 已实现（新窗口打开报告 HTML 再 `w.print()`），后端 `internal/server/report/template.html:60-66` 已带 `@page{size:A4 portrait;margin:14mm 12mm}` 与 `@media print` + `print-color-adjust:exact`。真正的问题只是按钮写"打印"，用户不知道它等于"存 PDF"                                                              | 已落地：按钮改「打印 / 存为 PDF」+ title 说明 A4 打印样式。**不需要任何新代码**                                     | 前端（已完成） |
| 3b | 报告时间/节点范围参数                    | **已确认：本轮不做**（2026-10-09）                                 | `handleReportGenerate` 的 scope 是按**生成者权限折算**的（`middleware_api.go:1526-1533` 注释："报告是下载物，不能绕过各域接口的权限校验"）→ 加"节点范围"参数会让用户可能选到无权查看的节点，**与权限模型冲突**；时间范围则等于要存历史快照，成本高 | 页面显式写清产品语义："报告是当前状态快照"。若产品坚持要时间范围，需后端先给出权限折算方案                                                        | 后端 + 产品     |
| 4 ✅ | 指标 label 过滤                    | **已实施**（2026-10-09） | `parseLabelQuery`（`query.go:1230-1235`）已支持 `?labels.<name>=<value>`，`handleQueryRange` L1104/1113 已在用——**是前端没接，不是后端没有**                                        | 前端接 `labels.` 参数 + 4 档时间范围（`rangeBounds` 已支持 4 档，只需接上）                                                | 前端          |
| 4b | 指标聚合（by/函数）                    | **砍掉**                                                    | 通用聚合**不存在**（`mwAggregateLatest` 只有 sum/avg/max 且仅用于中间件最新值，`middleware_api.go:781-782`）；指标浏览是低频页，造一套查询语言的性价比弱                                                   | 从 PS-21 移除，不再列为待办                                                                                     | 前端          |
| 5 ✅ | 资产模型"声明但无资产携带"的字段              | **已实施**（2026-10-09）                                        | `attrRows`（`AssetModelView.vue:280-294`）已把 draft 里未出现在 `current.attrs` 的 key 追加显示 → 前端已支持草稿态；接口存在（`api/asset.js:156/158`）                                      | 已落地三层标注：① 字段列「新增」角标（区分 `fresh` 本次新增 vs `uncarried` 声明了没数据）② 覆盖度列对 0 台携带的行显示「未携带」而非空进度条（空条会被读成"采集坏了"）③ 表下 `el-alert` 列出全部未携带字段 + 保存前 `confirm` 明示"只改模型定义、不会给任何资产赋值" | 前端（已完成） |
| ~~6~~ | ~~智能运维是否放开 PINNED~~ | **已决定：本轮不做，维持现状**（2026-10-09 确认） | 用户选择保留"AI 区块必须被看见"的定位 | **维持 `PINNED_KEYS` 现状**（`useOverviewLayout.js:17/36-47/82/95` 一律不动）。仅保留 PS-03 展示改进中的**可折叠**（折叠 ≠ 隐藏，不触碰 PINNED 逻辑）；`load()` 覆盖 localStorage 的行为本轮也不改 | 已定 |
| 7 ⏳ | 首页两个告警区块                       | **去重已实施（2026-10-09）；合并仍待产品**                     | `criticalAlerts`（span 4）与 `recentAlerts`（span 12）时间基准不同、滚动位置不同；critical 本质上是 recent 的子集 → 内容重叠                                                                 | 第一步已落地：`recentAlerts` 过滤掉已在 critical 出现的条目（**区块被用户隐藏时自动还原完整列表**，避免静默丢信息），空状态由「暂无告警记录」改为「最近 N 条已在上方紧急告警中显示」+ 列表底部提示去重条数。第二步（合并为 span 12 单区块 + 内部 Tab）仍待拍板 | 产品          |
| 8 ✅ | `ContainerView` namespace 列表接口 | **已实施**（2026-10-09）：不做下拉，改手输 + 自动补全 | 后端只有 `/api/v1/container/k8s/clusters`（`query.go:389`），**无 namespace 接口**；当前是手输 `.ns-input`（`ContainerView.vue:55-59`） | 已按"手输 + `el-autocomplete`"落地：建议源 = 查询结果里 namespace 列去重 + localStorage 历史（key `nebula-container-ns-history`，上限 12），零后端改动，拿到 70% 收益 | 前端（已完成） |
| 9 ✅ | 数据大屏是否受 EP base 变更影响           | **已核查：影响面比预想小得多（2026-10-09）**                                  | 核查更正：`screen/` 目录 **`el-table` 用量 = 0**（`ScreenView.vue` 无 el-table、`tabs/NetworkTab.vue` 只用 `el-select`/`el-option`）。全目录仅 `ScreenView.vue` 有 **2 个 `el-button`**（设置对话框）。此前"ScreenView 与 NetworkTab 用了 el-table"的说法不成立。另：大屏自身字号在 `@media (min-width:2400px/3440px)` 放大，与 EP base 不同源 | EP base 16→14 对大屏的影响 = 2 个对话框按钮 + 少量下拉变小 2px，**组件是变小不是变大，无溢出风险**。实测 `/screen` 在 1440px 下 `scrollWidth == innerWidth`。**无需回归清单** | 前端（已核查） |
| 10 ✅ | B10（NodeView 请求数）         | **已实测并已修复**（2026-10-09）                                 | 实测（Chrome headless + mock API，1440×900）：首屏 10 条、切「基础监控」Tab **96 条**（12 个 metric 各 8 次）、全 5 Tab 合计 **111 条** → **结论成立且比预估严重**。根因不是"缺批量接口"，而是 `setMonitorRef` 的 ref 回调每个面板挂载都触发一次**全量** `loadMonitor` | 已修：ref 回调改为 `if (!existed) loadMonitor(name, key)`——只补刷本次新建的图。复测：96 → **12 条**（每 metric 1 次，**零重复参数**），全 Tab 合计 111 → **27 条** | 前端（已完成） |
| 11 ✅ | 字号 13→14px 后表格溢出               | **已实测：字号不是溢出原因，溢出另有归属**（2026-10-09）                | 1440px 下 4 个页面表格**当前就已横向溢出**：hosts 112px、assets 136px、ops 222px、audit 234px。注入「EP base 16→14 + 根字号 16→14 + 全站 13px→14px、小于 12px→12px」（实测改到 100+ 处声明）后，**四个页面溢出量一字未变**（112/136/222/234），仅行高 57→59px（+3.5%）。原因：el-table 是 `table-layout: fixed`，列宽由 `width/min-width` 决定，与字号无关 | **溢出归 D8 列宽重排 + ≤1280 断点降级**，❌ 不得回滚字号——这是 §33.2 兜底顺序里唯一被实测否掉的一项。字号上调的真实代价是**纵向**（行高 +2px），批次 8 落地时只需复核列表一屏可见行数。**批次 8 已落地并复核（2026-10-09）**：4 页横向溢出仍为 **0**（字号对 `table-layout: fixed` 列宽零影响再次验证）、行高 34.5→38px、表头无裁切；新发现 `AlertsView` 规则表 68px 溢出属既有 D8 问题（详见 §1.4.7） | 前端（已实测） |
| 12 | EP base 变更是否影响大屏               | **已在 #9 回答**                                              | 同上：影响 `ScreenView.vue` / `NetworkTab.vue` 两处                                                                                                                   | 合并进 #9 的回归清单，不单独列项                                                                                    | —           |

### 33.2 实测项的验证方法（#10 / #11）

**B10 · NodeView 请求数**

1. 打开 `/node/<某台有数据的机器>`，切到 Tab2（面板页）
2. DevTools → Network → 过滤 `/api/v1/`，清空后刷新
3. 数请求条数：**≥ 20 条即结论成立**；若只有个位数，说明存在批量接口，B10 撤销
4. 成立时的改法优先级：① 面板数据合并为一个批量接口（最优）② 进入 tab 才请求（懒加载）③ 保持现状 + 加并发上限

**#11 · 字号上调后的表格溢出**

1. 批次 8 改完字号后，窗口设为 **1440px**
2. 逐个打开 `HostsView` / `AuditView` / `AssetListView` / `OpsView`
3. 控制台跑 `document.querySelector('.el-table__body-wrapper .el-scrollbar__wrap')?.scrollWidth > clientWidth` 判定是否横向溢出
4. **兜底顺序**（严格按序，不要跳步）：
   1. 走 `≤1280px` 断点降级（只收 `version`/`group` 这类**元数据**列）
   2. 列宽重排（D8）
   3. ❌ **绝不做**：回头收起磁盘读写 / 负载 / 流量等**指标类**列（见 §0.4 第 7 条）

### 33.2b 实测结果（2026-10-09 已执行）

**方法**：`vite dev`（127.0.0.1:5173）+ Chrome headless（playwright-core 驱动本机 Chrome，1440×900），用请求拦截把 `/api/v1/**` 全部替换为构造数据（`/auth/me` 回 503 → 前端视为未启用 RBAC，全权限放行），再逐 Tab 点击 / 逐路由打开并计数。脚本：`.workbuddy/tmp/ui-measure.mjs`（可复跑）。

**#10 · NodeView 请求数（结论：成立，且比预估严重）**

| 阶段                | 修复前  | 修复后  |
| ----------------- | ---- | ---- |
| 首屏（主机概览 Tab）      | 10 条 | 10 条 |
| 切「基础监控」Tab        | **96 条** | **12 条** |
| 进程监控 / 端口监控 / 防火墙 | 1 / 1 / 3 | 1 / 1 / 3 |
| **5 个 Tab 全走一遍合计** | **111 条** | **27 条** |

- 96 条的构成：`cpu_usage`、`mem_used_percent`、`load1/5/15`、`swap_used_percent`、`disk_used`、`disk_total`、`disk_read/write_rate`、`network_recv/sent_rate` 共 **12 个 metric 各 8 次**。
- 根因（已定位到行）：`NodeView.vue:630-644` 的 `setMonitorRef` 是 `v-for` 里**每个面板**的 ref 回调，回调内调用的是**不带 key 的全量** `loadMonitor(name)`；7 个面板挂载各触发一次全量 + `watch(activeTab)` 一次全量 = 8 次 × 12 metric = 96。
- 修复（一行语义）：回调改为只补刷**本次才建出来的图**——`const existed = !!monitorCharts[key]; ... if (!existed) loadMonitor(selected.value, key)`。
- 复测：每个 metric 恰好 1 次，**完全相同参数（metric+start+end+step）的重复请求 0 组**。

**#11 · 字号上调后的表格溢出（结论：字号不是原因，溢出是既有问题）**

1440×900 视口，注入「EP base 16→14 + 根字号 16→14 + 全站 13px→14px、\<12px→12px」（各页改到 26–115 处声明）：

| 页面                  | 基线溢出  | 上调后溢出  | 行高变化    | 页面请求数  |
| ------------------- | ----- | ----- | ------- | ----- |
| `HostsView`（#/hosts） | 112px | 112px | 57 → 59 | 9     |
| `AssetListView`     | 136px | 136px | —（无数据行） | 7     |
| `OpsView`           | 222px | 222px | —       | 7     |
| `AuditView`         | 234px | 234px | —       | 5     |

- **溢出量基线 = 上调后，一字未变**。`el-table` 为 `table-layout: fixed`，列宽由 `width/min-width` 决定，字号只影响单元格内高度与换行，不改变列宽 → 横向溢出与字号**无关**。
- 真实结论翻转：这四个页面在 1440px 下**本来就横向溢出 112–234px**，属于 **D8 列宽**问题（先前归因到"字号上调的副作用"是错的）。
- 副作用只有纵向：行高 57 → 59px（+3.5%）。批次 8 落地字号后，只需复核列表页一屏可见行数是否下降过多。
- 因此 §33.2 兜底顺序的第 3 条（"回头收起指标类列"）本来就禁止，第 1/2 条（断点降级、列宽重排）仍适用；**"回滚字号"不在有效选项里**。

**第二轮：列宽重排后复测（同日执行）**

按 §5.1 第 1 层把列宽重排铺到 4 个溢出页面后（详见 §5.1「实施记录」），同一脚本复测：

| 页面                  | 溢出（重排前） | 溢出（重排后） | 表头裁切 |
| ------------------- | ------- | ------- | ---- |
| `HostsView`         | 112px   | **0**   | 无    |
| `AssetListView`     | 136px   | **0**   | 无    |
| `OpsView`           | 222px   | **0**   | 无    |
| `AuditView`         | 234px   | **0**   | 无    |

- 诊断口径：除溢出总量外，还逐列检查 `th .cell` 的 `scrollWidth > clientWidth`（表头 `nowrap`，被裁不报错）。首轮重排后 `HostsView` 报出「Agent 版本 / 磁盘使用 / 负载」三列被裁 → 磁盘使用、负载回调原值，`Agent 版本` 改为短表头「版本」根治。
- 实测可用宽度 1142px（不是估算的 1168px）——差值来自滚动条与 EP 表格 cell padding，后续任何列宽预算都按 1142 算。
- 仍未做：`≤1280px` 断点降级（理由见 §5.1）。

### 33.3 需要真正拍板的只剩 1 项

14 行待办现状（2026-10-09 更新）：**1 项关闭**（#1 `/status` 路由）、**2 项定不做**（#3b 报告范围、#6 智能运维隐藏）、**1 项砍掉**（#4b 指标聚合）、**9 项已落地**（#2 #3 #4 #5 #8 + #7 的去重 + #10 实测并修复 + #11 实测 + #12 并入 #9）。真正还在等的只剩：

| 事项                | 状态 / 为什么绕不开                                            |
| ----------------- | ---------------------------------------------------- |
| ~~#3b 报告时间/节点范围~~ | **已定：本轮不做**。涉及权限模型，后端不给出折算方案，前端做了也是安全漏洞                |
| ~~#6 智能运维是否可隐藏~~  | **已定：维持不可隐藏**。产品定位取"AI 能力必须被看见"，`PINNED_KEYS` 不动 |
| **#7 两个告警区块是否合并** | **仍在等**（仅剩"合并"这一步）。首页信息架构的主干决策，影响 PS-03 的整页改造范围 |

**#7 未答复前的处理（已执行）**：去重部分**已落地**——`recentAlerts` 过滤掉已在 critical 出现的条目，且 critical 区块被隐藏时自动还原完整列表；空状态与列表底部都显式说明"N 条已在紧急告警中显示"，避免被读成"没有告警"。合并为 span 12 单区块（内部 Tab 紧急/最近/未恢复）的部分**继续暂缓**，不阻塞 PS-03 其余改造。
