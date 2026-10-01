package custom

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

var (
	ErrManagementRevision = errors.New("custom: configuration revision precondition failed")
	ErrManagementRetired  = errors.New("custom: configuration is retired")
)

// DefinitionManager exposes immutable custom-object type versions as CRUD
// resources. Update is append-only and retirement preserves all history.
type DefinitionManager struct {
	mu       sync.RWMutex
	store    *MemoryStore
	versions map[string][]CustomObjectDefinition
	retired  map[string]bool
}

func NewDefinitionManager(store *MemoryStore) *DefinitionManager {
	if store == nil {
		store = NewMemoryStore()
	}
	return &DefinitionManager{store: store, versions: make(map[string][]CustomObjectDefinition), retired: make(map[string]bool)}
}

func (m *DefinitionManager) Create(ctx context.Context, tenant string, definition CustomObjectDefinition) (CustomObjectDefinition, error) {
	return m.save(ctx, tenant, definition, 0)
}

func (m *DefinitionManager) Update(ctx context.Context, tenant string, definition CustomObjectDefinition, ifMatch uint64) (CustomObjectDefinition, error) {
	if ifMatch == 0 {
		return CustomObjectDefinition{}, ErrManagementRevision
	}
	return m.save(ctx, tenant, definition, ifMatch)
}

func (m *DefinitionManager) Get(tenant, kind, namespace string, version uint64) (CustomObjectDefinition, error) {
	return m.store.LoadObjectDefinition(context.Background(), tenant, kind, namespace, version)
}

func (m *DefinitionManager) List(tenant, kind, namespace string) []CustomObjectDefinition {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]CustomObjectDefinition, 0)
	for key, history := range m.versions {
		if !strings.HasPrefix(key, tenant+"\x00"+kind+"\x00"+namespace) {
			continue
		}
		for _, definition := range history {
			out = append(out, cloneDefinition(definition))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out
}

func (m *DefinitionManager) Retire(tenant, kind, namespace string, ifMatch uint64) error {
	if ifMatch == 0 {
		return ErrManagementRevision
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := definitionKey(tenant, kind, namespace)
	history := m.versions[key]
	if len(history) == 0 {
		return ErrManagementRevision
	}
	if history[len(history)-1].Version != ifMatch {
		return ErrManagementRevision
	}
	m.retired[key] = true
	return nil
}

func (m *DefinitionManager) IsRetired(tenant, kind, namespace string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.retired[definitionKey(tenant, kind, namespace)]
}

func (m *DefinitionManager) save(ctx context.Context, tenant string, definition CustomObjectDefinition, ifMatch uint64) (CustomObjectDefinition, error) {
	if m == nil || strings.TrimSpace(tenant) == "" {
		return CustomObjectDefinition{}, ErrManagementRevision
	}
	if err := definition.Validate(); err != nil {
		return CustomObjectDefinition{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := definitionKey(tenant, definition.Kind, definition.Namespace)
	history := m.versions[key]
	current := uint64(0)
	if len(history) > 0 {
		current = history[len(history)-1].Version
	}
	if current != ifMatch || definition.Version != current+1 || m.retired[key] {
		return CustomObjectDefinition{}, ErrManagementRevision
	}
	if err := m.store.SaveObjectDefinition(ctx, tenant, definition, ifMatch); err != nil {
		return CustomObjectDefinition{}, err
	}
	m.versions[key] = append(history, cloneDefinition(definition))
	return cloneDefinition(definition), nil
}

func definitionKey(tenant, kind, namespace string) string {
	return fmt.Sprintf("%s\x00%s\x00%s", tenant, kind, namespace)
}
func cloneDefinition(definition CustomObjectDefinition) CustomObjectDefinition {
	definition.Fields = cloneFieldDefinitions(definition.Fields)
	return definition
}
func cloneFieldDefinitions(fields map[string]FieldDefinition) map[string]FieldDefinition {
	out := make(map[string]FieldDefinition, len(fields))
	for key, value := range fields {
		out[key] = value
	}
	return out
}
