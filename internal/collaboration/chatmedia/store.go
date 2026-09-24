package chatmedia

import (
	"bytes"
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
		if !bytes.Equal(old.Content, a.Content) || old.MediaType != a.MediaType || old.Size != a.Size || old.Transcript != a.Transcript || old.AltText != a.AltText {
			return ErrInvalid
		}
		return nil
	}
	a.Content = append([]byte(nil), a.Content...)
	m.values[a.ArtifactID] = a
	return nil
}
func (m *MemoryStore) SetRenditions(ctx context.Context, id, tenant string, renditions map[string]Rendition) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.values[id]
	if !ok || a.TenantID != tenant {
		return ErrUnauthorized
	}
	if a.State != StateQuarantined && a.State != StateAdmitted {
		return ErrInvalid
	}
	if a.State == StateAdmitted && len(a.Renditions) != 0 {
		return nil
	}
	a.Renditions = make(map[string]Rendition, len(renditions))
	for key, rendition := range renditions {
		rendition.Content = append([]byte(nil), rendition.Content...)
		a.Renditions[key] = rendition
	}
	m.values[id] = a
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
	if a.State == StateAdmitted {
		return nil
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
	a.Renditions = nil
	return a, nil
}

func (m *MemoryStore) GetRendition(ctx context.Context, tenant, id, variant string) (Artifact, error) {
	if err := ctx.Err(); err != nil {
		return Artifact{}, err
	}
	m.mu.RLock()
	a, ok := m.values[id]
	m.mu.RUnlock()
	if !ok || a.TenantID != tenant {
		return Artifact{}, ErrUnauthorized
	}
	if a.State != StateAdmitted {
		return Artifact{}, ErrQuarantined
	}
	if a.MediaType == MediaGIF {
		a.Content = append([]byte(nil), a.Content...)
		a.Renditions = nil
		return a, nil
	}
	r, ok := a.Renditions[variant]
	if !ok {
		return Artifact{}, ErrUnsupported
	}
	a.Content = append([]byte(nil), r.Content...)
	a.MediaType, a.Size = r.MediaType, int64(len(r.Content))
	a.Renditions = nil
	return a, nil
}
