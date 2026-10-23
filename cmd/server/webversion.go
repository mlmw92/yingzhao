package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// webVersionRE 匹配 web/scripts/gen-version.mjs 生成的版本文件内容：
//
//	const o="1.43.0";export{o as W};
var webVersionRE = regexp.MustCompile(`"([0-9]+\.[0-9]+\.[0-9]+[^"]*)"`)

// readWebVersion 读取前端产物里的版本号。
//
// 为什么要读它：前端产物放在磁盘（webDir），升级时只换二进制就会出现
// 「后端 1.43、页面还是 1.30」的静默错配——页面能开、接口也通，
// 只有某些行为对不上，很难被发现。启动时比一次，不一致就告警。
func readWebVersion(webDir string) (string, error) {
	files, err := filepath.Glob(filepath.Join(webDir, "assets", "version-*.js"))
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", fmt.Errorf("未找到 %s/assets/version-*.js", webDir)
	}
	// 部署残留多个版本文件时取最新的那个（旧产物可能没清干净）。
	sort.Slice(files, func(i, j int) bool {
		si, ei := os.Stat(files[i])
		sj, ej := os.Stat(files[j])
		if ei != nil || ej != nil {
			return files[i] < files[j]
		}
		return si.ModTime().After(sj.ModTime())
	})
	data, err := os.ReadFile(files[0])
	if err != nil {
		return "", err
	}
	m := webVersionRE.FindSubmatch(data)
	if m == nil {
		return "", fmt.Errorf("%s 里没有版本号", files[0])
	}
	return string(m[1]), nil
}

// warnOnWebVersionMismatch 在启动时比对前端产物与二进制版本，不一致就告警。
//
// 前端产物不在二进制里：只换二进制、不换 web 是很容易发生的静默错配
// （页面能开、接口也通，但界面行为停在旧版本）。
func warnOnWebVersionMismatch(webDir, serverVersion string) {
	if strings.TrimSpace(webDir) == "" {
		return
	}
	webVer, err := readWebVersion(webDir)
	if err != nil {
		slog.Warn("未找到前端产物版本，可能未部署前端", "webDir", webDir, "err", err)
		return
	}
	if webVer != serverVersion {
		slog.Warn("前端产物与后端版本不一致，请重新部署 web",
			"web", webVer, "server", serverVersion, "webDir", webDir)
	}
}
