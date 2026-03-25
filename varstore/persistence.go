package varstore

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"

	protocol "github.com/yttydcs/myflowhub-proto/protocol/varstore"
)

type VarDocument = protocol.SetReq

type Persistence interface {
	LoadAll(ctx context.Context) ([]VarDocument, error)
	Save(ctx context.Context, doc VarDocument) error
	Delete(ctx context.Context, owner uint32, name string) error
}

func NewMemoryPersistence() Persistence {
	return &memoryPersistence{
		records: make(map[string]VarDocument),
	}
}

type memoryPersistence struct {
	mu      sync.RWMutex
	records map[string]VarDocument
}

func (p *memoryPersistence) LoadAll(_ context.Context) ([]VarDocument, error) {
	p.mu.RLock()
	docs := make([]VarDocument, 0, len(p.records))
	for _, doc := range p.records {
		docs = append(docs, doc)
	}
	p.mu.RUnlock()
	sort.Slice(docs, func(i, j int) bool {
		if docs[i].Owner != docs[j].Owner {
			return docs[i].Owner < docs[j].Owner
		}
		return docs[i].Name < docs[j].Name
	})
	return docs, nil
}

func (p *memoryPersistence) Save(_ context.Context, doc VarDocument) error {
	name := strings.TrimSpace(doc.Name)
	if doc.Owner == 0 || name == "" {
		return errors.New("owner/name required")
	}
	doc.Name = name
	p.mu.Lock()
	p.records[varStorageKey(doc.Owner, doc.Name)] = doc
	p.mu.Unlock()
	return nil
}

func (p *memoryPersistence) Delete(_ context.Context, owner uint32, name string) error {
	name = strings.TrimSpace(name)
	if owner == 0 || name == "" {
		return errors.New("owner/name required")
	}
	p.mu.Lock()
	delete(p.records, varStorageKey(owner, name))
	p.mu.Unlock()
	return nil
}

func varStorageKey(owner uint32, name string) string {
	return varKey(owner, strings.TrimSpace(name))
}

func varKey(owner uint32, name string) string {
	return strconv.FormatUint(uint64(owner), 10) + ":" + name
}
