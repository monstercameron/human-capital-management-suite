package timestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ErrEnrollmentCodeInvalid covers every reason a code cannot be redeemed
// right now: unknown, expired or already used. Callers must not be able to
// distinguish "already used" from "never existed" from the error alone,
// since that distinction would let an attacker probe for issued codes.
var ErrEnrollmentCodeInvalid = errors.New("time: enrollment code invalid, expired or already used")

// EnrollmentCode is the persisted record of a single-use code. Code is
// never stored; CodeHash is the only trace of it that survives.
type EnrollmentCode struct {
	TenantID, CodeHash, SiteID, ProfileID, Timezone, CreatedBy string
	ExpiresAt                                                  time.Time
	UsedAt                                                     *time.Time
	UsedByDeviceID                                             string
	CreatedAt                                                  time.Time
}

// HashEnrollmentCode returns the stored form of a raw enrollment code. The
// raw code is a bearer secret handed to whoever pairs the device (paper,
// QR, MDM payload); only its hash ever reaches this store.
func HashEnrollmentCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

// CreateEnrollmentCode records a single-use code an admin has generated.
// The caller is responsible for delivering the raw code (paper, QR, MDM
// config) exactly once; this store only ever sees and keeps its hash.
func (s *Store) CreateEnrollmentCode(ctx context.Context, tenant, code, siteID, profileID, timezone, createdBy string, expiresAt time.Time) error {
	if tenant == "" || code == "" || siteID == "" || profileID == "" || timezone == "" || createdBy == "" || expiresAt.IsZero() {
		return ErrInvalid
	}
	hash := HashEnrollmentCode(code)
	return s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		tag, err := tx.Exec(ctx, `INSERT INTO time_enrollment_code(tenant_id,code_hash,site_id,profile_id,timezone,created_by,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (tenant_id,code_hash) DO NOTHING`,
			tenant, hash, siteID, profileID, timezone, createdBy, expiresAt)
		if err != nil {
			return err
		}
		if tag != 1 {
			return ErrIdempotencyConflict
		}
		return nil
	})
}

// RedeemEnrollmentCode consumes a single-use code and enrolls the device
// that proved it, in one transaction. The UPDATE that marks the code used
// is guarded by "used_at IS NULL", so under concurrent redemption of the
// same code exactly one transaction's UPDATE affects a row; every other
// caller sees zero rows affected and gets ErrEnrollmentCodeInvalid, never a
// second device for the same code.
func (s *Store) RedeemEnrollmentCode(ctx context.Context, tenant, code, deviceID string, publicKey []byte, actorID string, now time.Time) (Device, error) {
	if tenant == "" || strings.TrimSpace(code) == "" || deviceID == "" || len(publicKey) == 0 || actorID == "" || now.IsZero() {
		return Device{}, ErrInvalid
	}
	hash := HashEnrollmentCode(code)
	var out Device
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE time_enrollment_code SET used_at=$1, used_by_device_id=$2 WHERE tenant_id=$3 AND code_hash=$4 AND used_at IS NULL AND expires_at > $1`,
			now, deviceID, tenant, hash)
		if err != nil {
			return err
		}
		if tag != 1 {
			return ErrEnrollmentCodeInvalid
		}
		var siteID, profileID, timezone string
		err = tx.QueryRow(ctx, `SELECT site_id,profile_id,timezone FROM time_enrollment_code WHERE tenant_id=$1 AND code_hash=$2`, tenant, hash).Scan(&siteID, &profileID, &timezone)
		if err != nil {
			return err
		}
		if err = insertDevice(ctx, tx, tenant, deviceID, publicKey, siteID, profileID, timezone, actorID); err != nil {
			return err
		}
		out, err = scanDevice(tx.QueryRow(ctx, `SELECT `+deviceColumns+` FROM time_device WHERE tenant_id=$1 AND id=$2`, tenant, deviceID))
		return err
	})
	if err != nil {
		return Device{}, err
	}
	return out, nil
}

// GetEnrollmentCode returns the record for a raw code, for admin display
// (issued, expired, used) without ever needing the raw code back.
func (s *Store) GetEnrollmentCode(ctx context.Context, tenant, code string) (EnrollmentCode, error) {
	if tenant == "" || code == "" {
		return EnrollmentCode{}, ErrInvalid
	}
	hash := HashEnrollmentCode(code)
	var out EnrollmentCode
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		row := tx.QueryRow(ctx, `SELECT tenant_id,code_hash,site_id,profile_id,timezone,created_by,expires_at,used_at,COALESCE(used_by_device_id::text,''),created_at FROM time_enrollment_code WHERE tenant_id=$1 AND code_hash=$2`, tenant, hash)
		err := row.Scan(&out.TenantID, &out.CodeHash, &out.SiteID, &out.ProfileID, &out.Timezone, &out.CreatedBy, &out.ExpiresAt, &out.UsedAt, &out.UsedByDeviceID, &out.CreatedAt)
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrNotFound
		}
		return err
	})
	if err != nil {
		return EnrollmentCode{}, err
	}
	return out, nil
}
