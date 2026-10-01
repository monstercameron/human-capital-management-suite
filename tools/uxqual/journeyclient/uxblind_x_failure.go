package journeyclient

import (
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	approvalStartDomainReason  = "intent.domain_unavailable"
	approvalStartStorageReason = "STORAGE_FAILED"
)

type approvalStartFailureKind string

const (
	approvalStartFailureDomain  approvalStartFailureKind = "domain_unavailable"
	approvalStartFailureStorage approvalStartFailureKind = "storage_failed"
	approvalStartFailureStage   approvalStartFailureKind = "stage_precondition"
)

func approvalStartFailureKindOf(err error) approvalStartFailureKind {
	reason := strings.TrimSpace(refusalReasonRef(err))
	switch {
	case strings.EqualFold(reason, approvalStartDomainReason), status.Code(err) == codes.Unavailable:
		return approvalStartFailureDomain
	case strings.EqualFold(reason, approvalStartStorageReason):
		return approvalStartFailureStorage
	case status.Code(err) == codes.FailedPrecondition:
		return approvalStartFailureStage
	default:
		return approvalStartFailureStage
	}
}

func approvalStartNotice(err error, copy productui.LocaleContext) *journey.Notice {
	var key string
	switch approvalStartFailureKindOf(err) {
	case approvalStartFailureDomain:
		key = "journey.error_domain_unavailable"
	case approvalStartFailureStorage:
		key = "journey.error_storage_failed"
	default:
		// Stage preconditions retain the existing refresh advice, which is
		// the safe recovery for a stale journey projection.
		return noticeFromError(err, copy)
	}
	return &journey.Notice{
		Tone:             toneDanger,
		Title:            copy.Text(key + "_title"),
		Detail:           copy.Text(key + "_detail"),
		TitleKey:         key + "_title",
		MessageKey:       key + "_detail",
		SupportReference: supportReference(err),
	}
}

func failedApprovalStartEvent(locale, actor string, at time.Time, err error) journey.TimelineEvent {
	copy := productui.ResolveProductLocale(locale)
	detailKey := "journey.timeline_start_failed_stage"
	switch approvalStartFailureKindOf(err) {
	case approvalStartFailureDomain:
		detailKey = "journey.timeline_start_failed_domain"
	case approvalStartFailureStorage:
		detailKey = "journey.timeline_start_failed_storage"
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = copy.Text("journey.timeline_system")
	}
	return journey.TimelineEvent{
		At:     copy.FormatTimestamp(at),
		Actor:  actor,
		Title:  copy.Text("journey.timeline_start_failed"),
		Detail: copy.Text(detailKey),
		Tone:   toneDanger,
	}
}

func (a *App) recordFailedApprovalStart(intentID string, err error) {
	if a == nil || strings.TrimSpace(intentID) == "" || err == nil {
		return
	}
	a.mu.Lock()
	if a.failedApprovalStarts == nil {
		a.failedApprovalStarts = make(map[string][]journey.TimelineEvent)
	}
	event := failedApprovalStartEvent(a.cfg.Locale, a.cfg.Subject, a.now(), err)
	// The renderer consumes newest-first timeline entries, matching the
	// server projection's order.
	a.failedApprovalStarts[intentID] = append([]journey.TimelineEvent{event}, a.failedApprovalStarts[intentID]...)
	a.mu.Unlock()
}
