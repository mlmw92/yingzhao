package config

import (
	"os"
	"path/filepath"
	"testing"
)

// 安装脚本生成的集中日志配置必须被服务端**读懂**。
//
// 为什么需要这条契约用例：install-server.sh 写的是 YAML 键名（logDir / logBackend /
// logVictoriaLogs.addr），而服务端认的是结构体 tag。两边任一处改名，症状都是
// **静默降级**——运维按安装脚本切到了外部日志后端，服务端却仍在用自研落盘
// （或反过来），没有任何报错，直到有人发现"日志怎么还在本地"。
//
// 下面的 YAML 片段**逐字来自 deploy/install-server.sh 生成的 log 块**
// （见该脚本 write_config 里的 $log_block 与周边注释）。改脚本时必须同步改这里，
// 反之亦然——两处不一致本身就是这条用例要暴露的问题。
func writeConfigFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "server.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	return path
}

func TestLoadInstallerGeneratedLogConfig(t *testing.T) {
	t.Run("VictoriaLogs 地址与超时", func(t *testing.T) {
		path := writeConfigFile(t, `mode: standalone
listen: ":8080"
tsdb:
  backend: victoriametrics
  addr: "http://127.0.0.1:8428"

# 集中日志存储：唯一后端是 VictoriaLogs（由 deploy/install-logs.sh 安装）
logVictoriaLogs:
  addr: "http://10.0.0.10:9428"
  queryTimeout: 45
`)
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("加载配置失败: %v", err)
		}
		if cfg.LogVictoriaLogs.Addr != "http://10.0.0.10:9428" {
			t.Fatalf("logVictoriaLogs.addr 未生效: %q（安装脚本与结构体 tag 可能已不一致）", cfg.LogVictoriaLogs.Addr)
		}
		if cfg.LogVictoriaLogs.QueryTimeout != 45 {
			t.Fatalf("queryTimeout 未生效: %d", cfg.LogVictoriaLogs.QueryTimeout)
		}
		// 未写的字段取默认值
		if cfg.LogVictoriaLogs.WriteTimeout != 10 {
			t.Fatalf("writeTimeout 应取默认 10，实际 %d", cfg.LogVictoriaLogs.WriteTimeout)
		}
	})

	// 兼容性：这次改动之前生成的 server.yaml 里还有 logBackend / logDir。
	// 它们必须**能被解析且被忽略**（不能让老配置直接解析失败），
	// 但不再生效——日志只走 VictoriaLogs。
	t.Run("旧配置里的 logBackend/logDir 被忽略", func(t *testing.T) {
		path := writeConfigFile(t, `mode: standalone
logDir: "/var/lib/monitor-server/logs"
logBackend: local
logVictoriaLogs:
  addr: "http://127.0.0.1:9428"
`)
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("老配置不应解析失败: %v", err)
		}
		if cfg.LogVictoriaLogs.Addr != "http://127.0.0.1:9428" {
			t.Fatalf("addr 未生效: %q", cfg.LogVictoriaLogs.Addr)
		}
	})
}

func TestDatabaseConfigDefaultsAndParse(t *testing.T) {
	cfg := Default()
	if cfg.Database.MaxOpenConns != 20 || cfg.Database.MaxIdleConns != 5 || cfg.Database.ConnMaxLifetime != 1800 {
		t.Fatalf("默认值 = %+v", cfg.Database)
	}
	path := writeConfigFile(t, `
database:
  dsn: "u:p@tcp(127.0.0.1:3306)/yingzhao?charset=utf8mb4"
  maxOpenConns: 50
`)
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Database.DSN == "" || got.Database.MaxOpenConns != 50 || got.Database.MaxIdleConns != 5 {
		t.Fatalf("解析结果 = %+v", got.Database)
	}
}

// 安装脚本生成的 database 段必须能被解析（driver + 两种 DSN 形态）。
func TestLoadInstallerGeneratedDatabaseConfig(t *testing.T) {
	cases := []struct {
		name, body string
	}{
		{"mysql", `mode: standalone
database:
  driver: "mysql"
  dsn: "yingzhao:pass@tcp(127.0.0.1:3306)/yingzhao?charset=utf8mb4"
  maxOpenConns: 20
  maxIdleConns: 5
  connMaxLifetimeSeconds: 1800
  masterKeyFile: "/etc/monitor-server/master.key"
`},
		{"postgres", `mode: standalone
database:
  driver: "postgres"
  dsn: "host=127.0.0.1 port=5432 user=yingzhao password=pass dbname=yingzhao sslmode=disable"
  masterKeyFile: "/etc/monitor-server/master.key"
`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeConfigFile(t, tc.body)
			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("加载配置失败: %v", err)
			}
			if cfg.Database.Driver != tc.name {
				t.Fatalf("driver = %q, want %q", cfg.Database.Driver, tc.name)
			}
			if cfg.Database.DSN == "" {
				t.Fatal("dsn 未解析")
			}
			if cfg.Database.MaxOpenConns != 20 {
				t.Fatalf("默认 maxOpenConns = %d", cfg.Database.MaxOpenConns)
			}
		})
	}
}
