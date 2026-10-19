// Command fastdfs-exporter 把 FastDFS 的集群状态暴露为 Prometheus 指标。
//
// 数据来源是 FastDFS 自带的 fdfs_monitor：既可以在宿主机上直接执行，
// 也可以通过 docker exec 在容器里执行。指标名与平台的采集契约
// （internal/agent/collector/fastdfs.go）一致，单位统一为字节。
//
// 设计取舍：不提供磁盘 IO 与网络累计值——fdfs_monitor 没有这两项，
// 与其上报 0 让人误以为「没有流量」，不如让对应指标缺席（界面显示 -）。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const mb = 1024 * 1024

// version 由构建脚本通过 -X main.version 注入。
var version = "dev"

type config struct {
	listen     string
	mode       string
	clientConf string
	container  string
	monitorBin string
	cacheTTL   time.Duration
	timeout    time.Duration
}

type stats struct {
	Groups      int64
	Storages    int64
	Active      int64
	TotalMB     int64
	FreeMB      int64
	TrunkFreeMB int64
}

var (
	reGroups   = regexp.MustCompile(`group count:\s*(\d+)`)
	reTotal    = regexp.MustCompile(`disk total space = (\d+) MB`)
	reFree     = regexp.MustCompile(`disk free space = (\d+) MB`)
	reTrunk    = regexp.MustCompile(`trunk free space = (\d+) MB`)
	reStorages = regexp.MustCompile(`storage server count = (\d+)`)
	reActive   = regexp.MustCompile(`active server count = (\d+)`)
)

// sumMatches 把同一 pattern 的所有命中相加。
// fdfs_monitor 每个 group 输出一段，多 group 时空间与计数都要累加。
func sumMatches(re *regexp.Regexp, output string) (int64, bool) {
	matches := re.FindAllStringSubmatch(output, -1)
	if len(matches) == 0 {
		return 0, false
	}
	var total int64
	for _, m := range matches {
		v, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			continue
		}
		total += v
	}
	return total, true
}

// parse 解析 fdfs_monitor 的输出。
//
// 一条可识别的数据都没有时返回错误：命令成功但输出为空/格式变了，
// 不能当成「集群健康」上报，否则页面会显示在线却没有数据。
func parse(output string) (stats, error) {
	var s stats
	var seen bool
	if v, ok := sumMatches(reGroups, output); ok {
		s.Groups, seen = v, true
	}
	if v, ok := sumMatches(reTotal, output); ok {
		s.TotalMB, seen = v, true
	}
	if v, ok := sumMatches(reFree, output); ok {
		s.FreeMB, seen = v, true
	}
	if v, ok := sumMatches(reTrunk, output); ok {
		s.TrunkFreeMB, seen = v, true
	}
	if v, ok := sumMatches(reStorages, output); ok {
		s.Storages, seen = v, true
	}
	if v, ok := sumMatches(reActive, output); ok {
		s.Active, seen = v, true
	}
	if !seen {
		return stats{}, errors.New("fdfs_monitor 输出里没有可识别的指标（格式可能已变化）")
	}
	return s, nil
}

// render 按平台契约渲染 Prometheus 文本。
func render(s stats) string {
	usedMB := s.TotalMB - s.FreeMB
	if usedMB < 0 {
		usedMB = 0
	}
	offline := s.Storages - s.Active
	if offline < 0 {
		offline = 0
	}
	lines := []string{
		"# HELP fastdfs_up FastDFS 集群可达（fdfs_monitor 采集成功）",
		"# TYPE fastdfs_up gauge",
		"fastdfs_up 1",
		"# TYPE fastdfs_group_count gauge",
		fmt.Sprintf("fastdfs_group_count %d", s.Groups),
		"# TYPE fastdfs_storage_count gauge",
		fmt.Sprintf("fastdfs_storage_count %d", s.Storages),
		"# TYPE fastdfs_storage_online_count gauge",
		fmt.Sprintf("fastdfs_storage_online_count %d", s.Active),
		"# TYPE fastdfs_storage_offline_count gauge",
		fmt.Sprintf("fastdfs_storage_offline_count %d", offline),
		"# TYPE fastdfs_total_space gauge",
		fmt.Sprintf("fastdfs_total_space %d", s.TotalMB*mb),
		"# TYPE fastdfs_free_space gauge",
		fmt.Sprintf("fastdfs_free_space %d", s.FreeMB*mb),
		"# TYPE fastdfs_used_space gauge",
		fmt.Sprintf("fastdfs_used_space %d", usedMB*mb),
		"# TYPE fastdfs_trunk_free_space gauge",
		fmt.Sprintf("fastdfs_trunk_free_space %d", s.TrunkFreeMB*mb),
	}
	return strings.Join(lines, "\n") + "\n"
}

// command 依据模式拼出要执行的命令。
func command(cfg config) (string, []string) {
	if cfg.mode == "docker" {
		return "docker", []string{"exec", cfg.container, cfg.monitorBin, cfg.clientConf}
	}
	return cfg.monitorBin, []string{cfg.clientConf}
}

// collect 执行一次采集并渲染指标；失败时返回 fastdfs_up 0。
func collect(cfg config) string {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	bin, args := command(cfg)
	out, err := exec.CommandContext(ctx, bin, args...).Output()
	if err != nil {
		log.Printf("采集失败 mode=%s err=%v", cfg.mode, err)
		return "# 采集失败: " + sanitize(err.Error()) + "\n# TYPE fastdfs_up gauge\nfastdfs_up 0\n"
	}
	s, err := parse(string(out))
	if err != nil {
		log.Printf("解析失败 mode=%s err=%v", cfg.mode, err)
		return "# 解析失败: " + sanitize(err.Error()) + "\n# TYPE fastdfs_up gauge\nfastdfs_up 0\n"
	}
	return render(s)
}

// sanitize 避免把换行带进 Prometheus 注释行。
func sanitize(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "\r", " ")
}

// exporter 带一个短缓存：一次采集要起进程，避免每个抓取都执行一遍。
type exporter struct {
	cfg config

	mu      sync.Mutex
	at      time.Time
	body    string
}

func (e *exporter) metrics() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if time.Since(e.at) > e.cfg.cacheTTL {
		e.body = collect(e.cfg)
		e.at = time.Now()
	}
	return e.body
}

func (e *exporter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if p := r.URL.Path; p != "/metrics" && p != "/" {
		http.NotFound(w, r)
		return
	}
	body := e.metrics()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprint(w, body)
}

func main() {
	cfg := config{}
	flag.StringVar(&cfg.listen, "listen", "127.0.0.1:9110", "监听地址")
	flag.StringVar(&cfg.mode, "mode", "local", "采集模式：local（本机 fdfs_monitor）| docker（docker exec）")
	flag.StringVar(&cfg.clientConf, "client-conf", "/etc/fdfs/client.conf", "client.conf 路径")
	flag.StringVar(&cfg.container, "container", "mw-fastdfs", "docker 模式下的容器名")
	flag.StringVar(&cfg.monitorBin, "monitor-bin", "fdfs_monitor", "fdfs_monitor 可执行文件名或绝对路径")
	flag.DurationVar(&cfg.cacheTTL, "cache", 10*time.Second, "采集结果缓存时长")
	flag.DurationVar(&cfg.timeout, "timeout", 15*time.Second, "单次采集超时")
	showVersion := flag.Bool("version", false, "打印版本号")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	if cfg.mode != "local" && cfg.mode != "docker" {
		fmt.Fprintf(os.Stderr, "未知 mode: %q（可选 local|docker）\n", cfg.mode)
		os.Exit(2)
	}

	log.Printf("fastdfs-exporter 启动 listen=%s mode=%s clientConf=%s container=%s",
		cfg.listen, cfg.mode, cfg.clientConf, cfg.container)
	srv := &http.Server{Addr: cfg.listen, Handler: &exporter{cfg: cfg}, ReadHeaderTimeout: 5 * time.Second}
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
