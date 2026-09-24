package journeyclient

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestTodo_REV_063_01_JourneyWriteRecovery(t *testing.T) {
	copy := productui.ResolveProductLocale("de-DE")
	notice := noticeFromError(status.Error(codes.Unauthenticated, "credential detail must stay private"), copy)
	if notice == nil || notice.RecoveryHref != "/workspace/login" || notice.RecoveryLabel != copy.Text("signed_out.signin") {
		t.Fatalf("unauthenticated write notice has no shared sign-in recovery: %+v", notice)
	}
	if notice.Title != copy.Text("signed_out.title") || notice.Detail != "" {
		t.Fatalf("unauthenticated notice does not use safe signed-out copy: %+v", notice)
	}
	markup, err := journey.RenderToString(journey.Page{Locale: "de-DE", Notice: notice})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `href="/workspace/login"`) || !strings.Contains(markup, notice.RecoveryLabel) {
		t.Fatalf("journey write notice omitted its localized sign-in link")
	}
	if strings.Contains(markup, "credential detail must stay private") || strings.Contains(markup, "Versuchen Sie es erneut") {
		t.Fatalf("journey write recovery leaked a transport detail or dead-end retry")
	}
}

func TestTodo_REV_063_01_Security(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		copy := productui.ResolveProductLocale(locale)
		notice := noticeFromError(status.Error(codes.Unauthenticated, "private credential"), copy)
		if notice == nil || notice.RecoveryHref != productui.UnauthenticatedRecovery().SignInHref || notice.RecoveryLabel != copy.Text("signed_out.signin") {
			t.Fatalf("%s recovery mapping = %+v", locale, notice)
		}
		if notice.Detail != "" || strings.Contains(notice.Title, "private credential") {
			t.Fatalf("%s recovery disclosed transport details: %+v", locale, notice)
		}
	}
	if notice := noticeFromError(status.Error(codes.PermissionDenied, "private detail"), productui.ResolveProductLocale("en-US")); notice == nil || notice.RecoveryHref != "" {
		t.Fatalf("permission denial was mapped to sign-in recovery: %+v", notice)
	}
	joined := errors.Join(
		status.Error(codes.Unavailable, "earlier outage"),
		fmt.Errorf("worker refresh: %w", status.Error(codes.Unauthenticated, "expired secret")),
	)
	if status.Code(joined) != codes.Unavailable {
		t.Fatalf("fixture's first status = %s, want UNAVAILABLE", status.Code(joined))
	}
	joinedNotice := noticeFromError(joined, productui.ResolveProductLocale("en-US"))
	if joinedNotice == nil || joinedNotice.RecoveryHref != "/workspace/login" {
		t.Fatalf("notice mapper missed nested UNAUTHENTICATED after an earlier status: %+v", joinedNotice)
	}
}

func TestErrorHasCodeTraversesJoinedAndWrappedErrors(t *testing.T) {
	err := errors.Join(
		status.Error(codes.Unavailable, "first"),
		fmt.Errorf("nested: %w", errors.Join(status.Error(codes.PermissionDenied, "middle"), status.Error(codes.Unauthenticated, "last"))),
	)
	if !ErrorHasCode(err, codes.Unauthenticated) {
		t.Fatal("status classifier missed a later nested UNAUTHENTICATED status")
	}
	if ErrorHasCode(err, codes.NotFound) {
		t.Fatal("status classifier invented a NOT_FOUND status")
	}
}
