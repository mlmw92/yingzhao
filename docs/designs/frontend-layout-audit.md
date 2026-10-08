# 前端布局与显示问题审查

- 审查日期：2026-10-08
- 审查范围：`web/src` 全部页面与组件（Vue3 + Element Plus，深色主题，CSS 变量令牌位于 `web/src/assets/style.css`）
- 审查方式：静态走查（模板 + 样式 + 令牌定义交叉核验），未实机渲染
- 结论分级：**严重**（功能语义丢失 / 内容不可见 / 页面不可用）、**中等**（观感割裂、窄屏错位、布局跳动）、**轻微**（规范不一致、死代码）
- 标注「疑似」的条目未做实机验证，需起服务后确认
- **修复进度**：第一批（失效变量 / 失效选择器 / 文案）、第二批（结构性）、第三批（响应式 / 加载态 / 交互）均已完成并通过 `npm run build` 与单元测试（28 文件 / 171 用例）；剩余项见第 11 节

---

## 0. 一句话结论

问题集中在四类，而不是零散的像素偏差：

1. **静默失效**：引用了根本不存在的 CSS 变量、或写错了选择器语法（`::deep`、后代选择器），浏览器丢弃整条规则却不报错 —— 表现是「样式没了 / 颜色不换肤」，最难自查。
2. **无响应式**：全站只有一处 `@media (max-width: 1100px)`，1100~1280 区间所有双列布局都在挤压；抽屉与弹窗宽度清一色写死 px。
3. **加载态缺失**：`style.css` 已定义 `.sk` 骨架类却几乎没被使用，多个页面先渲染错误空态再整块翻转。
4. **令牌被绕过**：硬编码色值散布各页，三套主题切换后不跟随。

---

## 1. 全站壳层（影响每一个页面）

| # | 级别 | 位置 | 问题 |
|---|---|---|---|
| A1 | 严重 | `layouts/MainLayout.vue:146`、`assets/style.css:63` | 没有任何窄屏收敛：侧栏 232px 常驻，全站样式只有一处 `@media(max-width:1100px)`（仅改 `.panel-grid`/`.manage-grid`）。1280 屏内容区只剩约 1008px，1024 屏约 752px；1100 以下才塌成单列，1100~1280 区间所有双列布局都在挤压 |
| A2 | 严重 | `components/Sidebar.vue:469` vs `:396` | 二级菜单选中光条偏移 12px。侧栏 `padding 12px`（:312）+ `.nav-group-items` `padding-left 24px`（:443）→ 二级项左缘在 36px，`-24px` 只回到 12px；一级项 `-12px` 落在 0px。代码注释（:468）写「取 -24px 才能回到侧栏内缘」，漏算了侧栏自身内边距，应为 `-36px`。两条光条不在同一竖线 |
| A3 | 严重 | `components/Sidebar.vue:330-333`、`AppTopBar.vue:162` | 侧栏品牌区下边框落在 y=68px（侧栏 padding-top 16 + 高 52），顶栏下边框落在 y=56px，两条同级分隔线撕开 12px 断口 |
| A4 | 中等 | `components/common/PageHeader.vue:23-49`、`SectionCard.vue:42-82` | 页头/卡片头都无 `flex-wrap`，且 `.ph-actions`/`.sec-actions` 是 `flex-shrink:0`。HostsView 塞 4 个按钮、NodeView 塞 4 个 KPI + 240px 下拉、AlertsView 塞约 640px 控件组 —— 窄屏必然横向溢出，标题区（`min-width:0`）反被压没。同项目 `.obj-head`（`style.css:857`）已写 `flex-wrap:wrap`，规范内部不一致 |
| A5 | 中等 | `components/common/SectionCard.vue:37-87` | `.sec-card` 无 `overflow:hidden`，`.sec-body` 有 `min-width:0` 但缺 `min-height:0`。卡片被限高时表格顶破底边与圆角 |
| A6 | 中等 | `assets/style.css:930-942` | `.save-bar` 是 `position:sticky` 却没给 `z-index`，后续有背景的兄弟元素会盖住它，阴影也被吃掉 |
| A7 | 中等 | `assets/style.css:216-230` | 滚动条只有 `::-webkit-scrollbar*`，缺 `scrollbar-width`/`scrollbar-color`；Firefox 下深色页面出现系统亮色滚动条 |
| A8 | 轻微 | `layouts/MainLayout.vue:153`、`Sidebar.vue:322` | 折叠宽 64px 在两个文件各硬编码一份，无令牌（对比 `--sidebar-w`），改一处即错位 |
| A9 | 轻微 | `layouts/MainLayout.vue:157`、`assets/style.css:277` | `padding: 18px 20px` 违反本文件 :174 自述的 4px 栅格（已有 `--sp-4:16`/`--sp-5:20`） |
| A10 | 严重 | `components/LoginView.vue:125-133,153-160` | 登录页 `position:fixed; inset:0` + flex 居中 + `overflow:hidden`，卡片 `width:380px` 写死。视口高度不足（约 600px 或缩放 150%）时上下双向溢出且滚不到；宽度 <412px 时横向被切。`.login-footer` 是 `position:absolute; bottom:20px`，会压在卡片上。同项目 `CommandPalette.vue:321` 的 `max-width: calc(100vw - 32px)` 才是对的写法 |
| A11 | 轻微 | `components/TopBar.vue` | 零样式死组件（无 `<style>` 块），全仓无引用，但类名与在用 `AppTopBar.vue` 冲突，误引用即产生裸布局 |

---

## 2. 静默失效：引用了不存在的 CSS 变量

已逐个核验定义数为 0。这类声明被浏览器丢弃却不报错，表现为「样式没了 / 颜色不换肤」。

| # | 级别 | 变量 | 使用处 | 后果 |
|---|---|---|---|---|
| B1 | 严重 | `--chart-yellow` | `screen/MiddlewareMonitorPanel.vue` 共 5 处（:234 类型卡色板、:267 健康分 70-89 档、:589 活跃告警、:1021 慢查询、:1091 哨兵徽章） | 令牌只定义了 cyan/blue/green/red/orange/purple/indigo，中间件大屏的整片警告色丢失，回退成继承色 |
| B2 | 中等 | `--bg-sunken`、`--font-mono` | `container/ContainerView.vue:1017,1019` | 日志块回退 4% 黑底，深色下几乎看不出底色；且唯独 `.log-pre` 没有 border，长日志边界消失 |
| B3 | 中等 | `--card` | `NotifyView.vue:252` | 卡片声明无效，吃 Element Plus 默认底 |
| B4 | 中等 | `--tag-bg`、`--tag-bg-2` | `LogsView.vue:812,824` | 永远回退硬编码的 EP 亮蓝/亮绿，换肤不联动 |
| B5 | 中等 | `--font-mono` | `AuditView.vue:585`、`rbac/RolesView.vue:510` | 项目令牌叫 `--mono`（JetBrains Mono 组），这里退回 Courier New，与全站等宽字体不一致 |
| B6 | 中等 | `--text-secondary`、`--bg-primary`、`--border-color`、`--text-primary`、`--card-bg` | `SiteFooter.vue:48,50,72,90`、`LoginView.vue:172` | 页脚钉死 `#8490a7`/`#e6ebf5` 等，三套主题下都不变；登录页 logo 背景退化为透明 |
| B7 | 中等 | `assets/style.css:997-1001`、`--el-color-error` 系列 | 全局 | `html.dark .el-tag--primary` 边框写死 `rgba(74,157,240,.32)`，同组 success/warning/danger/info 都走 `var(--*-bd)`，唯独它硬编码主题 b 的蓝 → 切青绿/紫主题出现「绿底+蓝边」。另 `--el-color-error` 完全未接管（:646-649），深色下保留浅粉底 |

---

## 3. 选择器写错导致整条规则失效

| # | 级别 | 位置 | 问题 |
|---|---|---|---|
| C1 | ~~严重~~ **已核验为误报** | 中间件各 Tab 的 `deep` 选择器 | 初查报告了「`::deep()` 写成双冒号」，这是**检索工具输出格式造成的误读**：输出形如 `1418::deep(.row-down)`，实际是「行号 1418 + 冒号 + `:deep(...)`」（单冒号，正确写法）。用 grep 复核：全仓 `::deep(` 命中 **0** 处、`:deep(` 命中 33 处、`::v-deep` 0 处。无需修改 |
| C2 | 严重 → **已修** | `SecurityView.vue:704-705` | `.row-critical :deep(.el-table__row)` 与 `.row-warning :deep(tr)` 是后代选择器，但 `row-class-name`（:539-541）的类名就加在 `<tr>` 自身，两条永远匹配不到 → 安全事件的紧急/警告行左侧色条完全不显示。且两处实现不一致（`box-shadow inset` vs `border-left`）。**修法**：改为 `:deep(tr.row-critical) td:first-child`，两档统一用 inset 阴影（表格是 separate 边框模型，tr 上的 box-shadow 不一定渲染） |
| C3 | 中等 → **已修** | `DialTestView.vue:101` | 模板写 `class="field-hint inline"`，样式里只有 `.hint-inline`（:240）→ 「对外发布」那句说明完全没有样式。**修法**：类名改为 `hint-inline` |

---

## 4. 首页概览

| # | 级别 | 位置 | 问题 |
|---|---|---|---|
| D1 | 严重 | `OverviewView.vue:28-30` + `overview/IntelligenceBlock.vue:2-13,57` | 外层已渲染 `<h3>智能运维</h3>`，IntelligenceBlock 内部又渲染 `DECISION SIGNALS` + `<h3>智能运维</h3>`，并自带 padding 20px/border/渐变嵌在 `.ov-card`（padding 16px）里 → 标题出现两次、双层边框、内边距与其余区块不统一 |
| D2 | 严重 | `OverviewView.vue:88,226-262`、`overview/MiddlewareOverview.vue:41-46` | `mwData` 初值 `{}` → 首帧所有中间件卡显示「尚未配置 / 点击前往…」，数据到达后整块翻转。同类：KPI 首帧为 0、HostOverview 先显示「暂无主机数据」。全流程无骨架屏（`style.css:771` 已有 `.sk` 类却没用） |
| D3 | 中等 | `overview/KpiBlock.vue:11-16,48-91,198-203` | `.kpi-spark`（36px）只在历史点 >1 时渲染，点存 localStorage 且按 `k.label` 为键 → 同一行有的卡有迷你图有的没有，两行行高不同；刷新攒够 2 点后卡片突然长高约 44px |
| D4 | 中等 | `overview/HostOverview.vue:80-83,170-176` | `shortName()` 取前 6 字符（注释写前 4），`.hc-label` 的 `max-width:90px` ellipsis 因此几乎永不触发 → 用户看到「web-se」这类被腰斩的主机名，且无 title 提示 |
| D5 | 中等 | `overview/MiddlewareOverview.vue:72-76` | `.mw-grid{repeat(4,1fr)}` 无断点，而 `OverviewView.vue:382` 在 ≤1100px 把该区块拉成 span 12 → 768px 下每卡约 155px，卡内 64px 甜甜圈 + `.mw-top-label{max-width:90px}` 直接挤爆。对比 `IntelligenceBlock.vue:58` 有 800px 断点 |
| D6 | 中等 | `OverviewView.vue:308-313,382-386`、`useOverviewLayout.js:8-13`、`KpiBlock.vue:136-141` | 1100~1400px 区间 span 4 仅约 300px，KpiBlock 内再 `repeat(2,1fr)` → 每卡约 138px，26px 的 `.kpi-v` 与 nowrap+ellipsis 的 `.kpi-f` 几乎必然把「在线 x · 离线 y」截断 |

---

## 5. 主机列表 / 主机详情 / 智能分析 / 指标浏览

| # | 级别 | 位置 | 问题 |
|---|---|---|---|
| E1 | 严重 | `NodeView.vue:177-202`、`:223-244` | 双重空态：进程表、告警表都是「`el-table` 常驻 + 下方 `EmptyState`」，而 `el-table` 没用 `#empty` 插槽覆盖 → 无数据时同时出现 EP 自带的「暂无数据」和下方 EmptyState，同一区域两个空提示。对照 `HostsView.vue:123-129` 用了 `#empty` 是正确的 |
| E2 | 严重 | `NodeView.vue:1263-1290`、`charts/echarts.js:362-364` | 图表永不 resize：`initChart` 无 autoresize，全文无 `window resize` 监听（只有切 Tab 时手动 `resize()`）。而 `.gauge-row`/`.metric-grid` 在 `NodeView.vue:1487-1493` 有 1100/640px 断点会从 4 列变 2/1 列 —— 容器宽度变了图不重绘，画布停在初始化宽度，被裁或留白。对比 `intelligence/BaselineTrendChart.vue:14` 是加了监听的 |
| E3 | 严重 | `intelligence/IntelligenceView.vue:16-17` | 同 E1：`el-table` 无 `#empty` 插槽 + 下方 `el-empty` 并存 |
| E4 | 中等 | `HostsView.vue:111-129`、`:276`、`:474` | 主机表无 `v-loading` 也无骨架屏，首屏与 30s 轮询都是「空表→突然满屏」；抽屉 `size="520px"`、对话框 `width="420px"` 写死，窄屏溢出（对比 `IntelligenceView.vue:19` 用 `min(760px,94vw)`） |
| E5 | 中等 | `NodeView.vue:58,535-540` | 「系统负载」直接 `{{ rt.load1 }}/{{ rt.load5 }}/{{ rt.load15 }}`，`rt` 初值全 0，WS 首帧前显示「0 / 0 / 0」，与同页其它字段的 `-` 口径不一致 |
| E6 | 中等 | `metrics/MetricsExploreView.vue:216` | 高度 `calc(100vh - 140px)` 写死，没算页脚 `SiteFooter.vue:47`（约 60px，且 `v-if` 按品牌配置动态显隐）→ 要么双滚动条，要么底部空约 48px |
| E7 | 中等 | `metrics/MetricsExploreView.vue:79,217,222-233` | `.left{width:320px}` 写死无断点，`.metric-tree-node` 只有 `min-width:0` 无 `text-overflow` → `node_disk_read_bytes_total` 这类长指标名溢出；`.chart{height:calc(100% - 70px)}` 的 70px 是猜的头部高度（实际约 72px），**疑似**溢出数像素 |
| E8 | 轻微 | `NodeView.vue:1353-1366` | `.port-item` 重复定义两遍，前一段被完全覆盖，是死代码 |

---

## 6. 资产台账 / 关系视图 / 配置项模型 / 配置巡检

| # | 级别 | 位置 | 问题 |
|---|---|---|---|
| F1 | 严重 | `asset/AssetModelView.vue:38,156,195,200`、`asset/AssetListView.vue:538`、`ApprovalView.vue:83,96` | Markdown 星号原样渲染：页面没有 Markdown 渲染器，正文里的成对 `**` 直接显示（如「Value 是**存进台账**的值」、「至少要有一个**别人**能批准」），把重点词框成错误形态 |
| F2 | 严重 | `asset/AssetModelView.vue:76-151` | 字段表 9 列，列宽合计约 1360px（内容区约 1114px）必然横滚，但「字段」列没有 `fixed` → 横向滚动后看不到自己在编辑哪个属性；同时无分页、无 `max-height`（:190 自认字段数会超上限），几十个字段时保存按钮沉到视口外 |
| F3 | 严重 | `asset/TopologyGraph.vue:107-134,228-230` | 关系图永不 resize：`echarts.init()` 只在 `render()` 调一次，无 resize 监听/ResizeObserver，而 `.topo-chart{width:100%}` → 侧栏折叠、窗口缩放后画布停在旧值。**疑似加重**：`AssetListView.vue:859` 把它放进 `el-dialog`，首次打开容器可能宽 0，打开后整块空白且不自愈 |
| F4 | 中等 | `assets/style.css:276-278` + `AssetListView.vue:42`、`OpsView.vue:42` | `.panel` 只有 padding，没有背景/描边/圆角 → 资产台账与节点操作页没有卡片感，透明表格直接浮在 `--s0` 上，而同项目 InspectView/ContainerView/AssetModelView 用的是 `SectionCard`（`--s1` 底 + 描边 + 圆角）。两个域容器语言不一致 |
| F5 | 中等 | `assets/style.css:324-332` + `AssetListView.vue:272-273,490,517` | 全局 `.tag` 只定义排版，颜色只由 `.online`/`.offline` 等修饰类提供。抽屉里「归属节点」「责任人」、关系表里「关系种类」的 `.tag` 无修饰类 → 透明底、继承文字色，看起来像普通文本而不是标签 |
| F6 | 中等 | `asset/AssetListView.vue:307-542` | 抽屉三个 Tab 内容量差异极大（属性对比 = 两张表 + descriptions + 两组按钮 / 变更历史 = 时间线 / 关联关系 = 两张表 + 说明）→ 切换时正文高度骤变、滚动位置重置；`AiDiagnosePanel` 常驻在 Tab 之外，切 Tab 时在视口里上下窜 |
| F7 | 中等 | `asset/AssetTopologyView.vue:307-312` | 硬编码脱离令牌：`:307` `#e6a23c`（与 `--warn:#ffb454` 不一致）、`:309` `.tag` 背景 `rgba(74,157,240,.14)`（换肤仍是极光蓝）、`:311` `#8a94a6`、`:312` 用 `ui-monospace` 而非 `var(--mono)` |
| F8 | 轻微 | `asset/AssetModelView.vue:138` | 覆盖度 `{{ row.assets }}/{{ current.assets }}` 无分母保护 → `0/0`、`pct()` 得 `0%`，进度条整条空。另 `:47-55` 与 `:69-74` 是同层级两个工具行，gap 分别 8px/10px，行高不齐 |

---

## 7. 容器 / 节点操作 / 审批 / 拨测

| # | 级别 | 位置 | 问题 |
|---|---|---|---|
| G1 | 严重 | `OpsView.vue:1631-1641` | `.ops-cell{opacity:0.45}`，仅 hover/focus-within 恢复 → 深色下「查看结果/复制批次/取消/删除」对比度极低，触屏与无鼠标场景等于隐藏；`focus-within` 对不可聚焦容器不生效 |
| G2 | 中等 | `OpsView.vue:45-59`、`:1400-1407` | 搜索框写死 250px，三个 select 写死 168/168/140/136px，`.op-tb-right{margin-left:auto}` → 窄屏换行后「列/密度/导出」这组按钮单独占一行并被推到最右，右侧悬空一撮、左侧大片空白；全组件无媒体查询 |
| G3 | 中等 | `container/ContainerView.vue:40` | 仍用 `el-tabs type="card"`，而 `style.css:821-822` 明确写了「废弃 card/border-card（深色下是厚边框盒子）」；且 :904-906 又把 `.el-tabs__content` 直接 `display:none`，是绕过组件语义的写法 |
| G4 | 中等 | `asset/InspectView.vue:185-223,347` | 两处长列表无分页无高度上限：差异项表连上限都没有，`findings` 多时整页极长；差异区在页面中部，下方还有标杆表，操作要来回长距离滚动 |
| G5 | 中等 | 共约 15 处：`AssetListView.vue:267,593,686,830,859`、`ContainerView.vue:166,187`、`OpsView.vue:214,323,359`、`ApprovalView.vue:105`、`AssetModelView.vue:154,172`、`DialTestView.vue:84` | 抽屉/弹窗宽度全部写死 px，<900px 视口下 900px 关系图弹窗、720px 日志抽屉直接溢出屏幕两侧 |
| G6 | 中等 | `DialTestView.vue:186` | 删除确认用原生 `window.confirm()`，系统白底弹窗与深色主题冲突，与全站 `ElMessageBox` 不一致；另 :162/182/190/201 失败只 `console.error`，界面无任何反馈 |
| G7 | 轻微 | `container/ContainerView.vue:353-355,339` | `detailRows` 取 `cells[1]`、`logText` 取 `cells[0]` 无兜底，单列行时详情为空、日志拼出 `undefined` 字面量 |
| G8 | 轻微 | `ApprovalView.vue:321-323,334-337` | `.approval-page{padding:12px}` 与 MainLayout 的 18px 20px 叠加成双层留白（`AssetListView.vue:1917`、`OpsView.vue:1254` 的注释明确写了「各页不再自带缩进」）；用 `el-card` 而非 `.card`/`SectionCard` |

---

## 8. 告警 / 通知 / 报告 / 日志 / 审计 / 安全

| # | 级别 | 位置 | 问题 |
|---|---|---|---|
| H1 | 严重 | `LogsView.vue:144-155,448,694-718` | 全文件无 `v-loading`/骨架屏，`lines` 初值 `[]` → 首屏先显示「没有命中的日志」这个错误结论，数据到达后整块跳成表格 |
| H2 | 严重 | `LogsView.vue:31`、`AuditView.vue:33` | `datetimerange` 写死 `style="width:360px"`。全局把 `--el-font-size-base` 提到 16px（`style.css:634`），EP 该组件需约 400px → 两个输入框各约 165px，装不下 `2026-01-01 00:00:00`，开始/结束时间被裁剪。这是两个页面最核心的筛选控件 |
| H3 | 严重 | `ReportView.vue:325-331` | `.report-iframe{height:860px}` 写死，与视口无关 → 内容长时 iframe 内部与页面嵌套双滚动；内容短时卡片下方留数百 px 空白；窄屏上预览区与按钮组被远远推开 |
| H4 | 中等 | `AuditView.vue:97-169` | 9 列最小宽合计约 1520px（「操作」300 +「变更摘要」300），内容区约 1134px → 「状态码/结果/资产改动」必须横滚才能看到，排障最关心的「成败」落在视野外；无列优先级或响应式收窄 |
| H5 | 中等 | `AuditView.vue:566-568`、`:128-131` | 颜色用 `span:nth-child(1/2/3)` 绑定到「修改/新增/删除」，但三个 `<span>` 都带 `v-if` → 「修改」为空时，「新增」落到 `:first-child` 显示成琥珀、「删除」显示成绿，状态色语义反。且三色是 EP 亮色硬编码 |
| H6 | 中等 | `AlertsView.vue:1604-1608` | `.ev-actions{display:flex;gap:8px}` 无 `flex-wrap`，抽屉 `size="480px"` 内容宽约 456px，而「认领/指派/关闭/重开/查看日志/AiDiagnosePanel/评论/返回」8 个控件总宽明显超过 → 按钮挤压变形或溢出抽屉 |
| H7 | 中等 | `AlertsView.vue:1510-1513` + `SectionCard.vue:77-82` | 「活跃/已恢复/全部」+ 批量确认 + nowrap 提示语 + 测试事件合计约 640px 不可收缩，≤1024px 卡片头溢出、页面出现横向滚动条 |
| H8 | 中等 | `SecurityView.vue:883-887` | `.defense-kpi{repeat(4,1fr)}` 是该文件唯一没有 1100px 断点（:840-843）的网格 → 平板宽度下 4 张卡被压到 <180px，22px 数字与 13px 标签换行错乱 |
| H9 | 中等 | `SecurityView.vue:189,227-236` | 安全事件表 `max-height="460"` 写死，而每页可切 10/20/50/100 → 切换时卡片高度在约 380↔460px 跳变；100 条时表体内滚，与分页器形成两个独立滚动区 |
| H10 | 中等 | `NotifyView.vue:219-221,22-37` | `.notify-view{max-width:920px}` 写死且整页无媒体查询 → 宽屏内容钉在左侧、右侧大片留白；SMTP 区 `el-col:span=12` 固定 50%，窄屏变成两个约 180px 的列挤压 |
| H11 | 中等 | `RuleModal.vue:2,90-102` | 弹窗 `width="680px"` 无 `max-width`/响应式；运算符 `el-col:span=7` 在 680px 里约 198px，扣掉 90px 标签后 select 仅约 108px → 「大于等于 ≥」被裁剪。`.rule-form` 用 `max-height:70vh` 而非弹性高度，窄屏下保存按钮需滚动遮罩才能触达 |
| H12 | 中等 | `ReportView.vue:121-127,169-178` | 无 `v-loading`/骨架屏，`history` 初值 `[]` 时先渲染「暂无历史报告」再跳变；`generate()` 成功后同理。另 :90-120 历史报告表格无分页无条数上限 |
| H13 | 轻微 | `SecurityView.vue:793-798` | 同一条「未通过」基线项里三处状态色不一致：圆点 `--danger` 红、名称 `--warn` 琥珀、状态文字 `--danger` 红 → 扫读时「算警告还是失败」没有统一答案 |
| H14 | 轻微 | `ReportView.vue:310-314,323-324`、`NotifyView.vue:222-244`、`AlertsView.vue:1542` | 改用 `PageHeader`/`SectionCard` 后遗留的死 CSS（`.page-header`/`.chart-section`/`.frame-head` 等模板里已无对应元素）；`AlertsView.vue:1542` 硬编码 `rgba(230,162,60,.08)` 未走 `--warn-dim` |

---

## 9. 系统设置 / 升级 / RBAC / 中间件 / 大屏 / 仪表盘

| # | 级别 | 位置 | 问题 |
|---|---|---|---|
| I1 | 严重 | `screen/ScreenView.vue:331-333,611-617` | 大屏 `width:100vw;height:100vh;overflow:hidden`，`.tab-content{overflow:hidden}`，而媒体查询只有 `min-width:2400px`(:756) 和 `3440px`(:780) → 1366/1600/非 16:9/带书签栏时，tab 内容直接被裁掉且无法滚动；KPI 条与底栏固定 48px 进一步压缩中间区 |
| I2 | 严重 | `screen/ScreenView.vue:376,408-415,460-468` | `.hud-side{width:300px}` 两侧共 600px 写死，`.hud-center{flex:1}` 缺 `min-width:0`，主标题 `font-size:32px;letter-spacing:.28em` → ≤1280 时中间列被压缩，配合根节点 `overflow:hidden`，主标题/装饰线被裁切 |
| I3 | 中等 | `screen/ScreenView.vue:334-340`、`:90` | grid 第 4 行固定 `48px`，但 footer 由 `v-if="cfg.modules.alerts"` 控制 → 在模块设置里关闭「底部告警滚动区」后，底部永久残留 48px 空白带 |
| I4 | 中等 | `screen/CenterPanel.vue:373`、`UpgradeView.vue:660` | `.mini-row{repeat(6,1fr)}` 但 `minis` 只有 5 项（:122-133）→ 底部永远空一格，与上方 5 列 KPI（:271）左右边界对不齐；`.ver-grid{repeat(3,1fr)}` 但模板只有 2 个 `.ver-item`（:11-18）→ 右侧空出 1/3 列宽 |
| I5 | 中等 | `screen/ScreenHealthScore.vue:140-144,164-166` | `.sh-bar-l{width:56px;white-space:nowrap}`（并重复声明），「告警健康度」5 字约 65px → 溢出并压在进度条上 |
| I6 | 中等 | `screen/tabs/NetworkTab.vue:5-10,580-589` | 使用原生 `<select>`，只改了控件本身外观 → `<option>` 下拉面板由系统绘制，深色大屏上弹出白色系统下拉。另 :550 `34% 1fr` + :556 左列 2×2 + :710 `.rank-row{18px 1fr 1fr 44px}` → 1366 下名称列只剩约 70px，长 URL 全靠 title |
| I7 | 中等 | `dashboard/DashboardView.vue:34-42,212,218` | 编辑态下删光面板 `activePanels` 返回 `[]`，`.grid` 塌陷为 0 高且无 EmptyState（非编辑态 :27-33 有）→ 页面看起来卡住。`height:calc(100vh-132px)` 把顶栏高度硬编码为 132px。`.grid{minmax(360px,1fr)}` 在 <392px 撑破产生横滚 |
| I8 | 中等 | `dashboard/PanelChart.vue:105` | 只监听 `window resize` → 侧栏折叠、网格列数变化这类不触发 window resize 的布局变化不会 resize 图表，配合 :117-124 的硬 `min-height`，图表与卡片宽度不匹配 |
| I9 | 中等 | `settings/AlertPipelineSubView.vue:78,233-250` | 「正则/替换为/值」列未给 `min-width`（其余列固定 674px）→ 容器不足时该列被压到接近 0，`.triple` 三个输入框叠成一条，实际无法输入；左列 14 行 textarea（约 300px）与右列 3 个 `max-height:220px` 的 `pre` 同处一行 → 两列高度严重不匹配，右侧底部大片空白 |
| I10 | 中等 | `settings/BrandSettingsSubView.vue:17-60` | 一个 `el-row` 内放 3 个 `el-col:md=12`（两列布局 3 项）→ 第 3 项（Logo）独占一行且右侧空半列，与上下两组各 2 项不对齐 |
| I11 | 中等 | `settings/SelfMonitorSubView.vue:47,189` | `el-descriptions :column="4" border` 固定 4 列放 16 项，无媒体查询 → 变窄时 8 个单元格同时换行，行高参差、标签值错位；`renderTrend()` 在 `nodeLabel` 为空时直接 `return` → 260px 高的图表区永久空白，既无空态也无错误说明 |
| I12 | 中等 | `rbac/RolesView.vue:68`、`rbac/UsersView.vue:77`、`UpgradeView.vue` 多处 | 固定宽对话框（720px / 560px），<768px 溢出屏幕（EP 不会自动降宽） |
| I13 | 轻微 | `rbac/UsersView.vue:450`、`rbac/RolesView.vue:408`、`UpgradeView.vue:238` | `.scope-warn{color:#e6a23c}`、内联 `color:#909399` 硬编码，未走 `--warn`/`--t3` |

---

## 10. 跨页面共性问题

1. **抽屉/弹窗宽度清一色写死 px**，无 `min(Xpx, 94vw)` 也无断点 —— 涉及约 15 处（见 G5、I12）。
2. **硬编码颜色绕过主题令牌**：`#e6a23c`、`#909399`、`#8a94a6`、`#f56c6c`、`#67c23a`、`#22d3ee`、`#cbd5e1`、`rgba(74,157,240,…)` 等散布在 asset / container / intelligence / screen / settings / rbac / audit / security 各页，换肤后不跟随。
3. **骨架屏缺失 → 空态闪现 + 布局跳动**：`style.css:771` 已定义 `.sk` 却几乎没被使用，LogsView / ReportView / HostsView / AssetListView 都是「先渲染错误空态 → 数据到达后整块翻转」。
4. **宽表无 `fixed` 首列**：AuditView（9 列）、AssetModelView（9 列）、LogsView（约 1140px）横滚后丢失行标识。
5. **死 CSS 堆积**：ReportView / NotifyView / DialTestView / IntelligenceView / DashboardView / MiddlewareView 都留有改用 `PageHeader`/`SectionCard` 后无人引用的旧规则，是后续视觉回归的隐患源。
6. **图表 resize 收口缺失**：NodeView、TopologyGraph、PanelChart 各自（或未）实现 resize，建议统一收到 `charts/echarts.js` 的 `initChart` 里用 `ResizeObserver`。

---

## 11. 修复计划

### 第一批：低风险、收益最大 —— **已完成**

- [x] B 组：11 个不存在的变量全部改到正确令牌；`--chart-yellow` 补进 `style.css` 的 chart 色板
- [x] B7：`el-tag--primary` 边框改从 `--accent` 推导（`color-mix`）；补 `--el-color-error` 系列映射
- [x] C2 / C3：SecurityView 两条后代选择器重写；DialTestView 类名对齐（C1 经核验为误报，见第 3 节）
- [x] F1：删除页面正文里原样渲染的 `**`（AssetModelView 4 处、AssetListView 1 处、ApprovalView 2 处）
- [x] A2：Sidebar 光条 `-24px` → `-36px`
- [x] E1 / E3：NodeView（进程表、告警表）、IntelligenceView 改用 `#empty` 插槽，去掉重复的 EmptyState

### 第二批：结构性 —— **大部分已完成**

- [x] A4 / A5：PageHeader / SectionCard 补 `flex-wrap`，`.sec-body` 补 `min-height:0`
- [x] A10：登录页改 `max-width:100%` + `overflow-y:auto`，页脚回到正常流（不再 absolute 压卡片）
- [x] H1 / H12：LogsView 区分「没查过 / 查过但没有」；ReportView 加 `historyLoading` 骨架，消除空态闪现
- [x] E2 / F3 / I8：图表 resize 收口到 `charts/echarts.js` 的 `initChart`（`ResizeObserver` + rAF 合并 + dispose 时断开），并把 12 处直接 `echarts.init` 的调用统一到 `initChart`
- [x] G5 / I12：抽屉/弹窗宽度批量改 `min(Xpx, 94vw)`（27 处）
- [x] D1：IntelligenceBlock 去掉重复标题与自带卡片外观，只保留工具行
- [x] I1 / I2：大屏 `.tab-content` 改 `overflow:auto`；`.hud-center` 补 `min-width:0`
- [x] I4 / I5：CenterPanel `repeat(6)` → `repeat(5)`、UpgradeView `.ver-grid` `repeat(3)` → `repeat(2)`、ScreenHealthScore `.sh-bar-l` 改 `min-width`
- [x] H8：`.defense-kpi` 补 1100px 断点
- [x] G1：`.ops-cell` 默认不透明度 `0.45` → `0.8`
- [x] E6 / E7：MetricsExplore 高度改 `clamp()`，`.right` 改 flex 列、图表用 `flex:1` 撑满（不再减一个猜出来的 70px）
- [x] F5：全局 `.tag` 补默认底色与文字色
- [x] H5：AuditView 变更摘要改用语义类名上色（不再用 `nth-child`），method 兜底空值
- [x] A6 / A7 / A8 / A3：`.save-bar` 补 `z-index`；补标准滚动条属性；折叠宽令牌化 `--sidebar-w-collapsed`；侧栏品牌区高度 52px → 40px（与顶栏 56px 对齐）
- [x] H2：LogsView / AuditView 的 `datetimerange` 360px → 400px + `max-width:100%`
- [x] H6 / G2：AlertsView `.ev-actions`、OpsView 工具条、AuditView `.toolbar` 补换行策略

### 第三批 —— **已完成**

**响应式与断点**
- [x] D5 / D6：首页 `.mw-grid` 补 1400 / 1100 / 680 三档断点；`.ov-block[data-span='4']` 在 1400px 以下降为 `span 6`
- [x] H10 / H11：NotifyView `max-width` 920 → 1120、SMTP 区改 `:xs="24" :sm="24" :md="12"`；RuleModal 弹窗 680 → 760 并单独收窄该行标签
- [x] I3：大屏 grid 末行 `48px` → `auto`，`.hud-bottom` 自带 `min-height:48px`（关掉模块后不再残留空白带）
- [x] A1：在 `style.css` 建立统一断点说明（1500 / 1400 / 1100 / 720 四档），`.panel-grid` 补 1400px 档
- [x] I6：NetworkTab 原生 `<select>` → `el-select`；`.nt-body` 补 1500px 断点改上下单列

**加载态与信息密度**
- [x] D2：首页 `mwData` 未到达时 MiddlewareOverview 渲染骨架卡（不再先显示「尚未配置」）
- [x] D3：KpiBlock 曲线未成形时用同高占位，卡片不再因攒够采样点而突然长高
- [x] E4：HostsView 加 `v-loading`
- [x] I7：Dashboard 编辑态删光面板后补「还没有面板」空态
- [x] H4：AuditView 首列 `fixed`、「操作 / 变更摘要」min-width 300 → 240
- [x] F2：AssetModelView 字段表首列 `fixed`
- [x] H9：SecurityView 去掉写死的 `max-height="460"`
- [x] I11：SelfMonitorSubView 指标明细改响应式列数；趋势区无节点标识时给空态说明
- [x] I9 / I10：AlertPipelineSubView「正则 / 替换为 / 值」列补 `min-width`；BrandSettingsSubView Logo 项占满整行

**规范清理**
- [x] G6：DialTestView 的 `window.confirm()` → `ElMessageBox`，并补上失败提示
- [x] F4：`.panel` 补齐容器外观（背景 / 描边 / 圆角），与 `SectionCard` 两种容器语言一致
- [x] F6 / F7 / F8：AssetListView 抽屉 Tab 内容区加 `min-height`（切换不再塌一下）；AssetTopologyView 硬编码色与字体收编到令牌
- [x] H7 / H13：AlertsView 卡片头提示语允许折行；SecurityView 同一基线项三处状态色统一为 `--danger`
- [x] 死 CSS 与死组件清理：删除无引用且无 `<style>` 块的 `TopBar.vue`；清除 ReportView / NotifyView 改用 PageHeader 后遗留的死规则
- [x] 硬编码色收编到令牌：`#e6a23c` → `--warn`、`#909399` → `--t3`、`#f56c6c` → `--danger`、`#8a94a6` → `--text-muted`；TopologyGraph 的图内配色改从 CSS 变量读（切主题不再停在极光蓝那一套）

---

## 12. 本轮实际改动的文件

第一批（失效变量 / 选择器 / 文案）：
`assets/style.css`、`Sidebar.vue`、`SecurityView.vue`、`DialTestView.vue`、`AssetModelView.vue`、`AssetListView.vue`、`ApprovalView.vue`、`LoginView.vue`、`SiteFooter.vue`、`NotifyView.vue`、`LogsView.vue`、`AuditView.vue`、`RolesView.vue`、`ContainerView.vue`、`NodeView.vue`、`IntelligenceView.vue`

第二批（结构性）：
`charts/echarts.js`（resize 收口）、`common/PageHeader.vue`、`common/SectionCard.vue`、`overview/IntelligenceBlock.vue`、`LoginView.vue`、`ReportView.vue`、`MetricsExploreView.vue`、`OpsView.vue`、`AlertsView.vue`、`screen/ScreenView.vue`、`screen/CenterPanel.vue`、`screen/ScreenHealthScore.vue`、`UpgradeView.vue`、`MainLayout.vue`
以及图表初始化收敛涉及的 12 个文件：`settings/SelfMonitorSubView.vue`、`rocketmq/RocketMQTab.vue`、`postgres/PostgresTab.vue`、`nginx/NginxTab.vue`、`mysql/MySQLTab.vue`、`mongo/MongoTab.vue`、`kafka/KafkaTab.vue`、`fastdfs/FastDFSTab.vue`、`docker/DockerTab.vue`、`AlertsView.vue`、`mw/LightCollectorTab.vue`、`asset/TopologyGraph.vue`

抽屉/弹窗宽度批量收敛（27 处）覆盖：AlertsView、ApprovalView、AuditView、DialTestView、GroupManage、HostsView、LogsView、OpsView、RuleModal、screen/HostDetailDialog、screen/InstanceDetailDialog、rbac/RolesView、rbac/UsersView、container/ContainerView、asset/AssetListView、asset/AssetModelView、ai/AiDiagnosePanel

第三批（响应式 / 加载态 / 交互）：
`OverviewView.vue`、`overview/MiddlewareOverview.vue`、`overview/KpiBlock.vue`、`HostsView.vue`、`AuditView.vue`、`SecurityView.vue`、`NotifyView.vue`、`RuleModal.vue`、`DialTestView.vue`、`dashboard/DashboardView.vue`、`settings/AlertPipelineSubView.vue`、`settings/BrandSettingsSubView.vue`、`settings/SelfMonitorSubView.vue`、`screen/ScreenView.vue`

---

## 13. 验证方式

**已做的验证**：第一批与第二批的全部改动都跑过 `npm run build`（0 error），并用 grep 复核了关键结论（变量定义数为 0、`:deep` 写法、收敛后无残留 `echarts.init`）。

**仍需实机验证**（静态审查判不了，依赖真实视口）：

- A1 / D5 / D6 / I1 / I2：挤压程度与断点取值，建议按 1920 / 1440 / 1280 三档走一遍
- D2 / H1 / H12：空态闪现修复后的实际观感
- I1：大屏在 1366 / 1600 / 非 16:9 下 `.tab-content` 改 `overflow:auto` 后的滚动表现
- 全部 `min(Xpx, 94vw)` 的弹窗在 <900px 视口下的表现

**主题回归**：三套主题（极光蓝 / 星云青绿 / 星河紫）需逐页走一遍，重点看 B 组（已改的 11 个变量）与散布各页的硬编码色（第三批待办）。
