package configdoc

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/mlmw92/yingzhao/internal/server/config"
)

// Persister 是一个配置文档的原始字节读写接口。
//
// 为什么是「原始字节」而不是结构化 JSON：现有 13 个配置 store 各自用 YAML 序列化，
// 换成 JSON 结构体要逐个补 tag、还有丢字段的风险。持久化层只负责搬字节，
// 序列化格式留给 store 自己，迁移时也能原样搬运文件内容。
type Persister interface {
	// Load 返回文档字节；不存在时返回 ErrNotFound。
	Load() ([]byte, error)
	// Save 写入文档字节。
	Save(data []byte) error
	// Name 用于日志（文件路径或 db:<namespace>）。
	Name() string
}

// FilePersister 用本地文件保存一个文档（测试与迁移源）。
type FilePersister struct {
	path string
	mu   sync.Mutex
}

func NewFilePersister(path string) *FilePersister { return &FilePersister{path: path} }

func (p *FilePersister) Name() string { return p.path }

func (p *FilePersister) Load() ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	data, err := os.ReadFile(p.path)
	if os.IsNotExist(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (p *FilePersister) Save(data []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return config.AtomicWrite(p.path, data)
}

// DBPersister 用 configdoc.Store 保存一个命名空间。
//
// 文档以 JSON 信封存储（format=yaml + base64 内容）；传入 key 时用 AES-256-GCM
// 加密（AI Key、通知口令等敏感配置必须走这条路径）。
type DBPersister struct {
	store  Store
	ns     string
	key    []byte
	format string

	mu      sync.Mutex
	version int64
}

func NewDBPersister(store Store, ns string, key []byte) *DBPersister {
	return &DBPersister{store: store, ns: ns, key: key, format: "yaml"}
}

// NewDBPersisterFormat 指定文档格式（yaml / json）。
// 格式只是标签：数据一律按原始字节保存，读取时不做转换。
func NewDBPersisterFormat(store Store, ns string, key []byte, format string) *DBPersister {
	if format == "" {
		format = "yaml"
	}
	return &DBPersister{store: store, ns: ns, key: key, format: format}
}

func (p *DBPersister) Name() string { return "db:" + p.ns }

type documentEnvelope struct {
	Format   string `json:"format"`
	Encoding string `json:"encoding"` // base64 | aes-gcm
	Nonce    string `json:"nonce,omitempty"`
	Data     string `json:"data"`
}

func (p *DBPersister) Load() ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	payload, version, err := p.store.Load(context.Background(), p.ns)
	if errors.Is(err, ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.version = version

	var env documentEnvelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return nil, fmt.Errorf("配置文档 %s 不是合法信封: %w", p.ns, err)
	}
	if env.Format != "yaml" && env.Format != "json" {
		return nil, fmt.Errorf("配置文档 %s 的格式 %q 不受支持", p.ns, env.Format)
	}
	raw, err := base64.StdEncoding.DecodeString(env.Data)
	if err != nil {
		return nil, fmt.Errorf("配置文档 %s 内容解码失败: %w", p.ns, err)
	}
	switch env.Encoding {
	case "base64":
		return raw, nil
	case "aes-gcm":
		if len(p.key) == 0 {
			return nil, fmt.Errorf("配置文档 %s 已加密，但未配置主密钥", p.ns)
		}
		nonce, err := base64.StdEncoding.DecodeString(env.Nonce)
		if err != nil {
			return nil, fmt.Errorf("配置文档 %s 的 nonce 解码失败: %w", p.ns, err)
		}
		return decrypt(p.key, nonce, raw)
	default:
		return nil, fmt.Errorf("配置文档 %s 的编码 %q 不受支持", p.ns, env.Encoding)
	}
}

func (p *DBPersister) Save(data []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	env := documentEnvelope{Format: p.format, Encoding: "base64"}
	if len(p.key) > 0 {
		nonce, cipherText, err := encrypt(p.key, data)
		if err != nil {
			return err
		}
		env.Encoding = "aes-gcm"
		env.Nonce = base64.StdEncoding.EncodeToString(nonce)
		env.Data = base64.StdEncoding.EncodeToString(cipherText)
	} else {
		env.Data = base64.StdEncoding.EncodeToString(data)
	}
	payload, err := json.Marshal(env)
	if err != nil {
		return err
	}
	version, err := p.store.Save(context.Background(), p.ns, payload, p.version)
	if err != nil {
		return err
	}
	p.version = version
	return nil
}

func encrypt(key, plain []byte) (nonce, cipherText []byte, err error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, fmt.Errorf("主密钥不可用: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	return nonce, gcm.Seal(nil, nonce, plain, nil), nil
}

func decrypt(key, nonce, cipherText []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("主密钥不可用: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, nonce, cipherText, nil)
	if err != nil {
		return nil, fmt.Errorf("配置文档解密失败（主密钥不匹配？）: %w", err)
	}
	return plain, nil
}

// LoadKey 读取主密钥（32 字节）；文件不存在时生成一个并以 0600 落盘。
//
// 与审计链密钥同样的取向：密钥不进数据库、不随配置扩散。
// 一次部署的所有实例必须共用同一份主密钥，否则另一实例解不开加密文档。
func LoadKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		if len(data) != 32 {
			return nil, fmt.Errorf("主密钥长度应为 32 字节，实际 %d", len(data))
		}
		return data, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("读取主密钥失败: %w", err)
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("生成主密钥失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, fmt.Errorf("写入主密钥失败: %w", err)
	}
	return key, nil
}
