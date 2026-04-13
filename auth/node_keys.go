package auth

// Context: This file belongs to the SubProto implementation layer around node_keys.

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
	"sort"
	"strings"
	"time"

	core "github.com/yttydcs/myflowhub-core"
	coreconfig "github.com/yttydcs/myflowhub-core/config"
)

const (
	nodeKeysFile                    = "config/node_keys.json"
	trustedNodesFile                = "config/trusted_nodes.json"
	confNodePrivKey                 = coreconfig.KeyAuthNodePrivKey
	confNodePubKey                  = coreconfig.KeyAuthNodePubKey
	confTrustedNodesKey             = coreconfig.KeyAuthTrustedNodes
	metaPendingRegisters            = "pending_registers"
	metaApprovedRegisters           = "approved_registers"
	metaRegisterPermits             = "register_permits"
	metaFirstRegisterBootstrapState = "first_register_bootstrap"
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

// loadTrustedBindings 读取 trusted_nodes 文件，将 bindings 与 admission state 一并恢复。
func loadTrustedBindings(cfg core.IConfig) (map[string]bindingRecord, map[uint32][]byte, map[string]pendingRegisterRecord, map[string]approvedRegisterRecord, map[string]registerPermitRecord, firstRegisterBootstrapState, uint32, error) {
	path := filepath.Clean(trustedNodesFile)
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		if errors.Is(err, os.ErrNotExist) || len(data) == 0 {
			return nil, nil, nil, nil, nil, firstRegisterBootstrapState{}, 0, nil
		}
		return nil, nil, nil, nil, nil, firstRegisterBootstrapState{}, 0, err
	}
	var tf trustedFile
	if err := json.Unmarshal(data, &tf); err != nil {
		return nil, nil, nil, nil, nil, firstRegisterBootstrapState{}, 0, err
	}
	whitelist := make(map[string]bindingRecord)
	trusted := make(map[uint32][]byte)
	pending := make(map[string]pendingRegisterRecord)
	approved := make(map[string]approvedRegisterRecord)
	permits := make(map[string]registerPermitRecord)
	bootstrapState := firstRegisterBootstrapState{}
	var maxNode uint32
	nowUnix := time.Now().UTC().Unix()

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
	if len(tf.Meta) > 0 {
		if raw, ok := tf.Meta[metaPendingRegisters]; ok && len(raw) > 0 {
			var items []pendingRegisterRecord
			if err := json.Unmarshal(raw, &items); err != nil {
				return nil, nil, nil, nil, nil, firstRegisterBootstrapState{}, 0, err
			}
			for _, item := range items {
				item.RequestID = strings.TrimSpace(item.RequestID)
				item.DeviceID = strings.TrimSpace(item.DeviceID)
				item.RequestedRole = strings.TrimSpace(item.RequestedRole)
				item.DisplayName = normalizeDisplayName(item.DisplayName)
				item.PubKey = strings.TrimSpace(item.PubKey)
				if item.RequestID == "" || item.DeviceID == "" {
					continue
				}
				if item.ExpiresAt != 0 && item.ExpiresAt <= nowUnix {
					continue
				}
				pending[item.RequestID] = item
			}
		}
		if raw, ok := tf.Meta[metaApprovedRegisters]; ok && len(raw) > 0 {
			var items []approvedRegisterRecord
			if err := json.Unmarshal(raw, &items); err != nil {
				return nil, nil, nil, nil, nil, firstRegisterBootstrapState{}, 0, err
			}
			for _, item := range items {
				item.RequestID = strings.TrimSpace(item.RequestID)
				item.DeviceID = strings.TrimSpace(item.DeviceID)
				item.Role = strings.TrimSpace(item.Role)
				if item.DeviceID == "" || item.NodeID == 0 {
					continue
				}
				if item.ExpiresAt != 0 && item.ExpiresAt <= nowUnix {
					continue
				}
				approved[item.DeviceID] = item
				if item.NodeID > maxNode {
					maxNode = item.NodeID
				}
			}
		}
		if raw, ok := tf.Meta[metaRegisterPermits]; ok && len(raw) > 0 {
			var items []registerPermitRecord
			if err := json.Unmarshal(raw, &items); err != nil {
				return nil, nil, nil, nil, nil, firstRegisterBootstrapState{}, 0, err
			}
			for _, item := range items {
				item.Permit = strings.TrimSpace(item.Permit)
				item.DeviceID = strings.TrimSpace(item.DeviceID)
				item.Role = strings.TrimSpace(item.Role)
				if item.Permit == "" || item.DeviceID == "" || item.Role == "" {
					continue
				}
				if item.ExpiresAt != 0 && item.ExpiresAt <= nowUnix {
					continue
				}
				permits[item.Permit] = item
			}
		}
		if raw, ok := tf.Meta[metaFirstRegisterBootstrapState]; ok && len(raw) > 0 {
			if err := json.Unmarshal(raw, &bootstrapState); err != nil {
				return nil, nil, nil, nil, nil, firstRegisterBootstrapState{}, 0, err
			}
			bootstrapState.DeviceID = strings.TrimSpace(bootstrapState.DeviceID)
			bootstrapState.Role = strings.TrimSpace(bootstrapState.Role)
			if bootstrapState.NodeID > maxNode {
				maxNode = bootstrapState.NodeID
			}
		}
	}
	return whitelist, trusted, pending, approved, permits, bootstrapState, maxNode, nil
}

// saveTrustedBindings 将 whitelist、trusted 和 admission state 持久化到同一文件。
func saveTrustedBindings(bindings map[string]bindingRecord, trusted map[uint32][]byte, pending map[string]pendingRegisterRecord, approved map[string]approvedRegisterRecord, permits map[string]registerPermitRecord, bootstrapState firstRegisterBootstrapState) error {
	path := filepath.Clean(trustedNodesFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	tf := trustedFile{
		Bindings: make(map[string]bindingPersist),
		Meta:     make(map[string]json.RawMessage),
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

	if len(pending) > 0 {
		items := make([]pendingRegisterRecord, 0, len(pending))
		for _, item := range pending {
			items = append(items, item)
		}
		sort.Slice(items, func(i, j int) bool { return items[i].RequestID < items[j].RequestID })
		raw, err := json.Marshal(items)
		if err != nil {
			return err
		}
		tf.Meta[metaPendingRegisters] = raw
	}
	if len(approved) > 0 {
		items := make([]approvedRegisterRecord, 0, len(approved))
		for _, item := range approved {
			items = append(items, item)
		}
		sort.Slice(items, func(i, j int) bool {
			if items[i].NodeID == items[j].NodeID {
				return items[i].DeviceID < items[j].DeviceID
			}
			return items[i].NodeID < items[j].NodeID
		})
		raw, err := json.Marshal(items)
		if err != nil {
			return err
		}
		tf.Meta[metaApprovedRegisters] = raw
	}
	if len(permits) > 0 {
		items := make([]registerPermitRecord, 0, len(permits))
		for _, item := range permits {
			items = append(items, item)
		}
		sort.Slice(items, func(i, j int) bool { return items[i].Permit < items[j].Permit })
		raw, err := json.Marshal(items)
		if err != nil {
			return err
		}
		tf.Meta[metaRegisterPermits] = raw
	}
	if bootstrapState.ConsumedEpoch > 0 {
		raw, err := json.Marshal(bootstrapState)
		if err != nil {
			return err
		}
		tf.Meta[metaFirstRegisterBootstrapState] = raw
	}
	if len(tf.Meta) == 0 {
		tf.Meta = nil
	}
	data, _ := json.MarshalIndent(tf, "", "  ")
	return os.WriteFile(path, data, 0o600)
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
