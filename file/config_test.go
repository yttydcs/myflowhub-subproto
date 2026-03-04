package file

import (
	"os"
	"path/filepath"
	"testing"

	core "github.com/yttydcs/myflowhub-core"
)

type stubConfig map[string]string

func (c stubConfig) Get(key string) (string, bool)   { v, ok := c[key]; return v, ok }
func (c stubConfig) Merge(core.IConfig) core.IConfig { return c }
func (c stubConfig) Set(key, val string)             { c[key] = val }
func (c stubConfig) Keys() []string                  { return nil }

func TestResolveRuntimeBaseDir_RelativeUsesExecutableDir(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Skipf("os.Executable failed: %v", err)
	}
	exe = filepath.Clean(exe)
	if exe == "" || !filepath.IsAbs(exe) {
		t.Skipf("unexpected executable path: %q", exe)
	}

	want := filepath.Join(filepath.Dir(exe), "file")
	got := resolveRuntimeBaseDir("./file")
	if got != want {
		t.Fatalf("resolveRuntimeBaseDir: got %q, want %q", got, want)
	}
}

func TestLoadConfig_DefaultBaseDirUsesExecutableDir(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Skipf("os.Executable failed: %v", err)
	}
	exe = filepath.Clean(exe)
	if exe == "" || !filepath.IsAbs(exe) {
		t.Skipf("unexpected executable path: %q", exe)
	}

	want := filepath.Join(filepath.Dir(exe), "file")
	got := loadConfig(nil).BaseDir
	if got != want {
		t.Fatalf("loadConfig default BaseDir: got %q, want %q", got, want)
	}
}

func TestLoadConfig_AbsoluteBaseDirPreserved(t *testing.T) {
	abs := t.TempDir()
	cfg := stubConfig{
		cfgBaseDir: abs,
	}
	got := loadConfig(cfg).BaseDir
	want := filepath.Clean(abs)
	if got != want {
		t.Fatalf("loadConfig absolute BaseDir: got %q, want %q", got, want)
	}
}
