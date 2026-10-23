package dockerproxy

import (
	"testing"
	"time"
)

// daemon 地址的两种 TCP 写法都要能连上。
//
// `tcp://host:2375` 是 agent.yaml 里 dockerInstances.addr 的**推荐写法**
// （文档与 model 注释都这么写）。如果这里只给裸 host:port 加 http://，
// 带前缀的写法会拼成 "http://tcp://host:2375" —— 一个永远连不上的地址，
// 而报错会被翻译成"daemon 不可达"，看起来像部署问题。
func TestClient_TCPAddrForms(t *testing.T) {
	cases := []struct{ addr, want string }{
		{"tcp://127.0.0.1:2375", "http://127.0.0.1:2375"},
		{"127.0.0.1:2375", "http://127.0.0.1:2375"},
		{"http://127.0.0.1:2375", "http://127.0.0.1:2375"},
		{"https://daemon.example:2376", "https://daemon.example:2376"},
	}
	for _, tc := range cases {
		_, base, err := client(tc.addr, time.Second)
		if err != nil {
			t.Fatalf("%s: %v", tc.addr, err)
		}
		if base != tc.want {
			t.Fatalf("%s 的请求基址 = %q，want %q", tc.addr, base, tc.want)
		}
	}
}
