package report

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nebula/monitor/internal/model"
	"github.com/nebula/monitor/internal/server/analysis"
	"github.com/nebula/monitor/internal/server/asset"
	"github.com/nebula/monitor/internal/server/instancereg"
	"github.com/nebula/monitor/internal/server/logstore"
	"github.com/nebula/monitor/internal/server/mwreg"
	"github.com/nebula/monitor/internal/server/node"
	"github.com/nebula/monitor/internal/server/security"
	"github.com/nebula/monitor/internal/server/storage"
)

//go:embed template.html
var reportFS embed.FS

// oneHourMs 一小时的毫秒数，用于报告时间窗口换算。
const oneHourMs = int64(3600 * 1000)

// ReportType 报告类型。
type ReportType string

const (
	// ReportDaily 日报。
	ReportDaily ReportType = "daily"
	// ReportWeekly 周报。
	ReportWeekly ReportType = "weekly"
	// ReportMonthly 月报。
	ReportMonthly ReportType = "monthly"
)

// ReportMeta 报告元数据（用于历史列表）。
type ReportMeta struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Period    string `json:"period"`
	Generated int64  `json:"generatedAt"`
	Path      string `json:"path"`
	// Scope 是本次报告的生成口径（人读文案）。报告是会被下载转发的文件，
	// 历史列表里要能一眼看出「这一份覆盖的是谁」。
	Scope string `json:"scope,omitempty"`
	// Sections 是实际生成的章节 ID；Skipped 是因权限不足被跳过的章节标题。
	Sections []string `json:"sections,omitempty"`
	Skipped  []string `json:"skipped,omitempty"`
}

// ---- 报告范围与章节（设计件 docs/superpowers/specs/2026-10-08-reporting-views-design.md） ----
//
// 报告此前**不接资源范围**：一律 `nodeMgr.ListNodes()` 拉全部节点。本批把范围能力补上，
// 同时补上「生成者缺某域权限则跳过该章节」——报告是下载物，一旦生成就脱离了原接口的
// 权限校验，不这么做 `report:export` 就成了读取三域数据的旁路。

// 报告章节 ID：前四个是既有章节，后三个是 2026-10-08 设计件新增的三域章节。
const (
	SectionHosts      = "hosts"
	SectionMiddleware = "middleware"
	SectionSecurity   = "security"
	SectionAnalysis   = "analysis"
	SectionAssets     = "assets"
	SectionContainer  = "container"
	SectionLogs       = "logs"
)

// sectionTitles 章节中文名（「已跳过」提示与历史列表都用它）。
var sectionTitles = map[string]string{
	SectionHosts:      "主机巡检明细",
	SectionMiddleware: "中间件监控明细",
	SectionSecurity:   "安全中心巡检",
	SectionAnalysis:   "智能分析与预测",
	SectionAssets:     "资产与配置",
	SectionContainer:  "容器与工作负载",
	SectionLogs:       "日志",
}

// SectionTitle 返回章节中文名（未知 ID 原样返回）。
func SectionTitle(id string) string {
	if t, ok := sectionTitles[id]; ok {
		return t
	}
	return id
}

// ValidSection 判断章节 ID 是否合法。
func ValidSection(id string) bool {
	_, ok := sectionTitles[id]
	return ok
}

// SectionPermission 返回章节要求的额外权限点（空串表示只需报告自身的权限）。
//
// 只有三域章节有额外要求。既有章节维持原状：本批不改变既有报告的行为，只保证新增的
// 三域数据不会绕过各自接口的权限校验。
func SectionPermission(id string) string {
	switch id {
	case SectionAssets:
		return "assets:read"
	case SectionContainer:
		return "container:read"
	case SectionLogs:
		return "logs:read"
	}
	return ""
}

// ScopeSnapshot 就是台账的范围快照（`asset.ScopeSnapshot`）——报告的范围语义与资产域
// **同一套实现**（节点维度 + 业务标签维度，三态口径完全一致）。
//
// 刻意不在这里另写一份：`all` / `limited`+空 / 缺省按受限这套口径平台里已经定过，
// 两端各写一份必然漂移（`model/danger.go` 的目录清单就吃过这个亏）。
type ScopeSnapshot = asset.ScopeSnapshot

// ScopeResolution 是「身份 → 范围 + 权限跳过」的折算结果（由 API 层按 Principal 算出）。
//
// 为什么不让 report 包自己取身份：报告还要被**定时器**调用，而定时器没有请求上下文
// （平台里所有执行入口都靠请求上下文取 Principal）。因此范围必须在「有人」的那一侧折算，
// 定时侧存快照、执行时再按保存者当前身份取交集（只朝收窄方向变）。
type ScopeResolution struct {
	Scope ScopeSnapshot
	// Skipped 是生成者**无权限**的章节：ID → 原因。由 API 层按身份算出。
	Skipped map[string]string
}

// scopeCovers 判断节点是否在范围内（all 恒 true）。
func scopeCovers(s ScopeSnapshot, node string) bool {
	if s.NodeMode != asset.ScopeModeLimited {
		return true
	}
	for _, n := range s.Nodes {
		if n == node {
			return true
		}
	}
	return false
}

// GenerateOptions 是一次生成的选项。
type GenerateOptions struct {
	Type ReportType
	// Sections 是要生成的章节；空 = 全选（默认全开）。
	Sections []string
	// Scope 是资源范围（节点维度 + 业务标签维度）。
	Scope ScopeSnapshot
	// Skipped 是生成者**无权限**的章节（ID → 原因）；由 API 层按身份算出。
	Skipped map[string]string
	// Actor 是触发者（手动为登录名，定时为 schedule）。
	Actor string
}

// selected 判断某章节是否被选中（Sections 为空 = 全选）。
func (o GenerateOptions) selected(id string) bool {
	if len(o.Sections) == 0 {
		return true
	}
	for _, s := range o.Sections {
		if s == id {
			return true
		}
	}
	return false
}

// ScopeInfo 是渲染到报告头部的生成口径。
type ScopeInfo struct {
	Actor    string   `json:"actor"`
	Desc     string   `json:"desc"`
	Nodes    int      `json:"nodes"`
	Sections []string `json:"sections"`
}

// SkippedSection 是一条「因权限不足被跳过」的章节说明。
type SkippedSection struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Reason string `json:"reason"`
}

// ---- 资产与配置章节（设计件 §4.2） ----

// AssetsSection 是「资产与配置」章节的数据。
//
// 只放**聚合**：属性值、责任人姓名、变更的旧值/新值一律不进报告——报告是会被下载转发的文件，
// 明细属于「要看就去台账页看」。
type AssetsSection struct {
	Total    int `json:"total"`
	Missing  int `json:"missing"`
	NoOwner  int `json:"noOwner"`
	Conflict int `json:"conflict"`
	Ignored  int `json:"ignored"`
	// Changes 是窗口内发生变更的字段数（含采集与人工）。
	Changes int `json:"changes"`
	// ChangeWindowDays 是上面那个窗口的天数——写进报告，避免「近 N 天」变成猜谜。
	ChangeWindowDays int `json:"changeWindowDays"`

	LinksTotal   int              `json:"linksTotal"`
	NoLinkAssets int              `json:"noLinkAssets"`
	LinkGroups   []AssetLinkGroup `json:"linkGroups,omitempty"`

	Inspect *AssetInspectInfo `json:"inspect,omitempty"`
}

// AssetLinkGroup 是关系总览的一行（按 来源类型 × 关系种类 × 目标类型 × 边来源 聚合）。
type AssetLinkGroup struct {
	FromType string `json:"fromType"`
	Kind     string `json:"kind"`
	ToType   string `json:"toType"`
	Source   string `json:"source"`
	Count    int    `json:"count"`
}

// AssetInspectInfo 是最近一次配置巡检的结论摘要。
//
// 不给具体差异条目：那是配置巡检页的活，报告只回答「跑了多少、差了多少、什么级别」。
type AssetInspectInfo struct {
	At        int64          `json:"at"`
	Actor     string         `json:"actor"`
	Assets    int            `json:"assets"`
	Findings  int            `json:"findings"`
	Baselined int            `json:"baselined"`
	Truncated bool           `json:"truncated"`
	ByLevel   map[string]int `json:"byLevel,omitempty"`
	// Sampled 是统计分级时实际读到的差异项条数（有上限）；与 Findings 不等时说明这是采样值。
	Sampled int `json:"sampled,omitempty"`
}

// assetChangeWindowDays 与台账页「近 7 天变更」同口径。
const assetChangeWindowDays = 7

// ---- 容器与工作负载章节（设计件 §4.3） ----

// ContainerSection 是「容器与工作负载」章节的数据。
//
// **整章是「当前快照」**：集群清单与聚合指标都来自即时查询（`QueryAllLatest`），平台没有它们的
// 历史留存，因此不做周期对比。报告里必须显式标注，否则那串数字会被读成「这一周的趋势」。
type ContainerSection struct {
	// SnapshotNote 是「当前快照」的说明（模板直接渲染它，口径只写一处）。
	SnapshotNote string `json:"snapshotNote"`

	ClustersTotal int `json:"clustersTotal"`
	ClustersUp    int `json:"clustersUp"`
	NodesTotal    int `json:"nodesTotal"`
	NodesReady    int `json:"nodesReady"`
	PodsTotal     int `json:"podsTotal"`
	PodsRunning   int `json:"podsRunning"`
	PodsPending   int `json:"podsPending"`
	PodsFailed    int `json:"podsFailed"`
	// PodsAbnormal 是「有效状态不正常」的 Pod 数（含 phase=Running 但容器 CrashLoopBackOff 的）。
	// 与上面几个 phase 桶**不是一个口径**：那些是生命周期阶段，这个是健康度。
	PodsAbnormal int `json:"podsAbnormal"`

	Workloads []ContainerWorkloadStat `json:"workloads,omitempty"`
	Clusters  []ContainerCluster      `json:"clusters,omitempty"`
	// AssetCounts 是台账里的容器类资产计数（类型键 → 条数）。
	AssetCounts map[string]int `json:"assetCounts,omitempty"`
}

// ContainerCluster 是集群清单的一行。
type ContainerCluster struct {
	Node     string `json:"node"`
	Instance string `json:"instance"`
	Name     string `json:"name"`
	Version  string `json:"version"`
	Group    string `json:"group"`
	Up       bool   `json:"up"`
}

// ContainerWorkloadStat 是一类工作负载的健康计数。
type ContainerWorkloadStat struct {
	Kind      string `json:"kind"`
	Total     int    `json:"total"`
	Unhealthy int    `json:"unhealthy"`
}

// ---- 日志章节（设计件 §4.4） ----

// LogsSection 是「日志」章节的数据：如实转述后端**自己回答**的能力与存储现状。
//
// 不放日志正文——报告是会被下载转发的文件；也不新建按天扫描统计（要扫分片，与 P2/M 不符）。
type LogsSection struct {
	Backend       string   `json:"backend"`
	FullTextIndex bool     `json:"fullTextIndex"`
	FieldIndex    bool     `json:"fieldIndex"`
	Notes         []string `json:"notes,omitempty"`
	ScanBudgetMB  int      `json:"scanBudgetMB,omitempty"`
	ScanBudgetLines int    `json:"scanBudgetLines,omitempty"`

	Sources   int    `json:"sources"`
	Nodes     int    `json:"nodes"`
	OldestDay string `json:"oldestDay,omitempty"`
	NewestDay string `json:"newestDay,omitempty"`
	Bytes     int64  `json:"bytes,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	// ProbeError 非空表示**探测不到**（不是「没有日志」）。
	ProbeError string `json:"probeError,omitempty"`
	// StorageNote 说明为什么存储统计是空的（受限身份下不展示全局口径的统计）。
	StorageNote string `json:"storageNote,omitempty"`
}

// allSections 是全部章节 ID（按报告里的出现顺序）。
var allSections = []string{
	SectionHosts, SectionMiddleware, SectionSecurity, SectionAnalysis,
	SectionAssets, SectionContainer, SectionLogs,
}

// scopeDesc 生成人读的范围描述（报告头部与历史列表都用它）。
func scopeDesc(s ScopeSnapshot, nodeCount int) string {
	if s.NodeMode != asset.ScopeModeLimited {
		return fmt.Sprintf("全部节点（%d 台）", nodeCount)
	}
	return fmt.Sprintf("指定 %d 个节点（%d 台在范围内）", len(s.Nodes), nodeCount)
}

// chartItem 一张概览图表（内联 SVG，以 template.HTML 类型避免被转义）。
type chartItem struct {
	Title string        `json:"title"`
	SVG   template.HTML `json:"svg"`
	Kind  string        `json:"kind"` // bar / line
}

// summaryStat 巡检概览指标。
type summaryStat struct {
	Total   int     `json:"total"`
	Online  int     `json:"online"`
	Offline int     `json:"offline"`
	CPUAvg  float64 `json:"cpuAvg"`
	CPUMax  float64 `json:"cpuMax"`
	MemAvg  float64 `json:"memAvg"`
	MemMax  float64 `json:"memMax"`
	DiskAvg float64 `json:"diskAvg"`
	DiskMax float64 `json:"diskMax"`
}

// hostTrend 单个主机的资源趋势（结构化，供前端绘制）。
type hostTrend struct {
	Node string      `json:"node"`
	CPU  []linePoint `json:"cpu"`
	Mem  []linePoint `json:"mem"`
	Disk []linePoint `json:"disk"`
}

// nodeStat 单主机巡检明细。
type nodeStat struct {
	Name        string  `json:"name"`
	IP          string  `json:"ip"`
	OS          string  `json:"os"`
	Group       string  `json:"group"`
	Status      string  `json:"status"` // online/offline
	Health      string  `json:"health"` // healthy/warning/critical
	HealthScore float64 `json:"healthScore"`
	CPUAvg      float64 `json:"cpuAvg"`
	CPUMax      float64 `json:"cpuMax"`
	MemAvg      float64 `json:"memAvg"`
	MemMax      float64 `json:"memMax"`
	DiskAvg     float64 `json:"diskAvg"`
	DiskMax     float64 `json:"diskMax"`
	LoadAvg     float64 `json:"loadAvg"`
	LastSeen    int64   `json:"lastSeen"`

	// 深化指标：网络流量、磁盘 IO、磁盘分区、Top 进程
	NetIfaces  []netIfaceStat `json:"netIfaces"`
	DiskIO     []diskIOStat   `json:"diskIO"`
	Partitions []diskPartStat `json:"partitions"`
	Processes  []procStat     `json:"processes"`
}

// mwInstance 单个中间件实例巡检明细，覆盖「连接数 / 响应时间 / 内存使用率 / 命中率」。
type mwInstance struct {
	Type       string             `json:"type"`
	Node       string             `json:"node"`
	Instance   string             `json:"instance"`
	Role       string             `json:"role"`
	Topology   string             `json:"topology"`
	Version    string             `json:"version"`
	Up         bool               `json:"up"`
	ConnUsed   float64            `json:"connUsed"`
	ConnMax    float64            `json:"connMax"`
	ConnPct    float64            `json:"connPct"`
	RespTime   float64            `json:"respTime"`   // 平均响应时间/时延（ms）
	MemPct     float64            `json:"memPct"`     // 内存使用率 %
	MemUsedMB  float64            `json:"memUsedMB"`  // 内存用量（MB）
	HitRate    float64            `json:"hitRate"`    // 命中率 %
	Throughput float64            `json:"throughput"` // 吞吐（ops/s 或 qps）
	Extra      string             `json:"extra"`
	Status     string             `json:"status"`
	Trend      []linePoint        `json:"trend"`
	Metrics    map[string]float64 `json:"metrics"`
}

// finding 一条具体巡检发现：问题描述 + 影响范围 + 修复建议。
type finding struct {
	Severity   string `json:"severity"`
	Category   string `json:"category"`
	Resource   string `json:"resource"`
	Title      string `json:"title"`
	Detail     string `json:"detail"`
	Impact     string `json:"impact"`
	Suggestion string `json:"suggestion"`
}

// reportData 是报告模板的数据模型。
type reportData struct {
	Period      string       `json:"period"`
	GeneratedAt int64        `json:"generatedAt"`
	Summary     summaryStat  `json:"summary"`
	Charts      []chartItem  `json:"charts"`
	Nodes       []nodeStat   `json:"nodes"`
	Middleware  []mwInstance `json:"middleware"`
	Findings    []finding    `json:"findings"`

	// 增强段：巡检结论/健康评级、环比、安全中心
	Conclusion   Conclusion           `json:"conclusion"`
	Comparison   *Comparison          `json:"comparison,omitempty"`
	Security     *SecuritySection     `json:"security,omitempty"`
	Intelligence *intelligenceSection `json:"intelligence,omitempty"`

	// 生成口径：这份文件要能解释自己（谁生成的、覆盖谁、跳过了什么）
	Scope   ScopeInfo        `json:"scope"`
	Skipped []SkippedSection `json:"skipped,omitempty"`

	// 三域章节（2026-10-08 设计件）：只放聚合，不放明细
	Assets    *AssetsSection    `json:"assets,omitempty"`
	Container *ContainerSection `json:"container,omitempty"`
	Logs      *LogsSection      `json:"logs,omitempty"`
}

type intelligenceSection struct {
	WindowHours           int                   `json:"windowHours"`
	ReadyNodeCount        int                   `json:"readyNodeCount"`
	InsufficientNodeCount int                   `json:"insufficientNodeCount"`
	RiskCount             int                   `json:"riskCount"`
	AnomalyCount          int                   `json:"anomalyCount"`
	UrgentCapacityCount   int                   `json:"urgentCapacityCount"`
	Hosts                 []analysis.HostResult `json:"hosts"`
}

// netIfaceStat 单网卡流量统计。
type netIfaceStat struct {
	Name     string  `json:"name"`
	RecvMBps float64 `json:"recvMBps"`
	SentMBps float64 `json:"sentMBps"`
	DropPps  float64 `json:"dropPps"`
}

// diskIOStat 单磁盘设备 IO 统计。
type diskIOStat struct {
	Device    string  `json:"device"`
	ReadMBps  float64 `json:"readMBps"`
	WriteMBps float64 `json:"writeMBps"`
	ReadIOPS  float64 `json:"readIOPS"`
	WriteIOPS float64 `json:"writeIOPS"`
}

// diskPartStat 单挂载点使用情况。
type diskPartStat struct {
	Mount   string  `json:"mount"`
	Device  string  `json:"device"`
	Fstype  string  `json:"fstype"`
	UsedPct float64 `json:"usedPct"`
	UsedGB  float64 `json:"usedGB"`
	TotalGB float64 `json:"totalGB"`
}

// procStat 进程资源占用快照。
type procStat struct {
	PID    int32   `json:"pid"`
	Name   string  `json:"name"`
	CPUPct float64 `json:"cpuPct"`
	MemPct float64 `json:"memPct"`
}

// Conclusion 巡检结论与健康评级。
type Conclusion struct {
	Rating         string         `json:"rating"`      // 健康/关注/预警
	RatingClass    string         `json:"ratingClass"` // healthy/warning/critical
	Score          float64        `json:"score"`       // 综合健康评分
	TotalFindings  int            `json:"totalFindings"`
	SeverityCounts map[string]int `json:"severityCounts"`
	TopRisks       []string       `json:"topRisks"`
	Summary        string         `json:"summary"`
}

// Comparison 与上一周期的环比对比。
type Comparison struct {
	OnlineRate      float64 `json:"onlineRate"`
	PrevOnlineRate  float64 `json:"prevOnlineRate"`
	CPUAvg          float64 `json:"cpuAvg"`
	PrevCPUAvg      float64 `json:"prevCPUAvg"`
	MemAvg          float64 `json:"memAvg"`
	PrevMemAvg      float64 `json:"prevMemAvg"`
	DiskMax         float64 `json:"diskMax"`
	PrevDiskMax     float64 `json:"prevDiskMax"`
	HealthScore     float64 `json:"healthScore"`
	PrevHealthScore float64 `json:"prevHealthScore"`
}

// SecuritySection 安全中心巡检发现。
type SecuritySection struct {
	HasData        bool                  `json:"hasData"`
	NodeCount      int                   `json:"nodeCount"`
	AvgScore       float64               `json:"avgScore"`
	LowScoreNodes  int                   `json:"lowScoreNodes"`
	CriticalEvents int                   `json:"criticalEvents"`
	WarningEvents  int                   `json:"warningEvents"`
	CVECount       int                   `json:"cveCount"`
	RiskNodes      []string              `json:"riskNodes"`
	LowScores      map[string]float64    `json:"lowScores"`
	Events         []model.SecurityEvent `json:"events"`
}

// Generator 巡检报告生成服务。
type Generator struct {
	store    storage.Storage
	nodeMgr  *node.Manager
	secStore *security.Store
	dir      string
	analyzer *analysis.Analyzer
	// mwRegistry 为中间件类型注册表（内置类型 + 模板派生类型）；未注入时退化为内置类型。
	mwRegistry *mwreg.Registry
	// assets 是资产台账聚合（未注入时报告不含「资产与配置」章节——库不可用不等于台账为空）。
	assets assetsReporter
	// logs 是日志后端（未注入时报告不含「日志」章节——未接入不等于没有日志）。
	logs logstore.LogStore

	mu      sync.Mutex
	history []ReportMeta
}

// assetsReporter 是报告需要的台账聚合能力（由 asset.Service 实现）。
//
// 抽成窄接口而不是直接持有 *asset.Service：与 API 包的 AssetProvider 同一取向——只依赖真正
// 用到的方法集合，测试里也才能用替身钉住「按什么条件取数」。
type assetsReporter interface {
	Stats(f asset.ListFilter, changesSince int64) (asset.Stats, error)
	LinkOverview(scope asset.ChangeScope) (asset.LinkOverview, error)
	InspectRunsInNodes(limit int, nodes []string) ([]asset.InspectRun, error)
	InspectFindingsInNodes(runID int64, limit int, nodes []string) ([]asset.InspectFinding, bool, error)
	// Count 用于容器章节的「台账里有多少容器类资产」——与列表共用条件（含资源范围）。
	Count(f asset.ListFilter) (int, error)
}

// SetAssetService 注入资产台账（可选）。
func (g *Generator) SetAssetService(svc assetsReporter) { g.assets = svc }

// SetLogStore 注入日志后端（可选）。
func (g *Generator) SetLogStore(s logstore.LogStore) { g.logs = s }

// SetMiddlewareRegistry 注入中间件类型注册表。未注入时报告只含内置类型（行为与改造前一致）。
func (g *Generator) SetMiddlewareRegistry(r *mwreg.Registry) { g.mwRegistry = r }

// mwTypes 返回报告要遍历的中间件类型（未注入注册表时用内置类型单例）。
func (g *Generator) mwTypes() []mwreg.Type {
	if g.mwRegistry != nil {
		return g.mwRegistry.Types()
	}
	return mwreg.BuiltinOnly().Types()
}

// NewGenerator 构造报告生成器。dir 为报告 HTML 存储目录。
func NewGenerator(store storage.Storage, mgr *node.Manager, secStore *security.Store, dir string) *Generator {
	if dir == "" {
		dir = "reports"
	}
	_ = os.MkdirAll(dir, 0o755)
	g := &Generator{store: store, nodeMgr: mgr, secStore: secStore, dir: dir}
	g.history = g.loadHistory()
	return g
}

// SetAnalyzer 注入可选智能分析服务；分析失败不影响原有巡检报告生成。
func (g *Generator) SetAnalyzer(analyzer *analysis.Analyzer) { g.analyzer = analyzer }

// Generate 生成指定类型的报告（全部章节、不限范围），返回报告 ID。
//
// 保留这个签名是为了不打扰既有调用方（调度器与测试）；需要范围或章节选择时用 GenerateWith。
func (g *Generator) Generate(rt ReportType) (string, error) {
	return g.GenerateWith(GenerateOptions{Type: rt})
}

// GenerateWith 按给定选项生成报告，返回报告 ID。
func (g *Generator) GenerateWith(opts GenerateOptions) (string, error) {
	rt := opts.Type
	if rt == "" {
		rt = ReportDaily
	}
	now := time.Now()
	var start, end time.Time
	switch rt {
	case ReportWeekly:
		end = now
		start = now.AddDate(0, 0, -7)
	case ReportMonthly:
		end = now
		start = now.AddDate(0, -1, 0)
	default:
		end = now
		start = now.AddDate(0, 0, -1)
	}
	data := g.collectData(opts, start, end, string(rt))
	html := renderHTML(data)
	id := fmt.Sprintf("%s-%s", string(rt), now.Format("20060102-150405"))
	path := filepath.Join(g.dir, id+".html")
	if err := os.WriteFile(path, []byte(html), 0o644); err != nil {
		return "", err
	}
	meta := ReportMeta{
		ID:        id,
		Type:      string(rt),
		Period:    data.Period,
		Generated: data.GeneratedAt,
		Path:      path,
		Scope:     data.Scope.Desc,
		Sections:  data.Scope.Sections,
	}
	for _, s := range data.Skipped {
		meta.Skipped = append(meta.Skipped, s.Title)
	}
	g.mu.Lock()
	g.history = append(g.history, meta)
	if len(g.history) > 50 {
		g.history = g.history[len(g.history)-50:]
	}
	g.persistHistoryLocked()
	g.mu.Unlock()
	return id, nil
}

// GetHTML 返回指定报告 ID 的 HTML 内容。
func (g *Generator) GetHTML(id string) (string, error) {
	path := filepath.Join(g.dir, id+".html")
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// History 返回报告历史列表（最新在前）。
func (g *Generator) History() []ReportMeta {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]ReportMeta, len(g.history))
	copy(out, g.history)
	// 倒序：最新在前
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// ReportStats 是报告文件的统计快照。
type ReportStats struct {
	Files int   `json:"files"`
	Bytes int64 `json:"bytes"`
}

// PruneResult 是一次报告清理的结果。
type PruneResult struct {
	Files   int   `json:"files"`
	History int   `json:"history"`
	Bytes   int64 `json:"bytes"`
}

// Stats 返回报告目录中 HTML 文件的数量与总占用。
func (g *Generator) Stats() ReportStats {
	entries, err := os.ReadDir(g.dir)
	if err != nil {
		return ReportStats{}
	}
	var out ReportStats
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".html" {
			continue
		}
		out.Files++
		if info, err := e.Info(); err == nil {
			out.Bytes += info.Size()
		}
	}
	return out
}

// PruneBefore 删除生成时间早于 cutoff 的报告文件，并同步裁剪历史记录。
//
// 文件与历史必须一起处理：只删文件会让历史列表出现点开即 404 的条目，
// 只删历史则文件永远留在磁盘上（这正是本项目报告目录无界增长的成因）。
func (g *Generator) PruneBefore(cutoff time.Time) PruneResult {
	var out PruneResult
	entries, err := os.ReadDir(g.dir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".html" {
				continue
			}
			info, err := e.Info()
			if err != nil || !info.ModTime().Before(cutoff) {
				continue
			}
			if err := os.Remove(filepath.Join(g.dir, e.Name())); err != nil {
				continue
			}
			out.Files++
			out.Bytes += info.Size()
		}
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	cutoffMs := cutoff.UnixMilli()
	kept := make([]ReportMeta, 0, len(g.history))
	for _, item := range g.history {
		if item.Generated > 0 && item.Generated < cutoffMs {
			out.History++
			continue
		}
		kept = append(kept, item)
	}
	if out.History > 0 {
		g.history = kept
		g.persistHistoryLocked()
	}
	return out
}

func (g *Generator) historyPath() string {
	return filepath.Join(g.dir, "history.json")
}

// Delete 删除单份报告（HTML 文件 + 历史条目）。
//
// 文件与历史必须一起删：只删文件会让历史列表出现点开即 404 的条目，
// 只删历史则文件永久留在磁盘上（与 PruneBefore 同一套取舍）。
func (g *Generator) Delete(id string) error {
	if id == "" || strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return fmt.Errorf("非法报告 ID: %q", id)
	}
	path := filepath.Join(g.dir, id+".html")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	kept := make([]ReportMeta, 0, len(g.history))
	found := false
	for _, item := range g.history {
		if item.ID == id {
			found = true
			continue
		}
		kept = append(kept, item)
	}
	if found {
		g.history = kept
		g.persistHistoryLocked()
	}
	return nil
}

func (g *Generator) loadHistory() []ReportMeta {
	b, err := os.ReadFile(g.historyPath())
	if err != nil {
		return nil
	}
	var hs []ReportMeta
	if json.Unmarshal(b, &hs) != nil {
		return nil
	}
	return hs
}

func (g *Generator) persistHistoryLocked() {
	b, _ := json.MarshalIndent(g.history, "", "  ")
	_ = os.WriteFile(g.historyPath(), b, 0o644)
}

// ---- 数据收集 ----

func (g *Generator) collectData(opts GenerateOptions, start, end time.Time, period string) reportData {
	startMs := start.UnixMilli()
	endMs := end.UnixMilli()
	step := oneHourMs
	scope := opts.Scope

	// 章节要生成 = 用户选了它 **且** 生成者有权看它。权限不足的章节在这里就断掉，
	// 而不是「先生成了再隐藏」——报告是会被下载转发的文件。
	denied := func(id string) bool {
		_, bad := opts.Skipped[id]
		return bad
	}
	want := func(id string) bool { return opts.selected(id) && !denied(id) }

	// 资源范围：报告此前一律拉全部节点，本批起按范围收窄。
	all := g.nodeMgr.ListNodes()
	nodes := make([]model.Node, 0, len(all))
	for _, n := range all {
		if scopeCovers(scope, n.Hostname) {
			nodes = append(nodes, n)
		}
	}
	online, offline := 0, 0
	for _, n := range nodes {
		if n.Status == "online" {
			online++
		} else {
			offline++
		}
	}

	nodeStats := make([]nodeStat, 0, len(nodes))
	trends := make([]hostTrend, 0, len(nodes))
	var sumCPUAvg, sumMemAvg, sumDiskAvg, sumCPUMax, sumMemMax, sumDiskMax float64

	for _, n := range nodes {
		ns := nodeStat{Name: n.Hostname, IP: n.IP, OS: n.OS, Group: n.Group, Status: n.Status, LastSeen: n.LastSeen}
		var cpuPts, memPts, diskPts []linePoint
		if s, err := g.store.QueryRange(n.Hostname, "cpu_usage", nil, startMs, endMs, step); err == nil {
			ns.CPUAvg, ns.CPUMax = avgMax(s)
			cpuPts = toPoints(s)
		}
		if s, err := g.store.QueryRange(n.Hostname, "mem_used_percent", nil, startMs, endMs, step); err == nil {
			ns.MemAvg, ns.MemMax = avgMax(s)
			memPts = toPoints(s)
		}
		if s, err := g.store.QueryRange(n.Hostname, "disk_used_percent", nil, startMs, endMs, step); err == nil {
			ns.DiskAvg, ns.DiskMax = avgMax(s)
			diskPts = toPoints(s)
		}
		if s, err := g.store.QueryRange(n.Hostname, "load1", nil, startMs, endMs, step); err == nil {
			if v, ok := lastValue(s); ok {
				ns.LoadAvg = v
			}
		}
		// 深化指标：网络流量、磁盘 IO、磁盘分区、Top 进程
		ns.NetIfaces = g.collectNetwork(n.Hostname, startMs, endMs, step)
		ns.DiskIO = g.collectDiskIO(n.Hostname, startMs, endMs, step)
		ns.Partitions = g.collectPartitions(n.Hostname, startMs, endMs)
		if pl := g.nodeMgr.LastPayload(n.Hostname); pl != nil {
			procs := make([]procStat, 0, len(pl.Processes))
			for _, p := range pl.Processes {
				procs = append(procs, procStat{PID: p.PID, Name: p.Name, CPUPct: p.CPU, MemPct: p.Mem})
			}
			sort.Slice(procs, func(i, j int) bool { return procs[i].CPUPct > procs[j].CPUPct })
			if len(procs) > 10 {
				procs = procs[:10]
			}
			ns.Processes = procs
		}
		ns.Health, ns.HealthScore = evalHostHealth(ns)
		nodeStats = append(nodeStats, ns)
		if n.Status == "online" {
			sumCPUAvg += ns.CPUAvg
			sumMemAvg += ns.MemAvg
			sumDiskAvg += ns.DiskAvg
			sumCPUMax += ns.CPUMax
			sumMemMax += ns.MemMax
			sumDiskMax += ns.DiskMax
		}
		trends = append(trends, hostTrend{Node: n.Hostname, CPU: cpuPts, Mem: memPts, Disk: diskPts})
	}

	onlineN := online
	if onlineN == 0 {
		onlineN = 1
	}
	summary := summaryStat{
		Total:   len(nodes),
		Online:  online,
		Offline: offline,
		CPUAvg:  round1(sumCPUAvg / float64(onlineN)),
		CPUMax:  round1(sumCPUMax / float64(onlineN)),
		MemAvg:  round1(sumMemAvg / float64(onlineN)),
		MemMax:  round1(sumMemMax / float64(onlineN)),
		DiskAvg: round1(sumDiskAvg / float64(onlineN)),
		DiskMax: round1(sumDiskMax / float64(onlineN)),
	}

	var mw []mwInstance
	if want(SectionMiddleware) {
		mw = g.collectMiddleware(scope, startMs, endMs, step)
	}
	var charts []chartItem
	if want(SectionHosts) {
		charts = buildCharts(nodeStats, trends)
	}
	var sec *SecuritySection
	if want(SectionSecurity) {
		sec = g.collectSecurity(scope, startMs, endMs)
	}
	var assets *AssetsSection
	if want(SectionAssets) {
		assets = g.collectAssets(scope)
	}
	var container *ContainerSection
	if want(SectionContainer) {
		container = g.collectContainer(scope)
	}
	var logs *LogsSection
	if want(SectionLogs) {
		logs = g.collectLogs(scope)
	}
	findings := buildFindings(nodeStats, mw)
	findings = append(findings, buildSecurityFindings(sec)...)
	sevOrder := map[string]int{"critical": 0, "warning": 1, "info": 2}
	sort.SliceStable(findings, func(i, j int) bool {
		return sevOrder[findings[i].Severity] < sevOrder[findings[j].Severity]
	})
	conclusion := g.buildConclusion(nodeStats, findings, sec)
	comparison := g.buildComparison(start, end, nodeStats, summary)
	var intelligence *intelligenceSection
	if want(SectionAnalysis) && g.analyzer != nil {
		result := g.analyzer.Summary(7*24*time.Hour, false)
		// 智能分析的 Hosts 也要按范围收窄：它是逐主机的结论，不能漏出范围外的主机。
		hosts := make([]analysis.HostResult, 0, len(result.Hosts))
		for _, h := range result.Hosts {
			if scopeCovers(scope, h.Node) {
				hosts = append(hosts, h)
			}
		}
		intelligence = &intelligenceSection{
			WindowHours: result.WindowHours, ReadyNodeCount: result.ReadyNodeCount, InsufficientNodeCount: result.InsufficientNodeCount,
			RiskCount: result.RiskCount, AnomalyCount: result.AnomalyCount, UrgentCapacityCount: result.UrgentCapacityCount, Hosts: hosts,
		}
	}

	// 生成口径：谁生成的、覆盖谁、实际出了哪些章节、哪些被跳过
	sections := make([]string, 0, len(allSections))
	for _, id := range allSections {
		if want(id) {
			sections = append(sections, id)
		}
	}
	skipped := make([]SkippedSection, 0, len(opts.Skipped))
	for id, reason := range opts.Skipped {
		// 只报「确实被跳过」的：用户自己没选的章节不算被跳过
		if opts.selected(id) {
			skipped = append(skipped, SkippedSection{ID: id, Title: SectionTitle(id), Reason: reason})
		}
	}
	sort.Slice(skipped, func(i, j int) bool { return skipped[i].ID < skipped[j].ID })

	return reportData{
		Period:       fmt.Sprintf("%s ~ %s", start.Format("2006-01-02 15:04"), end.Format("2006-01-02 15:04")),
		GeneratedAt:  time.Now().UnixMilli(),
		Summary:      summary,
		Charts:       charts,
		Nodes:        nodeStats,
		Middleware:   mw,
		Findings:     findings,
		Conclusion:   conclusion,
		Comparison:   comparison,
		Security:     sec,
		Intelligence: intelligence,
		Assets:       assets,
		Container:    container,
		Logs:         logs,
		Scope: ScopeInfo{
			Actor:    opts.Actor,
			Desc:     scopeDesc(scope, len(nodes)),
			Nodes:    len(nodes),
			Sections: sections,
		},
		Skipped: skipped,
	}
}

// ---- 主机深化指标采集 ----

// collectNetwork 聚合单主机各网卡的最新收发速率与丢包速率。
func (g *Generator) collectNetwork(node string, startMs, endMs, step int64) []netIfaceStat {
	recv, _ := g.store.QueryAllLatest("network_recv_rate", map[string]string{"node": node})
	sent, _ := g.store.QueryAllLatest("network_sent_rate", map[string]string{"node": node})
	drop, _ := g.store.QueryAllLatest("network_drop_rate", map[string]string{"node": node})
	byIface := map[string]*netIfaceStat{}
	get := func(iface string) *netIfaceStat {
		if iface == "" {
			iface = "eth0"
		}
		if byIface[iface] == nil {
			byIface[iface] = &netIfaceStat{Name: iface}
		}
		return byIface[iface]
	}
	add := func(series []model.Series, kind string) {
		for _, s := range series {
			iface := s.Labels["iface"]
			if iface == "" {
				iface = s.Labels["device"]
			}
			st := get(iface)
			if len(s.Points) > 0 {
				v := s.Points[len(s.Points)-1].Value
				switch kind {
				case "recv":
					st.RecvMBps = v / 1e6
				case "sent":
					st.SentMBps = v / 1e6
				case "drop":
					st.DropPps = v
				}
			}
		}
	}
	add(recv, "recv")
	add(sent, "sent")
	add(drop, "drop")
	out := make([]netIfaceStat, 0, len(byIface))
	for _, st := range byIface {
		out = append(out, *st)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].RecvMBps+out[i].SentMBps > out[j].RecvMBps+out[j].SentMBps
	})
	return out
}

// collectDiskIO 聚合单主机各磁盘设备的最新读写速率与 IOPS。
func (g *Generator) collectDiskIO(node string, startMs, endMs, step int64) []diskIOStat {
	read, _ := g.store.QueryAllLatest("disk_read_rate", map[string]string{"node": node})
	write, _ := g.store.QueryAllLatest("disk_write_rate", map[string]string{"node": node})
	riops, _ := g.store.QueryAllLatest("disk_read_iops", map[string]string{"node": node})
	wiops, _ := g.store.QueryAllLatest("disk_write_iops", map[string]string{"node": node})
	byDev := map[string]*diskIOStat{}
	get := func(dev string) *diskIOStat {
		if dev == "" {
			dev = "sda"
		}
		if byDev[dev] == nil {
			byDev[dev] = &diskIOStat{Device: dev}
		}
		return byDev[dev]
	}
	add := func(series []model.Series, kind string) {
		for _, s := range series {
			dev := s.Labels["device"]
			if dev == "" {
				dev = s.Labels["disk"]
			}
			st := get(dev)
			if len(s.Points) > 0 {
				v := s.Points[len(s.Points)-1].Value
				switch kind {
				case "read":
					st.ReadMBps = v / 1e6
				case "write":
					st.WriteMBps = v / 1e6
				case "riops":
					st.ReadIOPS = v
				case "wiops":
					st.WriteIOPS = v
				}
			}
		}
	}
	add(read, "read")
	add(write, "write")
	add(riops, "riops")
	add(wiops, "wiops")
	out := make([]diskIOStat, 0, len(byDev))
	for _, st := range byDev {
		out = append(out, *st)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ReadMBps+out[i].WriteMBps > out[j].ReadMBps+out[j].WriteMBps
	})
	return out
}

// collectPartitions 聚合单主机各挂载点的磁盘使用率与容量（按使用率降序取 Top）。
func (g *Generator) collectPartitions(node string, startMs, endMs int64) []diskPartStat {
	pct, _ := g.store.QueryAllLatest("disk_used_percent", map[string]string{"node": node})
	used, _ := g.store.QueryAllLatest("disk_used", map[string]string{"node": node})
	total, _ := g.store.QueryAllLatest("disk_total", map[string]string{"node": node})
	byMount := map[string]*diskPartStat{}
	get := func(mount string) *diskPartStat {
		if mount == "" {
			mount = "/"
		}
		if byMount[mount] == nil {
			byMount[mount] = &diskPartStat{Mount: mount}
		}
		return byMount[mount]
	}
	for _, s := range pct {
		mount := s.Labels["mount"]
		if mount == "" {
			mount = s.Labels["device"]
		}
		st := get(mount)
		st.Device = s.Labels["device"]
		st.Fstype = s.Labels["fstype"]
		if len(s.Points) > 0 {
			st.UsedPct = s.Points[len(s.Points)-1].Value
		}
	}
	for _, s := range used {
		mount := s.Labels["mount"]
		if mount == "" {
			mount = s.Labels["device"]
		}
		st := get(mount)
		if len(s.Points) > 0 {
			st.UsedGB = s.Points[len(s.Points)-1].Value / 1e9
		}
	}
	for _, s := range total {
		mount := s.Labels["mount"]
		if mount == "" {
			mount = s.Labels["device"]
		}
		st := get(mount)
		if len(s.Points) > 0 {
			st.TotalGB = s.Points[len(s.Points)-1].Value / 1e9
		}
	}
	out := make([]diskPartStat, 0, len(byMount))
	for _, st := range byMount {
		out = append(out, *st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UsedPct > out[j].UsedPct })
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}

// ---- 资产与配置聚合 ----

// collectAssets 汇总「资产与配置」章节；台账未注入（库不可用）时返回 nil，章节不渲染。
//
// 三项都复用台账已有的聚合实现（`Stats` / `LinkOverview` / 巡检记录），报告这边不新增查询逻辑，
// 因此不会出现「报告里的数字与台账页不一致」。
func (g *Generator) collectAssets(scope ScopeSnapshot) *AssetsSection {
	if g.assets == nil {
		return nil
	}
	since := time.Now().AddDate(0, 0, -assetChangeWindowDays).UnixMilli()
	// Apply 保留三态：all → nil（该维度不生效），limited → 非 nil（空即恒不匹配）。
	f := scope.Apply(asset.ListFilter{})
	stats, err := g.assets.Stats(f, since)
	if err != nil {
		slog.Warn("报告：统计资产摘要失败（本章节跳过）", "err", err)
		return nil
	}
	sec := &AssetsSection{
		Total: stats.Total, Missing: stats.Missing, NoOwner: stats.NoOwner,
		Conflict: stats.Conflict, Ignored: stats.Ignored, Changes: stats.Changes,
		ChangeWindowDays: assetChangeWindowDays,
	}
	// 关系总览与台账页共用 LinkOverview：边的**两端**都要在范围内（只判一端会泄露范围外对象）。
	if ov, err := g.assets.LinkOverview(asset.ChangeScope{Nodes: f.Nodes, LabelSelectors: f.LabelSelectors}); err == nil {
		sec.LinksTotal = ov.TotalLinks
		sec.NoLinkAssets = ov.AssetsWithoutLinks
		for _, gp := range ov.Groups {
			sec.LinkGroups = append(sec.LinkGroups, AssetLinkGroup{
				FromType: gp.FromType, Kind: string(gp.Kind), ToType: gp.ToType,
				Source: string(gp.Source), Count: gp.Count,
			})
		}
	} else {
		slog.Warn("报告：统计关系总览失败（该小节跳过）", "err", err)
	}
	// 最近一次配置巡检：只取结论计数与分级，不给具体差异条目。
	if runs, err := g.assets.InspectRunsInNodes(1, f.Nodes); err == nil && len(runs) > 0 {
		run := runs[0]
		info := &AssetInspectInfo{
			At: run.StartedAt, Actor: run.Actor, Assets: run.Assets, Findings: run.Findings,
			Baselined: run.Baselined, Truncated: run.Truncated,
		}
		if findings, _, err := g.assets.InspectFindingsInNodes(run.ID, 500, f.Nodes); err == nil && len(findings) > 0 {
			byLevel := make(map[string]int, 3)
			for _, fd := range findings {
				byLevel[string(fd.Level)]++
			}
			info.ByLevel = byLevel
			info.Sampled = len(findings)
		}
		sec.Inspect = info
	}
	return sec
}

// ---- 容器与工作负载聚合 ----

// collectContainer 汇总「容器与工作负载」章节。
//
// 与容器页共用同一批指标名（`k8s_*`）与同一个集群注册表（`instancereg`），因此不会出现
// 「报告里的数字与容器页不一致」。整章是当前快照，不做周期对比。
func (g *Generator) collectContainer(scope ScopeSnapshot) *ContainerSection {
	if g.store == nil {
		return nil
	}
	sec := &ContainerSection{
		SnapshotNote: "当前快照：容器指标与集群清单都是即时查询，平台不留存其历史，因此不做周期对比。",
	}
	// ① 集群清单：up 指标优先，注册表补全——Agent 离线时 up 指标会缺，
	// 不能因此把「已配置的集群」判成「不存在」。
	clusters := make(map[string]*ContainerCluster)
	var keys []string
	if series, err := g.store.QueryAllLatest("k8s_cluster_up", nil); err == nil {
		for _, s := range series {
			node, inst := s.Labels["node"], s.Labels["instance"]
			if node == "" || inst == "" || len(s.Points) == 0 || !scopeCovers(scope, node) {
				continue
			}
			key := node + "|" + inst
			if _, ok := clusters[key]; ok {
				continue
			}
			clusters[key] = &ContainerCluster{
				Node: node, Instance: inst, Name: s.Labels["name"],
				Version: s.Labels["version"], Group: s.Labels["group"],
				Up: s.Points[len(s.Points)-1].Value > 0,
			}
			keys = append(keys, key)
		}
	} else {
		slog.Warn("报告：查询 K8s 集群失败（容器章节的集群清单可能不全）", "err", err)
	}
	for _, ki := range instancereg.Default.K8sInstances() {
		if !scopeCovers(scope, ki.Node) {
			continue
		}
		key := ki.Node + "|" + ki.Instance
		if _, ok := clusters[key]; ok {
			continue
		}
		clusters[key] = &ContainerCluster{
			Node: ki.Node, Instance: ki.Instance, Name: ki.Name, Version: ki.Version, Group: ki.Group,
		}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		// 一个 K8s 集群都没有：章节不渲染，而不是给一份空表（那看起来像"采集坏了"）。
		return nil
	}
	sort.Strings(keys)
	for _, k := range keys {
		c := clusters[k]
		if c.Up {
			sec.ClustersUp++
		}
		sec.Clusters = append(sec.Clusters, *c)
	}
	sec.ClustersTotal = len(keys)

	// ② 聚合指标：按 node|instance 归集
	type k8sAgg struct {
		nodesTotal, nodesReady                       float64
		podsTotal, podsRunning, podsPending, podsFailed, podsAbnormal float64
		depTotal, depUnhealthy, stsTotal, stsUnhealthy, dsTotal, dsUnhealthy float64
	}
	aggs := make(map[string]*k8sAgg)
	get := func(key string) *k8sAgg {
		a := aggs[key]
		if a == nil {
			a = &k8sAgg{}
			aggs[key] = a
		}
		return a
	}
	setters := map[string]func(a *k8sAgg, v float64){
		"k8s_nodes_total":            func(a *k8sAgg, v float64) { a.nodesTotal = v },
		"k8s_nodes_ready":            func(a *k8sAgg, v float64) { a.nodesReady = v },
		"k8s_pods_total":             func(a *k8sAgg, v float64) { a.podsTotal = v },
		"k8s_pods_running":           func(a *k8sAgg, v float64) { a.podsRunning = v },
		"k8s_pods_pending":           func(a *k8sAgg, v float64) { a.podsPending = v },
		"k8s_pods_failed":            func(a *k8sAgg, v float64) { a.podsFailed = v },
		"k8s_deployments_total":      func(a *k8sAgg, v float64) { a.depTotal = v },
		"k8s_deployments_unhealthy":  func(a *k8sAgg, v float64) { a.depUnhealthy = v },
		"k8s_statefulsets_total":     func(a *k8sAgg, v float64) { a.stsTotal = v },
		"k8s_statefulsets_unhealthy": func(a *k8sAgg, v float64) { a.stsUnhealthy = v },
		"k8s_daemonsets_total":       func(a *k8sAgg, v float64) { a.dsTotal = v },
		"k8s_daemonsets_unhealthy":   func(a *k8sAgg, v float64) { a.dsUnhealthy = v },
	}
	for metric, set := range setters {
		series, err := g.store.QueryAllLatest(metric, nil)
		if err != nil {
			slog.Warn("报告：K8s 聚合指标查询失败", "metric", metric, "err", err)
			continue
		}
		for _, s := range series {
			node, inst := s.Labels["node"], s.Labels["instance"]
			if node == "" || inst == "" || len(s.Points) == 0 || !scopeCovers(scope, node) {
				continue
			}
			set(get(node+"|"+inst), s.Points[len(s.Points)-1].Value)
		}
	}
	// 异常 Pod 单独查一次：要区分「值为 0」与「指标不存在」（旧 Agent）——后者必须退回
	// pending+failed 口径，否则升级服务端后"异常 Pod"会直接变 0，把已存在的异常悄悄藏起来。
	abnormalSeen := make(map[string]bool)
	if series, err := g.store.QueryAllLatest("k8s_pods_abnormal", nil); err == nil {
		for _, s := range series {
			node, inst := s.Labels["node"], s.Labels["instance"]
			if node == "" || inst == "" || len(s.Points) == 0 || !scopeCovers(scope, node) {
				continue
			}
			key := node + "|" + inst
			abnormalSeen[key] = true
			get(key).podsAbnormal = s.Points[len(s.Points)-1].Value
		}
	}
	var depTotal, depUnhealthy, stsTotal, stsUnhealthy, dsTotal, dsUnhealthy int
	for key, a := range aggs {
		sec.NodesTotal += int(a.nodesTotal)
		sec.NodesReady += int(a.nodesReady)
		sec.PodsTotal += int(a.podsTotal)
		sec.PodsRunning += int(a.podsRunning)
		sec.PodsPending += int(a.podsPending)
		sec.PodsFailed += int(a.podsFailed)
		if abnormalSeen[key] {
			sec.PodsAbnormal += int(a.podsAbnormal)
		} else {
			sec.PodsAbnormal += int(a.podsPending + a.podsFailed)
		}
		depTotal += int(a.depTotal)
		depUnhealthy += int(a.depUnhealthy)
		stsTotal += int(a.stsTotal)
		stsUnhealthy += int(a.stsUnhealthy)
		dsTotal += int(a.dsTotal)
		dsUnhealthy += int(a.dsUnhealthy)
	}
	for _, w := range []ContainerWorkloadStat{
		{Kind: "Deployment", Total: depTotal, Unhealthy: depUnhealthy},
		{Kind: "StatefulSet", Total: stsTotal, Unhealthy: stsUnhealthy},
		{Kind: "DaemonSet", Total: dsTotal, Unhealthy: dsUnhealthy},
	} {
		if w.Total > 0 || w.Unhealthy > 0 {
			sec.Workloads = append(sec.Workloads, w)
		}
	}

	// ③ 台账里的容器类资产计数（Docker 容器以 middleware-instance 落库、自然键带 docker: 前缀，
	// 现有筛选没有"自然键前缀"这个条件，因此宁缺勿错：不列 Docker 一行）。
	if g.assets != nil {
		f := scope.Apply(asset.ListFilter{})
		counts := make(map[string]int)
		for _, tk := range []string{"pod", "workload", "service"} {
			ff := f
			ff.TypeKey = tk
			if n, err := g.assets.Count(ff); err == nil {
				counts[tk] = n
			}
		}
		if len(counts) > 0 {
			sec.AssetCounts = counts
		}
	}
	return sec
}

// ---- 日志聚合 ----

// collectLogs 汇总「日志」章节：如实转述后端**自己回答**的能力与存储现状。
//
// 存储统计是**全局口径**（后端不掌握按节点分片的统计能力），因此受限身份下不展示它——
// 只给能力说明，避免"来源数/节点数"把范围外的规模透出去。
func (g *Generator) collectLogs(scope ScopeSnapshot) *LogsSection {
	if g.logs == nil {
		return nil
	}
	c := g.logs.Capability()
	sec := &LogsSection{
		Backend: c.Backend, FullTextIndex: c.FullTextIndex, FieldIndex: c.FieldIndex, Notes: c.Notes,
	}
	if c.ScanBudget != nil {
		sec.ScanBudgetMB = int(c.ScanBudget.Bytes / (1024 * 1024))
		sec.ScanBudgetLines = c.ScanBudget.Lines
	}
	if scope.NodeMode == asset.ScopeModeLimited {
		sec.StorageNote = "受限身份下不展示日志存储统计：它按全局口径统计，无法按资源范围收窄。"
		return sec
	}
	sec.Sources = c.Storage.Sources
	sec.Nodes = c.Storage.Nodes
	sec.OldestDay = c.Storage.OldestDay
	sec.NewestDay = c.Storage.NewestDay
	sec.Bytes = c.Storage.Bytes
	sec.Truncated = c.Storage.Truncated
	sec.ProbeError = c.Storage.Err
	return sec
}

// ---- 安全中心聚合 ----

// collectSecurity 汇总巡检周期内的安全基线评分与告警事件。
func (g *Generator) collectSecurity(scope ScopeSnapshot, startMs, endMs int64) *SecuritySection {
	if g.secStore == nil {
		return nil
	}
	baselines := g.secStore.Baselines()
	events := g.secStore.Events(0, "", "")
	sec := &SecuritySection{
		LowScores: make(map[string]float64),
	}
	var sumScore float64
	scored := 0
	for _, b := range baselines {
		// 安全章节同样受资源范围约束：范围外的节点不能因为「出了一份报告」被看见。
		if !scopeCovers(scope, b.Node) {
			continue
		}
		scored++
		sumScore += b.Score
		if b.Score < 70 {
			sec.RiskNodes = append(sec.RiskNodes, b.Node)
			sec.LowScores[b.Node] = b.Score
		}
	}
	if scored > 0 {
		sec.NodeCount = scored
		sec.AvgScore = round1(sumScore / float64(scored))
	}
	sec.LowScoreNodes = len(sec.RiskNodes)
	for _, e := range events {
		if !scopeCovers(scope, e.Node) {
			continue
		}
		if e.Timestamp < startMs || e.Timestamp > endMs {
			continue
		}
		switch e.Severity {
		case model.SeverityCritical:
			sec.CriticalEvents++
		case model.SeverityWarning:
			sec.WarningEvents++
		}
		if strings.Contains(strings.ToLower(e.Category), "cve") || strings.Contains(strings.ToUpper(e.Message), "CVE") {
			sec.CVECount++
		}
		sec.Events = append(sec.Events, e)
	}
	if len(sec.Events) > 20 {
		sec.Events = sec.Events[:20]
	}
	// 「有数据」同样按范围内的口径算：范围外有基线不能让本章节看起来有内容。
	sec.HasData = scored > 0 || len(sec.Events) > 0
	return sec
}

// buildSecurityFindings 将安全中心告警转化为报告发现项。
func buildSecurityFindings(sec *SecuritySection) []finding {
	if sec == nil {
		return nil
	}
	var fs []finding
	for _, e := range sec.Events {
		sev := "info"
		switch e.Severity {
		case model.SeverityCritical:
			sev = "critical"
		case model.SeverityWarning:
			sev = "warning"
		}
		fs = append(fs, finding{
			Severity:   sev,
			Category:   "安全中心",
			Resource:   e.Node,
			Title:      fmt.Sprintf("安全事件：%s", e.Category),
			Detail:     fmt.Sprintf("%s（来源：%s）", e.Message, e.SourceLocation),
			Suggestion: "结合安全中心对应事件的处理建议及时处置，必要时联动封禁或隔离。",
		})
	}
	for node, score := range sec.LowScores {
		fs = append(fs, finding{
			Severity:   "warning",
			Category:   "安全中心",
			Resource:   node,
			Title:      fmt.Sprintf("%s 安全基线评分偏低", node),
			Detail:     fmt.Sprintf("安全基线评分 %d，低于阈值 70，存在配置或漏洞风险。", int(score)),
			Suggestion: "进入安全中心查看基线检查明细，按项修复后重新评估评分。",
		})
	}
	return fs
}

// ---- 巡检结论与环比 ----

// buildConclusion 汇总整体健康评级、各级别发现数、Top 风险与一句话结论。
func (g *Generator) buildConclusion(nodes []nodeStat, findings []finding, sec *SecuritySection) Conclusion {
	c := Conclusion{
		SeverityCounts: map[string]int{"critical": 0, "warning": 0, "info": 0},
		TopRisks:       []string{},
	}
	for _, f := range findings {
		c.SeverityCounts[f.Severity]++
		c.TotalFindings++
	}
	var sumHost float64
	onlineCnt := 0
	for _, n := range nodes {
		if n.Status == "online" {
			sumHost += n.HealthScore
			onlineCnt++
		}
	}
	if onlineCnt > 0 {
		c.Score = round1(sumHost / float64(onlineCnt))
	}
	rating, cls := "健康", "healthy"
	if c.SeverityCounts["critical"] > 0 {
		rating, cls = "预警", "critical"
	} else if c.SeverityCounts["warning"] > 0 {
		rating, cls = "关注", "warning"
	}
	c.Rating, c.RatingClass = rating, cls
	for _, f := range findings {
		if f.Severity == "critical" || f.Severity == "warning" {
			c.TopRisks = append(c.TopRisks, fmt.Sprintf("[%s] %s", f.Resource, f.Title))
		}
		if len(c.TopRisks) >= 5 {
			break
		}
	}
	c.Summary = fmt.Sprintf("本周期共纳管 %d 台主机（在线 %d），发现 %d 项风险（严重 %d / 警告 %d / 提示 %d）。综合健康评分 %.0f 分，整体评级：%s。",
		len(nodes), onlineCnt, c.TotalFindings, c.SeverityCounts["critical"], c.SeverityCounts["warning"], c.SeverityCounts["info"], c.Score, rating)
	return c
}

// windowAgg 计算指定窗口内的主机聚合指标（用于环比）。
func (g *Generator) windowAgg(start, end time.Time) (cpuAvg, memAvg, diskMax, healthAvg, onlineRate float64) {
	nodes := g.nodeMgr.ListNodes()
	if len(nodes) == 0 {
		return
	}
	startMs, endMs, step := start.UnixMilli(), end.UnixMilli(), oneHourMs
	on := 0
	var sCPU, sMem, sDisk, sHealth float64
	for _, n := range nodes {
		ns := nodeStat{Status: n.Status}
		if s, err := g.store.QueryRange(n.Hostname, "cpu_usage", nil, startMs, endMs, step); err == nil {
			ns.CPUAvg, ns.CPUMax = avgMax(s)
		}
		if s, err := g.store.QueryRange(n.Hostname, "mem_used_percent", nil, startMs, endMs, step); err == nil {
			ns.MemAvg, ns.MemMax = avgMax(s)
		}
		if s, err := g.store.QueryRange(n.Hostname, "disk_used_percent", nil, startMs, endMs, step); err == nil {
			ns.DiskAvg, ns.DiskMax = avgMax(s)
		}
		ns.Health, ns.HealthScore = evalHostHealth(ns)
		if n.Status == "online" {
			on++
			sCPU += ns.CPUAvg
			sMem += ns.MemAvg
			sDisk += ns.DiskMax
			sHealth += ns.HealthScore
		}
	}
	onN := on
	if onN == 0 {
		onN = 1
	}
	return sCPU / float64(onN), sMem / float64(onN), sDisk / float64(onN), sHealth / float64(onN), float64(on) / float64(len(nodes)) * 100
}

// buildComparison 对比上一周期（等长窗口）的关键指标。
func (g *Generator) buildComparison(start, end time.Time, nodes []nodeStat, summary summaryStat) *Comparison {
	dur := end.Sub(start)
	prevCPU, prevMem, prevDisk, prevHealth, prevOnline := g.windowAgg(start.Add(-dur), start)
	var sumHealth float64
	online := 0
	for _, n := range nodes {
		if n.Status == "online" {
			sumHealth += n.HealthScore
			online++
		}
	}
	curHealth := 0.0
	if online > 0 {
		curHealth = round1(sumHealth / float64(online))
	}
	curOnline := 0.0
	if summary.Total > 0 {
		curOnline = round1(float64(summary.Online) / float64(summary.Total) * 100)
	}
	return &Comparison{
		OnlineRate:      curOnline,
		PrevOnlineRate:  round1(prevOnline),
		CPUAvg:          summary.CPUAvg,
		PrevCPUAvg:      prevCPU,
		MemAvg:          summary.MemAvg,
		PrevMemAvg:      prevMem,
		DiskMax:         summary.DiskMax,
		PrevDiskMax:     prevDisk,
		HealthScore:     curHealth,
		PrevHealthScore: round1(prevHealth),
	}
}

// ---- 中间件采集 ----

// 报告所需的中间件类型定义已收敛到 mwreg 注册表（内置类型 + 模板派生类型）：
// 原先这里的 mwDefs 与 api 的 middlewareTypes/mwSummarySpecs、alert 的 serviceMetric 各自维护，
// 已经漂移出三处不一致（见 mwreg/builtin.go 的说明）。报告通过 SetMiddlewareRegistry 注入读取。

func (g *Generator) collectMiddleware(scope ScopeSnapshot, startMs, endMs, step int64) []mwInstance {
	var out []mwInstance
	for _, d := range g.mwTypes() {
		// 模板派生类型的存活指标是所有模板共用的 template_target_up，必须带上类型自身的标签过滤
		upSeries, err := g.store.QueryAllLatest(d.UpMetricForReport(), d.UpLabels)
		if err != nil || len(upSeries) == 0 {
			continue
		}
		latest := map[string]map[string]float64{}
		for _, m := range d.KeyMetrics {
			latest[m] = latestByInstance(g.store, m)
		}
		tp := d.ReportThroughputMetric
		for _, s := range upSeries {
			node := s.Labels["node"]
			// 中间件实例按它所属的 Agent 节点收窄：范围外的实例不能进报告。
			if !scopeCovers(scope, node) {
				continue
			}
			inst := s.Labels["instance"]
			up := false
			if len(s.Points) > 0 {
				up = s.Points[len(s.Points)-1].Value > 0
			}
			if d.ReportPresenceUp {
				// 以「指标是否有数据」判定存活（Docker 守护进程即如此：避免 0 容器被误判离线）
				up = true
			}
			key := node + "|" + inst
			mi := mwInstance{
				Type:     d.Key,
				Node:     node,
				Instance: inst,
				Role:     s.Labels["role"],
				Topology: s.Labels["topology"],
				Version:  s.Labels["version"],
				Up:       up,
			}
			lv := func(m string) float64 { return latest[m][key] }
			mi.ConnUsed = lv(d.ConnMetric)
			if tp != "" {
				mi.Throughput = lv(tp)
			}
			switch d.Key {
			case "redis":
				mi.ConnMax = lv("redis_maxclients")
				mi.MemPct = lv("redis_used_memory_percent")
				mi.MemUsedMB = lv("redis_used_memory") / 1e6
				mi.HitRate = lv("redis_hit_rate")
				mi.RespTime = lv("redis_cmd_latency_ms")
				if lag := lv("redis_replication_lag"); lag > 0 {
					mi.Extra = fmt.Sprintf("主从复制延迟 %.1fs", lag)
				}
			case "mysql":
				mi.ConnMax = lv("mysql_max_connections")
				mi.HitRate = lv("mysql_innodb_buffer_pool_hit_rate")
				mi.RespTime = lv("mysql_query_latency_ms")
				slaveLag := lv("mysql_seconds_behind_master")
				slow := lv("mysql_slow_queries")
				var parts []string
				if slaveLag > 0 {
					parts = append(parts, fmt.Sprintf("主从延迟 %.1fs", slaveLag))
				}
				if slow > 0 {
					parts = append(parts, fmt.Sprintf("慢查询 %d", int(slow)))
				}
				mi.Extra = strings.Join(parts, "；")
			case "postgres":
				mi.ConnMax = lv("postgres_max_connections")
				mi.HitRate = lv("postgres_cache_hit_ratio")
				mi.RespTime = lv("postgres_query_latency_ms")
				if lag := lv("postgres_replication_lag_bytes"); lag > 0 {
					mi.Extra = fmt.Sprintf("复制延迟 %.0fB", lag)
				}
			case "nginx":
				mi.ConnUsed = lv("nginx_active_connections")
				if rate := lv("nginx_access_requests_rate"); rate > 0 {
					mi.Extra = fmt.Sprintf("请求速率 %.1f 次/s", rate)
				}
			case "kafka":
				brokers := lv("kafka_broker_count")
				mi.ConnUsed, mi.ConnMax = brokers, brokers
				var kp []string
				if v := lv("kafka_offline_partitions"); v > 0 {
					kp = append(kp, fmt.Sprintf("离线分区 %d", int(v)))
				}
				if v := lv("kafka_under_replicated_partitions"); v > 0 {
					kp = append(kp, fmt.Sprintf("欠副本分区 %d", int(v)))
				}
				if v := lv("kafka_consumer_lag"); v > 0 {
					kp = append(kp, fmt.Sprintf("消费滞后 %.0f", v))
				}
				if v := lv("kafka_active_controller_count"); v != 1 {
					kp = append(kp, fmt.Sprintf("活跃控制器 %d（应为1）", int(v)))
				}
				mi.Extra = strings.Join(kp, "；")
			case "rocketmq":
				brokers := lv("rocketmq_broker_count")
				mi.ConnUsed, mi.ConnMax = brokers, brokers
				var rp []string
				if v := lv("rocketmq_message_accumulation"); v > 0 {
					rp = append(rp, fmt.Sprintf("消息堆积 %d", int(v)))
				}
				if v := lv("rocketmq_consumer_lag"); v > 0 {
					rp = append(rp, fmt.Sprintf("消费滞后 %.0f", v))
				}
				mi.Extra = strings.Join(rp, "；")
			case "mongodb":
				cur := lv("mongodb_connections_current")
				avail := lv("mongodb_connections_available")
				mi.ConnUsed, mi.ConnMax = cur, cur+avail
				var mp []string
				if h := lv("mongodb_repl_health"); h > 0 && h < 1 {
					mp = append(mp, fmt.Sprintf("复制健康 %.2f（异常）", h))
				}
				if v := lv("mongodb_repl_lag"); v > 0 {
					mp = append(mp, fmt.Sprintf("复制延迟 %.1fs", v))
				}
				mi.Extra = strings.Join(mp, "；")
			case "kubernetes":
				nt := lv("k8s_nodes_total")
				nr := lv("k8s_nodes_ready")
				mi.ConnUsed, mi.ConnMax = nr, nt
				var kdp []string
				if nt > 0 && nr < nt {
					kdp = append(kdp, fmt.Sprintf("未就绪节点 %d", int(nt-nr)))
				}
				if v := lv("k8s_pods_failed"); v > 0 {
					kdp = append(kdp, fmt.Sprintf("失败 Pod %d", int(v)))
				}
				if v := lv("k8s_pods_pending"); v > 0 {
					kdp = append(kdp, fmt.Sprintf("Pending Pod %d", int(v)))
				}
				if v := lv("k8s_deployments_unhealthy"); v > 0 {
					kdp = append(kdp, fmt.Sprintf("异常 Deployment %d", int(v)))
				}
				mi.Extra = strings.Join(kdp, "；")
			case "docker":
				total := lv("docker_containers_total")
				running := lv("docker_containers_running")
				mi.ConnUsed, mi.ConnMax = running, total
				var dp []string
				if v := lv("docker_containers_stopped"); v > 0 {
					dp = append(dp, fmt.Sprintf("已停止容器 %d", int(v)))
				}
				mi.Extra = strings.Join(dp, "；")
			}
			mi.Metrics = map[string]float64{}
			for _, m := range d.KeyMetrics {
				if v, ok := latest[m][key]; ok {
					mi.Metrics[m] = v
				}
			}
			if mi.ConnMax > 0 {
				mi.ConnPct = round1(mi.ConnUsed / mi.ConnMax * 100)
			}
			mi.Status = evalMwStatus(mi)
			if up && node != "" {
				mi.Trend = rangePoints(g.store, node, d.ConnMetric, inst, startMs, endMs, step)
			}
			out = append(out, mi)
		}
	}
	return out
}

// ---- 健康评估 ----

func evalHostHealth(ns nodeStat) (string, float64) {
	score := 100.0
	if ns.CPUMax >= cpuCrit {
		score -= 30
	} else if ns.CPUMax >= cpuWarn {
		score -= 15
	}
	if ns.MemMax >= memCrit {
		score -= 30
	} else if ns.MemMax >= memWarn {
		score -= 12
	}
	if ns.DiskMax >= diskCrit {
		score -= 25
	} else if ns.DiskMax >= diskWarn {
		score -= 10
	}
	if ns.LoadAvg >= 8 {
		score -= 20
	} else if ns.LoadAvg >= 4 {
		score -= 8
	}
	if ns.Status != "online" {
		score -= 40
	}
	if score < 0 {
		score = 0
	}
	switch {
	case score < 60:
		return "critical", round1(score)
	case score < 85:
		return "warning", round1(score)
	default:
		return "healthy", round1(score)
	}
}

func evalMwStatus(mi mwInstance) string {
	if !mi.Up {
		return "offline"
	}
	worst := "healthy"
	downgrade := func(to string) {
		if to == "critical" {
			worst = "critical"
		} else if to == "warning" && worst != "critical" {
			worst = "warning"
		}
	}
	switch mi.Type {
	case "redis":
		if mi.MemPct >= redisMemCrit {
			downgrade("critical")
		} else if mi.MemPct >= redisMemWarn {
			downgrade("warning")
		}
		if mi.HitRate > 0 && mi.HitRate < hitCrit {
			downgrade("critical")
		} else if mi.HitRate > 0 && mi.HitRate < hitWarn {
			downgrade("warning")
		}
		if mi.RespTime >= redisLatCrit {
			downgrade("critical")
		} else if mi.RespTime >= redisLatWarn {
			downgrade("warning")
		}
	case "mysql", "postgres":
		if mi.ConnPct >= connCrit {
			downgrade("critical")
		} else if mi.ConnPct >= connWarn {
			downgrade("warning")
		}
		if mi.HitRate > 0 && mi.HitRate < hitCrit {
			downgrade("critical")
		} else if mi.HitRate > 0 && mi.HitRate < hitWarn {
			downgrade("warning")
		}
		if mi.RespTime >= dbLatCrit {
			downgrade("critical")
		} else if mi.RespTime >= dbLatWarn {
			downgrade("warning")
		}
	}
	return worst
}

// ---- 图表构建 ----

func buildCharts(nodes []nodeStat, trends []hostTrend) []chartItem {
	var charts []chartItem
	{
		var items []barItem
		for _, n := range nodes {
			items = append(items, barItem{Label: n.Name, Value: n.CPUMax, Color: statusColor(n.Health)})
		}
		charts = append(charts, chartItem{
			Title: "各主机 CPU 峰值使用率（%）",
			Kind:  "bar",
			SVG:   template.HTML(barChart("各主机 CPU 峰值使用率（%）", items, "%", 560, 260)),
		})
	}
	{
		cnt := map[string]int{"healthy": 0, "warning": 0, "critical": 0, "offline": 0}
		for _, n := range nodes {
			if n.Status != "online" {
				cnt["offline"]++
				continue
			}
			cnt[n.Health]++
		}
		items := []barItem{
			{Label: "健康", Value: float64(cnt["healthy"]), Color: statusColor("healthy")},
			{Label: "关注", Value: float64(cnt["warning"]), Color: statusColor("warning")},
			{Label: "预警", Value: float64(cnt["critical"]), Color: statusColor("critical")},
			{Label: "离线", Value: float64(cnt["offline"]), Color: "#909399"},
		}
		charts = append(charts, chartItem{
			Title: "主机健康状态分布",
			Kind:  "bar",
			SVG:   template.HTML(barChart("主机健康状态分布", items, "台", 560, 240)),
		})
	}
	for _, t := range trends {
		if len(t.CPU) == 0 && len(t.Mem) == 0 && len(t.Disk) == 0 {
			continue
		}
		series := []lineSeries{
			{Name: "CPU", Color: "#409eff", Points: t.CPU},
			{Name: "内存", Color: "#e6a23c", Points: t.Mem},
			{Name: "磁盘", Color: "#f56c6c", Points: t.Disk},
		}
		charts = append(charts, chartItem{
			Title: "节点 " + t.Node + " 资源使用趋势（%）",
			Kind:  "line",
			SVG:   template.HTML(lineChart("节点 "+t.Node+" 资源使用趋势（%）", series, "%", 560, 260)),
		})
	}
	return charts
}

func statusColor(status string) string {
	switch status {
	case "healthy":
		return "#67c23a"
	case "warning":
		return "#e6a23c"
	case "critical":
		return "#f56c6c"
	case "offline":
		return "#909399"
	default:
		return "#409eff"
	}
}

// ---- 发现项构建 ----

func buildFindings(nodes []nodeStat, mw []mwInstance) []finding {
	var fs []finding
	sevRank := map[string]int{"critical": 0, "warning": 1, "info": 2}

	for _, n := range nodes {
		if n.Status != "online" {
			fs = append(fs, finding{
				Severity:   "critical",
				Category:   "主机资源",
				Resource:   n.Name,
				Title:      "主机离线",
				Detail:     fmt.Sprintf("主机 %s（%s）在巡检周期内状态为离线，最近一次上报时间为 %s，期间无监控数据回传。", n.Name, n.IP, relTime(n.LastSeen)),
				Impact:     "该主机上的所有业务与中间件指标不可见，发生故障时无法及时感知，存在监控盲区。",
				Suggestion: "检查该主机的 nebula-agent 进程是否存活（systemctl status nebula-agent）、主机网络连通性与到 Server 的链路；若已主动下线请确认维护窗口配置。",
			})
			continue
		}
		if n.CPUMax >= cpuWarn {
			sev := "warning"
			if n.CPUMax >= cpuCrit {
				sev = "critical"
			}
			fs = append(fs, finding{
				Severity:   sev,
				Category:   "主机资源",
				Resource:   n.Name,
				Title:      "CPU 使用率偏高",
				Detail:     fmt.Sprintf("主机 %s 巡检周期内 CPU 平均 %.1f%%、峰值 %.1f%%，多个采样点处于高位。", n.Name, n.CPUAvg, n.CPUMax),
				Impact:     "CPU 持续高位会导致进程调度延迟、请求排队与响应变慢，极端情况下触发进程超时或连锁雪崩。",
				Suggestion: "登录该机使用 `top`/`pidstat -u 1` 定位高 CPU 进程，排查异常批处理或死循环；必要时垂直扩容 CPU 或水平扩容；建议对 cpu_usage 配置 70%%/85%% 阈值告警提前预警。",
			})
		}
		if n.MemMax >= memWarn {
			sev := "warning"
			if n.MemMax >= memCrit {
				sev = "critical"
			}
			fs = append(fs, finding{
				Severity:   sev,
				Category:   "主机资源",
				Resource:   n.Name,
				Title:      "内存使用率偏高",
				Detail:     fmt.Sprintf("主机 %s 巡检周期内内存平均 %.1f%%、峰值 %.1f%%。", n.Name, n.MemAvg, n.MemMax),
				Impact:     "内存接近上限会触发系统 OOM Killer 随机终止进程，造成服务中断与数据不一致。",
				Suggestion: "使用 `free -h`/`smem` 排查内存占用最大的进程，确认是否存在内存泄漏；调优应用堆/JVM 参数或容器内存限制；必要时扩容内存并配置 mem_usage 告警。",
			})
		}
		if n.DiskMax >= diskWarn {
			sev := "warning"
			if n.DiskMax >= diskCrit {
				sev = "critical"
			}
			fs = append(fs, finding{
				Severity:   sev,
				Category:   "主机资源",
				Resource:   n.Name,
				Title:      "磁盘使用率偏高",
				Detail:     fmt.Sprintf("主机 %s 巡检周期内磁盘使用率平均 %.1f%%、峰值 %.1f%%，剩余可用空间受限。", n.Name, n.DiskAvg, n.DiskMax),
				Impact:     "磁盘写满将导致日志/数据无法落盘、数据库写入失败、应用异常甚至宕机。",
				Suggestion: "使用 `df -h`/`du -sh` 定位大目录，清理过期日志与临时文件、归档冷数据；对核心挂载配置 disk_usage 阈值告警并规划磁盘扩容。",
			})
		}
		if n.LoadAvg >= 1 {
			fs = append(fs, finding{
				Severity:   "warning",
				Category:   "主机资源",
				Resource:   n.Name,
				Title:      "系统负载偏高",
				Detail:     fmt.Sprintf("主机 %s 最近系统负载 load1 为 %.2f，处于较高水平。", n.Name, n.LoadAvg),
				Impact:     "高负载通常意味着 CPU 或 IO 资源出现瓶颈，系统吞吐下降、请求排队。",
				Suggestion: "结合 CPU/磁盘 IO 指标定位瓶颈来源（计算密集型或 IO 等待），针对性扩容或优化；负载持续高位时核查 top 中 D 状态（IO 等待）进程。",
			})
		}
	}

	for _, m := range mw {
		name := fmt.Sprintf("%s/%s", m.Type, m.Instance)
		if !m.Up {
			fs = append(fs, finding{
				Severity:   "critical",
				Category:   "中间件",
				Resource:   name,
				Title:      "中间件实例离线",
				Detail:     fmt.Sprintf("中间件实例 %s（类型 %s，节点 %s）探活失败，当前处于离线状态。", m.Instance, m.Type, m.Node),
				Impact:     "依赖该实例的业务功能受影响，可能出现缓存/查询失败或降级。",
				Suggestion: "检查实例进程状态与端口监听、网络连通性与访问凭证；若为复制集群请确认主从同步状态；恢复后关注连接数、命中率是否回到正常水平。",
			})
			continue
		}
		switch m.Type {
		case "redis":
			if m.HitRate > 0 && m.HitRate < hitWarn {
				sev := "warning"
				if m.HitRate < hitCrit {
					sev = "critical"
				}
				fs = append(fs, finding{
					Severity:   sev,
					Category:   "中间件",
					Resource:   name,
					Title:      "Redis 缓存命中率偏低",
					Detail:     fmt.Sprintf("Redis 实例 %s 命中率仅 %.1f%%，未命中请求将回源至后端存储。", m.Instance, m.HitRate),
					Impact:     "命中率下降会显著增加后端数据库压力，并使依赖缓存的接口响应变慢。",
					Suggestion: "检查 maxmemory 是否过小导致频繁驱逐；确认淘汰策略（建议 allkeys-lru）；排查大 key/热 key 与业务 key 设计；监控 redis_evicted_keys 与 rejected_connections。",
				})
			}
			if m.MemPct >= redisMemWarn {
				sev := "warning"
				if m.MemPct >= redisMemCrit {
					sev = "critical"
				}
				fs = append(fs, finding{
					Severity:   sev,
					Category:   "中间件",
					Resource:   name,
					Title:      "Redis 内存使用率偏高",
					Detail:     fmt.Sprintf("Redis 实例 %s 内存使用率 %.1f%%（约 %.0fMB），接近 maxmemory 上限。", m.Instance, m.MemPct, m.MemUsedMB),
					Impact:     "内存接近上限会触发 key 驱逐甚至拒绝写入，命中率下降并可能出现写入失败。",
					Suggestion: "适当上调 maxmemory（确保宿主机内存余量充足）；清理无效/过期数据；设置合理淘汰策略；持续监控 used_memory 与 evicted_keys。",
				})
			}
			if m.RespTime >= redisLatWarn {
				sev := "warning"
				if m.RespTime >= redisLatCrit {
					sev = "critical"
				}
				fs = append(fs, finding{
					Severity:   sev,
					Category:   "中间件",
					Resource:   name,
					Title:      "Redis 命令响应时间偏高",
					Detail:     fmt.Sprintf("Redis 实例 %s 命令平均响应时间 %.2fms，高于常态水平。", m.Instance, m.RespTime),
					Impact:     "命令时延升高会使调用方超时、链路整体变慢，影响上游业务 RT。",
					Suggestion: "排查慢命令（如 keys *、大 key、复杂 Lua）；检查网络与持久化阻塞（AOF/RDB fork）；对热 key 做本地/客户端缓存分散压力。",
				})
			}
		case "mysql":
			if m.ConnPct >= connWarn {
				sev := "warning"
				if m.ConnPct >= connCrit {
					sev = "critical"
				}
				fs = append(fs, finding{
					Severity:   sev,
					Category:   "中间件",
					Resource:   name,
					Title:      "MySQL 连接数使用率偏高",
					Detail:     fmt.Sprintf("MySQL 实例 %s 连接数使用率 %.1f%%（%.0f/%.0f），接近 max_connections 上限。", m.Instance, m.ConnPct, m.ConnUsed, m.ConnMax),
					Impact:     "连接耗尽会导致新连接被拒绝（Too many connections），应用报错或无法建立数据库连接。",
					Suggestion: "排查并优化连接池配置与空闲连接回收；定位长期占用连接的事务/慢查询；必要时调大 max_connections（受系统 ulimit 与内存约束）；配置连接数阈值告警。",
				})
			}
			if m.HitRate > 0 && m.HitRate < hitWarn {
				sev := "warning"
				if m.HitRate < hitCrit {
					sev = "critical"
				}
				fs = append(fs, finding{
					Severity:   sev,
					Category:   "中间件",
					Resource:   name,
					Title:      "MySQL InnoDB 缓冲池命中率偏低",
					Detail:     fmt.Sprintf("MySQL 实例 %s InnoDB 缓冲池命中率 %.1f%%，低于推荐值。", m.Instance, m.HitRate),
					Impact:     "缓冲池命中率下降会增加磁盘 IO，查询延迟上升，数据库整体吞吐受限。",
					Suggestion: "适当增大 innodb_buffer_pool_size（建议不超过物理内存的 75%）；排查全表扫描与大结果集查询；结合慢查询日志优化索引。",
				})
			}
			if m.RespTime >= dbLatWarn {
				sev := "warning"
				if m.RespTime >= dbLatCrit {
					sev = "critical"
				}
				fs = append(fs, finding{
					Severity:   sev,
					Category:   "中间件",
					Resource:   name,
					Title:      "MySQL 平均语句响应时间偏高",
					Detail:     fmt.Sprintf("MySQL 实例 %s 平均语句响应时间 %.2fms，高于常态水平。", m.Instance, m.RespTime),
					Impact:     "SQL 时延升高会拖慢调用方 RT，高并发下引发请求堆积与超时，影响上游业务。",
					Suggestion: "结合 performance_schema.events_statements_summary_by_digest 定位高耗时 SQL 类型；优化索引与执行计划；排查锁等待、全表扫描与临时表落盘；必要时扩容或读写分离。",
				})
			}
		case "postgres":
			if m.ConnPct >= connWarn {
				sev := "warning"
				if m.ConnPct >= connCrit {
					sev = "critical"
				}
				fs = append(fs, finding{
					Severity:   sev,
					Category:   "中间件",
					Resource:   name,
					Title:      "PostgreSQL 连接数使用率偏高",
					Detail:     fmt.Sprintf("PostgreSQL 实例 %s 连接数使用率 %.1f%%（%.0f/%.0f）。", m.Instance, m.ConnPct, m.ConnUsed, m.ConnMax),
					Impact:     "连接接近上限会造成新连接被拒绝，应用出现连接获取失败。",
					Suggestion: "优化连接池（如 pgbouncer）与空闲连接回收；排查长事务；必要时调大 max_connections；配置连接数阈值告警。",
				})
			}
			if m.HitRate > 0 && m.HitRate < hitWarn {
				sev := "warning"
				if m.HitRate < hitCrit {
					sev = "critical"
				}
				fs = append(fs, finding{
					Severity:   sev,
					Category:   "中间件",
					Resource:   name,
					Title:      "PostgreSQL 缓存命中率偏低",
					Detail:     fmt.Sprintf("PostgreSQL 实例 %s 缓存命中率 %.1f%%。", m.Instance, m.HitRate),
					Impact:     "缓存命中率下降增加磁盘读取，查询性能下降。",
					Suggestion: "适当增大 shared_buffers；排查大表顺序扫描；结合 pg_stat_statements 优化高频 SQL 与索引。",
				})
			}
			if m.RespTime >= dbLatWarn {
				sev := "warning"
				if m.RespTime >= dbLatCrit {
					sev = "critical"
				}
				fs = append(fs, finding{
					Severity:   sev,
					Category:   "中间件",
					Resource:   name,
					Title:      "PostgreSQL 平均语句响应时间偏高",
					Detail:     fmt.Sprintf("PostgreSQL 实例 %s 平均语句响应时间 %.2fms，高于常态水平。", m.Instance, m.RespTime),
					Impact:     "SQL 时延升高会拖慢调用方 RT，高并发下引发请求堆积与超时，影响上游业务。",
					Suggestion: "结合 pg_stat_statements 定位高耗时 SQL 与执行计划；优化索引、避免全表扫描与顺序扫描；排查锁等待与长事务；必要时扩容或读写分离。",
				})
			}
		case "kafka":
			if offline := m.Metrics["kafka_offline_partitions"]; offline > 0 {
				fs = append(fs, finding{Severity: "critical", Category: "中间件",
					Resource: name, Title: fmt.Sprintf("%s 存在离线分区", m.Instance),
					Detail: fmt.Sprintf("离线分区数 %d，可能导致数据不可用。", int(offline))})
			}
			if ur := m.Metrics["kafka_under_replicated_partitions"]; ur > 0 {
				fs = append(fs, finding{Severity: "warning", Category: "中间件",
					Resource: name, Title: fmt.Sprintf("%s 存在欠副本分区", m.Instance),
					Detail: fmt.Sprintf("欠副本分区数 %d，副本同步异常。", int(ur))})
			}
			if lag := m.Metrics["kafka_consumer_lag"]; lag > 100000 {
				fs = append(fs, finding{Severity: "warning", Category: "中间件",
					Resource: name, Title: fmt.Sprintf("%s 消费滞后偏高", m.Instance),
					Detail: fmt.Sprintf("消费滞后 %.0f 条，消费能力或下游处理存在瓶颈。", lag)})
			}
			if ctrl := m.Metrics["kafka_active_controller_count"]; ctrl != 1 {
				fs = append(fs, finding{Severity: "critical", Category: "中间件",
					Resource: name, Title: fmt.Sprintf("%s 控制器状态异常", m.Instance),
					Detail: fmt.Sprintf("活跃控制器数 %d（应为 1），集群选主异常。", int(ctrl))})
			}
		case "rocketmq":
			if acc := m.Metrics["rocketmq_message_accumulation"]; acc > 0 {
				fs = append(fs, finding{Severity: "warning", Category: "中间件",
					Resource: name, Title: fmt.Sprintf("%s 消息堆积", m.Instance),
					Detail: fmt.Sprintf("消息堆积 %d 条，消费速率不足。", int(acc))})
			}
			if lag := m.Metrics["rocketmq_consumer_lag"]; lag > 100000 {
				fs = append(fs, finding{Severity: "warning", Category: "中间件",
					Resource: name, Title: fmt.Sprintf("%s 消费滞后偏高", m.Instance),
					Detail: fmt.Sprintf("消费滞后 %.0f 条。", lag)})
			}
		case "mongodb":
			if h := m.Metrics["mongodb_repl_health"]; h > 0 && h < 1 {
				fs = append(fs, finding{Severity: "critical", Category: "中间件",
					Resource: name, Title: fmt.Sprintf("%s 复制健康异常", m.Instance),
					Detail: fmt.Sprintf("复制健康度 %.2f（正常为 1），副本集异常。", h)})
			}
			if lag := m.Metrics["mongodb_repl_lag"]; lag > 30 {
				fs = append(fs, finding{Severity: "warning", Category: "中间件",
					Resource: name, Title: fmt.Sprintf("%s 复制延迟偏高", m.Instance),
					Detail: fmt.Sprintf("复制延迟 %.1fs。", lag)})
			}
			if cur := m.Metrics["mongodb_connections_current"]; cur > 0 {
				avail := m.Metrics["mongodb_connections_available"]
				if cur+avail > 0 && cur/(cur+avail) > 0.9 {
					fs = append(fs, finding{Severity: "warning", Category: "中间件",
						Resource: name, Title: fmt.Sprintf("%s 连接使用率偏高", m.Instance),
						Detail: fmt.Sprintf("当前连接 %d，可用 %d，使用率 %.0f%%。", int(cur), int(avail), cur/(cur+avail)*100)})
				}
			}
		case "kubernetes":
			nt := m.Metrics["k8s_nodes_total"]
			nr := m.Metrics["k8s_nodes_ready"]
			if nt > 0 && nr < nt {
				fs = append(fs, finding{Severity: "warning", Category: "中间件",
					Resource: name, Title: fmt.Sprintf("%s 集群存在未就绪节点", m.Instance),
					Detail: fmt.Sprintf("节点总数 %d，就绪 %d。", int(nt), int(nr))})
			}
			if v := m.Metrics["k8s_pods_failed"]; v > 0 {
				fs = append(fs, finding{Severity: "warning", Category: "中间件",
					Resource: name, Title: fmt.Sprintf("%s 存在失败 Pod", m.Instance),
					Detail: fmt.Sprintf("失败 Pod 数 %d。", int(v))})
			}
			if v := m.Metrics["k8s_deployments_unhealthy"]; v > 0 {
				fs = append(fs, finding{Severity: "warning", Category: "中间件",
					Resource: name, Title: fmt.Sprintf("%s 存在异常 Deployment", m.Instance),
					Detail: fmt.Sprintf("异常 Deployment 数 %d。", int(v))})
			}
		case "docker":
			if v := m.Metrics["docker_containers_stopped"]; v > 0 {
				fs = append(fs, finding{Severity: "info", Category: "中间件",
					Resource: name, Title: fmt.Sprintf("%s 存在已停止容器", m.Instance),
					Detail: fmt.Sprintf("已停止容器 %d 个。", int(v))})
			}
		}
		if strings.Contains(m.Extra, "主从延迟") || strings.Contains(m.Extra, "复制延迟") {
			fs = append(fs, finding{
				Severity:   "warning",
				Category:   "中间件",
				Resource:   name,
				Title:      "复制延迟",
				Detail:     fmt.Sprintf("中间件实例 %s 存在复制延迟：%s。", m.Instance, m.Extra),
				Impact:     "读从库可能读到陈旧数据，存在数据一致性风险；延迟持续扩大可能引发复制中断。",
				Suggestion: "排查主库写入压力与从库硬件/IO 瓶颈，确认复制线程状态（SHOW REPLICA STATUS / pg_stat_replication）与网络带宽；对强一致读改走主库。",
			})
		}
	}

	sort.SliceStable(fs, func(i, j int) bool {
		return sevRank[fs[i].Severity] < sevRank[fs[j].Severity]
	})
	return fs
}

// ---- 工具函数 ----

func avgMax(series []model.Series) (avg, max float64) {
	var sum float64
	var cnt int
	for _, s := range series {
		for _, p := range s.Points {
			sum += p.Value
			cnt++
			if p.Value > max {
				max = p.Value
			}
		}
	}
	if cnt > 0 {
		avg = sum / float64(cnt)
	}
	return round1(avg), round1(max)
}

func lastValue(series []model.Series) (float64, bool) {
	for _, s := range series {
		if len(s.Points) > 0 {
			return s.Points[len(s.Points)-1].Value, true
		}
	}
	return 0, false
}

func toPoints(series []model.Series) []linePoint {
	if len(series) == 0 {
		return nil
	}
	pts := series[0].Points
	out := make([]linePoint, 0, len(pts))
	for _, p := range pts {
		out = append(out, linePoint{T: p.Timestamp, V: p.Value})
	}
	return out
}

func latestByInstance(store storage.Storage, metric string) map[string]float64 {
	out := map[string]float64{}
	series, err := store.QueryAllLatest(metric, nil)
	if err != nil {
		return out
	}
	for _, s := range series {
		node := s.Labels["node"]
		inst := s.Labels["instance"]
		if node == "" && inst == "" {
			continue
		}
		key := node + "|" + inst
		if len(s.Points) > 0 {
			out[key] = s.Points[len(s.Points)-1].Value
		}
	}
	return out
}

func rangePoints(store storage.Storage, node, metric, instance string, startMs, endMs, step int64) []linePoint {
	labels := map[string]string{}
	if instance != "" {
		labels["instance"] = instance
	}
	series, err := store.QueryRange(node, metric, labels, startMs, endMs, step)
	if err != nil || len(series) == 0 {
		return nil
	}
	return toPoints(series)
}

func relTime(ms int64) string {
	if ms == 0 {
		return "未知"
	}
	d := time.Now().UnixMilli() - ms
	if d < 0 {
		d = 0
	}
	switch {
	case d < 60*1000:
		return "刚刚"
	case d < 60*60*1000:
		return fmt.Sprintf("%.0f 分钟前", float64(d)/60000)
	case d < 24*60*60*1000:
		return fmt.Sprintf("%.0f 小时前", float64(d)/3600000)
	default:
		return fmt.Sprintf("%.0f 天前", float64(d)/86400000)
	}
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
func round2(v float64) float64 { return math.Round(v*100) / 100 }

// renderHTML 将报告数据渲染为完整 HTML。
func renderHTML(data reportData) string {
	funcs := template.FuncMap{
		"fmtTime": func(ms int64) string {
			if ms == 0 {
				return "-"
			}
			return time.UnixMilli(ms).Format("2006-01-02 15:04:05")
		},
		"statusLabel": func(s string) string {
			switch s {
			case "healthy":
				return "健康"
			case "warning":
				return "关注"
			case "critical":
				return "预警"
			case "offline":
				return "离线"
			default:
				return s
			}
		},
		"sevLabel": func(s string) string {
			switch s {
			case "critical":
				return "严重"
			case "warning":
				return "警告"
			case "info":
				return "提示"
			default:
				return s
			}
		},
		"f1": func(v float64) string { return formatNum(v) },
		"sevText": func(v interface{}) string {
			switch fmt.Sprintf("%v", v) {
			case "critical":
				return "严重"
			case "warning":
				return "警告"
			case "info":
				return "提示"
			default:
				return fmt.Sprintf("%v", v)
			}
		},
		"sevClass": func(v interface{}) string {
			switch fmt.Sprintf("%v", v) {
			case "critical":
				return "critical"
			case "warning":
				return "warning"
			default:
				return "info"
			}
		},
		"catLabel": func(cat string) string {
			m := map[string]string{
				"ssh_bruteforce":  "SSH 暴力破解",
				"ssh_audit":       "SSH 登录审计",
				"fim":             "文件完整性",
				"process_anomaly": "异常进程",
				"sudo_audit":      "sudo 审计",
				"cat_ban":         "fail2ban 封禁",
			}
			if s, ok := m[cat]; ok {
				return s
			}
			return cat
		},
		"cmpGood": func(cur, prev float64) string {
			d := cur - prev
			absd := d
			if absd < 0 {
				absd = -absd
			}
			if d > 0.0001 {
				return fmt.Sprintf(`<span class="cmp good">▲ %s</span>`, formatNum(absd))
			} else if d < -0.0001 {
				return fmt.Sprintf(`<span class="cmp bad">▼ %s</span>`, formatNum(absd))
			}
			return `<span class="cmp flat">—</span>`
		},
		"cmpBad": func(cur, prev float64) string {
			d := cur - prev
			absd := d
			if absd < 0 {
				absd = -absd
			}
			if d > 0.0001 {
				return fmt.Sprintf(`<span class="cmp bad">▲ %s</span>`, formatNum(absd))
			} else if d < -0.0001 {
				return fmt.Sprintf(`<span class="cmp good">▼ %s</span>`, formatNum(absd))
			}
			return `<span class="cmp flat">—</span>`
		},
		"spark": func(pts []linePoint) template.HTML {
			if len(pts) == 0 {
				return template.HTML("")
			}
			return template.HTML(sparkline(pts, "#409eff", 120, 34))
		},
	}
	tmplBytes, err := reportFS.ReadFile("template.html")
	if err != nil {
		return fmt.Sprintf("<html><body><pre>模板读取失败: %v</pre></body></html>", err)
	}
	tmpl := template.Must(template.New("report").Funcs(funcs).Parse(string(tmplBytes)))
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Sprintf("<html><body><pre>报告渲染失败: %v</pre></body></html>", err)
	}
	return buf.String()
}

// ---- 阈值常量 ----

const (
	// cpuWarn/cpuCrit CPU 使用率告警阈值（警告/紧急，%）。
	cpuWarn, cpuCrit = 70.0, 85.0
	// memWarn/memCrit 内存使用率告警阈值（警告/紧急，%）。
	memWarn, memCrit = 80.0, 90.0
	// diskWarn/diskCrit 磁盘使用率告警阈值（警告/紧急，%）。
	diskWarn, diskCrit = 80.0, 90.0
	// connWarn/connCrit TCP 连接数使用率告警阈值（警告/紧急，%）。
	connWarn, connCrit = 80.0, 90.0 // 连接数使用率 %
	// hitWarn/hitCrit 缓存命中率低于该值即告警（%，警告/紧急）。
	hitWarn, hitCrit = 90.0, 80.0 // 命中率低于该值告警（%）
	// redisMemWarn/redisMemCrit Redis 内存使用率告警阈值（警告/紧急，%）。
	redisMemWarn, redisMemCrit = 80.0, 90.0
	// redisLatWarn/redisLatCrit Redis 命令时延告警阈值（警告/紧急，ms）。
	redisLatWarn, redisLatCrit = 5.0, 20.0 // ms
	// dbLatWarn/dbLatCrit 关系型数据库平均语句时延告警阈值（警告/紧急，ms）。
	dbLatWarn, dbLatCrit = 50.0, 200.0 // ms，关系型数据库平均语句时延
)
