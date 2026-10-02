package application

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type PersonaAdminEvaluationExecutor interface {
	EvaluatePersonaVersion(context.Context, agenteval.PersonaEvaluationTarget) (agenteval.PersonaEvaluationReport, agenteval.PersonaSuite, error)
}

type personaAdminEvaluationReviewSource interface {
	ResolveCurrentReview(context.Context, string, int64) (agentpersonastore.VerifiedReview, error)
}

// PersonaAdminEvaluationService binds an administrator request to the latest
// independently reviewed immutable version, executes the configured evaluator,
// and records its signed result. It does not publish or mutate lifecycle state.
type PersonaAdminEvaluationService struct {
	Store           PersonaAdminLifecycleStore
	Versions        PersonaAdminInstallationProfiles
	Recorder        PersonaEvaluationRecorder
	Executor        PersonaAdminEvaluationExecutor
	SyntheticTenant string
	KeyID           string
	PrivateKey      ed25519.PrivateKey
	ModelDigest     string
	Now             func() time.Time
	FreshFor        time.Duration
	NewRunID        func() string
}

func (s *PersonaAdminEvaluationService) RunPersonaEvaluation(ctx context.Context, actor PersonaAdminCommandActor, personaID string) (productui.PersonaAdminEvaluationResult, error) {
	if s == nil || ctx == nil || s.Store == nil || s.Versions == nil || s.Recorder == nil || s.Executor == nil || s.Now == nil || s.NewRunID == nil ||
		values.TenantId(s.SyntheticTenant).Validate() != nil || len(s.PrivateKey) != ed25519.PrivateKeySize || !personaRequestDigest(s.ModelDigest) || !validPersonaAdminActor(actor.Principal) ||
		actor.Tenant != actor.Principal.Tenant() || actor.Subject != actor.Principal.Subject() || strings.TrimSpace(personaID) == "" {
		return productui.PersonaAdminEvaluationResult{}, ErrPersonaAdminEvaluationUnavailable
	}
	tenant, err := s.Store.ForTenant(ctx, actor.Tenant)
	if err != nil || tenant == nil {
		return productui.PersonaAdminEvaluationResult{}, ErrPersonaAdminEvaluationUnavailable
	}
	row, state, err := latestPersonaVersion(ctx, tenant, personaID, "")
	if err != nil || state != agentpersonastore.StateInReview {
		return productui.PersonaAdminEvaluationResult{}, ErrPersonaAdminEvaluationUnavailable
	}
	reviews, ok := tenant.(personaAdminEvaluationReviewSource)
	if !ok {
		return productui.PersonaAdminEvaluationResult{}, ErrPersonaAdminEvaluationUnavailable
	}
	review, err := reviews.ResolveCurrentReview(ctx, personaID, row.Version)
	if err != nil || review.Decision != "APPROVE" || !review.GrantCurrent || review.ReviewerID == actor.Subject {
		return productui.PersonaAdminEvaluationResult{}, ErrPersonaAdminEvaluationUnavailable
	}
	var profile agentpersona.PersonaProfile
	if json.Unmarshal(row.Profile, &profile) != nil || profile.PersonaID != personaID || int64(profile.Version) != row.Version || profile.EvalSuiteRef == "" {
		return productui.PersonaAdminEvaluationResult{}, ErrPersonaAdminEvaluationUnavailable
	}
	target := agenteval.PersonaEvaluationTarget{TenantID: string(actor.Tenant), SyntheticTenantID: s.SyntheticTenant, PersonaID: personaID, PersonaVersion: row.Version, ProfileDigest: row.ContentDigest, ModelDigest: s.ModelDigest, InvokerID: actor.Subject}
	report, suite, err := s.Executor.EvaluatePersonaVersion(ctx, target)
	if err != nil || suite.ID != profile.EvalSuiteRef {
		return productui.PersonaAdminEvaluationResult{}, ErrPersonaAdminEvaluationUnavailable
	}
	evidence, err := report.Evidence()
	if err != nil || evidence.Target != target || evidence.SuiteID != suite.ID || evidence.SuiteDigest != agenteval.PersonaSuiteDigest(suite) {
		return productui.PersonaAdminEvaluationResult{}, ErrPersonaAdminEvaluationUnavailable
	}
	issuer := PersonaEvaluationIssuer{Versions: s.Versions, Recorder: s.Recorder, TenantID: actor.Tenant, SuiteID: suite.ID, SuiteDigest: evidence.SuiteDigest, KeyID: s.KeyID, PrivateKey: s.PrivateKey, ModelDigest: s.ModelDigest, Now: s.Now, FreshFor: s.FreshFor, NewRunID: s.NewRunID}
	if _, err := issuer.Issue(ctx, report); err != nil {
		return productui.PersonaAdminEvaluationResult{}, err
	}
	result := productui.PersonaAdminEvaluationResult{Status: "PASSED"}
	for _, evaluated := range evidence.Cases {
		if evaluated.Passed {
			result.Passed++
			continue
		}
		result.Failed++
		result.FailingCases = append(result.FailingCases, evaluated.CaseID)
	}
	if !evidence.Passed {
		result.Status = "FAILED"
	}
	return result, nil
}

var _ PersonaAdminEvaluationRunner = (*PersonaAdminEvaluationService)(nil)
