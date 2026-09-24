// Package configbundlekill persists applied CP-008 switch evidence.
package configbundlekill

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/configbundle"
)

type DB interface{ dbport.Beginner }

type Applied struct {
	Switch  configbundle.SignedKillSwitch         `json:"switch"`
	Receipt configbundle.AppliedKillSwitchReceipt `json:"receipt"`
}

type Store struct {
	db        DB
	publicKey ed25519.PublicKey
	now       func() time.Time
}

func New(db DB, publicKeys ...ed25519.PublicKey) *Store {
	s := &Store{db: db, now: func() time.Time { return time.Now().UTC() }}
	if len(publicKeys) > 0 {
		s.publicKey = append(ed25519.PublicKey(nil), publicKeys[0]...)
	}
	return s
}

// SetClock supplies the trusted receiver clock. Production composition uses
// the default UTC clock; tests can make expiration boundaries deterministic.
func (s *Store) SetClock(clock func() time.Time) {
	if s != nil && clock != nil {
		s.now = clock
	}
}

// Guard serializes a rollout transition with durable CP-008 application for
// the same tenant. Both Guard and PutApplied take the same transaction lock,
// so another process cannot apply a kill between the decision and proceed.
func (s *Store) Guard(subject configbundle.KillSwitchTarget, proceed func(configbundle.KillDecision) error) error {
	return s.GuardContext(context.Background(), subject, proceed)
}

// GuardContext is Guard with caller cancellation propagated to the transaction.
func (s *Store) GuardContext(ctx context.Context, subject configbundle.KillSwitchTarget, proceed func(configbundle.KillDecision) error) error {
	if s == nil || s.db == nil || proceed == nil || len(s.publicKey) != ed25519.PublicKeySize || strings.TrimSpace(subject.TenantID) == "" {
		return configbundle.ErrKillSwitchReceipt
	}
	if ctx == nil {
		return configbundle.ErrKillSwitchReceipt
	}
	tenantID, err := uuid.Parse(subject.TenantID)
	if err != nil {
		return fmt.Errorf("configbundlekill: invalid tenant id: %w", err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("configbundlekill: guard begin: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return err
	}
	if err := tenantKillLock(ctx, tx, subject.TenantID); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT applied_record FROM configbundle_kill_switch WHERE tenant_id=$1 ORDER BY switch_id`, subject.TenantID)
	if err != nil {
		return err
	}
	var candidates []configbundle.SignedKillSwitch
	now := s.now().UTC()
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			rows.Close()
			return err
		}
		var item Applied
		if err := json.Unmarshal(payload, &item); err != nil {
			rows.Close()
			return fmt.Errorf("configbundlekill: corrupt applied record: %w", err)
		}
		if item.Switch.Request.Target.TenantID != subject.TenantID || item.Switch.Verify(s.publicKey) != nil || item.Receipt.Verify(s.publicKey) != nil ||
			item.Receipt.SwitchID != item.Switch.Request.SwitchID || item.Receipt.SwitchDigest != item.Switch.Digest || item.Receipt.Target != item.Switch.Request.Target {
			rows.Close()
			return configbundle.ErrKillSwitchReceipt
		}
		if item.Switch.Request.Target.Matches(subject) && now.Before(item.Switch.Request.ExpiresAt) {
			candidates = append(candidates, item.Switch)
		}
	}
	rowErr := rows.Err()
	rows.Close()
	if rowErr != nil {
		return rowErr
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := candidates[i].Request, candidates[j].Request
		if left.Priority != right.Priority {
			return left.Priority > right.Priority
		}
		if left.Target.Specificity() != right.Target.Specificity() {
			return left.Target.Specificity() > right.Target.Specificity()
		}
		if !left.IssuedAt.Equal(right.IssuedAt) {
			return left.IssuedAt.After(right.IssuedAt)
		}
		return candidates[i].Digest < candidates[j].Digest
	})
	decision := configbundle.KillDecision{Reason: "NO_ACTIVE_SWITCH"}
	if len(candidates) != 0 {
		selected := candidates[0]
		decision = configbundle.KillDecision{Disabled: true, SwitchID: selected.Request.SwitchID, SwitchDigest: selected.Digest,
			Priority: selected.Request.Priority, Reason: selected.Request.Reason, ExpiresAt: selected.Request.ExpiresAt}
	}
	if err := proceed(decision); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func tenantKillLock(ctx context.Context, tx dbport.Tx, tenantID string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "configbundle-kill:"+tenantID)
	return err
}

// PutApplied durably records a verified applied state. A retry is idempotent
// only when both signed digests match the immutable prior record.
func (s *Store) PutApplied(ctx context.Context, switchValue configbundle.SignedKillSwitch, receipt configbundle.AppliedKillSwitchReceipt) error {
	if s == nil || s.db == nil || len(s.publicKey) != ed25519.PublicKeySize || switchValue.Verify(s.publicKey) != nil || receipt.Verify(s.publicKey) != nil || receipt.SwitchID != switchValue.Request.SwitchID || receipt.SwitchDigest != switchValue.Digest || receipt.Target != switchValue.Request.Target {
		return configbundle.ErrKillSwitchReceipt
	}
	// Signature verification is performed by the owning CP-008 store, which
	// holds the trusted key. This adapter enforces identity and durable shape.
	if _, err := uuid.Parse(switchValue.Request.Target.TenantID); err != nil {
		return fmt.Errorf("configbundlekill: invalid tenant id: %w", err)
	}
	payload, err := json.Marshal(Applied{Switch: switchValue, Receipt: receipt})
	if err != nil {
		return err
	}
	tenantID := switchValue.Request.Target.TenantID
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("configbundlekill: begin: %w", err)
	}
	defer tx.Rollback(ctx)
	parsed, _ := uuid.Parse(tenantID)
	if err := tenancy.WithTenant(ctx, tx, parsed); err != nil {
		return err
	}
	if err := tenantKillLock(ctx, tx, tenantID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO configbundle_kill_switch (tenant_id,switch_id,switch_digest,receipt_digest,applied_record)
		VALUES ($1,$2,$3,$4,$5) ON CONFLICT (tenant_id,switch_id) DO NOTHING`, tenantID, switchValue.Request.SwitchID, switchValue.Digest, receipt.Digest, payload)
	if err != nil {
		return err
	}
	var existingSwitch, existingReceipt string
	row := tx.QueryRow(ctx, `SELECT switch_digest,receipt_digest FROM configbundle_kill_switch WHERE tenant_id=$1 AND switch_id=$2`, tenantID, switchValue.Request.SwitchID)
	if err := row.Scan(&existingSwitch, &existingReceipt); err != nil {
		return err
	}
	if existingSwitch != switchValue.Digest || existingReceipt != receipt.Digest {
		return configbundle.ErrKillSwitchConflict
	}
	return tx.Commit(ctx)
}

// ListApplied restores every applied switch for one tenant under tenant RLS.
func (s *Store) ListApplied(ctx context.Context, tenantID string) ([]Applied, error) {
	if s == nil || s.db == nil || len(s.publicKey) != ed25519.PublicKeySize {
		return nil, configbundle.ErrKillSwitchReceipt
	}
	parsed, err := uuid.Parse(tenantID)
	if err != nil {
		return nil, fmt.Errorf("configbundlekill: invalid tenant id: %w", err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("configbundlekill: begin: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, parsed); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT applied_record FROM configbundle_kill_switch WHERE tenant_id=$1 ORDER BY switch_id`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Applied
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var item Applied
		if err := json.Unmarshal(payload, &item); err != nil {
			return nil, fmt.Errorf("configbundlekill: corrupt applied record: %w", err)
		}
		if item.Switch.Request.Target.TenantID != tenantID || item.Switch.Verify(s.publicKey) != nil || item.Receipt.Verify(s.publicKey) != nil || item.Receipt.SwitchID != item.Switch.Request.SwitchID || item.Receipt.SwitchDigest != item.Switch.Digest || item.Receipt.Target != item.Switch.Request.Target {
			return nil, configbundle.ErrKillSwitchReceipt
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil && !errors.Is(err, dbport.ErrNoRows) {
		return nil, err
	}
	return result, nil
}
