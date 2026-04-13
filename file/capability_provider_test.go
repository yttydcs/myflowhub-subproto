package file

// Context: This file belongs to the SubProto implementation layer around capability_provider_test.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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

	listDesc, listInvoke, ok := reg.Lookup(capabilityFileList, "")
	if !ok || listInvoke == nil {
		t.Fatalf("expected %s capability registered", capabilityFileList)
	}
	readDesc, readInvoke, ok := reg.Lookup(capabilityFileReadText, "")
	if !ok || readInvoke == nil {
		t.Fatalf("expected %s capability registered", capabilityFileReadText)
	}
	mkdirDesc, mkdirInvoke, ok := reg.Lookup(capabilityFileMkdir, "")
	if !ok || mkdirInvoke == nil {
		t.Fatalf("expected %s capability registered", capabilityFileMkdir)
	}
	assertCapabilitySchema(t, listDesc.InputSchema, "List Directory", nil, map[string]string{
		"dir": "string",
	})
	assertCapabilitySchema(t, readDesc.InputSchema, "Read Text File", []string{"name"}, map[string]string{
		"dir":       "string",
		"name":      "string",
		"max_bytes": "integer",
	})
	assertCapabilitySchema(t, mkdirDesc.InputSchema, "Create Directory", []string{"name"}, map[string]string{
		"dir":  "string",
		"name": "string",
	})

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

func assertCapabilitySchema(t *testing.T, raw json.RawMessage, wantTitle string, wantRequired []string, wantProps map[string]string) {
	t.Helper()
	if len(raw) == 0 {
		t.Fatalf("expected input schema")
	}
	var schema struct {
		Title      string `json:"title"`
		Type       string `json:"type"`
		Required   []string
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("unmarshal schema err=%v", err)
	}
	if schema.Title != wantTitle || schema.Type != "object" {
		t.Fatalf("unexpected schema header: %+v", schema)
	}
	if !reflect.DeepEqual(schema.Required, wantRequired) {
		t.Fatalf("unexpected required fields: got=%v want=%v", schema.Required, wantRequired)
	}
	if len(schema.Properties) != len(wantProps) {
		t.Fatalf("unexpected property count: got=%d want=%d", len(schema.Properties), len(wantProps))
	}
	for key, wantType := range wantProps {
		got, ok := schema.Properties[key]
		if !ok {
			t.Fatalf("missing schema property %s", key)
		}
		if got.Type != wantType {
			t.Fatalf("unexpected type for %s: got=%s want=%s", key, got.Type, wantType)
		}
	}
}
