package file

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	core "github.com/yttydcs/myflowhub-core"
)

const (
	cfgBaseDir          = "file.base_dir"
	cfgMaxSizeBytes     = "file.max_size_bytes"
	cfgMaxConcurrent    = "file.max_concurrent"
	cfgChunkBytes       = "file.chunk_bytes"
	cfgIncompleteTTLSec = "file.incomplete_ttl_sec"
)

type handlerConfig struct {
	BaseDir          string
	MaxSizeBytes     uint64
	MaxConcurrent    int
	ChunkBytes       int
	IncompleteTTLSec int64
}

func loadConfig(cfg core.IConfig) handlerConfig {
	return handlerConfig{
		BaseDir:          resolveRuntimeBaseDir(readString(cfg, cfgBaseDir, "./file")),
		MaxSizeBytes:     readUint64(cfg, cfgMaxSizeBytes, 0),
		MaxConcurrent:    readInt(cfg, cfgMaxConcurrent, 4),
		ChunkBytes:       readInt(cfg, cfgChunkBytes, 256*1024),
		IncompleteTTLSec: readInt64(cfg, cfgIncompleteTTLSec, 3600),
	}
}

var (
	exeDirOnce   sync.Once
	exeDirCached string
)

func executableDir() string {
	exeDirOnce.Do(func() {
		exe, err := os.Executable()
		if err != nil {
			return
		}
		exe = strings.TrimSpace(exe)
		if exe == "" || !filepath.IsAbs(exe) {
			return
		}
		exeDirCached = filepath.Dir(exe)
	})
	return exeDirCached
}

// resolveRuntimeBaseDir resolves relative baseDir against the executable directory (exeDir).
// This avoids depending on the process CWD, making the default "./file" stable for hub_server.
func resolveRuntimeBaseDir(baseDir string) string {
	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		baseDir = "."
	}
	baseDir = filepath.Clean(baseDir)
	if filepath.IsAbs(baseDir) {
		return baseDir
	}
	if exeDir := executableDir(); strings.TrimSpace(exeDir) != "" {
		return filepath.Join(exeDir, baseDir)
	}
	return baseDir
}

func readString(cfg core.IConfig, key, def string) string {
	if cfg == nil {
		return def
	}
	if v, ok := cfg.Get(key); ok {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return def
}

func readInt(cfg core.IConfig, key string, def int) int {
	if cfg == nil {
		return def
	}
	if raw, ok := cfg.Get(key); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func readInt64(cfg core.IConfig, key string, def int64) int64 {
	if cfg == nil {
		return def
	}
	if raw, ok := cfg.Get(key); ok {
		if n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func readUint64(cfg core.IConfig, key string, def uint64) uint64 {
	if cfg == nil {
		return def
	}
	if raw, ok := cfg.Get(key); ok {
		if n, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64); err == nil {
			return n
		}
	}
	return def
}
