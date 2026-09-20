package journeyclient

import (
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// noActiveVersionRefusal is the refusal the intent service returns when no
// published version of the workflow has been approved and activated: a
// FAILED_PRECONDITION carrying the owned reason and the release rule.
func noActiveVersionRefusal(t *testing.T) error {
	t.Helper()
	st, err := status.New(codes.FailedPrecondition, "no published workflow version is active for this tenant").
		WithDetails(&commonv1.ErrorDetail{
			Code:      commonv1.ErrorCode_ERROR_CODE_FAILED_PRECONDITION,
			Retryable: false,
			ReasonRef: ReasonNoActiveWorkflowVersion,
			FieldViolations: []*commonv1.FieldViolation{{
				FieldPath:   "workflow_version",
				Description: "no published version of this workflow has been approved and activated",
				RuleRef:     "release.workflow_version_activation",
			}},
		})
	if err != nil {
		t.Fatalf("building the status: %v", err)
	}
	return st.Err()
}

// TestNoActiveWorkflowVersionRefusalIsActionableNotRetryable pins the copy a
// reader gets when the workflow has no active version. The old behaviour --
// "Service temporarily unavailable / It is safe to retry without changing the
// form" -- invited a retry that can never succeed: only an operator releasing
// a version resolves this. The notice must say what is wrong and who fixes
// it, in every supported locale, and must not tell anyone to retry.
func TestNoActiveWorkflowVersionRefusalIsActionableNotRetryable(t *testing.T) {
	refusal := noActiveVersionRefusal(t)

	notice := NoticeFromError(refusal)
	if notice == nil {
		t.Fatal("the refusal produced no notice")
	}
	if notice.TitleKey != "journey.error_no_active_workflow_version_title" ||
		notice.MessageKey != "journey.error_no_active_workflow_version_detail" {
		t.Fatalf("notice keys = %q/%q, want the no-active-version catalog entry", notice.TitleKey, notice.MessageKey)
	}
	if notice.Title != "No workflow version is active" {
		t.Fatalf("title = %q", notice.Title)
	}
	if !strings.Contains(notice.Detail, "administrator") {
		t.Fatalf("detail = %q; it must name who can fix this", notice.Detail)
	}
	for _, forbidden := range []string{"safe to retry", "temporarily unavailable", "try again"} {
		if strings.Contains(strings.ToLower(notice.Detail), forbidden) ||
			strings.Contains(strings.ToLower(notice.Title), forbidden) {
			t.Fatalf("notice %q / %q still invites a retry that cannot succeed", notice.Title, notice.Detail)
		}
	}
	// The owned reason is selection input, never rendered.
	if strings.Contains(notice.Detail, ReasonNoActiveWorkflowVersion) || strings.Contains(notice.Title, ReasonNoActiveWorkflowVersion) {
		t.Fatalf("notice rendered the owned reason reference: %q / %q", notice.Title, notice.Detail)
	}

	// Every supported locale answers with its own copy, not a fallback.
	for _, language := range productui.SupportedProductLocales() {
		locale := productui.ResolveProductLocale(language)
		for _, key := range []string{
			"journey.error_no_active_workflow_version_title",
			"journey.error_no_active_workflow_version_detail",
		} {
			result, err := locale.Resolve(key)
			if err != nil || result.Locale != language || result.FallbackPath != language || strings.TrimSpace(result.Text) == "" {
				t.Fatalf("%s %s = %+v, %v; want translated copy", language, key, result, err)
			}
		}
		localized := noticeFromError(refusal, locale)
		if localized.Title != locale.Text("journey.error_no_active_workflow_version_title") {
			t.Fatalf("%s title = %q", language, localized.Title)
		}
	}
}

// TestGenericUnavailableKeepsTheRetryCopy proves the honest refusal above did
// not swallow the transient one: a genuine UNAVAILABLE, and a
// FAILED_PRECONDITION that names no owned reason, both keep their own copy.
func TestGenericUnavailableKeepsTheRetryCopy(t *testing.T) {
	transient := NoticeFromError(status.Error(codes.Unavailable, "message"))
	if transient.Title != "Service temporarily unavailable" ||
		!strings.Contains(transient.Detail, "safe to retry") {
		t.Fatalf("transient notice = %q / %q", transient.Title, transient.Detail)
	}
	stage := NoticeFromError(status.Error(codes.FailedPrecondition, "message"))
	if stage.Title != "Not available at this stage" {
		t.Fatalf("precondition notice title = %q", stage.Title)
	}
	// An owned reason nobody reviewed copy for is not copy.
	st, err := status.New(codes.Unavailable, "message").
		WithDetails(&commonv1.ErrorDetail{ReasonRef: "workflow.some_future_reason"})
	if err != nil {
		t.Fatal(err)
	}
	unknown := NoticeFromError(st.Err())
	if unknown.Title != "Service temporarily unavailable" {
		t.Fatalf("unknown reason notice title = %q, want the code's entry", unknown.Title)
	}
	if key, ok := reasonCopyKey(""); ok || key != "" {
		t.Fatalf("an empty reason resolved copy key %q", key)
	}
	if refusalReasonRef(nil) != "" || refusalReasonRef(status.Error(codes.Unavailable, "no details")) != "" {
		t.Fatal("a refusal with no detail produced a reason reference")
	}
	if got := refusalReasonRef(noActiveVersionRefusal(t)); got != ReasonNoActiveWorkflowVersion {
		t.Fatalf("refusalReasonRef = %q", got)
	}
}
