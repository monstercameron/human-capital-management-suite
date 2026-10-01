package application

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
)

type manifestStoreFake struct {
	current                      agentmanifest.Manifest
	version                      agentmanifest.Manifest
	currentErr, versionErr       error
	currentTenant, versionTenant uuid.UUID
	versionID                    string
	versionNumber                uint64
}

func (s *manifestStoreFake) CurrentManifest(_ context.Context, tenant uuid.UUID, id string) (agentmanifest.Manifest, uint64, error) {
	s.currentTenant = tenant
	if s.currentErr != nil {
		return agentmanifest.Manifest{}, 0, s.currentErr
	}
	if s.current.ID != id {
		return agentmanifest.Manifest{}, 0, errors.New("missing")
	}
	return s.current, 1, nil
}

func (s *manifestStoreFake) ManifestVersion(_ context.Context, tenant uuid.UUID, id string, version uint64) (agentmanifest.Manifest, error) {
	s.versionTenant, s.versionID, s.versionNumber = tenant, id, version
	if s.versionErr != nil {
		return agentmanifest.Manifest{}, s.versionErr
	}
	return s.version, nil
}

func manifestAdapterFixture(t *testing.T) (agentmanifest.Manifest, agentpersona.AgentManifestRef) {
	t.Helper()
	ref := func(id string) agentmanifest.Reference {
		return agentmanifest.Reference{ID: id, Version: 1, SchemaVersion: 1, Digest: "sha256:" + strings.Repeat("0", 64)}
	}
	m := agentmanifest.Manifest{SchemaVersion: 1, ID: "agent.people", Version: 2, OwnerID: "owner", Purpose: "help", InstructionsDigest: "sha256:" + strings.Repeat("1", 64), SourceCeiling: []agentmanifest.Reference{}, ToolCeiling: []agentmanifest.Reference{}, ModelPolicy: ref("model"), AutonomyCeiling: "ASSISTED", Budget: agentmanifest.Budget{MaxCostMicros: 1, MaxInputTokens: 1, MaxOutputTokens: 1, MaxConcurrentRuns: 1}, OutputSchema: ref("output"), ContextGrants: []agentmanifest.Reference{}, EvaluationRefs: []agentmanifest.Reference{ref("eval")}}
	digest, err := m.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return m, agentpersona.AgentManifestRef{ID: m.ID, Version: uint32(m.Version), SchemaVersion: m.SchemaVersion, Digest: digest}
}

func TestTodo_AGENTP_003_ManifestStoreAdapter(t *testing.T) {
	m, ref := manifestAdapterFixture(t)
	tenant := uuid.New()
	store := &manifestStoreFake{current: m, version: m}
	a := AgentManifestStoreAdapter{Store: store, TenantID: tenant}
	got, err := a.ResolveAgentManifestContext(context.Background(), ref)
	if err != nil || !reflect.DeepEqual(got, m) {
		t.Fatalf("resolve = %+v, %v; want exact manifest", got, err)
	}
	if store.currentTenant != tenant || store.versionTenant != tenant || store.versionID != ref.ID || store.versionNumber != uint64(ref.Version) {
		t.Fatalf("store calls were not tenant/exact scoped: %+v", store)
	}
}

func TestTodo_AGENTP_003_ManifestStoreAdapterRejectsUnpublishedOrDigestMismatch(t *testing.T) {
	m, ref := manifestAdapterFixture(t)
	tests := []struct {
		name   string
		mutate func(*manifestStoreFake, *agentpersona.AgentManifestRef)
		want   error
	}{
		{name: "historical version", mutate: func(s *manifestStoreFake, r *agentpersona.AgentManifestRef) { s.current.Version = 3 }, want: ErrAgentManifestNotPublished},
		{name: "digest", mutate: func(_ *manifestStoreFake, r *agentpersona.AgentManifestRef) {
			r.Digest = "sha256:" + strings.Repeat("e", 64)
		}, want: ErrAgentManifestDigestMismatch},
		{name: "missing publication", mutate: func(s *manifestStoreFake, _ *agentpersona.AgentManifestRef) { s.currentErr = errors.New("not found") }, want: ErrAgentManifestNotPublished},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := &manifestStoreFake{current: m, version: m}
			changed := ref
			tc.mutate(s, &changed)
			_, err := (AgentManifestStoreAdapter{Store: s, TenantID: uuid.New()}).ResolveAgentManifest(changed)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestTodo_AGENTP_003_ManifestStoreAdapterFailsClosed(t *testing.T) {
	_, ref := manifestAdapterFixture(t)
	for _, adapter := range []AgentManifestStoreAdapter{{}, {Store: &manifestStoreFake{}, TenantID: uuid.Nil}} {
		if _, err := adapter.ResolveAgentManifest(ref); !errors.Is(err, ErrAgentManifestUnavailable) {
			t.Fatalf("error = %v, want unavailable", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &manifestStoreFake{}
	if _, err := (AgentManifestStoreAdapter{Store: s, TenantID: uuid.New()}).ResolveAgentManifestContext(ctx, ref); !errors.Is(err, ErrAgentManifestNotPublished) {
		t.Fatalf("cancelled context error = %v, want store refusal", err)
	}
}
