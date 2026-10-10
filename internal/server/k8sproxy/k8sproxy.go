// Package k8sproxy 管理 Server 侧的 K8s 集群连接配置，并提供只读的 apiserver 实时查询代理。
//
// 这是 ADR-0004 对 ADR-0003 的修订：平台定位升级为运维平台后，资源清单需要
// 「点开即得」的实时态——异步下发的延迟等于一个上报周期，不满足管理面的要求。
// 凭据由运维在 Server 上手工维护于独立配置文件（含 token/kubeconfig，文件权限应设
// 0600），每次查询时读取、修改即时生效、无需重启。指标仍走 Agent 采集入库——
// apiserver 本身没有时序数据，这是 K8s 的硬事实。两条通路并存：Agent 不可达或
// 网闸部署的场景仍走 ADR-0003 的异步任务。
package k8sproxy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Cluster 是一条 Server 侧的集群连接配置。Token/Kubeconfig/CABase64 是凭据，
// JSON 序列化时刻意不带 json tag（List 输出不含它们，避免经接口外泄）。
type Cluster struct {
	Name        string `yaml:"name" json:"name"`
	APIServer   string `yaml:"apiServer" json:"apiServer"`
	Token       string `yaml:"token"`
	Kubeconfig  string `yaml:"kubeconfig"`
	InsecureTLS bool   `yaml:"insecureTLS" json:"insecureTLS"`
	CABase64    string `yaml:"caBase64"`
}

type fileConfig struct {
	Clusters []Cluster `yaml:"clusters"`
}

// Manager 管理集群连接配置文件。文件由运维手工维护，查询时读取，改完即生效。
type Manager struct {
	path string
}

// New 创建 Manager（path 为配置文件路径，默认 <DataDir>/k8s-live.yaml）。
func New(path string) *Manager { return &Manager{path: path} }

// List 返回已配置的集群（凭据字段不外泄）。
func (m *Manager) List() []Cluster {
	f, err := m.load()
	if err != nil || f == nil {
		return nil
	}
	out := make([]Cluster, 0, len(f.Clusters))
	for _, c := range f.Clusters {
		out = append(out, Cluster{Name: c.Name, APIServer: c.APIServer, InsecureTLS: c.InsecureTLS})
	}
	return out
}

// Get 读取一条集群配置（含凭据，仅供代理转发使用）。
func (m *Manager) Get(name string) (Cluster, bool) {
	f, err := m.load()
	if err != nil || f == nil {
		return Cluster{}, false
	}
	for _, c := range f.Clusters {
		if c.Name == name {
			return c, true
		}
	}
	return Cluster{}, false
}

// Path 返回配置文件路径（错误提示里指路用）。
func (m *Manager) Path() string { return m.path }

func (m *Manager) load() (*fileConfig, error) {
	raw, err := os.ReadFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			return &fileConfig{}, nil
		}
		return nil, err
	}
	var f fileConfig
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("解析 %s 失败: %w", m.path, err)
	}
	return &f, nil
}

// conn 是一次查询用的 apiserver 连接上下文。
type conn struct {
	server string
	token  string
	client *http.Client
}

// buildConn 构建 apiserver 连接：kubeconfig 优先，显式 token 兜底。
func buildConn(c Cluster) (*conn, error) {
	tlsCfg := &tls.Config{InsecureSkipVerify: c.InsecureTLS}
	server := strings.TrimRight(strings.TrimSpace(c.APIServer), "/")
	token := strings.TrimSpace(c.Token)

	if kcRaw := strings.TrimSpace(c.Kubeconfig); kcRaw != "" {
		kc, err := parseKubeconfig([]byte(kcRaw))
		if err != nil {
			return nil, fmt.Errorf("解析 kubeconfig 失败: %w", err)
		}
		if server == "" {
			server = kc.server
		}
		if len(kc.caData) > 0 {
			pool := x509.NewCertPool()
			if pool.AppendCertsFromPEM(kc.caData) {
				tlsCfg.RootCAs = pool
			}
		}
		if token == "" {
			token = kc.token
		}
		if len(kc.certData) > 0 && len(kc.keyData) > 0 {
			cert, err := tls.X509KeyPair(kc.certData, kc.keyData)
			if err != nil {
				return nil, fmt.Errorf("解析客户端证书失败: %w", err)
			}
			tlsCfg.Certificates = []tls.Certificate{cert}
		}
	}

	if ca := strings.TrimSpace(c.CABase64); ca != "" {
		if raw, err := base64.StdEncoding.DecodeString(ca); err == nil {
			pool := x509.NewCertPool()
			if pool.AppendCertsFromPEM(raw) {
				tlsCfg.RootCAs = pool
			}
		}
	}

	if server == "" {
		return nil, fmt.Errorf("apiServer 与 kubeconfig 均未提供 server 地址")
	}
	return &conn{
		server: server,
		token:  token,
		client: &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{TLSClientConfig: tlsCfg}},
	}, nil
}

// GetJSON 向指定集群的 apiserver 发起 GET，把响应体原样透传。
// 路径必须以 / 开头；凭据未配置的集群返回错误（由调用方决定如何呈现）。
func (m *Manager) GetJSON(ctx context.Context, name, path string) (json.RawMessage, error) {
	c, ok := m.Get(name)
	if !ok {
		return nil, fmt.Errorf("集群 %q 未在 %s 中配置", name, m.path)
	}
	conn, err := buildConn(c)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, conn.server+path, nil)
	if err != nil {
		return nil, err
	}
	if conn.token != "" {
		req.Header.Set("Authorization", "Bearer "+conn.token)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := conn.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("apiserver 返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.RawMessage(body), nil
}

// ---- kubeconfig YAML 的精简解析（取 current-context 对应的 cluster 与 user）----

type kubeconfigFile struct {
	CurrentContext string `yaml:"current-context"`
	Contexts       []struct {
		Name    string `yaml:"name"`
		Context struct {
			Cluster string `yaml:"cluster"`
			User    string `yaml:"user"`
		} `yaml:"context"`
	} `yaml:"contexts"`
	Clusters []struct {
		Name    string `yaml:"name"`
		Cluster struct {
			Server                   string `yaml:"server"`
			CertificateAuthorityData string `yaml:"certificate-authority-data"`
		} `yaml:"cluster"`
	} `yaml:"clusters"`
	Users []struct {
		Name string `yaml:"name"`
		User struct {
			Token                 string `yaml:"token"`
			ClientCertificateData string `yaml:"client-certificate-data"`
			ClientKeyData         string `yaml:"client-key-data"`
		} `yaml:"user"`
	} `yaml:"users"`
}

type kubeconfigData struct {
	server   string
	caData   []byte
	token    string
	certData []byte
	keyData  []byte
}

func parseKubeconfig(raw []byte) (*kubeconfigData, error) {
	var f kubeconfigFile
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	cc := strings.TrimSpace(f.CurrentContext)
	if cc == "" && len(f.Clusters) > 0 {
		cc = f.Clusters[0].Name
	}
	var clusterName, userName string
	for _, c := range f.Contexts {
		if c.Name == cc {
			clusterName, userName = c.Context.Cluster, c.Context.User
			break
		}
	}
	out := &kubeconfigData{}
	for _, c := range f.Clusters {
		if c.Name == clusterName {
			out.server = c.Cluster.Server
			if d := strings.TrimSpace(c.Cluster.CertificateAuthorityData); d != "" {
				out.caData, _ = base64.StdEncoding.DecodeString(d)
			}
		}
	}
	for _, u := range f.Users {
		if u.Name == userName {
			out.token = u.User.Token
			if d := strings.TrimSpace(u.User.ClientCertificateData); d != "" {
				out.certData, _ = base64.StdEncoding.DecodeString(d)
			}
			if d := strings.TrimSpace(u.User.ClientKeyData); d != "" {
				out.keyData, _ = base64.StdEncoding.DecodeString(d)
			}
		}
	}
	return out, nil
}
