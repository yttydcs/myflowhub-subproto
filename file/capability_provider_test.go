package file

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	coreconfig "github.com/yttydcs/myflowhub-core/config"
	execcap "github.com/yttydcs/myflowhub-subproto/exec/capability"
)

func TestFileCapabilitiesListReadTextMkdir(t *testing.T) {
	baseDir := t.TempDir()
	cfg := coreconfig.NewMap(map[string]string{
		cfgBaseDir: baseDir,
	})
	NewHandlerWithConfig(cfg, nil)
	reg := execcap.SharedRegistry(cfg)

	_, listInvoke, ok := reg.Lookup(capabilityFileList, "")
	if !ok || listInvoke == nil {
		t.Fatalf("expected %s capability registered", capabilityFileList)
	}
	_, readInvoke, ok := reg.Lookup(capabilityFileReadText, "")
	if !ok || readInvoke == nil {
		t.Fatalf("expected %s capability registered", capabilityFileReadText)
	}
	_, mkdirInvoke, ok := reg.Lookup(capabilityFileMkdir, "")
	if !ok || mkdirInvoke == nil {
		t.Fatalf("expected %s capability registered", capabilityFileMkdir)
	}

	if _, err := mkdirInvoke(context.Background(), json.RawMessage(`{"dir":"","name":"docs"}`)); err != nil {
		t.Fatalf("mkdir capability err=%v", err)
	}
	path := filepath.Join(baseDir, "docs", "hello.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir path err=%v", err)
	}
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file err=%v", err)
	}

	rawList, err := listInvoke(context.Background(), json.RawMessage(`{"dir":"docs"}`))
	if err != nil {
		t.Fatalf("list capability err=%v", err)
	}
	var listResp map[string]any
	if err := json.Unmarshal(rawList, &listResp); err != nil {
		t.Fatalf("unmarshal list result err=%v", err)
	}

	rawRead, err := readInvoke(context.Background(), json.RawMessage(`{"dir":"docs","name":"hello.txt"}`))
	if err != nil {
		t.Fatalf("read_text capability err=%v", err)
	}
	var readResp map[string]any
	if err := json.Unmarshal(rawRead, &readResp); err != nil {
		t.Fatalf("unmarshal read result err=%v", err)
	}
	if readResp["text"] != "hello" {
		t.Fatalf("unexpected read_text result=%v", readResp)
	}
}
