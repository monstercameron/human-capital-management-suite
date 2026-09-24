package assessment

import (
	"crypto/ed25519"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/privacy/inventory"
)

// ProcessingMode identifies processing that requires a current risk
// assessment before a tenant may activate it.
type ProcessingMode uint8

const (
	ProcessingRoutine ProcessingMode = iota + 1
	ProcessingHighRisk
	ProcessingAutomatedDecision
	ProcessingRestrictedField
)

// ActivationRequest binds an activation decision to the tenant context,
// exact approved PRIV-001 inventory release, and trusted reviewer registry
// supplied by the serving composition. TenantID and TrustedReviewers must
// come from the authenticated tenant and reviewer trust configuration; they
// must never be populated from an assessment or activation payload.
type ActivationRequest struct {
	TenantID         string
	ActivityID       string
	ActivityVersion  string
	Mode             ProcessingMode
	Inventory        inventory.Executable
	Assessment       *Assessment
	Now              time.Time
	TrustedReviewers map[string]ed25519.PublicKey
}

// AuthorizeActivityActivation checks an activation against the exact
// validated PRIV-001 release and then applies the signed-assessment gate for
// high-risk, automated-decision, and restricted-field processing. Callers
// should use ProcessingAutomatedDecision or ProcessingRestrictedField for
// those paths rather than deriving a lower-risk mode from user input.
func AuthorizeActivityActivation(request ActivationRequest) error {
	if strings.TrimSpace(request.TenantID) == "" || strings.TrimSpace(request.ActivityID) == "" ||
		strings.TrimSpace(request.ActivityVersion) == "" || request.Now.IsZero() {
		return fmt.Errorf("%w: activation tenant, activity, version, and time are required", ErrActivityUnavailable)
	}
	if request.Mode < ProcessingRoutine || request.Mode > ProcessingRestrictedField {
		return fmt.Errorf("%w: unsupported processing mode %d", ErrActivityUnavailable, request.Mode)
	}
	pack, err := inventory.CurrentTransferRulePack()
	if err != nil {
		return fmt.Errorf("%w: current transfer rules: %v", ErrInventoryMismatch, err)
	}
	validated, err := inventory.ValidateExecutableWithTransferPolicy(request.Inventory.Inventory, pack, request.Now)
	if err != nil || request.Inventory.Digest == "" || validated.Digest != request.Inventory.Digest {
		if err != nil {
			return fmt.Errorf("%w: %v", ErrInventoryMismatch, err)
		}
		return fmt.Errorf("%w: release digest does not match validated inventory", ErrInventoryMismatch)
	}
	approved := false
	for _, activity := range validated.Inventory.Activities {
		if activity.ID == request.ActivityID && activity.Version == request.ActivityVersion && activity.Status == inventory.StatusApproved {
			approved = true
			break
		}
	}
	if !approved {
		return fmt.Errorf("%w: activity=%s version=%s", ErrActivityUnavailable, request.ActivityID, request.ActivityVersion)
	}
	if request.Mode == ProcessingRoutine {
		return nil
	}
	if request.Assessment != nil && request.Assessment.ActivityVersion != request.ActivityVersion {
		return fmt.Errorf("%w: assessment covers activity version %s, activation requests %s", ErrAssessmentMismatch, request.Assessment.ActivityVersion, request.ActivityVersion)
	}
	if request.Assessment != nil && request.Assessment.InventoryDigest != validated.Digest {
		return fmt.Errorf("%w: assessment covers inventory %s, activation uses %s", ErrAssessmentMismatch, request.Assessment.InventoryDigest, validated.Digest)
	}
	return AuthorizeActivation(
		request.Assessment,
		request.TenantID,
		request.ActivityID,
		request.Mode == ProcessingHighRisk,
		request.Mode == ProcessingAutomatedDecision,
		request.Mode == ProcessingRestrictedField,
		request.Now,
		request.TrustedReviewers,
	)
}
