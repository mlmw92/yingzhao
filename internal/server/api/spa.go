package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// RegisterDashboard 将前端 SPA 注册到 mux 根路径（从磁盘 webDir 读取）。
// 改前端只需替换 webDir 下文件并重启 server，无需重新编译二进制。
func (a *API) RegisterDashboard(mux *http.ServeMux) {
	webDir := a.webDir
	if webDir == "" {
		webDir = "/etc/monitor-server/web"
	}
	indexFile := filepath.Join(webDir, "index.html")
	if _, err := os.Stat(indexFile); err != nil {
		mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "前端资源未找到（webDir="+webDir+"），请把 web/dist 拷贝到该目录", http.StatusInternalServerError)
		})
		return
	}
	fs := http.FileServer(http.Dir(webDir))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		serveIndex := func() {
			// index 必须每次回源：否则部署后浏览器一直用旧 index，
			// 它引用的带旧哈希的 chunk 已被删除
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFile(w, r, indexFile)
		}
		// SPA：根路径或找不到的静态资源回退到 index.html
		if r.URL.Path == "/" {
			serveIndex()
			return
		}
		fp := filepath.Join(webDir, filepath.Clean(r.URL.Path))
		if _, err := os.Stat(fp); err != nil {
			// 带扩展名的静态资源（如 /assets/xxx.js、/xxx.css）缺失应返回 404，
			// 否则回退 index.html 会被浏览器当作 JS/CSS 模块解析而报
			// “Failed to fetch dynamically imported module” 等难懂错误。
			if filepath.Ext(r.URL.Path) != "" {
				http.NotFound(w, r)
				return
			}
			serveIndex()
			return
		}
		// 带内容哈希的资源文件名唯一，可以长缓存
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		fs.ServeHTTP(w, r)
	})
}
