package truststore

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/partnerapp"
)

// MachineRegistry adapts the durable machine-client tables to the
// partnerapp.ClientRegistry port: the token endpoint's policy decisions
// read domain types keyed by tenant key, while storage stays in uuid
// tenant rows. This mirrors how roleaccessstore fronts the roleaccess
// port: the data package imports the port, never the reverse.
type MachineRegistry struct {
	store  *Store
	tenant func(string) uuid.UUID
}

// Registry returns the ClientRegistry port over s. tenant maps a tenant key
// to its storage id (production: the deterministic SHA1 derivation the
// cell bootstraps tenants with).
func (s *Store) Registry(tenant func(string) uuid.UUID) *MachineRegistry {
	return &MachineRegistry{store: s, tenant: tenant}
}

var _ partnerapp.ClientRegistry = (*MachineRegistry)(nil)

// tenantIDByKey resolves a tenant key to its storage id.
func (r *MachineRegistry) tenantIDByKey(tenantKey string) (uuid.UUID, error) {
	if tenantKey == "" {
		return uuid.Nil, failure(CodeTenantRequired, "tenant", "", errors.New("tenant key is required"))
	}
	if r.tenant == nil {
		return uuid.Nil, failure(CodeTenantRequired, "tenant", tenantKey, errors.New("no tenant mapper configured"))
	}
	id := r.tenant(tenantKey)
	if id == uuid.Nil {
		return uuid.Nil, failure(CodeNotFound, "tenant", tenantKey, errors.New("tenant not found"))
	}
	return id, nil
}

func toMachineClient(in MachineClientRecord, tenantKey string) partnerapp.MachineClient {
	out := partnerapp.MachineClient{
		Tenant: tenantKey, ClientID: in.ClientID, Owner: in.Owner, Status: in.Status,
		Scopes: append([]string(nil), in.Scopes...), Purpose: in.Purpose,
		DataDomains:     append([]string(nil), in.DataDomains...),
		FieldSubset:     append([]string(nil), in.FieldSubset...),
		IPAllowlist:     append([]string(nil), in.IPAllowlist...),
		CertFingerprint: in.CertFingerprint,
		Revoked:         in.Revoked,
	}
	if in.ExpiresAt != nil {
		out.ExpiresAt = in.ExpiresAt.UTC()
	}
	if in.LastUsedAt != nil {
		out.LastUsedAt = in.LastUsedAt.UTC()
	}
	return out
}

func toMachineClientKey(in MachineClientKeyRecord) partnerapp.MachineClientKey {
	out := partnerapp.MachineClientKey{
		ClientID: in.ClientID, KID: in.KID, Alg: in.Alg,
		JWK: append([]byte(nil), in.PublicJWK...), NotBefore: in.NotBefore.UTC(), Revoked: in.Revoked,
	}
	if in.ExpiresAt != nil {
		out.ExpiresAt = in.ExpiresAt.UTC()
	}
	return out
}

// LoadClient implements partnerapp.ClientRegistry.
func (r *MachineRegistry) LoadClient(ctx context.Context, tenantKey, clientID string) (partnerapp.MachineClient, error) {
	id, err := r.tenantIDByKey(tenantKey)
	if err != nil {
		return partnerapp.MachineClient{}, err
	}
	rec, err := r.store.LoadMachineClient(ctx, id, clientID)
	if err != nil {
		return partnerapp.MachineClient{}, err
	}
	return toMachineClient(rec, tenantKey), nil
}

// LoadClientKeys implements partnerapp.ClientRegistry.
func (r *MachineRegistry) LoadClientKeys(ctx context.Context, tenantKey, clientID string) ([]partnerapp.MachineClientKey, error) {
	id, err := r.tenantIDByKey(tenantKey)
	if err != nil {
		return nil, err
	}
	recs, err := r.store.LoadMachineClientKeys(ctx, id, clientID)
	if err != nil {
		return nil, err
	}
	out := make([]partnerapp.MachineClientKey, 0, len(recs))
	for _, rec := range recs {
		out = append(out, toMachineClientKey(rec))
	}
	return out, nil
}

// RecordClientUse implements partnerapp.ClientRegistry.
func (r *MachineRegistry) RecordClientUse(ctx context.Context, tenantKey, clientID string, now time.Time) error {
	id, err := r.tenantIDByKey(tenantKey)
	if err != nil {
		return err
	}
	return r.store.TouchMachineClientUse(ctx, id, clientID, now)
}

// RegisterClient registers one machine client through the port. It exists
// for onboarding flows and tests; production registration is a governed
// write outside this package.
func (r *MachineRegistry) RegisterClient(ctx context.Context, tenantKey string, in partnerapp.MachineClient) error {
	id, err := r.tenantIDByKey(tenantKey)
	if err != nil {
		return err
	}
	var expiresAt *time.Time
	if !in.ExpiresAt.IsZero() {
		exp := in.ExpiresAt.UTC()
		expiresAt = &exp
	}
	return r.store.RegisterMachineClient(ctx, id, MachineClientRecord{
		TenantID: id, ClientID: in.ClientID, Owner: in.Owner, Status: in.Status,
		Scopes: in.Scopes, Purpose: in.Purpose, DataDomains: in.DataDomains,
		FieldSubset: in.FieldSubset, IPAllowlist: in.IPAllowlist,
		CertFingerprint: in.CertFingerprint,
		ExpiresAt:       expiresAt, Revoked: in.Revoked,
	})
}

// RegisterClientKey registers one client assertion key through the port.
func (r *MachineRegistry) RegisterClientKey(ctx context.Context, tenantKey string, in partnerapp.MachineClientKey) error {
	id, err := r.tenantIDByKey(tenantKey)
	if err != nil {
		return err
	}
	var expiresAt *time.Time
	if !in.ExpiresAt.IsZero() {
		exp := in.ExpiresAt.UTC()
		expiresAt = &exp
	}
	return r.store.RegisterMachineClientKey(ctx, id, MachineClientKeyRecord{
		TenantID: id, ClientID: in.ClientID, KID: in.KID, Alg: in.Alg,
		PublicJWK: in.JWK, NotBefore: in.NotBefore.UTC(), ExpiresAt: expiresAt, Revoked: in.Revoked,
	})
}
