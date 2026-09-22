package chatmedia

import (
	"context"
	"errors"
	"sync"
)

// MemoryStore is a concurrency-safe reference store for tests and local
// development. It deliberately keeps quarantined records in the same map but
// Get refuses every state other than ADMITTED.
type MemoryStore struct {
	mu     sync.RWMutex
	values map[string]Artifact
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{values: map[string]Artifact{}} }
func (m *MemoryStore) Quarantine(ctx context.Context, a Artifact) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.ArtifactID == "" || a.TenantID == "" || len(a.Content) == 0 {
		return ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.values[a.ArtifactID]; ok {
		if old.TenantID != a.TenantID || old.ConversationID != a.ConversationID {
			return ErrUnauthorized
		}
		return nil
	}
	a.Content = append([]byte(nil), a.Content...)
	m.values[a.ArtifactID] = a
	return nil
}
func (m *MemoryStore) SetVerdict(ctx context.Context, id, tenant string, state ArtifactState, reason, scanner string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if state != StateAdmitted && state != StateRejected {
		return ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.values[id]
	if !ok || a.TenantID != tenant {
		return ErrUnauthorized
	}
	a.State = state
	a.Reason = reason
	a.ScannerID = scanner
	m.values[id] = a
	return nil
}
func (m *MemoryStore) Get(ctx context.Context, tenant, id string) (Artifact, error) {
	if err := ctx.Err(); err != nil {
		return Artifact{}, err
	}
	m.mu.RLock()
	a, ok := m.values[id]
	m.mu.RUnlock()
	if !ok || a.TenantID != tenant {
		return Artifact{}, errors.New("chatmedia: artifact not found")
	}
	if a.State != StateAdmitted {
		return Artifact{}, ErrQuarantined
	}
	a.Content = append([]byte(nil), a.Content...)
	return a, nil
}
