package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	core "github.com/yttydcs/myflowhub-core"
	coreconfig "github.com/yttydcs/myflowhub-core/config"
)

const (
	nodeKeysFile        = "config/node_keys.json"
	trustedNodesFile    = "config/trusted_nodes.json"
	confNodePrivKey     = coreconfig.KeyAuthNodePrivKey
	confNodePubKey      = coreconfig.KeyAuthNodePubKey
	confTrustedNodesKey = coreconfig.KeyAuthTrustedNodes
)

type nodeKeys struct {
	PrivKey string `json:"privkey"` // base64 DER
	PubKey  string `json:"pubkey"`  // base64 DER
}

type bindingPersist struct {
	NodeID uint32   `json:"node_id"`
	PubKey string   `json:"pubkey,omitempty"`
	Role   string   `json:"role,omitempty"`
	Perms  []string `json:"perms,omitempty"`
}

type trustedFile struct {
	Bindings map[string]bindingPersist  `json:"bindings,omitempty"` // deviceID -> binding
	Meta     map[string]json.RawMessage `json:"meta,omitempty"`     // reserved
}

// loadOrCreateNodeKeys 加载节点密钥，若不存在则生成并写入文件与配置。
func loadOrCreateNodeKeys(cfg core.IConfig) (*ecdsa.PrivateKey, string, error) {
	privStr, _ := cfg.Get(confNodePrivKey)
	pubStr, _ := cfg.Get(confNodePubKey)
	if strings.TrimSpace(privStr) != "" && strings.TrimSpace(pubStr) != "" {
		priv, err := parsePrivKey(privStr)
		if err == nil {
			return priv, pubStr, nil
		}
	}
	// 尝试从文件加载
	if k, err := readNodeKeysFile(); err == nil && k.PrivKey != "" && k.PubKey != "" {
		if priv, err := parsePrivKey(k.PrivKey); err == nil {
			cfg.Set(confNodePrivKey, k.PrivKey)
			cfg.Set(confNodePubKey, k.PubKey)
			return priv, k.PubKey, nil
		}
	}
	// 生成新的
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, "", err
	}
	privDER, _ := x509.MarshalECPrivateKey(priv)
	pubDER, _ := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	privB64 := base64.StdEncoding.EncodeToString(privDER)
	pubB64 := base64.StdEncoding.EncodeToString(pubDER)
	_ = writeNodeKeysFile(nodeKeys{PrivKey: privB64, PubKey: pubB64})
	cfg.Set(confNodePrivKey, privB64)
	cfg.Set(confNodePubKey, pubB64)
	return priv, pubB64, nil
}

// loadTrustedBindings 读取 trusted_nodes 文件，将 bindings 注入 whitelist，同时补足 trusted 节点。
// 返回 whitelist、trusted 映射，以及文件中出现的最大 NodeID。
func loadTrustedBindings(cfg core.IConfig) (map[string]bindingRecord, map[uint32][]byte, uint32) {
	path := filepath.Clean(trustedNodesFile)
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return nil, nil, 0
	}
	var tf trustedFile
	if err := json.Unmarshal(data, &tf); err != nil {
		return nil, nil, 0
	}
	whitelist := make(map[string]bindingRecord)
	trusted := make(map[uint32][]byte)
	var maxNode uint32

	// bindings: device -> {node_id, pubkey, role, perms}
	for dev, entry := range tf.Bindings {
		dev = strings.TrimSpace(dev)
		if dev == "" || entry.NodeID == 0 {
			continue
		}
		var pubRaw []byte
		if strings.TrimSpace(entry.PubKey) != "" {
			if _, raw, err := parseECPubKey(entry.PubKey); err == nil {
				pubRaw = raw
				if _, ok := trusted[entry.NodeID]; !ok {
					trusted[entry.NodeID] = raw
				}
			}
		}
		rec := bindingRecord{
			NodeID: entry.NodeID,
			Role:   entry.Role,
			Perms:  cloneSlice(entry.Perms),
			PubKey: cloneSlice(pubRaw),
		}
		whitelist[dev] = rec
		if entry.NodeID > maxNode {
			maxNode = entry.NodeID
		}
	}

	// flatten trusted to cfg (legacy usage)
	if cfg != nil && len(trusted) > 0 {
		strMap := make(map[uint32]string, len(trusted))
		for id, raw := range trusted {
			strMap[id] = base64.StdEncoding.EncodeToString(raw)
		}
		buf, _ := json.Marshal(strMap)
		cfg.Set(confTrustedNodesKey, string(buf))
	}
	return whitelist, trusted, maxNode
}

// saveTrustedBindings 将 whitelist 与 trusted map 持久化到同一文件。
func saveTrustedBindings(bindings map[string]bindingRecord, trusted map[uint32][]byte) {
	path := filepath.Clean(trustedNodesFile)
	_ = os.MkdirAll(filepath.Dir(path), 0o755)

	tf := trustedFile{
		Bindings: make(map[string]bindingPersist),
	}

	for dev, rec := range bindings {
		dev = strings.TrimSpace(dev)
		if dev == "" || rec.NodeID == 0 {
			continue
		}
		entry := bindingPersist{
			NodeID: rec.NodeID,
			Role:   rec.Role,
			Perms:  cloneSlice(rec.Perms),
		}
		if len(rec.PubKey) > 0 {
			entry.PubKey = base64.StdEncoding.EncodeToString(rec.PubKey)
		} else if raw, ok := trusted[rec.NodeID]; ok && len(raw) > 0 {
			entry.PubKey = base64.StdEncoding.EncodeToString(raw)
		}
		tf.Bindings[dev] = entry
	}

	data, _ := json.MarshalIndent(tf, "", "  ")
	_ = os.WriteFile(path, data, 0o600)
}

func parsePrivKey(b64 string) (*ecdsa.PrivateKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return nil, err
	}
	priv, err := x509.ParseECPrivateKey(raw)
	if err != nil {
		return nil, err
	}
	if priv == nil || priv.Curve != elliptic.P256() {
		return nil, errors.New("not p256")
	}
	return priv, nil
}

func readNodeKeysFile() (nodeKeys, error) {
	var k nodeKeys
	path := filepath.Clean(nodeKeysFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return k, err
	}
	err = json.Unmarshal(data, &k)
	return k, err
}

func writeNodeKeysFile(k nodeKeys) error {
	path := filepath.Clean(nodeKeysFile)
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	data, _ := json.MarshalIndent(k, "", "  ")
	return os.WriteFile(path, data, 0o600)
}
