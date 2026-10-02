package agentstore

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

var ErrSupportDailyLimit = errors.New("agentstore: support daily limit reached")

func agentuxDemoInboxDigest(value string) bool {
	raw, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return strings.HasPrefix(value, "sha256:") && value == strings.ToLower(value) && err == nil && len(raw) == 32
}

type SupportPlanReceipt struct {
	TenantID                              uuid.UUID
	MessageID, RunID, PlanRef, PlanDigest string
	RecordedAt                            time.Time
}

func (s *SupportInboxStore) agentuxDemoFenceTx(ctx context.Context, tenant uuid.UUID, fn func(dbport.Tx) error) error {
	separate, ok := s.runner.(interface {
		RunTenantFenceTx(context.Context, uuid.UUID, func(dbport.Tx) error) error
	})
	if !ok {
		return ErrSupportInboxInvalid
	}
	return separate.RunTenantFenceTx(ctx, tenant, fn)
}

// WithSupportRunFence serializes processing by the immutable tenant/email key.
// The lock covers retries while business effects keep their own idempotency.
func (s *SupportInboxStore) WithSupportRunFence(ctx context.Context, tenant uuid.UUID, message string, fn func() error) error {
	if s == nil || s.runner == nil || ctx == nil || tenant == uuid.Nil || !agentuxDemoClean(message, 128) || fn == nil {
		return ErrSupportInboxInvalid
	}
	return s.agentuxDemoFenceTx(ctx, tenant, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "support-run:"+tenant.String()+":"+message); err != nil {
			return err
		}
		return fn()
	})
}

// ReserveSupportEffect counts unique email effects, never retry attempts. The
// transaction lock closes the concurrent check/insert race at the daily cap.
func (s *SupportInboxStore) ReserveSupportEffect(ctx context.Context, tenant uuid.UUID, message, skill string, now time.Time, limit uint32) error {
	if s == nil || s.runner == nil || ctx == nil || tenant == uuid.Nil || !agentuxDemoClean(message, 128) || (skill != "support.create_ticket" && skill != "support.alert_channel") || now.IsZero() || limit == 0 || limit > 25 {
		return ErrSupportInboxInvalid
	}
	return s.runner.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "support-budget:"+tenant.String()+":"+skill); err != nil {
			return err
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM support_effect_reservation WHERE tenant_id=$1 AND skill_id=$2 AND message_id=$3)`, tenant, skill, message).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return nil
		}
		var count uint32
		day := now.UTC().Format(time.DateOnly)
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM support_effect_reservation WHERE tenant_id=$1 AND skill_id=$2 AND budget_day=$3::date`, tenant, skill, day).Scan(&count); err != nil {
			return err
		}
		if count >= limit {
			return ErrSupportDailyLimit
		}
		_, err := tx.Exec(ctx, `INSERT INTO support_effect_reservation(tenant_id,skill_id,message_id,budget_day,reserved_at) VALUES($1,$2,$3,$4::date,$5)`, tenant, skill, message, day, now.UTC())
		return err
	})
}

func (s *SupportInboxStore) RecordSupportPlan(ctx context.Context, r SupportPlanReceipt) error {
	if s == nil || s.runner == nil || ctx == nil || r.TenantID == uuid.Nil || !agentuxDemoClean(r.MessageID, 128) || !agentuxDemoClean(r.RunID, 256) || !agentuxDemoClean(r.PlanRef, 512) || !agentuxDemoInboxDigest(r.PlanDigest) || r.RecordedAt.IsZero() {
		return ErrSupportInboxInvalid
	}
	return s.runner.RunTenantTx(ctx, r.TenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO support_run_plan(tenant_id,message_id,run_id,plan_ref,plan_digest,recorded_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, r.TenantID, r.MessageID, r.RunID, r.PlanRef, r.PlanDigest, r.RecordedAt.UTC())
		if err != nil {
			return err
		}
		var run, ref, digest string
		if err := tx.QueryRow(ctx, `SELECT run_id,plan_ref,plan_digest FROM support_run_plan WHERE tenant_id=$1 AND message_id=$2`, r.TenantID, r.MessageID).Scan(&run, &ref, &digest); err != nil {
			return err
		}
		if run != r.RunID || ref != r.PlanRef || digest != r.PlanDigest {
			return ErrSupportInboxReplay
		}
		return nil
	})
}

func (s *SupportInboxStore) GetSupportPlan(ctx context.Context, tenant uuid.UUID, message string) (SupportPlanReceipt, error) {
	if s == nil || s.runner == nil || ctx == nil || tenant == uuid.Nil || !agentuxDemoClean(message, 128) {
		return SupportPlanReceipt{}, ErrSupportInboxInvalid
	}
	r := SupportPlanReceipt{TenantID: tenant, MessageID: message}
	err := s.runner.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT run_id,plan_ref,plan_digest,recorded_at FROM support_run_plan WHERE tenant_id=$1 AND message_id=$2`, tenant, message).Scan(&r.RunID, &r.PlanRef, &r.PlanDigest, &r.RecordedAt)
	})
	if errors.Is(err, dbport.ErrNoRows) {
		return r, ErrSupportInboxNotFound
	}
	if err != nil {
		return r, fmt.Errorf("support plan read: %w", err)
	}
	r.RecordedAt = r.RecordedAt.UTC()
	return r, nil
}
