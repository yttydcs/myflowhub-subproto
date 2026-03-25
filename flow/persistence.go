package flow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	protocol "github.com/yttydcs/myflowhub-proto/protocol/flow"
)

type FlowDocument = protocol.SetReq

type Persistence interface {
	LoadAll(ctx context.Context) ([]FlowDocument, error)
	Save(ctx context.Context, doc FlowDocument) error
	Delete(ctx context.Context, flowID string) error
}

func NewJSONPersistence(baseDir string) Persistence {
	return &jsonPersistence{baseDir: strings.TrimSpace(baseDir)}
}

type jsonPersistence struct {
	baseDir string
}

func (p *jsonPersistence) LoadAll(_ context.Context) ([]FlowDocument, error) {
	baseDir := strings.TrimSpace(p.baseDir)
	if baseDir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})
	docs := make([]FlowDocument, 0, len(entries))
	for _, entry := range entries {
		if entry == nil || entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(baseDir, name))
		if err != nil {
			continue
		}
		var doc FlowDocument
		if err := json.Unmarshal(raw, &doc); err != nil {
			continue
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

func (p *jsonPersistence) Save(_ context.Context, doc FlowDocument) error {
	baseDir := strings.TrimSpace(p.baseDir)
	path, err := flowFilePath(baseDir, doc.FlowID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, raw, 0o644)
}

func (p *jsonPersistence) Delete(_ context.Context, flowID string) error {
	baseDir := strings.TrimSpace(p.baseDir)
	path, err := flowFilePath(baseDir, flowID)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
