package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// DatabasePersonaCandidateScope resolves the immutable production candidate,
// the evaluator-only synthetic provision and the reviewer's current grant.
// It is composed by the evaluator, never exposed as a chat service option.
type DatabasePersonaCandidateScope struct {
	Target     agenteval.PersonaEvaluationTarget
	Versions   PersonaAdminInstallationProfiles
	Provisions agentstore.SyntheticTenantProvisionReader
	Reviewer   PersonaReviewAuthority
	ReviewerID string
	Sources    agentegress.SourceClassificationVerifier
	TenantUUID func(values.TenantId) uuid.UUID
	Now        func() time.Time
}

func (s *DatabasePersonaCandidateScope) AuthorizeSyntheticPersonaEvaluation(ctx context.Context, target agenteval.PersonaEvaluationTarget) error {
	if s == nil || ctx == nil || s.Versions == nil || s.Provisions == nil || s.Reviewer == nil || s.Sources == nil || s.TenantUUID == nil || s.Now == nil ||
		target != s.Target || target.TenantID == target.SyntheticTenantID || !required(s.ReviewerID) || !required(target.InvokerID) ||
		values.TenantId(target.TenantID).Validate() != nil || values.TenantId(target.SyntheticTenantID).Validate() != nil ||
		!personaRequestDigest(target.ProfileDigest) || !personaRequestDigest(target.ModelDigest) {
		return agenteval.ErrPersonaEvaluation
	}
	production, synthetic := s.TenantUUID(values.TenantId(target.TenantID)), s.TenantUUID(values.TenantId(target.SyntheticTenantID))
	if production == uuid.Nil || synthetic == uuid.Nil || production == synthetic {
		return agenteval.ErrPersonaEvaluation
	}
	provision, err := s.Provisions.SyntheticTenantProvision(ctx, production, target.SyntheticTenantID)
	now := s.Now().UTC()
	if err != nil || provision.TenantID != production || provision.SuiteTenantID != target.SyntheticTenantID ||
		provision.Purpose != "agent-evaluation" || provision.Status != "ACTIVE" || provision.RevokedAt != nil ||
		provision.IssuedAt.After(now) || !provision.ExpiresAt.After(now) || provision.ToolOwnerProfile != "fixture-only/v1" || !required(provision.MarkerID) {
		return agenteval.ErrPersonaEvaluation
	}
	ids := map[string]bool{}
	for _, id := range []string{provision.GrantStoreID, provision.TaskStoreID, provision.BudgetLedgerID, provision.AuditStoreID, provision.ToolOwnerID} {
		if !required(id) || ids[id] {
			return agenteval.ErrPersonaEvaluation
		}
		ids[id] = true
	}
	row, err := s.Versions.GetVersion(ctx, values.TenantId(target.TenantID), target.PersonaID, target.PersonaVersion)
	var profile agentpersona.PersonaProfile
	if err != nil || row.TenantID.String() != target.TenantID || row.PersonaID != target.PersonaID || row.Version != target.PersonaVersion ||
		row.ContentDigest != target.ProfileDigest || json.Unmarshal(row.Profile, &profile) != nil {
		return agenteval.ErrPersonaEvaluation
	}
	sealed, err := agentpersona.Seal(profile)
	if err != nil || sealed.Digest != target.ProfileDigest || profile.PersonaID != target.PersonaID || int64(profile.Version) != target.PersonaVersion {
		return agenteval.ErrPersonaEvaluation
	}
	return s.Reviewer.AuthorizePersonaReview(ctx, values.TenantId(target.TenantID), s.ReviewerID)
}

func (s *DatabasePersonaCandidateScope) AuthorizePersonaCandidateModelRequest(ctx context.Context, target agenteval.PersonaEvaluationTarget, request AgentModelExecutorRequest) error {
	if err := s.AuthorizeSyntheticPersonaEvaluation(ctx, target); err != nil {
		return err
	}
	if request.Task.TenantID != target.SyntheticTenantID || request.Outbound.Tenant != target.SyntheticTenantID ||
		request.Outbound.Principal != target.InvokerID || len(request.Outbound.Fields) == 0 {
		return agenteval.ErrPersonaEvaluation
	}
	row, err := s.Versions.GetVersion(ctx, values.TenantId(target.TenantID), target.PersonaID, target.PersonaVersion)
	var profile agentpersona.PersonaProfile
	if err != nil || json.Unmarshal(row.Profile, &profile) != nil || request.Task.AgentID != profile.Manifest.Digest {
		return agenteval.ErrPersonaEvaluation
	}
	for _, field := range request.Outbound.Fields {
		// Candidate work can contain fixture data and invoker questions only.
		// Production directory, documents and peer-history source classes are
		// deliberately excluded from this dedicated execution deployment.
		text, ok := field.Value.(string)
		source := request.FieldSources[field.Name]
		if !ok || source != "synthetic-fixture" && source != "persona-model-tool-proposal" && source != "persona-untrusted-tool-result" || len(field.Provenance) == 0 {
			return agenteval.ErrPersonaEvaluation
		}
		digest := sha256.Sum256([]byte(text))
		if err := s.Sources.VerifySourceClassification(ctx, agentegress.SourceClassificationRequest{Tenant: target.SyntheticTenantID,
			Purpose: request.Outbound.Purpose, FieldName: field.Name, SourceClass: source, DataClass: field.Class,
			Provenance: field.Provenance, ValueDigest: "sha256:" + hex.EncodeToString(digest[:])}); err != nil {
			return err
		}
	}
	return nil
}
