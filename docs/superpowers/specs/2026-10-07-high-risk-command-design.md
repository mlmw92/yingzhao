# 高危命令拦截（危险参数 + 事后发现）设计

- 日期：2026-10-07
- 状态：待评审（D1~D8）
- 归属：能力全景表 §3.7 行 185「高危命令拦截」（P2 / M，2026-10-05 明确为**保留项**）
- 一句话：**不提供任意执行，所以没有"即将执行的任意命令"可拦**——本批做的是两件仍然成立的事：
  ① 让**危险参数**付出更高代价（强制第二个人 / 更强确认）；② 从**已有的 sudo 日志**里事后发现高危命令。

---

## 1. 起因与依据

行 185 原文（关键部分）：

> **与会话录制无关**：拦截可落在统一下行通道 / 命令审计上，**不需要会话代理**。2026-10-05 随「砍掉堡垒机」
> 明确为**保留项**；但落点随 3.4 的定界一起变了——**既然不提供任意命令执行，"拦住一条即将执行的任意命令"
> 这件事就不存在**，价值转到两处：① 对**参数化动作的危险参数**做拦截 / 二次确认（如 `file.push` 覆盖系统文件、
> `svc.restart` 核心单元）；② 从 **SSH / sudo 日志**里发现高危命令（**事后发现**，不是事前阻断）。

另有一条同域的既有 TODO（能力全景表 §3.4 后第 3 条末尾）："**界面展示待做**（要把结论送到 Server
需新增上报字段，属后续小步）"——那是 Agent 侧 `CheckGuardRisks` 的结论，属**另一件小事**（见 D7）。

---

## 2. 现状（调研结论，均已核实到行）

| 事实 | 证据 |
|---|---|
| **已有"危险组合"判定**：`file.push` 允许的目录覆盖了①系统会自执行的目录、②可重启单元的可执行文件目录 | `internal/agent/ops/guardcheck.go:67` `CheckGuardRisks`、`:50` `persistenceDirs`、`:119` `SystemdExecutables`；`GuardRisk.Describe()` :37 已给人话 |
| 但它**只在 Agent 启动时跑一次、只打 `slog.Warn`**，不上报、不拦截 | `cmd/agent/main.go:148-150` |
| 它只覆盖"**目录 × 单元**组合"，**不判断单条参数值**（`file.push` 写 `/etc/passwd`、`svc.restart` 重启 `sshd.service` 都不在它的视野里） | `guardcheck.go:75-100` |
| 参数校验**只保证"是合法值"**，完全不判断危险：path 正则只要求"绝对路径、每段不以 `.` 结尾"；unit 正则只要求 `字母数字@._-` + `.service` | `internal/model/ops.go:83`（`OpsFilePathPattern`）、`:58`（`OpsUnitPattern`） |
| 唯一现成的"危险值"判定是 **禁 setuid/setgid 的 mode 正则** | `model/ops.go:90` `OpsFileModePattern` |
| **"关键 systemd 单元"与"敏感文件路径"清单不存在** | 全仓无 `criticalUnit` 类清单；`defense.go:145,520` 只硬编码 `sshd.service` 用于探测 |
| **审批门已就绪**：五处入口共用一个 `approvalGate`，返回 202 + `pending` | `internal/server/api/approval_api.go:63`；调用点 `ops_api.go:196,346`、`defense_api.go:129`、`upgrade.go:63,131`、`query.go:821,861` |
| 但审批判定**只看 kind/readOnly，不看参数值** | `internal/server/approval/config.go:171` `RequiresApproval(kind, readOnly)`；`needsApproval` :47 也没接参数 |
| **sudo 命令全文已被采集**（在 `SecurityEvent.Detail["command"]` 里），存储/接口/前端齐备 | `internal/agent/collector/security.go:392` `collectSudo`（正则 :60 捕获 `COMMAND=` 之后全部）；`model/security.go:23`；`security/store.go:106` `Ingest`；`security_api.go:155`；`SecurityView.vue:150` |
| **SSH 侧没有命令文本，且认证日志里本来就没有** | `security.go:181` `collectSSH` 只捕获用户名/来源 IP/成败（正则 :49-55） |
| 前端确认框**只按 kind 区分**（`file.push` 多一句"写到哪"），不按参数值给不同危险提示 | `OpsView.vue:740`；`submitDispatch` :719 |
| 安全事件类别是硬编码清单，新增一类要改两处 | `SecurityView.vue:453-460`（`categoryOptions`）、`:488-497`（`catLabel`） |

---

## 3. 设计

### 3.1 总原则：这是"提高人为代价"，不是"授权判定"

平台**不提供任意执行**，目标机器是否放行由**它自己的 `guards.ops`** 决定（"机器自身的同意优先于中心的授权"）。
因此本批的判定**不是权限**，而是**提醒与代价**：

- **审批开启**时：危险参数**无条件**进审批（不看 `requireFor` 是否覆盖该动作）；
- **审批关闭**时：危险参数**不硬拦**（单管理员部署下硬拦等于把功能锁死），但
  ① 前端确认框必须**点名说清哪里危险**，② 服务端**写一条显式审计**（`Action: "danger-param"`）。

> 这条差别是本设计的核心取舍：**"危险"不等于"禁止"**。把危险参数直接拒掉，结果是运维改用别的手段
> （登机器手改）绕开平台——那比让风险可见更糟（与 §3.4 边界"只告警不拒绝"同一取向）。

### 3.2 危险清单与判定（落点①）

新增 **`internal/model/danger.go`**（放 `model` 是因为要两端共用：Agent 侧的 `guardcheck` 已经有
一份"系统会自执行的目录"清单，两端各写一份必然漂移——正则在 `model/ops.go` 已经是这个先例）：

```go
// DangerRule 一条危险判定规则。
type DangerRule struct {
    Kind   string   // 动作标识（svc.restart / file.push）
    Param  string   // 参数名（unit / path）
    Match  []string // 前缀或精确匹配的清单
    Exact  bool     // true=精确匹配（单元名），false=前缀匹配（目录/路径）
    Reason string   // 人话理由（必须写：误报会让人绕过审批）
}
```

清单**只收"必然危险"的**（D4），先给这些：

| 动作 | 参数 | 命中 | 理由 |
|---|---|---|---|
| `file.push` | `path` | 前缀命中**系统会自执行的目录**（`/etc/systemd/system/`、`/etc/init.d/`、`/etc/cron.d/`、`/etc/profile.d/`、`/etc/ld.so.conf.d/`、`/etc/sudoers.d/`、`/root/.ssh/`） | 写进去的内容会被系统自己执行（`guardcheck.go:50` 已有同一份清单，本批把它**收敛到一处**） |
| `file.push` | `path` | 精确命中**账户与凭据文件**（`/etc/passwd`、`/etc/shadow`、`/etc/sudoers`、`/root/.ssh/authorized_keys`、`/etc/ssh/sshd_config`） | 直接改变谁能登进这台机器 |
| `svc.restart` | `unit` | 精确命中**关键单元**（`sshd.service`、`systemd-logind.service`、`dbus.service`、`systemd-networkd.service`、`NetworkManager.service`、`firewalld.service`、`ufw.service`、`docker.service`、`containerd.service`、`kubelet.service`、`crond.service`/`cron.service`、`monitor-agent.service`） | 重启它可能让这台机器**失联**或让平台自己掉线 |
| `file.push` | `mode` | — | 已由 `OpsFileModePattern` 禁 setuid/setgid，无需重复 |

判定函数：`func DangerFindings(kind string, params map[string]string) []DangerFinding`，返回
`{Rule, Param, Value, Reason}` 列表（**列表**而不是布尔：一条动作可能同时命中多条，界面要能全列出来）。

### 3.3 接入（落点①）

- **判定点**：`ops_api.go:195`（单发）与 `:341`（批量）——两处都已经拿到 `ops.Validate` 归一化后的 `params`，
  这是"参数级"判定唯一的自然位置。
- **审批门**：`approvalGate` 增加一个"危险参数"入参（或在其内部按 kind+params 自查）——
  命中时**绕过 `requireFor` 判定直接进审批**（仅当审批开启）。
- **审计**：无论审批开关，命中危险参数都写一条 `Category: "ops"`, `Action: "danger-param"`，
  `Detail` 带动作、目标、命中的规则与理由。
- **前端**：`OpsView.vue` 的确认框按 `DangerFindings` **逐条列出理由**（现在只按 kind 给一句通用文案）。
  这是本批**用户能直接看见的价值**：写 `/etc/passwd` 与写 `/opt/app/conf` 的确认框不再是同一句话。

### 3.4 事后发现（落点②，只做 sudo 侧）

新增 **`internal/server/security/highrisk.go`**：

```go
// ScanCommand 从一条命令文本里找出高危特征，返回命中的规则与理由。
func ScanCommand(cmd string) []CommandFinding
```

规则（**只看"必然危险"的形态**，不做启发式）：`rm -rf /`（根目录递归删）、`dd ... of=/dev/`、
`mkfs`、`chmod -R 777 /`、`chown -R ... /`、`curl|sh` / `wget|sh`（管道执行下载内容）、
`> /etc/passwd`（重定向覆盖账户文件）、`iptables -F`、`history -c`（抹痕迹）、
`systemctl stop sshd` / `stop monitor-agent`（断自己的后路）、`kill -9 1`、`useradd` / `passwd` 改凭据。

接入点：**`security.Store.Ingest`**（`store.go:106`）——单一收口，任何来路的 sudo 事件都会过一遍。
命中则派生一条新类别的安全事件：

- 新增类别常量 `SecurityCatHighRiskCmd = "high_risk_cmd"`；
- 事件 `Message` 给人话（"高危命令：递归删除根目录"），`Detail` 带**命令原文**与命中的规则；
- **去重（D6）**：按 `node + 规则 + 命令摘要` 在窗口内只报一次（否则一条循环脚本会刷屏——
  去重是这类"事后发现"能不能被使用的前提）。

前端：`SecurityView.vue` 的 `categoryOptions` / `catLabel` 各加一项（展示位现成）。

---

## 4. 决策点（D1~D8）

| # | 问题 | 建议 | 理由 |
|---|---|---|---|
| **D1** | 范围 | **只做 ① + ② 的 sudo 侧**；SSH 侧命令**不做** | SSH 认证日志里**本来就没有命令**；要拿命令得引入 auditd/`bash_history`/PTY 采集——那正是行 185 明确**不需要**的会话代理方向，且是另一个量级的采集面 |
| **D2** | 清单写死还是可配置 | **写死，但集中在 `internal/model` 一处** | 清单是可评审的代码，比配置面更容易被审；集中一处避免两端漂移（Agent 侧 `guardcheck` 的目录清单同时收敛到这里） |
| **D3** | 危险参数的效果 | 审批**开启**时**无条件进审批**；审批**关闭**时**不硬拦**，但强提示 + 显式审计 | "危险"不等于"禁止"：硬拦会把人逼去登机器手改，风险反而看不见（与 §3.4「只告警不拒绝」同一取向） |
| **D4** | 误报控制 | 只收**必然危险**的清单项，每条必须写人话理由 | 误报会让人绕过审批；判定过严与过松都直接决定这项能力会不会被用起来 |
| **D5** | sudo 识别放哪 | `security.Store.Ingest`（单一收口） | 任何来路的 sudo 事件都过一遍，不会因为将来多一条上报路径而漏掉 |
| **D6** | 派生事件去重 | 按 `node + 规则 + 命令摘要` 在窗口内只报一次 | 不去重则一条 `while true; do sudo rm ...; done` 会刷屏，事件列表直接不可用 |
| **D7** | 组合护栏结论上报 | **本批不做**，列为后续小步 | 那是 §3.4 第 3 条的既有 TODO（"属后续小步"），需要**改 Agent 上报体**（两端都要升级）；本批刻意保持**只动 Server 与前端**，把风险面压小 |
| **D8** | 前端 | OpsView 确认框逐条列危险理由；SecurityView 加一类事件 | 这是用户唯一能直接看见的收益：同一个动作写不同路径，确认框不再长一个样 |

---

## 5. 批次划分（三笔）

1. **危险判定**：`internal/model/danger.go`（清单 + `DangerFindings`）+ 把 Agent 侧 `guardcheck` 的目录清单
   收敛到这一处 + 用例（含"不误报"的负例）。
2. **接入审批与审计**：`needsApproval`/`approvalGate` 接参数级判定 + 显式审计 + `OpsView` 确认框逐条理由 + 用例。
3. **sudo 事后发现**：`security/highrisk.go`（`ScanCommand`）+ `Ingest` 接入 + 去重 + 新事件类别 + 前端类别 + 用例。

**本批只动 Server 与前端**（D7）——不需要升 Agent。

---

## 6. 实机验证计划（dev-server）

脚本：`dist/highrisk-e2e.py`（`core` 跑核对、`cleanup` 只收尾）。

**sudo 侧怎么造日志**：用 `logger -p authpriv.notice -t sudo '<标记>: alice : TTY=pts/0 ; PWD=/root ; USER=root ; COMMAND=/bin/rm -rf /'`
注入——格式必须与 sudo 真实写法一致（采集正则要求 `sudo: <user> : … USER=… ; COMMAND=…`），
且要用 `authpriv` 设施才会落到 `/var/log/auth.log`（`logger` 默认进 `user.*`，那是 syslog 不是 auth.log）。
收尾用 `sed -i` 删掉带标记的行。

1. **危险参数 + 审批关闭（决定性）**：提交 `file.push` 写 `/etc/systemd/system/x.service`
   → 仍**直接下发（201）**（不硬拦），但审计里出现 `danger-param`，且理由点明"该目录会被系统自己执行"；
2. **危险参数 + 审批开启（决定性）**：把 `requireFor` 配成**不含该动作**的清单（例如只留 `["defense"]`），
   再提交一个**危险参数**的写动作 → **仍然进审批（202）**（危险参数绕过 `requireFor`）；
   对照：同一配置下提交一个**普通参数**的同一动作 → **直接 201 下发**（证明是"参数"在起作用，不是"动作"）；
3. **不误报**：`file.push` 写 `/opt/app/conf/app.conf`、`svc.restart` 重启 `nginx.service`
   → **不进审批、无 `danger-param` 审计**；
4. **sudo 事后发现**：用 `logger` 造一条含 `rm -rf /` 的 sudo 日志 → 安全事件里出现 `high_risk_cmd`
   且 `Detail` 带命令原文；**同一命令再灌一次 → 不新增事件**（去重）；
5. **不误报（sudo 侧）**：普通 `sudo systemctl status nginx` 不产生事件。

**未覆盖边界（不得视为通过）**：浏览器实点（确认框文案）；SSH 侧命令（D1 明确不做）；
组合护栏上报（D7）；清单在其它发行版上的完备性（清单是"必然危险"的子集，不是全集）。

---

## 7. 待评审

D1~D8 若都按建议，则按 §5 的三笔开工；有异议的挑出来（最可能被质疑的是 **D3 不硬拦** 与 **D1 不做 SSH 侧**）。

## 8. 实施记录（2026-10-07，Server 1.30.51）

三笔都已落地，D1~D8 全部按建议执行（**只动 Server 与前端**，未碰 Agent）。

**① 危险判定**：新增 `internal/model/danger.go`（`PersistenceDirs` / `SensitiveFiles` / `CriticalUnits`
三份清单 + `DangerRules` + `DangerFindings`）。**Agent 侧 `guardcheck.go` 的目录清单已收敛到这一处**
（`var persistenceDirs = model.PersistenceDirs`）——两端各写一份必然漂移，而漂移的表现是
"Agent 认为是持久化目录、Server 不认为"这种自相矛盾。清单**只收必然危险的项**：普通业务服务
（`nginx` / `mysql`）与普通路径（`/opt/app/...`）刻意不在内，否则每次重启都弹警告、最后没人看警告。

**② 接入审批与审计**：`approvalGate` 增加参数级判定——**不需要改签名**，因为 `approval.Request`
里本来就带着归一化后的 `Params`。行为：命中危险参数时**无论审批开关都先写一条 `danger-param` 审计**
（审批关闭时它是唯一留痕），且在**审批开启**时**绕过 `requireFor` 直接进审批**；
审批关闭时**不硬拦**（仍 201）。前端 `OpsView` 的确认框把命中的理由**逐条**列出，
标题也改成「确认下发（含危险参数）」。危险规则随动作目录接口下发（`dangerRules`），
**清单只有一份**，前端只做"精确/前缀"这点简单匹配。

**③ sudo 事后发现**：新增 `internal/server/security/highrisk.go`（13 条规则 + `ScanCommand`），
在 `security.Store.Ingest` **单一收口**接入（任何来路的 sudo 事件都会过一遍），命中即派生一条
`high_risk_cmd` 事件（`SeverityCritical`，`Detail` 带**命令原文**与规则）。**去重**按
`节点 + 规则 + 命令摘要 + 10 分钟窗口`——不去重则一条 `while true; do sudo rm -rf /; done`
会把事件列表刷满，而这类事件的全部价值就是"出现时值得马上看一眼"。

**实施中的两处要点**：

1. **规则必须把清单值一起下发给前端**。第一版 `DangerRules()` 只给了 `rule/kind/param/reason`，
   前端拿到也没法匹配——确认框就永远不提示。清单不是秘密（它是策略不是凭据），
   带出去换来的是"清单只有一份"，这比前端自己维护一份会漂移的名单好得多。
2. **严重级别用 `critical` 而不是 `warning`**：这一类的定位就是"值得马上看一眼"，
   落在 warning 里会和常规 sudo 流水混在一起，等于没有信号。

**测试**：后端 `go test ./...` 全绿；新增 `internal/model` **9 条**（各规则命中、**普通路径/普通单元
不误报**、多规则同时命中、规则自洽）、`internal/server/api` **4 条**（**审批关闭时危险参数仍 201
但留下 `danger-param` 审计**、**审批开启且 `requireFor` 不含该动作时危险参数仍进审批 202 + 对照组
普通参数 201**、普通参数不留危险审计、动作目录下发规则清单）、`internal/server/security` **6 条**
（13 条规则命中表、**不误报**表、只对 sudo 事件派生、事件形状、**同命令去重 + 出窗再报 + 跨节点独立**、
原始 sudo 事件仍保留）。前端 `npm test` **26 文件 152 项**全绿（新增 `api/ops.test.js` 5 条），
`npm run build` 通过。

**未覆盖边界（不得视为通过）**：浏览器实点（确认框文案）；SSH 侧命令（D1 明确不做）；
组合护栏结论上报（D7，需改 Agent 上报体，列为后续小步）；清单在其它发行版上的完备性
（是"必然危险"的子集，不是全集）；规则的**实际拦截效果**依赖真实 sudo 日志，实机只能造几条来验。

## 9. 实机验证结果（2026-10-07，dev-server 升 Server 1.30.52）

`dist/highrisk-e2e.py core`：**15 项通过 / 1 项失败**。失败的**只有 sudo 侧的端到端**（见下），
危险参数侧（本批主交付）**全部通过**，含两条决定性判据与对照组：

| # | 判据 | 实测 |
|---|---|---|
| ① | **危险参数 + 审批关闭（决定性）** | **201** 直接下发（不硬拦）✓ 且留下 `danger-param` 审计，**点名了参数值与理由** ✓ |
| ② | 普通参数不留危险审计 | 201 ✓，**没有** `danger-param` ✓ |
| ③ | **危险参数 + 审批开启但 `requireFor` 不含该动作（决定性）** | **202 仍然进审批** ✓ |
| ④ | **对照组**：同配置下普通参数 | **201 直接下发** ✓（证明是"参数"在起作用，不是"动作"） |
| ⑤ | sudo 侧端到端 | **未能验证**（原因见下） |

### 9.1 这一轮抓到的**真缺陷**（已修，1.30.52）

首轮注入 `COMMAND=/bin/rm -rf /` 后没有事件，而裸 `rm -rf /` 的用例是绿的。
用真实正则一验，**采集正则命中、高危规则没命中**：

```
采集正则命中: True     捕获的命令: '/bin/rm -rf / # e2e-highrisk-probe'
高危规则命中: False    ← 真缺陷
```

**sudo 日志记的是完整路径**（真实日志样例里也是 `COMMAND=/usr/bin/sed …`），而规则只匹配裸命令名
`rm` → **真实场景下几乎一条都命中不了**。这正是 e2e 的价值：单测覆盖的是我想象中的形态，
实机用的是真实形态。已修（抽出 `cmdPrefix`，边界字符集含 `=` 并允许绝对路径前缀；
管道右侧同样处理），并补了真实形态的回归用例与完整路径的负例。

### 9.2 仍未验证：sudo 侧的端到端（**既存状况，不是本批引入**）

修完规则后重跑，仍然没有事件。逐层诊断的结论：

- 注入的日志行**确实落到了** `/var/log/auth.log` ✓，且格式与采集正则**逐字匹配** ✓（已用真实正则验过 ✓）；
- 采集管线本身是活的 ✓（同一文件上的 `ssh_bruteforce` 事件一直在产，最新一条是 22:16:48 ✓）；
- 但 **`sudo_audit` 事件计数几小时未变**（始终 8 条）✗ —— 而 auth.log 里同期有 11 条真实的
  `COMMAND=` 行（都是真实 sudo 调用）✗。

即：**这台机器上的 `collectSudo` 没有产出新事件**。`collectSudo` 的代码注释里记着一个**同类旧坑**
（"早先复用 sshOff……表现为「sudo 审计一条都收不到」，且没有任何报错"），因此这**很可能是同一类问题的残留变体**。

### 9.3 已定位并修复（1.30.53，改的是 **Agent**）

根因是**四个参数的乘积**，不是某一行代码写错：

| 事实 | 值 |
|---|---|
| 日志读取偏移的存放 | **只在内存**（`sshOff`/`sudoOff` 是普通 map）→ **每次重启从字节 0 重读** |
| 单周期最多解析 | `securityMaxBytes = 4 MiB` |
| 上报周期 | 约 **5 分钟**（`ssh_bruteforce` 事件的时间戳正好 5 分钟一条） |
| dev-server 的 auth.log | **45 MB** |

→ 从字节 0 追到文件尾要约 **60 分钟**，而期间任何一次重启（升级/崩溃/手工重启）都让它从头再来
→ **sudo 采集长期卡在文件最前面那几 MiB**。库里那 4 条 `sudo_audit` 是 09-26/09-28/10-06 的——
**文件还小的时候它能读全，长过预算之后就不行了**，与观察完全吻合。
SSH 侧没有暴露同样症状，是因为它的暴力破解是**聚合**事件、从文件前部就能产出。

排查中**被证据否掉**的假设（都查过、都不是）：采集正则不匹配（用真实正则验过，逐字匹配）；
事件被 200 条上限截断（截断告警 0 条）；采集器每周期重建（只在构造时创建一次）；
偏移没跨周期推进（用 5 MiB 文件写了用例，绿）。

**修法两条并用**（`internal/agent/collector/security_offsets.go`）：① **持久化偏移**
（与 FIM 基线同目录的 `log_offsets.json`，原子写），重启接着上次读；② **冷启动跳到末尾窗口**
（没有可恢复状态时从"末尾往前 1 MiB"开始，并对齐到下一行行首）——历史日志没必要每装一次就重扫，
而"看不到新日志"是要命的那一侧。用例 4 条（冷启动读到末尾、跳过历史、偏移跨重启存活、轮转回退）。

**因此落点②的实机验证要在升到 1.30.53 之后重跑**（本批改的是 Agent，两端都要升级）。

**因此本批的 sudo 侧只能算"逻辑有用例覆盖"，不算实机已验证**：13 条规则、去重窗口、
不误报都有单测 ✓，但**整条链路（Agent 采集 → 上报 → Server 派生）在本机没有跑通过** ✗。
首轮那两条绿色的"去重生效""普通命令不误报"是**空对空**的（当时没有任何事件，去重与误报都无从谈起），
**不计入**。
