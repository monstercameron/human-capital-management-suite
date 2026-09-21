package version

import (
	"errors"
	"testing"
)

func TestErrors_Smoke(t *testing.T) {
	if ErrVersion == nil {
		t.Fatalf("ErrVersion is nil")
	}
	if CodeMissingPublicationTime == "" {
		t.Fatalf("CodeMissingPublicationTime is empty")
	}
	if CodeMissingSemanticVersion == "" {
		t.Fatalf("CodeMissingSemanticVersion is empty")
	}
	if CodeInvalidSemanticVersion == "" || CodeSemanticVersionConflict == "" || CodeVersionNotNewer == "" {
		t.Fatalf("semantic version refusal codes are empty")
	}
	if CodeCompilationRejected == "" {
		t.Fatalf("CodeCompilationRejected is empty")
	}
	if CodePlanDigestMismatch == "" {
		t.Fatalf("CodePlanDigestMismatch is empty")
	}
	if CodeInvalidPin == "" {
		t.Fatalf("CodeInvalidPin is empty")
	}
	if CodeUnknownVersion == "" {
		t.Fatalf("CodeUnknownVersion is empty")
	}
	if CodeAmbiguousPin == "" {
		t.Fatalf("CodeAmbiguousPin is empty")
	}
	if CodeUnauthorizedActivation == "" {
		t.Fatalf("CodeUnauthorizedActivation is empty")
	}
	if CodeChangedAfterReview == "" {
		t.Fatalf("CodeChangedAfterReview is empty")
	}
	if CodeFailedTest == "" {
		t.Fatalf("CodeFailedTest is empty")
	}
	if CodeUnresolvedDependency == "" {
		t.Fatalf("CodeUnresolvedDependency is empty")
	}
	if CodeRetiredCannotActivate == "" {
		t.Fatalf("CodeRetiredCannotActivate is empty")
	}
	if CodeAnotherVersionActive == "" {
		t.Fatalf("CodeAnotherVersionActive is empty")
	}
	if CodeNotActive == "" {
		t.Fatalf("CodeNotActive is empty")
	}
	if CodeAlreadyRetired == "" {
		t.Fatalf("CodeAlreadyRetired is empty")
	}
	if CodeUnknownRecord == "" {
		t.Fatalf("CodeUnknownRecord is empty")
	}
	if CodeRecordMutated == "" {
		t.Fatalf("CodeRecordMutated is empty")
	}
	if CodeInvalidRecord == "" {
		t.Fatalf("CodeInvalidRecord is empty")
	}
}

func TestErrors_ErrorsIs(t *testing.T) {
	if !errors.Is(ErrVersion, ErrVersion) {
		t.Fatalf("errors.Is failed for ErrVersion")
	}
	if ErrVersion.Error() == "" {
		t.Fatalf("ErrVersion Error empty")
	}
}
