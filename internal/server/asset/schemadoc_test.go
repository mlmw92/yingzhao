//go:build schemadoc

package asset

// 本文件只在显式导出表结构文档时编译：
//
//	go test -tags schemadoc ./internal/server/asset -run TestDumpSchemaScripts
//
// 输出到 docs/designs/database-schema/{mysql,postgres,dm}.sql，带表/列注释。
// 普通 `go test ./...` 不会执行它（不会产生写文件的副作用）。

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mlmw92/yingzhao/internal/server/migrate"
)

var tableComments = map[string]string{
	"config_documents":          "整份读写的配置文档（JSON 信封；敏感内容 AES-GCM 加密）",
	"nodes":                     "节点注册信息与最近心跳",
	"node_groups":               "节点分组",
	"asset_types":               "资产类型与类型模型",
	"assets":                    "资产台账主表",
	"asset_attrs":               "资产属性（采集 / 人工 / 派生）",
	"asset_links":               "资产关系",
	"asset_link_suppressions":   "人工删除过的采集关系（阻止采集侧重建）",
	"asset_changes":             "资产字段级变更记录",
	"snapshots":                 "配置快照头",
	"snapshot_fields":           "配置快照字段",
	"asset_seen":                "资产最近一次采集上报时间",
	"inspect_runs":              "巡检批次",
	"inspect_run_manifest":      "巡检批次清单标记",
	"inspect_run_members":       "巡检批次包含的资产",
	"inspect_findings":          "巡检差异项",
	"inspect_finding_baselines": "差异项与标杆来源的关联",
	"inspect_baselines":         "资产类型的巡检标杆",
	"asset_ignored":             "人工忽略的资产",
	"asset_labels":              "资产标签",
	"audit_events":              "审计事件（seq + prev_hash + hash 组成哈希链）",
	"audit_chain":               "审计链头（单行，FOR UPDATE 串行追加）",
	"audit_pruned":              "合法裁剪区间与 HMAC 锚点",
	"alert_acks":                "告警处置（认领 / 指派 / 关闭 / 评论）",
	"ops_tasks":                 "下行操作任务",
	"ops_meta":                  "下行任务元数据（序号等）",
	"ops_caps":                  "节点声明的可执行动作",
	"schema_version":            "资产库结构版本",
	"schema_migrations":         "迁移版本与校验和",
	"migration_lock":            "达梦迁移锁控制表",
}

var columnComments = map[string]map[string]string{
	"config_documents": {
		"namespace":  "配置命名空间（notify / ui / alert_rules …）",
		"version":    "乐观锁版本号，每次写入 +1",
		"payload":    "JSON 信封：format / encoding / nonce / data",
		"updated_at": "最后写入时间（epoch ms）",
		"updated_by": "最后写入人（预留）",
	},
	"nodes": {
		"hostname":     "主机名（主键，Agent 上报原样）",
		"mode":         "运行模式 collect / edge / hub",
		"display_name": "界面显示名（人工设置，不改 hostname）",
		"ip":           "最近上报 IP",
		"os":           "操作系统",
		"arch":         "CPU 架构",
		"group_name":   "所属分组（Server 端管理）",
		"labels":       "标签 JSON",
		"version":      "Agent 版本",
		"host_info":    "主机硬件信息 JSON（注册级字段）",
		"log_sources":  "日志来源 JSON",
		"status":       "online / offline",
		"last_seen":    "最近心跳时间（epoch ms）",
		"created_at":   "首次注册时间",
		"updated_at":   "最近写入时间",
	},
	"node_groups": {
		"name":        "分组名（主键）",
		"description": "分组说明",
		"created_at":  "创建时间",
	},
	"asset_types": {
		"key":     "类型键（host / redis / mysql …）",
		"title":   "显示名",
		"builtin": "是否内置类型（1/0）",
		"schema":  "类型模型 JSON（关注字段、属性元数据）",
	},
	"assets": {
		"id":          "自增主键",
		"type_key":    "资产类型键",
		"natural_key": "自然键（类型内唯一：hostname / host:port …）",
		"name":        "显示名",
		"node":        "归属节点",
		"created_at":  "创建时间",
		"updated_at":  "最近更新时间",
	},
	"asset_attrs": {
		"asset_id":   "资产 ID",
		"key":        "属性键",
		"value":      "属性值（文本）",
		"source":     "来源 discovery / manual / derived",
		"updated_at": "更新时间",
		"updated_by": "更新人",
	},
	"asset_links": {
		"id":         "自增主键",
		"from_id":    "起点资产 ID",
		"to_id":      "终点资产 ID",
		"kind":       "关系类型（runs_on / depends_on …）",
		"source":     "来源 discovery / manual",
		"created_at": "创建时间",
	},
	"asset_link_suppressions": {
		"id":         "自增主键",
		"from_id":    "起点资产 ID",
		"to_id":      "终点资产 ID",
		"kind":       "关系类型",
		"created_at": "抑制时间",
		"created_by": "操作人",
	},
	"asset_changes": {
		"id":         "自增主键",
		"asset_id":   "资产 ID",
		"field":      "变更字段",
		"old_value":  "旧值",
		"new_value":  "新值",
		"source":     "变更来源",
		"actor":      "操作人 / 采集器",
		"kind":       "变更类型（create / update / delete …）",
		"at":         "变更时间（epoch ms）",
		"request_id": "关联请求 ID（与审计对齐）",
	},
	"snapshots": {
		"id":       "自增主键",
		"asset_id": "资产 ID",
		"taken_at": "快照时间",
	},
	"snapshot_fields": {
		"snapshot_id": "快照 ID",
		"key":         "字段名",
		"value":       "字段值",
	},
	"asset_seen": {
		"asset_id":     "资产 ID（主键）",
		"last_seen_at": "最近一次采集上报时间",
	},
	"inspect_runs": {
		"id":         "自增主键",
		"scope":      "巡检范围",
		"actor":      "触发人 / 调度器",
		"started_at": "开始时间",
		"assets":     "巡检资产数",
		"baselined":  "命中标杆数",
		"findings":   "差异数",
		"truncated":  "是否因上限截断（1/0）",
	},
	"inspect_run_manifest": {
		"run_id": "巡检批次 ID（主键）",
	},
	"inspect_run_members": {
		"run_id":      "巡检批次 ID",
		"asset_id":    "资产 ID",
		"node_at_run": "巡检时的归属节点",
		"baselined":   "该资产是否命中标杆（1/0）",
		"findings":    "该资产的差异数",
	},
	"inspect_findings": {
		"id":         "自增主键",
		"run_id":     "巡检批次 ID",
		"asset_id":   "资产 ID",
		"asset_type": "资产类型键（快照）",
		"asset_key":  "资产自然键（快照）",
		"asset_name": "资产显示名（快照）",
		"node":       "归属节点（快照）",
		"field":      "差异字段",
		"kind":       "差异类型（missing / extra / changed …）",
		"level":      "级别（warning / critical …）",
		"expected":   "期望值",
		"actual":     "实际值",
		"at":         "产生时间",
	},
	"inspect_finding_baselines": {
		"finding_id": "差异项 ID（主键）",
		"asset_id":   "标杆来源资产 ID",
	},
	"inspect_baselines": {
		"type_key":    "资产类型键（主键）",
		"asset_id":    "标杆资产 ID",
		"asset_key":   "标杆资产自然键（快照）",
		"snapshot_id": "标杆快照 ID",
		"set_by":      "设置人",
		"set_at":      "设置时间",
	},
	"asset_ignored": {
		"asset_id":   "资产 ID（主键）",
		"reason":     "忽略原因",
		"ignored_by": "操作人",
		"ignored_at": "忽略时间",
	},
	"asset_labels": {
		"asset_id":   "资产 ID",
		"key":        "标签键",
		"value":      "标签值",
		"updated_at": "更新时间",
		"updated_by": "更新人",
	},
	"audit_events": {
		"id":         "自增主键",
		"time_ms":    "事件时间（epoch ms）",
		"user_name":  "操作人",
		"method":     "HTTP 方法",
		"path":       "请求路径",
		"status":     "HTTP 状态码",
		"remote_ip":  "来源 IP",
		"source_loc": "IP 属地",
		"succeeded":  "是否成功（1/0）",
		"category":   "分类（asset / ops / security …）",
		"action":     "动作标识",
		"detail":     "详情",
		"request_id": "请求关联 ID",
		"seq":        "链上序号（0 = 启用链之前的历史行）",
		"prev_hash":  "前一条哈希",
		"hash":       "本条哈希",
		"key_fp":     "链密钥指纹",
	},
	"audit_chain": {
		"id":              "固定为 1（单行）",
		"seq":             "当前链头序号",
		"hash":            "当前链头哈希",
		"key_fingerprint": "链密钥指纹",
		"updated_ms":      "更新时间",
	},
	"audit_pruned": {
		"from_seq":  "裁剪区间起始 seq",
		"to_seq":    "裁剪区间结束 seq",
		"head_hash": "区间末条哈希（校验锚点）",
		"rows":      "被裁行数",
		"pruned_ms": "裁剪时间",
		"mac":       "裁剪记录的 HMAC",
	},
	"alert_acks": {
		"ack_key":       "处置键 rule|host|instance|startsAt",
		"rule":          "规则名",
		"host":          "主机",
		"instance":      "实例",
		"starts_at":     "告警触发时间",
		"status":        "pending / ack / closed",
		"user_name":     "最近操作人",
		"assignee":      "当前处理人",
		"time_ms":       "最近操作时间",
		"ack_time_ms":   "认领时间",
		"close_time_ms": "关闭时间",
		"close_reason":  "关闭原因",
		"comments":      "评论 JSON",
	},
	"ops_tasks": {
		"id":         "任务 ID（ops-N / ob-N）",
		"batch_id":   "批次 ID",
		"node":       "目标节点",
		"kind":       "动作标识",
		"state":      "queued / running / succeeded / failed / canceled / expired",
		"created_at": "创建时间",
		"done_at":    "结束时间",
		"task_json":  "完整任务 JSON（参数 / 结果 / 操作人）",
	},
	"ops_meta": {
		"key":   "元数据键（目前只有 seq）",
		"value": "元数据值",
	},
	"ops_caps": {
		"node":  "节点名（主键）",
		"kinds": "该节点可执行动作（逗号分隔）",
	},
	"schema_version": {
		"id":      "固定为 1",
		"version": "资产库结构版本",
	},
	"schema_migrations": {
		"version":    "迁移版本号（主键）",
		"name":       "迁移名",
		"checksum":   "迁移 SQL 的校验和",
		"applied_at": "应用时间（epoch ms）",
	},
	"migration_lock": {
		"id":     "固定为 1",
		"holder": "持锁实例标识（预留）",
	},
}

var createTableRE = regexp.MustCompile(`(?i)CREATE TABLE IF NOT EXISTS\s+([A-Za-z_][A-Za-z0-9_]*)`)
var columnLineRE = regexp.MustCompile("^\\s*[`\"]?([A-Za-z_][A-Za-z0-9_]*)[`\"]?\\s+(BIGINT|INT|VARCHAR|LONGTEXT|TEXT|TINYINT|SMALLINT|JSON|CLOB|BIGSERIAL)\\b")
var constraintWords = map[string]bool{
	"PRIMARY": true, "UNIQUE": true, "CONSTRAINT": true, "FOREIGN": true, "CHECK": true,
}

func TestDumpSchemaScripts(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "docs", "designs", "database-schema")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	dumps := []struct {
		name    string
		dialect string
		stmts   []string
	}{
		{"mysql", "mysql", mySQLSchemaStatements},
		{"postgres", "postgres", postgresSchemaStatements},
		{"dm", "dm", dmSchemaStatements},
	}
	for _, d := range dumps {
		path := filepath.Join(dir, d.name+".sql")
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(f, "-- yingzhao 状态库表结构（%s）\n", d.name)
		fmt.Fprintln(f, "-- 由 internal/server 的迁移与 schema_*.go 生成；改表结构请改源码后重新导出：")
		fmt.Fprintln(f, "--   go test -tags schemadoc ./internal/server/asset -run TestDumpSchemaScripts")
		fmt.Fprintln(f)

		ms, err := migrate.Load(d.dialect)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range ms {
			fmt.Fprintf(f, "-- ===== 迁移 %04d %s =====\n", m.Version, m.Name)
			writeDDL(f, m.SQL)
			fmt.Fprintln(f)
		}
		for _, stmt := range d.stmts {
			writeDDL(f, stmt)
			fmt.Fprintln(f)
		}
		if d.name == "dm" {
			fmt.Fprintln(f, "-- ===== 索引（启动时先查 USER_INDEXES 再建）=====")
			for _, idx := range dmIndexStatements {
				fmt.Fprintf(f, "-- %s\n%s;\n", idx.name, idx.ddl)
			}
			fmt.Fprintln(f)
			fmt.Fprintln(f, "-- ===== 迁移锁控制表（由迁移执行器创建）=====")
			fmt.Fprintln(f, "-- migration_lock：达梦迁移锁控制表")
			fmt.Fprintln(f, "CREATE TABLE IF NOT EXISTS migration_lock(")
			fmt.Fprintln(f, "  id     INT PRIMARY KEY,        -- 固定为 1")
			fmt.Fprintln(f, "  holder VARCHAR(64)             -- 持锁实例标识（预留）")
			fmt.Fprintln(f, ");")
		}
		writeSchemaMigrations(f, d.name)
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// writeDDL 给 CREATE TABLE 加表/列注释；其余语句原样输出。
func writeDDL(f *os.File, stmt string) {
	stmt = strings.TrimSpace(stmt)
	m := createTableRE.FindStringSubmatch(stmt)
	if m == nil {
		fmt.Fprintf(f, "%s;\n", stmt)
		return
	}
	table := m[1]
	if c := tableComments[table]; c != "" {
		fmt.Fprintf(f, "-- %s：%s\n", table, c)
	}
	lines := strings.Split(stmt, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if cm := columnLineRE.FindStringSubmatch(trimmed); cm != nil && !constraintWords[strings.ToUpper(cm[1])] {
			col := strings.ToLower(cm[1])
			if c := columnComments[table][col]; c != "" {
				if i == len(lines)-1 {
					fmt.Fprintf(f, "%s;  -- %s\n", line, c)
				} else {
					fmt.Fprintf(f, "%s  -- %s\n", line, c)
				}
				continue
			}
		}
		if i == len(lines)-1 {
			fmt.Fprintf(f, "%s;\n", line)
		} else {
			fmt.Fprintln(f, line)
		}
	}
}

// writeSchemaMigrations 补上迁移执行器创建的表（不在迁移文件里）。
func writeSchemaMigrations(f *os.File, dialect string) {
	fmt.Fprintln(f, "-- ===== 迁移执行器创建 =====")
	fmt.Fprintln(f, "-- schema_migrations：迁移版本与校验和")
	fmt.Fprintln(f, "CREATE TABLE IF NOT EXISTS schema_migrations(")
	fmt.Fprintln(f, "  version    INT PRIMARY KEY,   -- 迁移版本号")
	fmt.Fprintln(f, "  name       VARCHAR(128) NOT NULL, -- 迁移名")
	fmt.Fprintln(f, "  checksum   VARCHAR(64) NOT NULL,  -- 迁移 SQL 的校验和")
	fmt.Fprintln(f, "  applied_at BIGINT NOT NULL        -- 应用时间（epoch ms）")
	if dialect == "mysql" {
		fmt.Fprintln(f, ") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;")
	} else {
		fmt.Fprintln(f, ");")
	}
}
