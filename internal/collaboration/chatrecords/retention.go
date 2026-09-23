package chatrecords

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"
)

// RetentionMode selects exactly one cutoff. The empty policy preserves all records.
type RetentionMode string

const (
	RetentionAge        RetentionMode = "AGE"
	RetentionDate       RetentionMode = "BEFORE_DATE"
	RetentionBudget     RetentionMode = "SIZE_BUDGET"
	maxRetentionAgeDays uint32        = 36525
)

// RetentionPolicy is tenant owned. Kind is empty for a tenant-wide policy.
// A size budget is always tenant-wide; it cannot be divided safely among rooms.
// Persisting this policy does not enable deletion: chat lacks complete copy and
// byte accounting for media and derived records, so no sweeper consumes it yet.
type RetentionPolicy struct {
	TenantID    string
	Kind        string
	Mode        RetentionMode
	AgeDays     uint32
	BeforeDate  time.Time
	BudgetBytes int64
	Revision    uint64
	UpdatedBy   string
	UpdatedAt   time.Time
}

func (p RetentionPolicy) valid() bool {
	if strings.TrimSpace(p.TenantID) == "" || p.Revision == 0 {
		return false
	}
	switch p.Kind {
	case "", "PUBLIC_CHANNEL", "PRIVATE_CHANNEL", "DIRECT", "GROUP":
	default:
		return false
	}
	switch p.Mode {
	case RetentionAge:
		return p.AgeDays > 0 && p.AgeDays <= maxRetentionAgeDays && p.BeforeDate.IsZero() && p.BudgetBytes == 0
	case RetentionDate:
		return p.AgeDays == 0 && !p.BeforeDate.IsZero() && !p.BeforeDate.Equal(time.Unix(0, 0)) && p.BudgetBytes == 0
	case RetentionBudget:
		return p.Kind == "" && p.AgeDays == 0 && p.BeforeDate.IsZero() && p.BudgetBytes > 0
	default:
		return false
	}
}

// RetentionItem represents a complete logical record including every durable
// copy. Complete and BytesKnown must be proven by the inventory reader.
type RetentionItem struct {
	TenantID, RecordID, ConversationID, ConversationKind string
	CreatedAt                                            time.Time
	Bytes                                                int64
	BytesKnown, Complete, HoldsKnown                     bool
	HoldIDs                                              []string
}

type RetentionCandidate struct {
	RecordID string
	Reason   string
}

type RetentionPreview struct {
	TenantID       string
	PolicyRevision uint64
	Candidates     []RetentionCandidate
	Blocked        []RetentionCandidate
	KnownBytes     int64
	UnknownItems   int
}

// EvaluateRetention is a dry run. Unknown copies, unknown byte sizes, and
// unresolved holds block selection. It never changes records or policy.
func EvaluateRetention(p RetentionPolicy, items []RetentionItem, activeHolds map[string]bool, now time.Time) (RetentionPreview, error) {
	if !p.valid() || now.IsZero() {
		return RetentionPreview{}, ErrInvalid
	}
	now = now.UTC()
	out := RetentionPreview{TenantID: p.TenantID, PolicyRevision: p.Revision}
	ordered := append([]RetentionItem(nil), items...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].CreatedAt.Equal(ordered[j].CreatedAt) {
			return ordered[i].RecordID < ordered[j].RecordID
		}
		return ordered[i].CreatedAt.Before(ordered[j].CreatedAt)
	})
	for _, item := range ordered {
		if item.TenantID != p.TenantID || item.RecordID == "" || item.CreatedAt.IsZero() || item.CreatedAt.After(now) || item.Bytes < 0 {
			return RetentionPreview{}, ErrInvalid
		}
		if item.BytesKnown {
			if item.Bytes > math.MaxInt64-out.KnownBytes {
				return RetentionPreview{}, ErrInvalid
			}
			out.KnownBytes += item.Bytes
		}
		if !item.BytesKnown || !item.Complete || !item.HoldsKnown {
			out.UnknownItems++
		}
	}
	remaining := out.KnownBytes
	for _, item := range ordered {
		if p.Kind != "" && p.Kind != item.ConversationKind {
			continue
		}
		due := false
		switch p.Mode {
		case RetentionAge:
			due = !item.CreatedAt.After(now.AddDate(0, 0, -int(p.AgeDays)))
		case RetentionDate:
			due = item.CreatedAt.Before(p.BeforeDate)
		case RetentionBudget:
			due = remaining > p.BudgetBytes || out.UnknownItems > 0
		}
		if !due {
			continue
		}
		reason := ""
		if p.Mode == RetentionBudget && out.UnknownItems > 0 {
			reason = "UNKNOWN_TENANT_USAGE"
		} else if !item.Complete || !item.BytesKnown || !item.HoldsKnown {
			reason = "INCOMPLETE_COPY_INVENTORY"
		} else if len(item.HoldIDs) > 0 {
			for _, id := range item.HoldIDs {
				active, known := activeHolds[id]
				if !known || active || id == "" {
					reason = "LEGAL_HOLD"
					break
				}
			}
		}
		if reason != "" {
			out.Blocked = append(out.Blocked, RetentionCandidate{RecordID: item.RecordID, Reason: reason})
			continue
		}
		out.Candidates = append(out.Candidates, RetentionCandidate{RecordID: item.RecordID, Reason: string(p.Mode)})
		remaining -= item.Bytes
	}
	return out, nil
}

// RetentionRepository is optional so existing records repositories remain
// source compatible. Durable implementations own revision checked writes.
type RetentionRepository interface {
	GetRetentionPolicy(context.Context, string, string) (RetentionPolicy, bool, error)
	PutRetentionPolicy(context.Context, RetentionPolicy, uint64) (RetentionPolicy, error)
}

func (s *Service) GetRetentionPolicy(ctx context.Context, actor, tenant, kind string) (RetentionPolicy, bool, error) {
	if _, err := s.authorize(ctx, actor, tenant, "retention.read", kind); err != nil {
		return RetentionPolicy{}, false, err
	}
	repo, ok := s.Repo.(RetentionRepository)
	if !ok {
		return RetentionPolicy{}, false, ErrInvalid
	}
	return repo.GetRetentionPolicy(ctx, tenant, kind)
}

func (s *Service) PutRetentionPolicy(ctx context.Context, actor string, policy RetentionPolicy, expected uint64) (RetentionPolicy, error) {
	if _, err := s.authorize(ctx, actor, policy.TenantID, "retention.configure", policy.Kind); err != nil {
		return RetentionPolicy{}, err
	}
	policy.Revision = expected + 1
	policy.UpdatedBy = actor
	policy.UpdatedAt = s.now()
	if !policy.valid() || (policy.Mode == RetentionDate && policy.BeforeDate.After(policy.UpdatedAt)) {
		return RetentionPolicy{}, ErrInvalid
	}
	repo, ok := s.Repo.(RetentionRepository)
	if !ok {
		return RetentionPolicy{}, ErrInvalid
	}
	return repo.PutRetentionPolicy(ctx, policy, expected)
}
