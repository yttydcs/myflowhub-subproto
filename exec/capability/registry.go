package capability

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"sync"
)

var (
	ErrMethodRequired  = errors.New("capability method required")
	ErrProviderMissing = errors.New("capability provider required")
	ErrTimeoutInvalid  = errors.New("capability default_timeout_ms invalid")
	ErrConflict        = errors.New("capability key conflict")
)

type InvokeFunc func(ctx context.Context, args json.RawMessage) (json.RawMessage, error)

type Descriptor struct {
	Provider         string            `json:"provider"`
	Method           string            `json:"method"`
	Version          string            `json:"version,omitempty"`
	InputSchema      json.RawMessage   `json:"input_schema,omitempty"`
	OutputSchema     json.RawMessage   `json:"output_schema,omitempty"`
	DefaultTimeoutMs int               `json:"default_timeout_ms,omitempty"`
	Permissions      []string          `json:"permissions,omitempty"`
	Tags             map[string]string `json:"tags,omitempty"`
}

type entry struct {
	desc   Descriptor
	invoke InvokeFunc
}

type Registry struct {
	mu      sync.RWMutex
	entries map[string]entry
}

var sharedRegistries sync.Map

func NewRegistry() *Registry {
	return &Registry{
		entries: make(map[string]entry),
	}
}

func SharedRegistry(scope any) *Registry {
	key := sharedScopeKey(scope)
	if key == 0 {
		return NewRegistry()
	}
	if existing, ok := sharedRegistries.Load(key); ok {
		if reg, ok2 := existing.(*Registry); ok2 && reg != nil {
			return reg
		}
	}
	reg := NewRegistry()
	actual, _ := sharedRegistries.LoadOrStore(key, reg)
	if got, ok := actual.(*Registry); ok && got != nil {
		return got
	}
	return reg
}

func (r *Registry) Register(desc Descriptor, invoke InvokeFunc) error {
	if r == nil {
		return nil
	}
	normalized, err := normalizeDescriptor(desc)
	if err != nil {
		return err
	}
	key := descriptorKey(normalized.Method, normalized.Version)

	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.entries[key]; ok {
		if existing.desc.Provider == normalized.Provider && descriptorEqual(existing.desc, normalized) {
			if invoke != nil {
				existing.invoke = invoke
				r.entries[key] = existing
			}
			return nil
		}
		return ErrConflict
	}
	r.entries[key] = entry{desc: normalized, invoke: invoke}
	return nil
}

func (r *Registry) Lookup(method, version string) (Descriptor, InvokeFunc, bool) {
	if r == nil {
		return Descriptor{}, nil, false
	}
	key := descriptorKey(method, version)
	r.mu.RLock()
	got, ok := r.entries[key]
	r.mu.RUnlock()
	if !ok {
		return Descriptor{}, nil, false
	}
	return cloneDescriptor(got.desc), got.invoke, true
}

func (r *Registry) List() []Descriptor {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	out := make([]Descriptor, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, cloneDescriptor(e.desc))
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Method != out[j].Method {
			return out[i].Method < out[j].Method
		}
		if out[i].Version != out[j].Version {
			return out[i].Version < out[j].Version
		}
		return out[i].Provider < out[j].Provider
	})
	return out
}

func sharedScopeKey(scope any) uintptr {
	if scope == nil {
		return 0
	}
	v := reflect.ValueOf(scope)
	if !v.IsValid() || v.Kind() != reflect.Pointer {
		return 0
	}
	return v.Pointer()
}

func normalizeDescriptor(desc Descriptor) (Descriptor, error) {
	out := cloneDescriptor(desc)
	out.Provider = strings.TrimSpace(out.Provider)
	out.Method = strings.TrimSpace(out.Method)
	out.Version = strings.TrimSpace(out.Version)
	if out.Provider == "" {
		return Descriptor{}, ErrProviderMissing
	}
	if out.Method == "" {
		return Descriptor{}, ErrMethodRequired
	}
	if out.DefaultTimeoutMs < 0 {
		return Descriptor{}, ErrTimeoutInvalid
	}
	for idx := range out.Permissions {
		out.Permissions[idx] = strings.TrimSpace(out.Permissions[idx])
	}
	if len(out.Tags) > 0 {
		for key, val := range out.Tags {
			trimmed := strings.TrimSpace(key)
			delete(out.Tags, key)
			if trimmed == "" {
				continue
			}
			out.Tags[trimmed] = strings.TrimSpace(val)
		}
	}
	return out, nil
}

func descriptorKey(method, version string) string {
	return strings.TrimSpace(method) + "\x00" + strings.TrimSpace(version)
}

func cloneDescriptor(in Descriptor) Descriptor {
	out := Descriptor{
		Provider:         in.Provider,
		Method:           in.Method,
		Version:          in.Version,
		DefaultTimeoutMs: in.DefaultTimeoutMs,
	}
	if len(in.InputSchema) > 0 {
		out.InputSchema = cloneRaw(in.InputSchema)
	}
	if len(in.OutputSchema) > 0 {
		out.OutputSchema = cloneRaw(in.OutputSchema)
	}
	if len(in.Permissions) > 0 {
		out.Permissions = append([]string(nil), in.Permissions...)
	}
	if len(in.Tags) > 0 {
		out.Tags = make(map[string]string, len(in.Tags))
		for key, val := range in.Tags {
			out.Tags[key] = val
		}
	}
	return out
}

func descriptorEqual(a, b Descriptor) bool {
	if a.Provider != b.Provider || a.Method != b.Method || a.Version != b.Version || a.DefaultTimeoutMs != b.DefaultTimeoutMs {
		return false
	}
	if string(a.InputSchema) != string(b.InputSchema) {
		return false
	}
	if string(a.OutputSchema) != string(b.OutputSchema) {
		return false
	}
	if len(a.Permissions) != len(b.Permissions) {
		return false
	}
	for idx := range a.Permissions {
		if a.Permissions[idx] != b.Permissions[idx] {
			return false
		}
	}
	if len(a.Tags) != len(b.Tags) {
		return false
	}
	for key, val := range a.Tags {
		if b.Tags[key] != val {
			return false
		}
	}
	return true
}

func cloneRaw(in json.RawMessage) json.RawMessage {
	if len(in) == 0 {
		return nil
	}
	out := make([]byte, len(in))
	copy(out, in)
	return out
}
