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
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// PersonaEvaluationRecorder retains signed evidence through the configured
// signature verifier. An issuer never writes publication state directly.
type PersonaEvaluationRecorder interface {
	Record(context.Context, values.TenantId, agentpersonastore.SignedPersonaEvaluation) error
}

// PersonaEvaluationIssuer signs only sealed, independently measured reports
// from the synthetic persona evaluation runner. Its private key is supplied
// by the trusted evaluator composition and never by an administration request.
type PersonaEvaluationIssuer struct {
	Versions    PersonaAdminInstallationProfiles
	Recorder    PersonaEvaluationRecorder
	TenantID    values.TenantId
	SuiteID     string
	SuiteDigest string
	KeyID       string
	PrivateKey  ed25519.PrivateKey
	ModelDigest string
	Now         func() time.Time
	FreshFor    time.Duration
	NewRunID    func() string
}

// Issue checks the report against the persisted immutable profile, selected
// suite and pinned model before creating a signed durable evaluation record.
// Failed reports are retained as failures and cannot authorize publication.
func (s *PersonaEvaluationIssuer) Issue(ctx context.Context, report agenteval.PersonaEvaluationReport) (string, error) {
	if s == nil || ctx == nil || s.Versions == nil || s.Recorder == nil || s.TenantID.Validate() != nil || s.Now == nil || s.NewRunID == nil || s.FreshFor <= 0 || s.FreshFor > 30*24*time.Hour || len(s.PrivateKey) != ed25519.PrivateKeySize {
		return "", ErrPersonaEvaluationEvidenceUnavailable
	}
	evidence, err := report.Evidence()
	if err != nil || evidence.Target.TenantID != string(s.TenantID) || evidence.SuiteID != s.SuiteID || s.SuiteDigest == "" || evidence.SuiteDigest != s.SuiteDigest || evidence.Target.ModelDigest != s.ModelDigest {
		return "", ErrPersonaEvaluationEvidenceUnavailable
	}
	qualifiedKey, err := PersonaEvaluationVerificationKeyID(string(s.TenantID), s.SuiteID, s.KeyID)
	if err != nil {
		return "", err
	}
	row, err := s.Versions.GetVersion(ctx, s.TenantID, evidence.Target.PersonaID, evidence.Target.PersonaVersion)
	if err != nil || row.TenantID != s.TenantID || row.PersonaID != evidence.Target.PersonaID || row.Version != evidence.Target.PersonaVersion || row.ContentDigest != evidence.Target.ProfileDigest {
		return "", ErrPersonaEvaluationEvidenceUnavailable
	}
	var profile agentpersona.PersonaProfile
	if json.Unmarshal(row.Profile, &profile) != nil || profile.PersonaID != row.PersonaID || int64(profile.Version) != row.Version || profile.EvalSuiteRef != s.SuiteID {
		return "", ErrPersonaEvaluationEvidenceUnavailable
	}
	sealed, err := agentpersona.Seal(profile)
	if err != nil || sealed.Digest != row.ContentDigest {
		return "", ErrPersonaEvaluationEvidenceUnavailable
	}
	now, runID := s.Now().UTC(), s.NewRunID()
	if now.IsZero() || evidence.EvaluatedAt.IsZero() || evidence.FinishedAt.Before(evidence.EvaluatedAt) || evidence.FinishedAt.After(now) || !evidence.EvaluatedAt.Add(s.FreshFor).After(now) || runID == "" || strings.TrimSpace(runID) != runID {
		return "", ErrPersonaEvaluationEvidenceUnavailable
	}
	claim := agentpersonastore.PersonaEvaluationClaim{TenantID: string(s.TenantID), RunID: runID,
		PersonaID: row.PersonaID, PersonaVersion: row.Version, ProfileDigest: row.ContentDigest,
		SuiteDigest: evidence.SuiteDigest, RunDigest: evidence.RunDigest, ModelDigest: evidence.Target.ModelDigest,
		Passed: evidence.Passed, IssuedAt: evidence.EvaluatedAt.UTC(), ExpiresAt: evidence.EvaluatedAt.Add(s.FreshFor).UTC(), KeyID: qualifiedKey}
	signed, err := agentpersonastore.SignPersonaEvaluationClaim(s.PrivateKey, claim)
	if err != nil {
		return "", err
	}
	if err := s.Recorder.Record(ctx, s.TenantID, signed); err != nil {
		return "", err
	}
	return runID, nil
}
