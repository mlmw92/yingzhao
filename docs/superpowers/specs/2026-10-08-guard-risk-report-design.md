# 组合护栏风险上报与展示设计

- 日期：2026-10-08
- 状态：待评审（D1~D7）
- 归属：能力全景表 §3.4 后第 3 条的既有 TODO——"**界面展示待做**（要把结论送到 Server 需新增上报字段，属后续小步）"
- 一句话：判定**已经有了**，本批只把它**送出去并显示出来**；不改判定。

---

## 1. 起因与依据

能力全景表 §3.4 后第 3 条（"组合护栏：可写「可执行目录」+ 可重启单元 = RCE"）末尾写着：

> **界面展示待做**（要把结论送到 Server 需新增上报字段，属后续小步）。

`docs/testing/2026-10-05-platform-validation-matrix.md:191` 的 P0 建议也是同一件事：
"把 Agent 的 `CheckGuardRisks` 组合风险告警**上报并展示**，不新增任意执行"。

**为什么值得做**：`CheckGuardRisks` 现在只在 Agent **启动时打一行 `slog.Warn`**——
那条日志**只有登机器看 journal 的人才知道**。而它描述的是一台机器"已经等于任意代码执行"的配置：
`file.push` 允许写的目录覆盖了某个可重启单元的可执行文件，或者覆盖了系统自己会执行的目录
（systemd/cron/profile.d/sudoers.d/root 的 `.ssh`）。**平台侧完全看不到这件事**，
配出这个组合的人也不会意识到自己配出了一个 RCE。

---

## 2. 现状（调研结论，均已核实到行）

| 事实 | 证据 |
|---|---|
| 判定已实现：两类风险 `exec`（可重启单元的可执行文件落在允许写入的目录里）/ `persistence`（允许写的是系统会自执行的目录） | `internal/agent/ops/guardcheck.go:67` `CheckGuardRisks`、`GuardRisk{Kind, AllowedDir, Exposed, Unit}` :22-34、`Describe()` :37 已给人话 |
| 目录清单已收敛到 `model.PersistenceDirs`（本批之前刚做的） | `guardcheck.go:50` |
| 运行时机：**Agent 启动时一次**，结论**只打日志** | `cmd/agent/main.go:148-150` |
| 上报体 `ReportPayload` 已有 `SecurityEvents` / `SecurityBaseline`，且**有"null vs []"的既有约定**（`Processes` / `Listeners`：null=旧 Agent 未采集，[]=已采集为空） | `internal/model/metric.go:100-152`（:114 / :149 的注释） |
| 服务端已有"按节点的状态表"范式可照抄：`baseline map[string]model.SecurityBaseline`、`defenseStatus map[string]*model.DefenseStatus`，含 load/save 与 `SaveDefenseStatus(node, st)` | `internal/server/security/store.go:54-86`、`:181`、`:245` |
| 展示位现成：安全中心页（事件表 / 基线明细卡片） | `web/src/components/SecurityView.vue` |

---

## 3. 设计

### 3.1 类型：搬到 `model`

`GuardRisk` 从 `agent/ops` 搬到 `internal/model`（连同 `Describe()`）：它要**随上报体序列化**，
而且界面要用同一句话（两处各写一份措辞必然不一致——`Describe()` 的注释里本来就写着"日志与后续的
界面展示共用，避免两处措辞不一致"，本批正好把它兑现）。

### 3.2 上报：`ReportPayload.GuardRisks`

```go
// GuardRisks 是本机护栏组合风险的当前清单（Agent 启动时判定一次）。
// **刻意不用 omitempty**：null = 旧 Agent 未上报，[] = 已检查且无风险——
// 沿用 Processes / Listeners 的既有约定。少了这个区分，界面会把"没上报"显示成"没有风险"，
// 而这两件事的处置完全不同（前者要去升级 Agent，后者是安全的）。
GuardRisks []GuardRisk `json:"guardRisks"`
```

Agent 侧：`CheckGuardRisks` 的结论（启动时算一次）存到采集器上，每轮上报都带上。
**判定时机不变**（仍是启动时一次）——护栏配置本身就是启动时读的，改了配置要重启 Agent 才生效，
这与 `guards.ops` 的既有语义一致；本批不引入"每轮重新 `systemctl show` 一遍"的开销。

### 3.3 服务端：状态表 + 接口

- `security.Store` 增加 `guardRisks map[string][]model.GuardRisk` 与
  `SaveGuardRisks(node, risks)`（照 `SaveDefenseStatus` 的形状），随 `save()` 落盘。
- 新增 `GET /api/v1/security/guard-risks`（权限 `security:read`，**按节点范围过滤**）：
  返回 `{nodes: [{node, displayName, risks: [...], reportedAt}], enabled}`。
- **不并入** `/security/baselines`：那一条是"合规评分"，这一条是"配置等于 RCE"，
  混在一张卡上会让人把两者当同一量级的提示。

### 3.4 展示（前端）

安全中心加一张卡「**护栏组合风险**」：

- 无事时不显示空卡（或显示一句"未发现"）；**未上报**（null）显示"该节点 Agent 未上报（需升级）"；
- 每条风险一行：节点 + 人话描述（`Describe()`）+ 命中类型（`exec` / `persistence`）+ 涉及的目录/单元；
- 卡片头部一句**下一步指引**："要消除它，去该机器的 `agent.yaml` 收掉 `guards.ops.file.dirs`
  或 `guards.ops.units` 里的一个"——只告警不拒绝是刻意的，但**必须告诉人怎么消掉**。

### 3.5 事件与告警（D3）

首次出现某条风险时，**派生一条去重的安全事件**（新类别 `guard_risk`，`critical` 级），
走既有的安全事件链路（因此也会进告警）。理由：

- **状态视图回答"现在有没有"**（改好配置 → 风险消失 → 卡片干净）；
- **事件回答"曾经有过"**（历史留痕，事后复盘"这台机器什么时候配出过 RCE 组合"）。
  两者不重复，缺一不可。
- 去重按 `节点 + 风险摘要`，与高危命令那批同一套思路（否则每次上报都来一条）。

---

## 4. 决策点（D1~D7）

| # | 问题 | 建议 | 理由 |
|---|---|---|---|
| **D1** | 上报成"状态"还是"事件" | **状态为主**（每次上报带当前清单） | 改好配置后风险应当**消失**；只做事件会留下一条永久的旧告警，人会被训练成忽略它 |
| **D2** | null 与 [] 要不要区分 | **必须区分**（不用 `omitempty`） | 沿用 `Processes`/`Listeners` 的既有约定；把"没上报"显示成"没有风险"是危险的误导（该机器可能是旧 Agent，压根没检查过） |
| **D3** | 要不要进告警 | **状态展示 + 首次出现派生一条去重事件**（因此会告警） | "配置等于 RCE"值得推一次；但只推一次（去重），且历史事件与当前状态各司其职 |
| **D4** | 判定要不要一起改 | **不改判定**，只做上报与展示 | 判定已被评审过（含"只告警不拒绝"的取舍）；本批是"小步"，把判定与传输分开，出问题时能定位到哪一层 |
| **D5** | 权限与范围 | `security:read` + 按节点范围过滤 | 与安全中心其它数据一致；风险描述里含**该机器的目录与单元名**，属于不该外泄的信息 |
| **D6** | 前端位置 | 安全中心新卡（不并入基线卡） | 见 §3.3：两者不是同一量级的提示 |
| **D7** | 升级面 | **改 Agent 上报体 → 两端都要升** | 与上一批（补传排空）同类；旧 Agent 不报该字段时按 D2 显示"未上报" |

---

## 5. 批次划分（两笔）

1. **上报与存储**：`model.GuardRisk`（搬移 + `Describe`）＋ Agent 侧把启动时的结论随上报带出
   ＋ `ReportPayload.GuardRisks` ＋ `security.Store.SaveGuardRisks` ＋ 派生去重事件 ＋ 用例。
2. **接口与前端**：`GET /api/v1/security/guard-risks`（含范围过滤）＋ 安全中心「护栏组合风险」卡
   ＋ 用例 ＋ 文档对账。

---

## 6. 实机验证计划（dev-server）

1. **空态**：该机当前**没有**组合风险（`guards.ops.file.dirs` 只有 `/tmp/nebula-filepush`，
   units 为空）→ 上报 `[]` → 界面显示"未发现"，**不是**"未上报"；
2. **有风险（决定性）**：临时把 `guards.ops` 配成风险组合（例如 `file.dirs` 加上 `/etc/cron.d`
   ——那是"系统自己会执行的目录"，**不需要** units 清单）→ 重启 Agent → 上报该风险 →
   接口能查到、界面能显示、且**派生了一条 `guard_risk` 事件**；**验完恢复配置并重启**；
3. **改回去就消失（决定性）**：恢复配置重启后 → 接口里该节点风险为空 → 但**事件仍在**（历史留痕）；
4. **未上报**：把 Agent 换回旧版本或清空该字段 → 界面显示"未上报"而不是"无风险"（这条靠用例覆盖即可）。

**未覆盖边界（不得视为通过）**：浏览器实点；`exec` 类风险（要真造一个"可写目录覆盖某单元可执行文件"
的组合，且该单元要真在 units 清单里——可在真机上造，但需谨慎选择不影响业务的单元，列入可选）；
多实例/HA。

---

## 7. 待评审

D1~D7 若都按建议，则按 §5 的两笔开工；有异议的挑出来（最可能被质疑的是 **D3 要不要进告警**）。
