package scheduled

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
)

var (
	ErrInvalidFiring   = errors.New("scheduled agent dispatch: invalid firing")
	ErrFiringRefused   = errors.New("scheduled agent dispatch: current schedule policy refused firing")
	ErrReceiptConflict = errors.New("scheduled agent dispatch: receipt key has a different result")
	ErrWorkRefused     = errors.New("scheduled agent dispatch: current authority refused work")
)

// Firing binds one published schedule revision and resolved occurrence to the
// exact immutable agent version selected by the schedule target.
type Firing struct {
	Occurrence schedule.Occurrence
	Target     schedule.AgentRunTarget
}

// SourceIdentity derives the canonical schedule source key. The digest binds
// tenant, schedule ID and revision, occurrence key, and the complete agent
// version reference, including its manifest digest.
func (f Firing) SourceIdentity() (agentrun.SourceIdentity, error) {
	ref := f.Occurrence.Trigger
	if !required(ref.TenantID) || !required(ref.ID) || !required(ref.Version) ||
		!required(f.Occurrence.Key) ||
		(f.Occurrence.Source != schedule.SourceCron && f.Occurrence.Source != schedule.SourceCalendar) ||
		!required(f.Target.Agent.ID) || !required(f.Target.Agent.Version) || !validAgentDigest(f.Target.Agent.Digest) ||
		!required(f.Target.SponsorID) || !required(f.Target.Purpose) ||
		f.Target.Budget.MaxCostMicros == 0 || f.Target.Budget.MaxInputTokens == 0 || f.Target.Budget.MaxOutputTokens == 0 ||
		!required(f.Target.Destination.AudienceID) || !required(f.Target.Destination.AudienceSnapshotID) || !validAgentDigest(f.Target.Destination.AudienceDigest) {
		return agentrun.SourceIdentity{}, ErrInvalidFiring
	}
	h := sha256.New()
	for _, value := range []string{ref.TenantID, ref.ID, ref.Version, f.Occurrence.Key, f.Target.Agent.ID, f.Target.Agent.Version, f.Target.Agent.Digest} {
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(len(value)))
		_, _ = h.Write(size[:])
		_, _ = h.Write([]byte(value))
	}
	return agentrun.SourceIdentity{
		TenantID: ref.TenantID,
		Kind:     agentrun.SourceSchedule,
		Key:      "schedule:v1:" + hex.EncodeToString(h.Sum(nil)),
		Ref:      ref.String(),
	}, nil
}

// ScheduleState rechecks the source owner's current publication, pause state,
// and sponsor/grant eligibility before a new inbox admission.
type ScheduleState interface {
	CheckCurrent(context.Context, Firing) error
}

// Inbox is implemented by the shared AGENT-015 admission service. Its store
// must enforce durable source uniqueness across agent-worker restarts.
type Inbox interface {
	Admit(context.Context, agentrun.Request) (agentrun.Record, bool, error)
}

// Receipt is the replayable acknowledgement retained by the scheduling owner.
type Receipt struct {
	SourceKey     string
	RunRequestID  string
	RequestDigest string
	Decision      agentrun.Decision
	RefusalCode   string
}

// ReceiptStore is the schedule owner's durable receipt port. CreateOrGet must
// reject the same key carrying a different request or admission outcome.
type ReceiptStore interface {
	Get(context.Context, string) (Receipt, bool, error)
	CreateOrGet(context.Context, Receipt) (Receipt, bool, error)
}

// WorkFence rechecks current schedule status and run authority before a
// worker claims a run or starts a tool. The implementation resolves the
// durable run by receipt ID and must fail closed on missing or revoked state.
type WorkFence interface {
	CheckBeforeWork(context.Context, Receipt) error
}

// Outcome reports one admission and whether this dispatch replayed an
// existing inbox decision or receipt.
type Outcome struct {
	Receipt   Receipt
	Duplicate bool
}

// Dispatcher coordinates the schedule check, run inbox, and schedule receipt.
// It uses at-least-once delivery; inbox source-key uniqueness resolves the
// crash window between admission and receipt persistence.
type Dispatcher struct {
	schedule ScheduleState
	inbox    Inbox
	receipts ReceiptStore
	work     WorkFence
}

// NewDispatcher requires all owner ports and fails closed when any is
// unavailable.
func NewDispatcher(scheduleState ScheduleState, inbox Inbox, receipts ReceiptStore, work WorkFence) (*Dispatcher, error) {
	if scheduleState == nil || inbox == nil || receipts == nil || work == nil {
		return nil, fmt.Errorf("%w: all owner ports are required", ErrInvalidFiring)
	}
	return &Dispatcher{schedule: scheduleState, inbox: inbox, receipts: receipts, work: work}, nil
}

// Dispatch admits one due occurrence and records its replayable receipt. The
// request's tenant and pinned agent must agree with the occurrence target;
// all authority and request fields remain subject to AGENT-015 verification.
func (d *Dispatcher) Dispatch(ctx context.Context, firing Firing, request agentrun.Request) (Outcome, error) {
	if d == nil {
		return Outcome{}, ErrInvalidFiring
	}
	source, err := firing.SourceIdentity()
	if err != nil {
		return Outcome{}, err
	}
	if request.Agent != targetAgentRef(firing.Target) ||
		(request.Source.TenantID != "" && request.Source.TenantID != source.TenantID) ||
		request.Principal.Mode != agentrun.ModeSponsored || request.Principal.SponsorID != firing.Target.SponsorID ||
		request.Principal.InvokerID != "" || request.Principal.DelegatedCredentialRef != "" ||
		request.Purpose != firing.Target.Purpose || request.Budget != targetBudget(firing.Target.Budget) ||
		request.Audience != targetAudience(firing.Target.Destination) {
		return Outcome{}, ErrInvalidFiring
	}
	request.Source = source
	if prior, found, err := d.receipts.Get(ctx, source.Key); err != nil {
		return Outcome{}, err
	} else if found {
		if !receiptMatches(prior, source.Key) {
			return Outcome{}, ErrReceiptConflict
		}
		return Outcome{Receipt: prior, Duplicate: true}, nil
	}
	if err := d.schedule.CheckCurrent(ctx, firing); err != nil {
		return Outcome{}, fmt.Errorf("%w: %v", ErrFiringRefused, err)
	}
	record, created, err := d.inbox.Admit(ctx, request)
	if err != nil {
		return Outcome{}, err
	}
	if err := agentrun.ValidateAdmissionRecord(record); err != nil || record.Request.Source != source || record.Request.Agent != targetAgentRef(firing.Target) {
		return Outcome{}, fmt.Errorf("%w: inbox returned a mismatched admission record", ErrInvalidFiring)
	}
	receipt := Receipt{SourceKey: source.Key, RunRequestID: record.ID, RequestDigest: record.RequestDigest,
		Decision: record.Decision, RefusalCode: record.RefusalCode}
	stored, inserted, err := d.receipts.CreateOrGet(ctx, receipt)
	if err != nil {
		return Outcome{}, err
	}
	if !receiptMatches(stored, source.Key) || stored != receipt {
		return Outcome{}, ErrReceiptConflict
	}
	return Outcome{Receipt: stored, Duplicate: !created || !inserted}, nil
}

// CheckBeforeWork is the mandatory worker-side hook for a scheduled run. The
// run worker must call it before claiming execution and again before each
// admitted effect so a pause or revoked grant after enqueue fences the run.
func (d *Dispatcher) CheckBeforeWork(ctx context.Context, receipt Receipt) error {
	if d == nil || d.work == nil || !required(receipt.SourceKey) || !receiptMatches(receipt, receipt.SourceKey) || receipt.Decision != agentrun.DecisionAccepted || receipt.RefusalCode != "" {
		return ErrWorkRefused
	}
	stored, found, err := d.receipts.Get(ctx, receipt.SourceKey)
	if err != nil {
		return fmt.Errorf("%w: load dispatch receipt: %v", ErrWorkRefused, err)
	}
	if !found || stored != receipt {
		return ErrWorkRefused
	}
	if err := d.work.CheckBeforeWork(ctx, receipt); err != nil {
		return fmt.Errorf("%w: %v", ErrWorkRefused, err)
	}
	return nil
}

func receiptMatches(r Receipt, key string) bool {
	return r.SourceKey == key && required(r.RunRequestID) && required(r.RequestDigest)
}

func required(value string) bool { return value != "" && strings.TrimSpace(value) == value }

func validAgentDigest(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, char := range value[len("sha256:"):] {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func targetAgentRef(target schedule.AgentRunTarget) agentrun.VersionRef {
	return agentrun.VersionRef{AgentID: target.Agent.ID, Version: target.Agent.Version, Digest: target.Agent.Digest}
}

func targetBudget(budget schedule.AgentRunBudget) agentrun.Budget {
	return agentrun.Budget{MaxCostMicros: budget.MaxCostMicros, MaxInputTokens: budget.MaxInputTokens, MaxOutputTokens: budget.MaxOutputTokens}
}

func targetAudience(destination schedule.AgentRunDestination) agentrun.AudienceScope {
	return agentrun.AudienceScope{ID: destination.AudienceID, SnapshotID: destination.AudienceSnapshotID, Digest: destination.AudienceDigest}
}
