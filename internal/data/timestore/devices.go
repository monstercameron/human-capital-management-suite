package timestore

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Device history event kinds. Every transition that changes a device row
// writes exactly one history entry carrying one of these.
const (
	DeviceEnrolled       = "ENROLLED"
	DeviceKeyRotated     = "KEY_ROTATED"
	DeviceSuspended      = "SUSPENDED"
	DeviceResumed        = "RESUMED"
	DeviceRevoked        = "REVOKED"
	DeviceSiteReassigned = "SITE_REASSIGNED"
)

// Device states.
const (
	DeviceStateActive    = "ACTIVE"
	DeviceStateSuspended = "SUSPENDED"
	DeviceStateRevoked   = "REVOKED"
)

// Device is the current-state row for one enrolled clock or kiosk.
type Device struct {
	TenantID, ID                       string
	PublicKey                          []byte
	SiteID, ProfileID, Timezone, State string
	Revision                           int64
	CreatedAt, UpdatedAt               time.Time
}

// DeviceHistoryEntry is one append-only record of a device transition.
type DeviceHistoryEntry struct {
	ID, TenantID, DeviceID, Kind, ActorID, Reason string
	Revision                                      int64
	CreatedAt                                     time.Time
}

func scanDevice(row dbport.Row) (Device, error) {
	var d Device
	err := row.Scan(&d.TenantID, &d.ID, &d.PublicKey, &d.SiteID, &d.ProfileID, &d.Timezone, &d.State, &d.Revision, &d.CreatedAt, &d.UpdatedAt)
	if errors.Is(err, dbport.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	return d, err
}

const deviceColumns = `tenant_id,id,public_key,site_id,profile_id,timezone,state,revision,created_at,updated_at`

// GetDevice returns the current-state row for a device under tenant RLS.
func (s *Store) GetDevice(ctx context.Context, tenant, id string) (Device, error) {
	if tenant == "" || id == "" {
		return Device{}, ErrInvalid
	}
	var out Device
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		d, err := scanDevice(tx.QueryRow(ctx, `SELECT `+deviceColumns+` FROM time_device WHERE tenant_id=$1 AND id=$2`, tenant, id))
		out = d
		return err
	})
	return out, err
}

// DevicesBySite lists a site's devices ordered by ID, for admin listing and
// sync fan-out.
func (s *Store) DevicesBySite(ctx context.Context, tenant, siteID string, limit int) ([]Device, error) {
	if tenant == "" || siteID == "" || limit <= 0 || limit > 1000 {
		return nil, ErrInvalid
	}
	out := make([]Device, 0, limit)
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+deviceColumns+` FROM time_device WHERE tenant_id=$1 AND site_id=$2 ORDER BY id LIMIT $3`, tenant, siteID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			d, err := scanDevice(rows)
			if err != nil {
				return err
			}
			out = append(out, d)
		}
		return rows.Err()
	})
	return out, err
}

// History returns a device's append-only transition history, oldest first.
func (s *Store) DeviceHistory(ctx context.Context, tenant, id string, limit int) ([]DeviceHistoryEntry, error) {
	if tenant == "" || id == "" || limit <= 0 || limit > 1000 {
		return nil, ErrInvalid
	}
	out := make([]DeviceHistoryEntry, 0, limit)
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,tenant_id,device_id,revision,kind,actor_id,reason,created_at FROM time_device_history WHERE tenant_id=$1 AND device_id=$2 ORDER BY revision LIMIT $3`, tenant, id, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var h DeviceHistoryEntry
			if err := rows.Scan(&h.ID, &h.TenantID, &h.DeviceID, &h.Revision, &h.Kind, &h.ActorID, &h.Reason, &h.CreatedAt); err != nil {
				return err
			}
			out = append(out, h)
		}
		return rows.Err()
	})
	return out, err
}

// insertDevice creates the device row and its ENROLLED history entry inside
// the caller's transaction. enrollment.go calls this after redeeming a code;
// it is not exported so every device is born through a redeemed enrollment
// code or a future equivalent in this package.
func insertDevice(ctx context.Context, tx dbport.Tx, tenant, id string, publicKey []byte, siteID, profileID, timezone, actorID string) error {
	if _, err := tx.Exec(ctx, `INSERT INTO time_device(tenant_id,id,public_key,site_id,profile_id,timezone,state,revision) VALUES($1,$2,$3,$4,$5,$6,'ACTIVE',1)`, tenant, id, publicKey, siteID, profileID, timezone); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO time_device_history(tenant_id,id,device_id,revision,kind,actor_id,reason) VALUES($1,$2,$3,1,$4,$5,'')`, tenant, uuid.NewString(), id, DeviceEnrolled, actorID)
	return err
}

// transitionDevice loads the device row for update, checks the expected
// revision, lets apply mutate a copy, writes the new row and one history
// entry, all in one transaction. Every device state change in this file
// goes through it so the current row and its history can never disagree.
func (s *Store) transitionDevice(ctx context.Context, tenant, id, actorID, reason, kind string, expectedRevision int64, apply func(*Device)) (Device, error) {
	if tenant == "" || id == "" || actorID == "" || expectedRevision <= 0 {
		return Device{}, ErrInvalid
	}
	var out Device
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		d, err := scanDevice(tx.QueryRow(ctx, `SELECT `+deviceColumns+` FROM time_device WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, id))
		if err != nil {
			return err
		}
		if d.Revision != expectedRevision {
			return ErrRevisionConflict
		}
		apply(&d)
		d.Revision = expectedRevision + 1
		tag, err := tx.Exec(ctx, `UPDATE time_device SET public_key=$1,site_id=$2,profile_id=$3,timezone=$4,state=$5,revision=$6,updated_at=now() WHERE tenant_id=$7 AND id=$8 AND revision=$9`,
			d.PublicKey, d.SiteID, d.ProfileID, d.Timezone, d.State, d.Revision, tenant, id, expectedRevision)
		if err != nil {
			return err
		}
		if tag != 1 {
			return ErrRevisionConflict
		}
		if _, err = tx.Exec(ctx, `INSERT INTO time_device_history(tenant_id,id,device_id,revision,kind,actor_id,reason) VALUES($1,$2,$3,$4,$5,$6,$7)`,
			tenant, uuid.NewString(), id, d.Revision, kind, actorID, reason); err != nil {
			return err
		}
		out = d
		return nil
	})
	return out, err
}

// RotateDeviceKey replaces a device's public key after it proves possession
// of the new key pair through whatever channel calls this (out of scope
// here); the store only records the outcome.
func (s *Store) RotateDeviceKey(ctx context.Context, tenant, id string, newPublicKey []byte, actorID, reason string, expectedRevision int64) (Device, error) {
	if len(newPublicKey) == 0 {
		return Device{}, ErrInvalid
	}
	return s.transitionDevice(ctx, tenant, id, actorID, reason, DeviceKeyRotated, expectedRevision, func(d *Device) { d.PublicKey = newPublicKey })
}

// SuspendDevice marks a device SUSPENDED. A suspended device is not revoked:
// it can be resumed without a fresh enrollment code.
func (s *Store) SuspendDevice(ctx context.Context, tenant, id, actorID, reason string, expectedRevision int64) (Device, error) {
	return s.transitionDevice(ctx, tenant, id, actorID, reason, DeviceSuspended, expectedRevision, func(d *Device) { d.State = DeviceStateSuspended })
}

// ResumeDevice returns a SUSPENDED device to ACTIVE.
func (s *Store) ResumeDevice(ctx context.Context, tenant, id, actorID, reason string, expectedRevision int64) (Device, error) {
	return s.transitionDevice(ctx, tenant, id, actorID, reason, DeviceResumed, expectedRevision, func(d *Device) { d.State = DeviceStateActive })
}

// RevokeDevice marks a device REVOKED. Revocation is terminal in this store;
// a revoked device needs a new enrollment code to return to service. Taking
// effect on the next call, and turning a revoked device's queued offline
// punches into exceptions, is the ingest layer's job, not this store's.
func (s *Store) RevokeDevice(ctx context.Context, tenant, id, actorID, reason string, expectedRevision int64) (Device, error) {
	return s.transitionDevice(ctx, tenant, id, actorID, reason, DeviceRevoked, expectedRevision, func(d *Device) { d.State = DeviceStateRevoked })
}

// ReassignDeviceSite moves a device to another site and timezone.
func (s *Store) ReassignDeviceSite(ctx context.Context, tenant, id, newSiteID, newTimezone, actorID, reason string, expectedRevision int64) (Device, error) {
	if newSiteID == "" || newTimezone == "" {
		return Device{}, ErrInvalid
	}
	return s.transitionDevice(ctx, tenant, id, actorID, reason, DeviceSiteReassigned, expectedRevision, func(d *Device) {
		d.SiteID, d.Timezone = newSiteID, newTimezone
	})
}
