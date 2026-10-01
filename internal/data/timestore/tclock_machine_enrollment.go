package timestore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// MachineEnrollmentInput is the storage-safe portion of a machine
// enrollment command. It contains public proof material only.
type MachineEnrollmentInput struct {
	Tenant, EnrollmentCode, DeviceID, MachineClientID string
	Owner, SiteID, ProfileID, Timezone                string
	PublicKey, Challenge, Signature, KeyID            []byte
	Scopes                                            []string
	Purpose, IdempotencyKey                           string
	ExpiresAt, Now                                    time.Time
}

// PendingMachineEnrollment is the durable saga hand-off to the core
// machine-client registry.
type PendingMachineEnrollment struct {
	Tenant, DeviceID, MachineClientID, Owner string
	SiteID, ProfileID, Timezone              string
	PublicKey, Challenge, Signature, KeyID   []byte
	Scopes                                   []string
	Purpose, IdempotencyKey                  string
	ExpiresAt, Now                           time.Time
	State, CredentialRef                     string
}

// PendingMachineEnrollmentFor returns the tenant-scoped pending record used
// to validate a registry binding before activation.
func (s *Store) PendingMachineEnrollmentFor(ctx context.Context, tenant, deviceID string) (PendingMachineEnrollment, error) {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(deviceID) == "" {
		return PendingMachineEnrollment{}, ErrInvalid
	}
	var out PendingMachineEnrollment
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var scopes []byte
		err := tx.QueryRow(ctx, `SELECT device_id,machine_client_id,owner,site_id,profile_id,timezone,public_key,challenge,signature,key_id,scopes,purpose,expires_at,idempotency_key,state,credential_ref FROM time_machine_enrollment_pending WHERE tenant_id=$1 AND device_id=$2`, tenant, deviceID).Scan(&out.DeviceID, &out.MachineClientID, &out.Owner, &out.SiteID, &out.ProfileID, &out.Timezone, &out.PublicKey, &out.Challenge, &out.Signature, &out.KeyID, &scopes, &out.Purpose, &out.ExpiresAt, &out.IdempotencyKey, &out.State, &out.CredentialRef)
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := json.Unmarshal(scopes, &out.Scopes); err != nil {
			return err
		}
		out.Tenant = tenant
		return nil
	})
	return out, err
}

// PrepareMachineEnrollment consumes a code, stores a pending device and
// appends the hand-off event in one transaction. Repeating the exact request
// is idempotent; a code or key cannot be rebound to another device.
func (s *Store) PrepareMachineEnrollment(ctx context.Context, in MachineEnrollmentInput) (PendingMachineEnrollment, error) {
	if err := validateMachineEnrollment(in); err != nil {
		return PendingMachineEnrollment{}, err
	}
	hash := HashEnrollmentCode(in.EnrollmentCode)
	var out PendingMachineEnrollment
	err := s.RunTenantTx(ctx, in.Tenant, func(tx dbport.Tx) error {
		var p PendingMachineEnrollment
		var scopes []byte
		err := tx.QueryRow(ctx, `SELECT device_id,machine_client_id,owner,site_id,profile_id,timezone,public_key,challenge,signature,key_id,scopes,purpose,expires_at,idempotency_key,state,credential_ref FROM time_machine_enrollment_pending WHERE tenant_id=$1 AND enrollment_code_hash=$2 FOR UPDATE`, in.Tenant, hash).Scan(
			&p.DeviceID, &p.MachineClientID, &p.Owner, &p.SiteID, &p.ProfileID, &p.Timezone, &p.PublicKey, &p.Challenge, &p.Signature, &p.KeyID, &scopes, &p.Purpose, &p.ExpiresAt, &p.IdempotencyKey, &p.State, &p.CredentialRef)
		if err == nil {
			if !sameMachineEnrollment(in, p) {
				return ErrEnrollmentCodeInvalid
			}
			if err := json.Unmarshal(scopes, &p.Scopes); err != nil {
				return err
			}
			p.Tenant, p.Now = in.Tenant, in.Now
			out = p
			return nil
		}
		if !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		var usedAt *time.Time
		var site, profile, timezone string
		err = tx.QueryRow(ctx, `SELECT used_at,site_id,profile_id,timezone FROM time_enrollment_code WHERE tenant_id=$1 AND code_hash=$2 FOR UPDATE`, in.Tenant, hash).Scan(&usedAt, &site, &profile, &timezone)
		if errors.Is(err, dbport.ErrNoRows) || usedAt != nil || !in.Now.Before(in.ExpiresAt) {
			return ErrEnrollmentCodeInvalid
		}
		if site != in.SiteID || profile != in.ProfileID || timezone != in.Timezone {
			return ErrEnrollmentCodeInvalid
		}
		scopesJSON, err := json.Marshal(in.Scopes)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO time_machine_enrollment_pending(tenant_id,device_id,machine_client_id,enrollment_code_hash,owner,site_id,profile_id,timezone,public_key,challenge,signature,key_id,scopes,purpose,expires_at,idempotency_key) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13::jsonb,$14,$15,$16)`, in.Tenant, in.DeviceID, in.MachineClientID, hash, in.Owner, in.SiteID, in.ProfileID, in.Timezone, in.PublicKey, in.Challenge, in.Signature, string(in.KeyID), scopesJSON, in.Purpose, in.ExpiresAt, in.IdempotencyKey)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE time_enrollment_code SET used_at=$1,used_by_device_id=$2 WHERE tenant_id=$3 AND code_hash=$4 AND used_at IS NULL`, in.Now, in.DeviceID, in.Tenant, hash)
		if err != nil {
			return err
		}
		payload, err := json.Marshal(map[string]any{"device_id": in.DeviceID, "machine_client_id": in.MachineClientID, "key_id": string(in.KeyID), "purpose": in.Purpose})
		if err != nil {
			return err
		}
		if err := appendOutbox(ctx, tx, in.Tenant, "clock.machine_enrollment.pending", 1, payload); err != nil {
			return err
		}
		out = PendingMachineEnrollment{Tenant: in.Tenant, DeviceID: in.DeviceID, MachineClientID: in.MachineClientID, Owner: in.Owner, SiteID: in.SiteID, ProfileID: in.ProfileID, Timezone: in.Timezone, PublicKey: append([]byte(nil), in.PublicKey...), Challenge: append([]byte(nil), in.Challenge...), Signature: append([]byte(nil), in.Signature...), KeyID: append([]byte(nil), in.KeyID...), Scopes: append([]string(nil), in.Scopes...), Purpose: in.Purpose, IdempotencyKey: in.IdempotencyKey, ExpiresAt: in.ExpiresAt, Now: in.Now, State: "PENDING"}
		return nil
	})
	return out, err
}

// ActivateMachineEnrollment binds a real registry credential reference to a
// pending row and creates the active device atomically. Replays return the
// already activated device after verifying the reference.
func (s *Store) ActivateMachineEnrollment(ctx context.Context, tenant, deviceID, credentialRef string) (Device, error) {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(deviceID) == "" || strings.TrimSpace(credentialRef) == "" {
		return Device{}, ErrInvalid
	}
	var out Device
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var p PendingMachineEnrollment
		var scopes []byte
		err := tx.QueryRow(ctx, `SELECT device_id,machine_client_id,owner,site_id,profile_id,timezone,public_key,challenge,signature,key_id,scopes,purpose,expires_at,idempotency_key,state,credential_ref FROM time_machine_enrollment_pending WHERE tenant_id=$1 AND device_id=$2 FOR UPDATE`, tenant, deviceID).Scan(&p.DeviceID, &p.MachineClientID, &p.Owner, &p.SiteID, &p.ProfileID, &p.Timezone, &p.PublicKey, &p.Challenge, &p.Signature, &p.KeyID, &scopes, &p.Purpose, &p.ExpiresAt, &p.IdempotencyKey, &p.State, &p.CredentialRef)
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if p.State == "ACTIVATED" {
			if p.CredentialRef != credentialRef {
				return ErrIdempotencyConflict
			}
			out, err = scanDevice(tx.QueryRow(ctx, `SELECT `+deviceColumns+` FROM time_device WHERE tenant_id=$1 AND id=$2`, tenant, deviceID))
			return err
		}
		if err := insertDevice(ctx, tx, tenant, p.DeviceID, p.PublicKey, p.SiteID, p.ProfileID, p.Timezone, p.Owner); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE time_machine_enrollment_pending SET state='ACTIVATED',credential_ref=$1,updated_at=now() WHERE tenant_id=$2 AND device_id=$3 AND expires_at > now() AND state='PENDING'`, credentialRef, tenant, deviceID)
		if err != nil {
			return err
		}
		if tag != 1 {
			return ErrEnrollmentCodeInvalid
		}
		payload, err := json.Marshal(map[string]any{"device_id": deviceID, "machine_client_id": p.MachineClientID, "credential_ref": credentialRef})
		if err != nil {
			return err
		}
		if err := appendOutbox(ctx, tx, tenant, "clock.machine_enrollment.activated", 1, payload); err != nil {
			return err
		}
		out, err = scanDevice(tx.QueryRow(ctx, `SELECT `+deviceColumns+` FROM time_device WHERE tenant_id=$1 AND id=$2`, tenant, deviceID))
		return err
	})
	return out, err
}

func validateMachineEnrollment(in MachineEnrollmentInput) error {
	if strings.TrimSpace(in.Tenant) == "" || strings.TrimSpace(in.EnrollmentCode) == "" || in.DeviceID == "" || in.MachineClientID == "" || in.Owner == "" || in.SiteID == "" || in.ProfileID == "" || in.Timezone == "" || len(in.PublicKey) == 0 || len(in.Challenge) == 0 || len(in.Signature) == 0 || len(in.KeyID) == 0 || len(in.Scopes) == 0 || in.Purpose == "" || in.ExpiresAt.IsZero() || in.Now.IsZero() || !in.Now.Before(in.ExpiresAt) || in.IdempotencyKey == "" {
		return ErrInvalid
	}
	return nil
}

func sameMachineEnrollment(in MachineEnrollmentInput, p PendingMachineEnrollment) bool {
	return in.DeviceID == p.DeviceID && in.MachineClientID == p.MachineClientID && in.Owner == p.Owner && in.SiteID == p.SiteID && in.ProfileID == p.ProfileID && in.Timezone == p.Timezone && in.Purpose == p.Purpose && in.IdempotencyKey == p.IdempotencyKey && string(in.PublicKey) == string(p.PublicKey) && string(in.Challenge) == string(p.Challenge) && string(in.Signature) == string(p.Signature) && string(in.KeyID) == string(p.KeyID) && in.ExpiresAt.Equal(p.ExpiresAt) && sameStringSlice(in.Scopes, p.Scopes)
}

func sameStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
