# 资产属性字典与枚举设计件（「资产生命周期 / 成本 / 维保」的前置）

- 批次：29（待排期）
- 日期：2026-10-08
- 对应全景表：§3.2「资产生命周期（上线/下线/退役/成本/维保）」（**未实现** / P2 / M / 归属 D2）的**前置**；本件本身是一个**尚未登记的新功能点**，待评审后补入全景表
- 状态：**已评审通过（2026-10-08，D1~D9 全部按建议执行）**——D1 扩 `AttrMeta`（不新建表）、D2 五种类型、D3 只校验人工值、D4 必填不阻断保存、D5 label 不落 attrs、D6 本期不做按字段筛选、D7 建议项一键添加不自动写入、D8 到期提醒另批、D9 字典作用域类型内。

## 1. 起因：为什么不是直接做「资产生命周期」

全景表 §3.2 那行写的是「资产生命周期（上线/下线/退役/成本/维保）| 未实现 | P2 | M | 前置：资产台账」。

但台账代码里有一条**明确的定界**，把这类"展示型管理属性"挡在外面：

```go
// internal/server/asset/model.go:461-466
// OwnerKey 是「责任人」的约定属性键。
//
// 责任人是唯一被列表、摘要与筛选直接消费的管理属性，因此约定一个固定键；
// 其余管理属性（业务系统 / 环境 / 维保到期 / 资产编号…）保持自由键——
// 在字典与枚举能力上线前，不把一堆展示字段固化成领域概念。
const OwnerKey = "owner"
```

同一句话在设计件里也写过一遍（`2026-09-30-asset-cmdb-design.md:269`）：**「其余管理属性（业务系统/环境/维保到期…）保持自由键，等字典枚举能力上线再固化」**。

也就是说：

- **「维保到期」不是"还没排到"，而是"被一条定界挡着"**——它被刻意留在自由键里，等一个前置；
- 那个前置是「**属性字典与枚举能力**」（让管理属性有类型与取值范围），**而它从未被登记成全景表的任何一行**；
- 若直接去做"生命周期阶段 + 成本字段 + 维保到期"，等于绕过自己写下的定界，并且会**再固化成一批硬编码字段**（今天有 6 个内置类型，将来加类型还要再来一遍）。

**所以本件先做前置**：让"有哪些管理属性、各自什么类型、取值可以是哪些"变成**用户可配的载体**。前置上线后，「资产生命周期 / 成本 / 维保」不再需要各自的代码——它们是**用户在这个载体上定义的几个属性**。

> 这正是平台已有的取向：`TypeSchema.Tolerance` 的注释写着「容忍度**天然长在这一套 schema 上**，另起一处就会再造第二套定义」（`model.go:185-187`）。字典与枚举同理——它长在 `TypeSchema` 上，不新起一张表。

## 2. 现状（有依据）

| 事实 | 依据 |
|---|---|
| 属性是**自由键值**，同一 key 可同时有采集值与人工值（联合主键含 source） | `asset/model.go:405-411`（`Attr{Key,Value,Source,UpdatedAt,UpdatedBy}`）、`:444-459`（`Value()` 人工值优先） |
| 属性来源只有两种：`discovery` / `manual` | `asset/model.go:24-45` |
| 约定键**只有两个**：`owner`（责任人）、`logSource`（日志来源声明） | `asset/model.go:461-474`、`:476-492`；注释说明了"为什么只有它们被固化" |
| 类型模型已存在，且是"类型 × 字段画像"的载体 | `asset/model.go:181-188`（`TypeSchema{RuntimeFields, FocusFields, AttrMeta, Tolerance}`）；`asset_types.schema` 列（`asset/store.go:27`） |
| `AttrMeta` 目前**只影响展示**：中文名 / 单位 / 备注 | `asset/model.go:168-173`；「只影响展示，不参与比对、不进导出列」 |
| 模型有**预留位**先例：`Tolerance` 明确写了"本批不实现，但天然长在这套 schema 上" | `asset/model.go:185-187` |
| 模型接口与页面已存在 | `GET/PUT /api/v1/asset-types`（`internal/server/api/asset_type_api.go`）；前端 `web/src/components/asset/AssetModelView.vue`（可改中文名/单位/备注与比对档位） |
| 人工值写入口**只有一处**：`PUT /assets/{id}` 的 `attrs`（只写 manual 来源） | `internal/server/api/asset_api.go:1082-1092`（`assetWriteBody`）、`:1154-1212`（`handleAssetUpdate`）；批量维护的 5 个动作里没有属性写（`asset_batch_api.go:34-40`） |
| 台账**没有**任何类型/枚举/必填校验；写入即存 | 同上，`attrs` 直接落库 |
| 内置类型 6 个、**不支持自定义类型** | `asset/model.go:132-141`、`:149` |
| 「到期提醒」的范式**已存在**，可复用 | 拨测证书到期：`dialtest/scheduler.go:147-150` + 指标 `dial_test_cert_expiry` + 内置规则「证书即将到期 / 已过期」（`alert/rules.go:225,228`） |
| 全仓 grep：`warranty` **0 命中**；`lifecycle`/`cost`/`退役`/`报废` 均无实现 | 属**从零新建** |

## 3. 范围

**做**：

1. **属性字典**：在 `TypeSchema.AttrMeta` 上增加**类型**（text / number / date / enum / bool）与 **enum 取值**（含显示名）；
2. **写入口校验**：`PUT /assets/{id}` 与 `POST /assets` 的人工值按字典校验，非法值**明确 400** 并说明是哪个字段；
3. **配置项模型页**：可为每个字段配类型与枚举（沿用现有页面，不新建页面）；
4. **台账编辑弹窗按字典渲染**：enum → 下拉、date → 日期选择、bool → 开关、number → 数字输入。

**不做**（明确划线）：

- **不新建字典表**：字典长在 `TypeSchema` 上（同 `Tolerance` 的取向）。
- **不校验采集值**：字典是**人对台账的约定**，采集值来自真实环境（且采集侧不受平台控制）。用它拒收采集数据，会把"真实但不合规"的机器挡在台账外——那正是台账最该记录的东西。**校验只作用于 manual 来源**。
- **不做自定义资产类型**：与 `model.go:149` 的既定结论一致。
- **不做"到期提醒"**：维保/证书到期的提醒范式已存在（拨测证书那套），但本批只做字典；提醒另批（见 §7）。
- **不做按字典字段的列表筛选**：现有 `keyword` 已覆盖属性值搜索；按字段精确筛选是另一件事（见 D6）。
- **不做跨类型的全局字典表**（见 D9）。

## 4. 设计

### 4.1 字典挂靠 `TypeSchema.AttrMeta`（不新起一处）

```go
// AttrMeta 是属性的**说明与约束**（本批起从"只影响展示"扩到"约束人工值"）。
type AttrMeta struct {
    Title string `json:"title,omitempty"`
    Unit  string `json:"unit,omitempty"`
    Note  string `json:"note,omitempty"`
    // ---- 本批新增 ----
    // Kind 是字段类型：text（默认）/ number / date / enum / bool。
    Kind string `json:"kind,omitempty"`
    // Enum 仅在 Kind=enum 时有意义；Value 是存进 attrs 的值，Label 是界面显示名。
    Enum []EnumOption `json:"enum,omitempty"`
    // Required 表示"人工值缺失时界面提示"（**不阻断保存**，见 D4）。
    Required bool `json:"required,omitempty"`
}

type EnumOption struct {
    Value string `json:"value"`
    Label string `json:"label,omitempty"`
}
```

**为什么扩 `AttrMeta` 而不是新表**：`TypeSchema` 已经是"类型 × 字段画像"的唯一载体，`Tolerance` 的注释已经把这条取向写死了；另起一张表意味着"字段有两处定义"，巡检、导出、页面都要各读一处。

**兼容**：`Kind` 为空 = `text`（不校验，今天的形态）；`DecodeTypeSchema` 对坏数据仍按既有取向降级不报错。

### 4.2 校验只作用于人工值

| 类型 | 校验规则 | 拒绝时的响应 |
|---|---|---|
| text | 不校验（默认） | — |
| number | 可解析为数字（`strconv.ParseFloat`） | 400 `字段 cpuCost 需要是数字` |
| date | 可解析为 `YYYY-MM-DD`（也接受 RFC3339，存原样） | 400 `字段 warrantyUntil 需要是 YYYY-MM-DD` |
| enum | 值必须命中 `Enum[].Value` | 400 `字段 env 的取值不在枚举内（可选：prod / test）` |
| bool | `true`/`false`（大小写不敏感；**存原样，不归一化**，与 date 同口径） | 400 `字段 isHA 需要是 true 或 false` |

**校验位置**：`asset.Service` 层（写入口唯一），API 层只负责把错误映射成 400——与"人工值只写 manual"同处一层，任何调用方都绕不过。

### 4.3 枚举的显示名

`Enum[].Label` 缺省时界面显示 `Value` 本身。**不把 Label 存进 attrs**：存进去之后"改显示名"就要改数据，而显示名是展示约定（与 `AttrMeta.Title` 同一性质）。

### 4.4 「生命周期 / 成本 / 维保」怎么落地

前置上线后，这三件事**不需要各自的代码**，而是用户在自己的类型上配几个属性：

| 需求 | 配法 |
|---|---|
| 生命周期阶段 | 键 `lifecycle`，Kind=enum，Enum=`在用 / 闲置 / 下线 / 退役`（值用 `active`/`idle`/`offline`/`retired`） |
| 成本 | 键 `cost`，Kind=number，Unit=`元/月` |
| 维保到期 | 键 `warrantyUntil`，Kind=date |
| 业务系统 / 环境 / 资产编号 | 同法，各自 enum 或 text |

**是否预置**见 D7：建议在配置项模型页提供一组**建议项**（一键添加，不自动写入）——预置写进库会让"我没配过却出现了字段"变成一个谜。

### 4.5 接口与前端

- **接口**：`PUT /api/v1/asset-types/{key}` 的 `schema` 直接承载新增字段（**不新增路由**）；人工值写入的校验错误沿用既有 400 通道。
- **配置项模型页**（`AssetModelView.vue`）：每个字段行增加「类型」下拉与（Kind=enum 时的）枚举编辑器（值 + 显示名，可增删排序）。
- **台账编辑弹窗**（`AssetListView.vue:647-697`）：按 Kind 渲染控件；enum 显示 Label；非法值在提交前就拦一次（服务端仍是权威）。
- **详情抽屉属性对比**：enum 的生效值旁边显示 Label（原值仍可见，便于排障）。

## 5. 决策点

| # | 决策 | 建议 | 影响 |
|---|---|---|---|
| **D1** | 字典挂靠：扩 `AttrMeta` / 新建 `attr_definitions` 表 | **扩 `AttrMeta`** | 新表会让字段有两处定义（巡检/导出/页面各读一处），与 `Tolerance` 的既有取向相反 |
| **D2** | 类型集合 | **text / number / date / enum / bool** | 少了 date 就表达不了维保到期；少了 bool 就得用 enum 凑 |
| **D3** | 校验范围 | **只校验人工值** | 校验采集值会拒收真实数据，且采集侧不受平台控制 |
| **D4** | `Required` 的语义 | **界面提示，不阻断保存**（台账是"如实记录"，不是表单系统） | 若改成阻断，会出现"这台机器没填成本就建不了档" |
| **D5** | 枚举显示名 | **`{value,label}`，label 不落 attrs** | 落库后改显示名要改数据 |
| **D6** | 是否支持按字典字段筛选列表 | **本批不做** | 现有 keyword 已能搜到属性值；按字段精确筛选要动列表接口与索引，另批 |
| **D7** | 是否预置生命周期/成本/维保属性 | **给"建议项"一键添加，不自动写入** | 自动写入会出现"我没配过却多了字段" |
| **D8** | 到期提醒（维保到期 / 证书到期统一） | **本批不做**，另批复用拨测证书那套（`dial_test_cert_expiry` + 内置阈值规则） | 它需要"按属性到期日生成指标"，是采集/告警侧的活 |
| **D9** | 字典作用域：类型内 / 全局共享 | **类型内**，界面提供"从其他类型复制字段" | 全局字典表要处理"同名不同义"，且与 `asset_types` 的既有结构冲突 |

## 6. 实施批次

| 子批次 | 内容 | 依赖 |
|---|---|---|
| **29a** | 模型与校验：`AttrMeta` 扩展 + `DecodeTypeSchema` 兼容 + `Service` 层人工值校验 + 单测 | 无 |
| **29b** | 配置项模型页：字段类型与枚举编辑器 | 29a |
| **29c** | 台账编辑弹窗按字典渲染 + 详情显示 Label | 29a |
| **29d** | 建议项（lifecycle / cost / warrantyUntil / env 等）一键添加 | 29b |

## 7. 未覆盖边界

- **采集值不受字典约束**（有意）：一台机器的 `env` 由采集得到 `unknown` 时不会被拦下。
- **枚举改名/删值不迁移历史数据**：改枚举后，已存的人工值可能落在枚举外——**读取不受影响**（只校验写入），但界面会把它显示成原值 + 一个"不在枚举内"的提示（实现时要覆盖这条）。
- **不做到期提醒**：维保到期目前只是"一个日期属性"，不会自动告警（见 D8）。
- **跨类型一致性靠人**：`env` 在 host 与 middleware-instance 上要各配一次（见 D9）。
- **多实例 / 并发写**：`asset_types.schema` 是整列覆盖写（沿用现有 `PUT` 语义），并发编辑会互相覆盖——与今天的模型页同一限制。

## 8. 实施记录

### 批次 29a~29d（2026-10-08，未打包）

**29a 模型与校验（后端）**：

- `asset/model.go`：`AttrMeta` 从"只影响展示"扩到"约束人工值"——新增 `Kind`（text / number / date / enum / bool）、`Enum []EnumOption`（value + label）、`Required`；新增 `KindNormalized()`（空值与未知取值按 text，**读侧宽松**）、`ValidateAttrValue()`（空值一律放行；number / date / bool / enum 各有明确提示）、`validateAttrMeta()`（写侧：类型受支持、enum 有取值且不重复）；`ValidateTypeSchema` 纳入字典校验。
- `asset/service.go`：`Apply` 在写库前对 **manual 来源**的属性调用 `validateManualAttrs`（按 key 排序遍历，错误信息可预期）；`cleanAttrMeta` 修正「空壳判定」——只配了类型/必填的字段**不再被丢掉**（这正是"我配了却没生效"的典型来源）。
- 测试 `asset/attr_dict_test.go`（4 例）：取值校验正负例、字典写侧校验与读侧宽松、**字典只拦人工值不拦采集值**、模型改完立刻生效（缓存失效）。

**29b / 29c / 29d 前端**：

- `AssetModelView.vue`：字段表新增「类型」列与「枚举取值」编辑器（Value 进台账、显示名只影响界面）；「添加建议属性」一键把 `lifecycle`（在用 / 闲置 / 下线 / 退役）、`cost`、`warrantyUntil`、`env`、`system`、`assetNo` 加进**编辑态**（**不自动写入**）；表格数据改为「后端字段 ∪ 编辑态新增字段」（否则刚加的字段根本不显示）。
- `AssetListView.vue`：新建与编辑弹窗的「值」输入按字典渲染（enum 下拉 / date 选择 / bool 开关 / number 提示）；字典按类型懒加载并缓存，取不到时退回自由文本输入（不阻断编辑）；新建弹窗换类型时重载字典。
- 测试 `AssetModelView.test.js`（4 例）：两列渲染、**类型随模型一起提交**（守"被空壳判定吃掉"）、字典回填、建议属性加入并提交。

**验证**：`go build ./...`、`go test ./...`（31 包全绿）、前端 `vitest run` 全绿。

**已打包 1.30.58**（full 201 MiB / upgrade 81 MiB）。打包时本机 jq 损坏、python 不可用，`release.sh` 的 manifest 自校验被跳过，已**手工核对**：6 个二进制齐全、manifest 引用的 8 个 component（含易漏的 `deploy/agent-install.sh`）全在包内、`web/index.html` 在包内、server 二进制内嵌 `1.30.58`。

**已实机验证（2026-10-08，dev-server 1.30.58）**：`dist/asset-attr-e2e.py core` **41 项全过**——

- 字典落库：`env`（enum 2 项）/ `cost`（number + 单位）/ `warrantyUntil`（date）/ `isHA`（bool），以及**只配了「类型」的字段**（守 `cleanAttrMeta` 的修正：旧代码按「三项都空」把它丢掉）。
- 人工值校验：enum / number / date / bool 的非法值**全部 400 且点名到字段与可选值**；只配类型的字段同样被拦；合法值更新生效。
- 放行面：空值不触发校验、未配字典的字段维持自由键且值正常落库。
- **采集值不受影响（决定性判据）**：把真实采集值 `os = linux ubuntu 22.04` 配成枚举（枚举内不含它）后，**采集资产仍在、值原样保留**；而同一字段的**人工**写入被 400 拦住——证明校验只作用于人工值。
- 字典写侧：未知类型 / enum 无取值 / enum 取值重复均 400；只读身份（仅 `assets:read`）改字典 403、读模型 200。
- 收尾：`attrMeta` 恢复原状、测试资产彻底删除（验证不留残留）。

**界面已实测（同环境真实浏览器）**：模型页出现「类型」与「枚举取值」两列、「添加建议属性」弹窗含 6 个建议项（lifecycle·枚举 / cost·数字 / warrantyUntil·日期 / env·枚举 / system·文本 / assetNo·文本）；勾选后进入字段表编辑态，**保存后经 API 独立核对确实落库**；台账编辑弹窗填入 `lifecycle` 键后，值输入**从文本框变成下拉**，选项显示 label「在用 / 闲置 / 下线 / 退役」——即「字典懒加载 → 按类型渲染控件」这条链路在真实环境成立（vitest 用的是 mock，此前没被真实跑过）。截图 `dist/attr-dict-ui.png`。

**一处文档勘误（实现正确）**：§4.2 原写 bool「归一化成小写」，实际实现是**校验大小写不敏感、存储存原样**（与 `isDateLike` 的「存原样，不归一化」、`normalizeValue` 的「展示值始终保留原样」同口径）。已按实现改文档。

**未做**：`Required` 目前只是模型字段（**界面提示尚未接**，见 §7）。
