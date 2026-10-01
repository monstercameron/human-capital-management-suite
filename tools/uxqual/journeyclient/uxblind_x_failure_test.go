package journeyclient

import (
	"strings"
	"testing"
	"time"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestTodo_UXBLIND_076(t *testing.T) {
	copy := productui.ResolveProductLocale("de-DE")
	domainErr := status.Error(codes.Unavailable, "approval route unavailable")
	if got := approvalStartNotice(domainErr, copy); got == nil || got.TitleKey != "journey.error_domain_unavailable_title" {
		t.Fatalf("domain notice = %+v, want domain-specific copy", got)
	}
	storageStatus, err := status.New(codes.Internal, "storage failed").WithDetails(&commonv1.ErrorDetail{ReasonRef: approvalStartStorageReason})
	if err != nil {
		t.Fatal(err)
	}
	if got := approvalStartNotice(storageStatus.Err(), copy); got == nil || got.TitleKey != "journey.error_storage_failed_title" {
		t.Fatalf("storage notice = %+v, want storage-specific copy", got)
	}
	stage := approvalStartNotice(status.Error(codes.FailedPrecondition, "not startable"), copy)
	if stage == nil || stage.TitleKey != "journey.error_precondition_title" {
		t.Fatalf("stage notice = %+v, want refresh advice", stage)
	}

	at := time.Date(2026, 9, 28, 21, 53, 0, 0, time.UTC)
	event := failedApprovalStartEvent("de-DE", "Avery", at, domainErr)
	if event.Tone != toneDanger || event.Actor != "Avery" || event.At != copy.FormatTimestamp(at) {
		t.Fatalf("failed event = %+v, want localized time, actor and danger tone", event)
	}
	if !strings.Contains(event.Detail, "Genehmigungsdienst") {
		t.Fatalf("failed event detail = %q, want domain reason", event.Detail)
	}

	client := New(Config{Locale: "de-DE", Subject: "Avery"}, nil, nil, func() time.Time { return at })
	client.recordFailedApprovalStart("intent-076", domainErr)
	client.mu.Lock()
	got := append([]journey.TimelineEvent(nil), client.failedApprovalStarts["intent-076"]...)
	client.mu.Unlock()
	if len(got) != 1 || got[0].Title == "" || !strings.Contains(got[0].Detail, "Genehmigungsdienst") {
		t.Fatalf("recorded history = %+v, want one failed attempt", got)
	}
}

func TestTodo_UXBLIND_076_Browser(t *testing.T) {
	for _, locale := range productui.SupportedProductLocales() {
		t.Run(locale, func(t *testing.T) {
			event := failedApprovalStartEvent(locale, "", time.Date(2026, 9, 28, 21, 53, 0, 0, time.UTC), status.Error(codes.FailedPrecondition, "stale stage"))
			copy := productui.ResolveProductLocale(locale)
			if event.Actor != copy.Text("journey.timeline_system") || event.Title != copy.Text("journey.timeline_start_failed") {
				t.Fatalf("history event = %+v, want localized system failed-start entry", event)
			}
		})
	}
}
