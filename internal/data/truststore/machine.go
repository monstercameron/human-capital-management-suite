package truststore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Machine-client registry for INTAPI-001: the server-side authority the
// OAuth client-credentials token endpoint joins a client assertion against.
// A client row carries who owns it and what it may do; its keys carry the
// public material client assertions are verified with. Both tables are
// caller-driven control state: UPDATE is retained (revocation, expiry, last
// use, rotation) but DELETE is never granted.

// Machine-client lifecycle states.
const (
	MachineClientActive    = "active"
	MachineClientSuspended = "suspended"
	MachineClientRevoked   = "revoked"
)

// MachineClientRecord is the lossless storage projection of one
// machine_client row.
type MachineClientRecord struct {
	TenantID    uuid.UUID
	RowID       uuid.UUID
	ClientID    string
	Owner       string
	Status      string
	Scopes      []string
	Purpose     string
	DataDomains []string
	FieldSubset []string
	IPAllowlist []string
	// CertFingerprint is the lowercase hex SHA-256 of the DER client
	// certificate the terminator verified, for mutual-TLS client auth.
	// Empty means the client has no mTLS binding.
	CertFingerprint string
	ExpiresAt       *time.Time
	Revoked         bool
	LastUsedAt      *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// MachineClientKeyRecord is the lossless storage projection of one
// machine_client_key row. PublicJWK is the public key in JWK form; private
// material never reaches this table.
type MachineClientKeyRecord struct {
	TenantID  uuid.UUID
	RowID     uuid.UUID
	ClientID  string
	KID       string
	Alg       string
	PublicJWK json.RawMessage
	NotBefore time.Time
	ExpiresAt *time.Time
	Revoked   bool
}

func encodeStringSet(values []string) ([]byte, error) {
	if values == nil {
		return []byte(`[]`), nil
	}
	return json.Marshal(values)
}

func decodeStringSet(raw []byte, table, key, field string) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, failure(CodeDatabase, table, key, fmt.Errorf("decode %s: %w", field, err))
	}
	return out, nil
}

func validateMachineClient(in MachineClientRecord) error {
	if in.ClientID == "" || in.Owner == "" || in.Purpose == "" {
		return invalid("machine_client", in.ClientID, "client id, owner and purpose are required")
	}
	switch in.Status {
	case "", MachineClientActive, MachineClientSuspended, MachineClientRevoked:
	default:
		return invalid("machine_client", in.ClientID, "unknown status")
	}
	if in.ExpiresAt != nil && in.ExpiresAt.IsZero() {
		return invalid("machine_client", in.ClientID, "expiry must be a real instant")
	}
	return nil
}

// RegisterMachineClient inserts one client registration. A client identity
// cannot be silently replaced: a repeated client id is a typed duplicate.
func (s *Store) RegisterMachineClient(ctx context.Context, tenantID uuid.UUID, in MachineClientRecord) error {
	if tenantID == uuid.Nil || in.TenantID != uuid.Nil && in.TenantID != tenantID {
		return failure(CodeTenantRequired, "machine_client", in.ClientID, errors.New("tenant is missing or mismatched"))
	}
	if err := validateMachineClient(in); err != nil {
		return err
	}
	status := in.Status
	if status == "" {
		status = MachineClientActive
	}
	scopes, err := encodeStringSet(in.Scopes)
	if err != nil {
		return failure(CodeInvalid, "machine_client", in.ClientID, err)
	}
	domains, err := encodeStringSet(in.DataDomains)
	if err != nil {
		return failure(CodeInvalid, "machine_client", in.ClientID, err)
	}
	fields, err := encodeStringSet(in.FieldSubset)
	if err != nil {
		return failure(CodeInvalid, "machine_client", in.ClientID, err)
	}
	allowlist, err := encodeStringSet(in.IPAllowlist)
	if err != nil {
		return failure(CodeInvalid, "machine_client", in.ClientID, err)
	}
	return s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO machine_client
				(tenant_id,row_id,client_id,owner,status,scopes,purpose,data_domains,field_subset,ip_allowlist,cert_fingerprint,expires_at,revoked)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,''),$12,$13)`,
			tenantID, nonNilUUID(in.RowID), in.ClientID, in.Owner, status, scopes, in.Purpose,
			domains, fields, allowlist, in.CertFingerprint, nullableTimePtr(in.ExpiresAt), in.Revoked)
		if err != nil {
			return classifyWriteError("machine_client", in.ClientID, err)
		}
		return nil
	})
}

func nullableTimePtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return nullableTime(*t)
}

func scanMachineClient(row dbport.Row, table, key string) (MachineClientRecord, error) {
	var out MachineClientRecord
	var scopes, domains, fields, allowlist []byte
	var expiresAt, lastUsedAt, createdAt, updatedAt *time.Time
	if err := row.Scan(
		&out.TenantID, &out.RowID, &out.ClientID, &out.Owner, &out.Status,
		&scopes, &out.Purpose, &domains, &fields, &allowlist, &out.CertFingerprint,
		&expiresAt, &out.Revoked, &lastUsedAt, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return MachineClientRecord{}, failure(CodeNotFound, table, key, errors.New("client not found"))
		}
		return MachineClientRecord{}, failure(CodeDatabase, table, key, err)
	}
	var err error
	if out.Scopes, err = decodeStringSet(scopes, table, key, "scopes"); err != nil {
		return MachineClientRecord{}, err
	}
	if out.DataDomains, err = decodeStringSet(domains, table, key, "data_domains"); err != nil {
		return MachineClientRecord{}, err
	}
	if out.FieldSubset, err = decodeStringSet(fields, table, key, "field_subset"); err != nil {
		return MachineClientRecord{}, err
	}
	if out.IPAllowlist, err = decodeStringSet(allowlist, table, key, "ip_allowlist"); err != nil {
		return MachineClientRecord{}, err
	}
	out.ExpiresAt = expiresAt
	out.LastUsedAt = lastUsedAt
	if createdAt != nil {
		out.CreatedAt = createdAt.UTC()
	}
	if updatedAt != nil {
		out.UpdatedAt = updatedAt.UTC()
	}
	return out, nil
}

// LoadMachineClient loads one client registration by id.
func (s *Store) LoadMachineClient(ctx context.Context, tenantID uuid.UUID, clientID string) (MachineClientRecord, error) {
	if tenantID == uuid.Nil {
		return MachineClientRecord{}, failure(CodeTenantRequired, "machine_client", clientID, errors.New("tenant is required"))
	}
	var out MachineClientRecord
	err := s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		var err error
		out, err = scanMachineClient(tx.QueryRow(ctx, `
			SELECT tenant_id,row_id,client_id,owner,status,scopes,purpose,data_domains,field_subset,
				ip_allowlist,COALESCE(cert_fingerprint,''),expires_at,revoked,last_used_at,created_at,updated_at
			FROM machine_client WHERE tenant_id=$1 AND client_id=$2`,
			tenantID, clientID), "machine_client", clientID)
		return err
	})
	return out, err
}

// SetMachineClientState changes lifecycle state and revocation together so a
// suspension or revocation is one atomic control write. now stamps updated_at.
func (s *Store) SetMachineClientState(ctx context.Context, tenantID uuid.UUID, clientID, status string, revoked bool, now time.Time) (MachineClientRecord, error) {
	if tenantID == uuid.Nil {
		return MachineClientRecord{}, failure(CodeTenantRequired, "machine_client", clientID, errors.New("tenant is required"))
	}
	switch status {
	case MachineClientActive, MachineClientSuspended, MachineClientRevoked:
	default:
		return MachineClientRecord{}, invalid("machine_client", clientID, "unknown status")
	}
	if now.IsZero() {
		return MachineClientRecord{}, invalid("machine_client", clientID, "timestamp is required")
	}
	var out MachineClientRecord
	err := s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		affected, err := tx.Exec(ctx, `
			UPDATE machine_client SET status=$3, revoked=$4, updated_at=$5
			WHERE tenant_id=$1 AND client_id=$2`, tenantID, clientID, status, revoked, now.UTC())
		if err != nil {
			return classifyWriteError("machine_client", clientID, err)
		}
		if affected == 0 {
			return failure(CodeNotFound, "machine_client", clientID, errors.New("client not found"))
		}
		var err2 error
		out, err2 = scanMachineClient(tx.QueryRow(ctx, `
			SELECT tenant_id,row_id,client_id,owner,status,scopes,purpose,data_domains,field_subset,
				ip_allowlist,COALESCE(cert_fingerprint,''),expires_at,revoked,last_used_at,created_at,updated_at
			FROM machine_client WHERE tenant_id=$1 AND client_id=$2`,
			tenantID, clientID), "machine_client", clientID)
		return err2
	})
	return out, err
}

// TouchMachineClientUse records the last successful authentication without
// touching lifecycle state.
func (s *Store) TouchMachineClientUse(ctx context.Context, tenantID uuid.UUID, clientID string, now time.Time) error {
	if tenantID == uuid.Nil {
		return failure(CodeTenantRequired, "machine_client", clientID, errors.New("tenant is required"))
	}
	if now.IsZero() {
		return invalid("machine_client", clientID, "timestamp is required")
	}
	return s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		affected, err := tx.Exec(ctx, `
			UPDATE machine_client SET last_used_at=$3, updated_at=$3
			WHERE tenant_id=$1 AND client_id=$2`, tenantID, clientID, now.UTC())
		if err != nil {
			return classifyWriteError("machine_client", clientID, err)
		}
		if affected == 0 {
			return failure(CodeNotFound, "machine_client", clientID, errors.New("client not found"))
		}
		return nil
	})
}

func validateMachineClientKey(in MachineClientKeyRecord) error {
	if in.ClientID == "" || in.KID == "" {
		return invalid("machine_client_key", in.ClientID, "client id and key id are required")
	}
	switch in.Alg {
	case "EdDSA", "RS256", "ES256":
	default:
		return invalid("machine_client_key", in.KID, "unknown algorithm")
	}
	if len(in.PublicJWK) == 0 {
		return invalid("machine_client_key", in.KID, "public JWK is required")
	}
	var probe map[string]any
	if err := json.Unmarshal(in.PublicJWK, &probe); err != nil {
		return invalid("machine_client_key", in.KID, "public JWK is not JSON")
	}
	if probe["kty"] == nil || probe["crv"] == nil && probe["n"] == nil && probe["x"] == nil {
		return invalid("machine_client_key", in.KID, "public JWK names no key material")
	}
	if in.NotBefore.IsZero() {
		return invalid("machine_client_key", in.KID, "not_before is required")
	}
	if in.ExpiresAt != nil && !in.ExpiresAt.After(in.NotBefore) {
		return invalid("machine_client_key", in.KID, "expires_at must be after not_before")
	}
	return nil
}

// RegisterMachineClientKey adds one assertion-verification key to a client.
// Key identity cannot be silently replaced: a repeated kid is a typed
// duplicate. Private material must never reach this table; only the key id,
// algorithm and public JWK are stored.
func (s *Store) RegisterMachineClientKey(ctx context.Context, tenantID uuid.UUID, in MachineClientKeyRecord) error {
	if tenantID == uuid.Nil || in.TenantID != uuid.Nil && in.TenantID != tenantID {
		return failure(CodeTenantRequired, "machine_client_key", in.KID, errors.New("tenant is missing or mismatched"))
	}
	if err := validateMachineClientKey(in); err != nil {
		return err
	}
	return s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO machine_client_key
				(tenant_id,row_id,client_id,kid,alg,public_jwk,not_before,expires_at,revoked)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			tenantID, nonNilUUID(in.RowID), in.ClientID, in.KID, in.Alg, in.PublicJWK,
			in.NotBefore.UTC(), nullableTimePtr(in.ExpiresAt), in.Revoked)
		if err != nil {
			return classifyWriteError("machine_client_key", in.KID, err)
		}
		return nil
	})
}

// LoadMachineClientKeys returns every key row for a client ordered by kid.
// Callers filter validity windows and revocation for the instant they
// authenticate at; storage never decides what counts as usable.
func (s *Store) LoadMachineClientKeys(ctx context.Context, tenantID uuid.UUID, clientID string) ([]MachineClientKeyRecord, error) {
	if tenantID == uuid.Nil {
		return nil, failure(CodeTenantRequired, "machine_client_key", clientID, errors.New("tenant is required"))
	}
	var out []MachineClientKeyRecord
	err := s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT tenant_id,row_id,client_id,kid,alg,public_jwk,not_before,expires_at,revoked
			FROM machine_client_key WHERE tenant_id=$1 AND client_id=$2 ORDER BY kid`,
			tenantID, clientID)
		if err != nil {
			return failure(CodeDatabase, "machine_client_key", clientID, err)
		}
		defer rows.Close()
		for rows.Next() {
			var rec MachineClientKeyRecord
			var expiresAt *time.Time
			if err := rows.Scan(&rec.TenantID, &rec.RowID, &rec.ClientID, &rec.KID, &rec.Alg,
				&rec.PublicJWK, &rec.NotBefore, &expiresAt, &rec.Revoked); err != nil {
				return failure(CodeDatabase, "machine_client_key", clientID, err)
			}
			rec.NotBefore = rec.NotBefore.UTC()
			if expiresAt != nil {
				exp := expiresAt.UTC()
				rec.ExpiresAt = &exp
			}
			out = append(out, rec)
		}
		return rows.Err()
	})
	return out, err
}

// RevokeMachineClientKey marks one key revoked so assertions under it fail
// closed while the row itself stays for audit.
func (s *Store) RevokeMachineClientKey(ctx context.Context, tenantID uuid.UUID, clientID, kid string) error {
	if tenantID == uuid.Nil {
		return failure(CodeTenantRequired, "machine_client_key", kid, errors.New("tenant is required"))
	}
	return s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		affected, err := tx.Exec(ctx, `
			UPDATE machine_client_key SET revoked=true
			WHERE tenant_id=$1 AND client_id=$2 AND kid=$3`, tenantID, clientID, kid)
		if err != nil {
			return classifyWriteError("machine_client_key", kid, err)
		}
		if affected == 0 {
			return failure(CodeNotFound, "machine_client_key", kid, errors.New("key not found"))
		}
		return nil
	})
}
