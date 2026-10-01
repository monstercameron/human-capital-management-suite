package agentpersonastore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona/limits"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Reserve atomically admits a persona mention against all of its durable
// tenant buckets. Rows are locked in bucket-kind order so concurrent tenants
// cannot deadlock one another.
func (s *TenantStore) Reserve(ctx context.Context, policy limits.Policy, req limits.Request, at time.Time) (limits.Token, error) {
	if err := validateLimitInputs(s, policy, req, at); err != nil {
		return limits.Token{}, err
	}
	if req.TenantID != string(s.tenant) {
		return limits.Token{}, fmt.Errorf("%w: request tenant does not match store", limits.ErrInvalid)
	}
	starts := limitStarts(req, policy, at)
	tx, err := s.begin(ctx)
	if err != nil {
		return limits.Token{}, err
	}
	defer tx.Rollback(ctx)
	for _, bucket := range starts.buckets() {
		if _, err := tx.Exec(ctx, `INSERT INTO persona_limit_buckets
			(tenant_id,bucket_kind,bucket_key,window_start)
			VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`, s.tenantID, bucket.kind, bucket.key, bucket.start); err != nil {
			return limits.Token{}, fmt.Errorf("agentpersonastore: create limit bucket: %w", err)
		}
	}
	rows, err := tx.Query(ctx, `SELECT bucket_kind,bucket_key,window_start,admission_count,active_count,used_spend,reserved_spend
		FROM persona_limit_buckets
		WHERE tenant_id=$1 AND ((bucket_kind=$2 AND bucket_key=$3 AND window_start=$4)
		 OR (bucket_kind=$5 AND bucket_key=$6 AND window_start=$7)
		 OR (bucket_kind=$8 AND bucket_key=$9 AND window_start=$10))
		ORDER BY bucket_kind,bucket_key,window_start FOR UPDATE`, s.tenantID,
		"CONVERSATION", starts.conversation.key, starts.conversation.start,
		"INVOKER", starts.invoker.key, starts.invoker.start,
		"PERSONA", starts.persona.key, starts.persona.start)
	if err != nil {
		return limits.Token{}, fmt.Errorf("agentpersonastore: lock limit buckets: %w", err)
	}
	defer rows.Close()
	buckets := make(map[string]limitBucket, 3)
	for rows.Next() {
		var b limitBucket
		if err := rows.Scan(&b.kind, &b.key, &b.start, &b.admissions, &b.active, &b.used, &b.reserved); err != nil {
			return limits.Token{}, fmt.Errorf("agentpersonastore: scan limit bucket: %w", err)
		}
		buckets[b.kind+"|"+b.key] = b
	}
	if err := rows.Err(); err != nil {
		return limits.Token{}, fmt.Errorf("agentpersonastore: read limit buckets: %w", err)
	}
	inv, conv, person := buckets["INVOKER|"+starts.invoker.key], buckets["CONVERSATION|"+starts.conversation.key], buckets["PERSONA|"+starts.persona.key]
	if err := checkLimit(policy, inv, conv, person, req.EstimatedSpendMicros, at); err != nil {
		return limits.Token{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE persona_limit_buckets SET admission_count=admission_count+1,active_count=active_count+1,revision=revision+1
		WHERE tenant_id=$1 AND bucket_kind='INVOKER' AND bucket_key=$2 AND window_start=$3`, s.tenantID, inv.key, inv.start); err != nil {
		return limits.Token{}, fmt.Errorf("agentpersonastore: advance invoker limit: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE persona_limit_buckets SET admission_count=admission_count+1,revision=revision+1
		WHERE tenant_id=$1 AND bucket_kind='CONVERSATION' AND bucket_key=$2 AND window_start=$3`, s.tenantID, conv.key, conv.start); err != nil {
		return limits.Token{}, fmt.Errorf("agentpersonastore: advance conversation limit: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE persona_limit_buckets SET reserved_spend=reserved_spend+$4,revision=revision+1
		WHERE tenant_id=$1 AND bucket_kind='PERSONA' AND bucket_key=$2 AND window_start=$3`, s.tenantID, person.key, person.start, req.EstimatedSpendMicros); err != nil {
		return limits.Token{}, fmt.Errorf("agentpersonastore: reserve persona spend: %w", err)
	}
	id := uuid.NewString()
	if _, err := tx.Exec(ctx, `INSERT INTO persona_limit_reservations
		(tenant_id,reservation_id,invoker_bucket_key,invoker_window_start,conversation_bucket_key,conversation_window_start,
		 persona_bucket_key,persona_window_start,policy_version,estimate,created_at,state)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'OPEN')`, s.tenantID, id,
		inv.key, inv.start, conv.key, conv.start, person.key, person.start, policy.Version, req.EstimatedSpendMicros, at.UTC()); err != nil {
		return limits.Token{}, fmt.Errorf("agentpersonastore: record persona reservation: %w", err)
	}
	if err := commit(ctx, tx); err != nil {
		return limits.Token{}, err
	}
	return limits.Token{ID: id, Key: id, InvokerKey: inv.keyWithStart(), ConversationKey: conv.keyWithStart(), PersonaKey: person.keyWithStart(), PolicyVersion: policy.Version, Estimate: req.EstimatedSpendMicros, At: at.UTC()}, nil
}

// Settle records actual spend and returns unused reserved spend atomically.
func (s *TenantStore) Settle(ctx context.Context, policy limits.Policy, token limits.Token, actual int64) error {
	if actual < 0 {
		return fmt.Errorf("%w: negative actual spend", limits.ErrInvalid)
	}
	return s.closeLimitReservation(ctx, policy, token, actual, "SETTLED")
}

// Release refunds all reserved spend while retaining the admission count.
func (s *TenantStore) Release(ctx context.Context, policy limits.Policy, token limits.Token) error {
	return s.closeLimitReservation(ctx, policy, token, 0, "RELEASED")
}

type limitBucket struct {
	kind, key                          string
	start                              time.Time
	admissions, active, used, reserved int64
}

func (b limitBucket) keyWithStart() string {
	return b.key + "\x00" + b.start.UTC().Format(time.RFC3339Nano)
}

type bucketRef struct {
	kind, key string
	start     time.Time
}
type limitRefs struct{ invoker, conversation, persona bucketRef }

func (r limitRefs) buckets() []bucketRef { return []bucketRef{r.invoker, r.conversation, r.persona} }

func limitStarts(req limits.Request, policy limits.Policy, at time.Time) limitRefs {
	return limitRefs{
		invoker:      bucketRef{"INVOKER", req.TenantID + "|" + req.InvokerID + "|" + req.PersonaID + "|" + fmt.Sprint(req.PersonaVersion), bucketStart(at, policy.InvokerPerPersona.Window)},
		conversation: bucketRef{"CONVERSATION", req.TenantID + "|" + req.ConversationID, bucketStart(at, policy.Conversation.Window)},
		persona:      bucketRef{"PERSONA", req.TenantID + "|" + req.PersonaID, bucketStart(at, 24*time.Hour)},
	}
}
func bucketStart(at time.Time, window time.Duration) time.Time {
	return time.Unix(0, at.UTC().UnixNano()/int64(window)*int64(window)).UTC()
}

func validateLimitInputs(s *TenantStore, p limits.Policy, r limits.Request, at time.Time) error {
	if s == nil || strings.TrimSpace(string(s.tenant)) == "" || !pValid(p) || !rValid(r) || at.IsZero() {
		return fmt.Errorf("%w: invalid policy, request or admission time", limits.ErrInvalid)
	}
	if r.PolicyVersion != p.Version {
		return &limits.Denial{Code: limits.DenialPolicyVersion, Scope: limits.ScopePolicy, PolicyVersion: p.Version}
	}
	return nil
}
func pValid(p limits.Policy) bool {
	return p.Version != "" && p.InvokerPerPersona.Max > 0 && p.InvokerPerPersona.Window > 0 && p.InvokerConcurrency > 0 && p.Conversation.Max > 0 && p.Conversation.Window > 0 && p.PersonaDailySpendMicros > 0
}
func rValid(r limits.Request) bool {
	return r.TenantID != "" && r.InvokerID != "" && r.ConversationID != "" && r.PersonaID != "" && r.PersonaVersion > 0 && r.PolicyVersion != "" && r.EstimatedSpendMicros > 0
}

func checkLimit(p limits.Policy, inv, conv, person limitBucket, estimate int64, at time.Time) error {
	if inv.admissions >= p.InvokerPerPersona.Max {
		return &limits.Denial{Code: limits.DenialInvokerRate, Scope: limits.ScopeInvoker, PolicyVersion: p.Version, Limit: p.InvokerPerPersona.Max, Observed: inv.admissions, RetryAfter: retryAfter(at, p.InvokerPerPersona.Window)}
	}
	if inv.active >= p.InvokerConcurrency {
		return &limits.Denial{Code: limits.DenialInvokerConcurrency, Scope: limits.ScopeInvoker, PolicyVersion: p.Version, Limit: p.InvokerConcurrency, Observed: inv.active, RetryAfter: retryAfter(at, p.InvokerPerPersona.Window)}
	}
	if conv.admissions >= p.Conversation.Max {
		return &limits.Denial{Code: limits.DenialConversationRate, Scope: limits.ScopeConversation, PolicyVersion: p.Version, Limit: p.Conversation.Max, Observed: conv.admissions, RetryAfter: retryAfter(at, p.Conversation.Window)}
	}
	if person.used+person.reserved+estimate > p.PersonaDailySpendMicros {
		return &limits.Denial{Code: limits.DenialPersonaSpend, Scope: limits.ScopePersona, PolicyVersion: p.Version, Limit: p.PersonaDailySpendMicros, Observed: person.used + person.reserved, RetryAfter: retryAfter(at, 24*time.Hour)}
	}
	return nil
}
func retryAfter(at time.Time, window time.Duration) time.Duration {
	return bucketStart(at, window).Add(window).Sub(at.UTC())
}

func (s *TenantStore) closeLimitReservation(ctx context.Context, policy limits.Policy, token limits.Token, actual int64, state string) error {
	if s == nil || token.ID == "" || token.PolicyVersion != policy.Version {
		return limits.ErrPolicyVersion
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var estimate int64
	var invKey, convKey, personKey string
	var invStart, convStart, personStart time.Time
	var current, version string
	err = tx.QueryRow(ctx, `SELECT estimate,invoker_bucket_key,invoker_window_start,conversation_bucket_key,conversation_window_start,persona_bucket_key,persona_window_start,policy_version,state
		FROM persona_limit_reservations WHERE tenant_id=$1 AND reservation_id=$2 FOR UPDATE`, s.tenantID, token.ID).Scan(&estimate, &invKey, &invStart, &convKey, &convStart, &personKey, &personStart, &version, &current)
	if errors.Is(err, dbport.ErrNoRows) || current != "OPEN" {
		return limits.ErrReservationClosed
	}
	if err != nil {
		return fmt.Errorf("agentpersonastore: read persona reservation: %w", err)
	}
	if version != policy.Version || estimate != token.Estimate {
		return limits.ErrPolicyVersion
	}
	if actual > estimate {
		return limits.ErrSpendExceeds
	}
	if _, err := tx.Exec(ctx, `UPDATE persona_limit_buckets SET active_count=active_count-1,revision=revision+1 WHERE tenant_id=$1 AND bucket_kind='INVOKER' AND bucket_key=$2 AND window_start=$3`, s.tenantID, invKey, invStart); err != nil {
		return fmt.Errorf("agentpersonastore: close invoker limit: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE persona_limit_buckets SET reserved_spend=reserved_spend-$4,used_spend=used_spend+$5,revision=revision+1 WHERE tenant_id=$1 AND bucket_kind='PERSONA' AND bucket_key=$2 AND window_start=$3`, s.tenantID, personKey, personStart, estimate, actual); err != nil {
		return fmt.Errorf("agentpersonastore: settle persona spend: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE persona_limit_reservations SET state=$3,actual=$4,closed_at=now(),revision=revision+1 WHERE tenant_id=$1 AND reservation_id=$2 AND state='OPEN'`, s.tenantID, token.ID, state, actual); err != nil {
		return fmt.Errorf("agentpersonastore: close persona reservation: %w", err)
	}
	return commit(ctx, tx)
}
