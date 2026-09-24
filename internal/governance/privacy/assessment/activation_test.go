package assessment

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/privacy/inventory"
)

func rev005Executable(t *testing.T, activityVersion string) inventory.Executable {
	t.Helper()
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a := inventory.ProcessingActivity{
		ID: "automated-risk-scoring", Version: activityVersion, Status: inventory.StatusApproved,
		Controller: "acme", Processor: "hcm", Purpose: "automated workforce risk scoring",
		DataSubjects: []string{"employees"}, DataCategories: []string{"risk-signals"},
		Systems: []string{"risk-engine", "risk-store"}, Recipients: []string{"risk-store"},
		Regions: []string{"EU"}, LawfulBasis: "legitimate-interest", Retention: "90-days",
		SecurityControls: []string{"encryption"}, DPIARef: "privacy-risk-assessment/automated-risk-scoring/2",
		Obligations: []inventory.ObligationDeadline{
			{Kind: inventory.MonitoringConsent, Jurisdiction: "EU", RulePackRelease: "eu-2026.1", DeadlineHours: 24, EffectiveFrom: from},
			{Kind: inventory.BreachNotification, Jurisdiction: "EU", RulePackRelease: "eu-2026.1", DeadlineHours: 72, EffectiveFrom: from},
		},
	}
	f := inventory.ProcessingDataFlow{
		ID: "risk-engine-to-store", ActivityID: a.ID, Version: a.Version,
		SourceSystem: "risk-engine", DestinationSystem: "risk-store", Recipient: "risk-store",
		SourceRegion: "EU", TransferPolicyVersion: "privacy-transfer-rules-2026.09.1",
		Controller: a.Controller, Processor: a.Processor, Purpose: a.Purpose,
		DataCategories: []string{"risk-signals"}, Operations: []string{"store"},
		TransferRegions: []string{"EU"}, ContractRefs: []string{"internal-processing"},
		Safeguards: []string{"eu.adequacy.decision.v1"}, SecurityControls: []string{"encryption"},
		RetentionRef: a.Retention, EffectiveFrom: from,
	}
	release, err := inventory.ValidateExecutable(inventory.Inventory{Activities: []inventory.ProcessingActivity{a}, Flows: []inventory.ProcessingDataFlow{f}})
	if err != nil {
		t.Fatalf("validate PRIV-001 release: %v", err)
	}
	return release
}

func TestTodo_REV_005_02_ActivationIntegration(t *testing.T) {
	assessment, reviewers := rev005Assessment(t)
	release := rev005Executable(t, "2026-09")
	_, privateKey := rev005Keys()
	assessment.InventoryDigest = release.Digest
	if err := SignAssessment(assessment, privateKey); err != nil {
		t.Fatalf("sign inventory-bound assessment: %v", err)
	}
	for _, mode := range []ProcessingMode{ProcessingAutomatedDecision, ProcessingRestrictedField, ProcessingHighRisk} {
		if err := AuthorizeActivityActivation(ActivationRequest{
			TenantID: "tenant-acme", ActivityID: "automated-risk-scoring", ActivityVersion: "2026-09",
			Mode: mode, Inventory: release, Assessment: assessment, Now: rev005Now, TrustedReviewers: reviewers,
		}); err != nil {
			t.Errorf("mode %v was blocked: %v", mode, err)
		}
	}
	missing := ActivationRequest{TenantID: "tenant-acme", ActivityID: "automated-risk-scoring", ActivityVersion: "2026-09", Mode: ProcessingAutomatedDecision, Inventory: release, Now: rev005Now, TrustedReviewers: reviewers}
	if err := AuthorizeActivityActivation(missing); !errors.Is(err, ErrAssessmentMissing) {
		t.Errorf("missing assessment error = %v, want ErrAssessmentMissing", err)
	}
	wrongTenant := ActivationRequest{TenantID: "tenant-other", ActivityID: "automated-risk-scoring", ActivityVersion: "2026-09", Mode: ProcessingAutomatedDecision, Inventory: release, Assessment: assessment, Now: rev005Now, TrustedReviewers: reviewers}
	if err := AuthorizeActivityActivation(wrongTenant); !errors.Is(err, ErrAssessmentMismatch) {
		t.Errorf("cross-tenant assessment error = %v, want ErrAssessmentMismatch", err)
	}
	staleActivity := ActivationRequest{TenantID: "tenant-acme", ActivityID: "automated-risk-scoring", ActivityVersion: "2026-10", Mode: ProcessingRestrictedField, Inventory: release, Assessment: assessment, Now: rev005Now, TrustedReviewers: reviewers}
	if err := AuthorizeActivityActivation(staleActivity); !errors.Is(err, ErrActivityUnavailable) {
		t.Errorf("unpublished activity version error = %v, want ErrActivityUnavailable", err)
	}
	wrongVersionAssessment := *assessment
	wrongVersionAssessment.ActivityVersion = "2026-08"
	if err := SignAssessment(&wrongVersionAssessment, privateKey); err != nil {
		t.Fatalf("sign old activity assessment: %v", err)
	}
	wrongVersion := ActivationRequest{TenantID: "tenant-acme", ActivityID: "automated-risk-scoring", ActivityVersion: "2026-09", Mode: ProcessingAutomatedDecision, Inventory: release, Assessment: &wrongVersionAssessment, Now: rev005Now, TrustedReviewers: reviewers}
	if err := AuthorizeActivityActivation(wrongVersion); !errors.Is(err, ErrAssessmentMismatch) {
		t.Errorf("old activity-version assessment error = %v, want ErrAssessmentMismatch", err)
	}
	corrupt := release
	corrupt.Inventory.Activities[0].Purpose = "unreviewed purpose"
	request := ActivationRequest{TenantID: "tenant-acme", ActivityID: "automated-risk-scoring", ActivityVersion: "2026-09", Mode: ProcessingAutomatedDecision, Inventory: corrupt, Assessment: assessment, Now: rev005Now, TrustedReviewers: reviewers}
	if err := AuthorizeActivityActivation(request); !errors.Is(err, ErrInventoryMismatch) {
		t.Errorf("tampered inventory error = %v, want ErrInventoryMismatch", err)
	}
}
