package agentpersonastore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	agentrollout "github.com/monstercameron/human-capital-management-suite/internal/agentsystem/rollout"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// VersionRolloutProgress is the durable CAS cursor for an immutable plan.
type VersionRolloutProgress struct {
	Revision   int64  `json:"revision"`
	Cursor     int    `json:"cursor"`
	Stage      string `json:"stage"`
	ApproverID string `json:"approver_id"`
}

func (s *TenantStore) SaveVersionRollout(ctx context.Context, plan agentrollout.VersionPlan) (VersionRolloutProgress, error) {
	if s == nil || ctx == nil || plan.TenantID != string(s.tenant) || strings.TrimSpace(plan.ID) == "" {
		return VersionRolloutProgress{}, fmt.Errorf("%w: rollout tenant and id are required", ErrInvalid)
	}
	if err := plan.Verify(); err != nil {
		return VersionRolloutProgress{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	b, err := json.Marshal(plan)
	if err != nil {
		return VersionRolloutProgress{}, fmt.Errorf("%w: encode rollout plan: %v", ErrInvalid, err)
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return VersionRolloutProgress{}, err
	}
	defer tx.Rollback(ctx)
	n, err := tx.Exec(ctx, `INSERT INTO persona_version_rollout (tenant_id,rollout_id,plan_digest,plan) VALUES ($1,$2,$3,$4::jsonb) ON CONFLICT DO NOTHING`, s.tenantID, plan.ID, plan.Digest, b)
	if err != nil {
		return VersionRolloutProgress{}, fmt.Errorf("agentpersonastore: save rollout: %w", err)
	}
	if n == 0 {
		return VersionRolloutProgress{}, fmt.Errorf("%w: rollout already exists", ErrConflict)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO persona_version_rollout_progress (tenant_id,rollout_id,revision,cursor,stage) VALUES ($1,$2,1,0,'PREVIEWED')`, s.tenantID, plan.ID); err != nil {
		return VersionRolloutProgress{}, fmt.Errorf("agentpersonastore: save rollout progress: %w", err)
	}
	if err := commit(ctx, tx); err != nil {
		return VersionRolloutProgress{}, err
	}
	return VersionRolloutProgress{Revision: 1, Stage: "PREVIEWED"}, nil
}

func (s *TenantStore) GetVersionRollout(ctx context.Context, id string) (agentrollout.VersionPlan, VersionRolloutProgress, error) {
	if s == nil || ctx == nil || strings.TrimSpace(id) == "" {
		return agentrollout.VersionPlan{}, VersionRolloutProgress{}, fmt.Errorf("%w: rollout id is required", ErrInvalid)
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return agentrollout.VersionPlan{}, VersionRolloutProgress{}, err
	}
	defer tx.Rollback(ctx)
	var b []byte
	var p VersionRolloutProgress
	err = tx.QueryRow(ctx, `SELECT r.plan,p.revision,p.cursor,p.stage,p.approver_id FROM persona_version_rollout r JOIN persona_version_rollout_progress p USING (tenant_id,rollout_id) WHERE r.tenant_id=$1 AND r.rollout_id=$2`, s.tenantID, id).Scan(&b, &p.Revision, &p.Cursor, &p.Stage, &p.ApproverID)
	if errors.Is(err, dbport.ErrNoRows) {
		return agentrollout.VersionPlan{}, VersionRolloutProgress{}, fmt.Errorf("%w: rollout %s", ErrNotFound, id)
	}
	if err != nil {
		return agentrollout.VersionPlan{}, VersionRolloutProgress{}, err
	}
	var plan agentrollout.VersionPlan
	if err := json.Unmarshal(b, &plan); err != nil {
		return agentrollout.VersionPlan{}, VersionRolloutProgress{}, fmt.Errorf("agentpersonastore: decode rollout: %w", err)
	}
	if err := commit(ctx, tx); err != nil {
		return agentrollout.VersionPlan{}, VersionRolloutProgress{}, err
	}
	return plan, p, nil
}

func (s *TenantStore) ApproveVersionRollout(ctx context.Context, id, digest, actor string, expectedProgressRevision int64) (VersionRolloutProgress, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(digest) == "" || strings.TrimSpace(actor) == "" || expectedProgressRevision <= 0 {
		return VersionRolloutProgress{}, fmt.Errorf("%w: rollout approval fields are required", ErrInvalid)
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return VersionRolloutProgress{}, err
	}
	defer tx.Rollback(ctx)
	var planDigest string
	var p VersionRolloutProgress
	err = tx.QueryRow(ctx, `SELECT r.plan_digest,p.revision,p.cursor,p.stage,p.approver_id FROM persona_version_rollout r JOIN persona_version_rollout_progress p USING (tenant_id,rollout_id) WHERE r.tenant_id=$1 AND r.rollout_id=$2 FOR UPDATE OF p`, s.tenantID, id).Scan(&planDigest, &p.Revision, &p.Cursor, &p.Stage, &p.ApproverID)
	if errors.Is(err, dbport.ErrNoRows) {
		return VersionRolloutProgress{}, fmt.Errorf("%w: rollout", ErrNotFound)
	}
	if err != nil {
		return VersionRolloutProgress{}, err
	}
	if digest != planDigest {
		return VersionRolloutProgress{}, fmt.Errorf("%w: digest", ErrConflict)
	}
	if p.Revision != expectedProgressRevision || p.Stage != "PREVIEWED" {
		return VersionRolloutProgress{}, fmt.Errorf("%w: rollout progress", ErrConflict)
	}
	approvalID := uuid.NewString()
	if _, err = tx.Exec(ctx, `INSERT INTO persona_version_rollout_approval (tenant_id,approval_id,rollout_id,plan_digest,actor_id) VALUES ($1,$2,$3,$4,$5)`, s.tenantID, approvalID, id, digest, actor); err != nil {
		return VersionRolloutProgress{}, fmt.Errorf("agentpersonastore: record rollout approval: %w", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE persona_version_rollout_progress SET revision=revision+1,stage='APPROVED',approver_id=$3,updated_at=now() WHERE tenant_id=$1 AND rollout_id=$2 AND revision=$4`, s.tenantID, id, actor, expectedProgressRevision); err != nil {
		return VersionRolloutProgress{}, err
	}
	p.Revision++
	p.Stage = "APPROVED"
	p.ApproverID = actor
	if err := commit(ctx, tx); err != nil {
		return VersionRolloutProgress{}, err
	}
	return p, nil
}

func (s *TenantStore) PromoteVersionRollout(ctx context.Context, id, digest, actor string, expectedProgressRevision int64) (VersionRolloutProgress, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return VersionRolloutProgress{}, err
	}
	defer tx.Rollback(ctx)
	var p VersionRolloutProgress
	var planDigest string
	err = tx.QueryRow(ctx, `SELECT r.plan_digest,p.revision,p.cursor,p.stage,p.approver_id
		FROM persona_version_rollout r JOIN persona_version_rollout_progress p USING(tenant_id,rollout_id)
		WHERE r.tenant_id=$1 AND r.rollout_id=$2 FOR UPDATE OF p`, s.tenantID, id).
		Scan(&planDigest, &p.Revision, &p.Cursor, &p.Stage, &p.ApproverID)
	if errors.Is(err, dbport.ErrNoRows) {
		return VersionRolloutProgress{}, fmt.Errorf("%w: rollout", ErrNotFound)
	}
	if err != nil {
		return VersionRolloutProgress{}, err
	}
	if digest != planDigest || p.Revision != expectedProgressRevision || p.Stage != "CANARY_COMPLETE" {
		return VersionRolloutProgress{}, fmt.Errorf("%w: canary promotion fence", ErrConflict)
	}
	if actor == "" || actor != p.ApproverID {
		return VersionRolloutProgress{}, fmt.Errorf("%w: approving actor required", ErrConflict)
	}
	if _, err = tx.Exec(ctx, `UPDATE persona_version_rollout_progress SET revision=revision+1,stage='APPROVED',updated_at=now() WHERE tenant_id=$1 AND rollout_id=$2 AND revision=$3 AND stage='CANARY_COMPLETE'`, s.tenantID, id, expectedProgressRevision); err != nil {
		return VersionRolloutProgress{}, err
	}
	p.Revision++
	p.Stage = "APPROVED"
	if err := commit(ctx, tx); err != nil {
		return VersionRolloutProgress{}, err
	}
	return p, nil
}

func policyDigest(policy ChannelPolicy) string {
	b, _ := json.Marshal(policy)
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ApplyVersionRollout advances exactly one candidate. All evidence and fences
// are checked under the same transaction as the ACTIVE installation update.
func (s *TenantStore) ApplyVersionRollout(ctx context.Context, id, digest, actor string, expectedProgressRevision int64, candidate agentrollout.VersionCandidate, target PersonaInstallation) (PersonaInstallation, VersionRolloutProgress, error) {
	if s == nil || ctx == nil || strings.TrimSpace(id) == "" || strings.TrimSpace(digest) == "" || strings.TrimSpace(actor) == "" || expectedProgressRevision <= 0 {
		return PersonaInstallation{}, VersionRolloutProgress{}, fmt.Errorf("%w: rollout application fields are required", ErrInvalid)
	}
	if candidate.InstallationID != target.InstallationID || target.State != InstallationActive {
		return PersonaInstallation{}, VersionRolloutProgress{}, fmt.Errorf("%w: active target required", ErrConflict)
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return PersonaInstallation{}, VersionRolloutProgress{}, err
	}
	defer tx.Rollback(ctx)
	var planJSON []byte
	var planDigest string
	var p VersionRolloutProgress
	err = tx.QueryRow(ctx, `SELECT r.plan,r.plan_digest,p.revision,p.cursor,p.stage,p.approver_id FROM persona_version_rollout r JOIN persona_version_rollout_progress p USING(tenant_id,rollout_id) WHERE r.tenant_id=$1 AND r.rollout_id=$2 FOR UPDATE OF p`, s.tenantID, id).Scan(&planJSON, &planDigest, &p.Revision, &p.Cursor, &p.Stage, &p.ApproverID)
	if errors.Is(err, dbport.ErrNoRows) {
		return PersonaInstallation{}, VersionRolloutProgress{}, fmt.Errorf("%w: rollout", ErrNotFound)
	}
	if err != nil {
		return PersonaInstallation{}, VersionRolloutProgress{}, err
	}
	var plan agentrollout.VersionPlan
	if err = json.Unmarshal(planJSON, &plan); err != nil {
		return PersonaInstallation{}, VersionRolloutProgress{}, err
	}
	if err = plan.Verify(); err != nil || plan.Digest != planDigest || digest != planDigest || p.Revision != expectedProgressRevision || p.Stage != "APPROVED" || p.Cursor >= len(plan.Candidates) {
		return PersonaInstallation{}, VersionRolloutProgress{}, fmt.Errorf("%w: rollout plan or progress", ErrConflict)
	}
	want := plan.Candidates[p.Cursor]
	if want != candidate {
		return PersonaInstallation{}, VersionRolloutProgress{}, fmt.Errorf("%w: candidate cursor", ErrConflict)
	}
	securityKeys := []string{
		"persona-security:" + s.tenantID.String() + ":INSTALLATION:" + candidate.InstallationID,
		"persona-security:" + s.tenantID.String() + ":PERSONA:" + plan.AgentID,
		"persona-security:" + s.tenantID.String() + ":TENANT:" + string(s.tenant),
		"persona-security:" + s.tenantID.String() + ":VERSION:" + plan.AgentID + ":" + fmt.Sprintf("%d", plan.Version),
	}
	sort.Strings(securityKeys)
	for _, key := range securityKeys {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, key); err != nil {
			return PersonaInstallation{}, VersionRolloutProgress{}, fmt.Errorf("agentpersonastore: lock rollout security scope: %w", err)
		}
	}
	// Match the lifecycle writer's advisory key so a publication append cannot
	// race the current-state/evidence read below.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, string(s.tenant)+":"+plan.AgentID+":"+fmt.Sprintf("%d", plan.Version)); err != nil {
		return PersonaInstallation{}, VersionRolloutProgress{}, fmt.Errorf("agentpersonastore: lock rollout lifecycle: %w", err)
	}
	var cur PersonaInstallation
	var policyJSON []byte
	err = tx.QueryRow(ctx, `SELECT installation_id,persona_id,persona_version,conversation_id,conversation_class,installer_id,channel_policy,state,suspension_reason,revision,revocation_epoch,created_at,updated_at FROM persona_installations WHERE tenant_id=$1 AND installation_id=$2 FOR UPDATE`, s.tenantID, target.InstallationID).Scan(&cur.InstallationID, &cur.PersonaID, &cur.PersonaVersion, &cur.ConversationID, &cur.ConversationClass, &cur.InstallerID, &policyJSON, &cur.State, &cur.SuspensionReason, &cur.Revision, &cur.RevocationEpoch, &cur.CreatedAt, &cur.UpdatedAt)
	if err != nil {
		return PersonaInstallation{}, VersionRolloutProgress{}, err
	}
	if target.TenantID != s.tenant || cur.PersonaID != plan.AgentID || target.PersonaID != cur.PersonaID || target.ConversationID != cur.ConversationID || target.Revision != cur.Revision || target.RevocationEpoch != cur.RevocationEpoch {
		return PersonaInstallation{}, VersionRolloutProgress{}, fmt.Errorf("%w: target snapshot is stale", ErrConflict)
	}
	if cur.State != InstallationActive || cur.PersonaVersion != candidate.Version || target.PersonaVersion != plan.Version || cur.Revision != candidate.Revision || cur.RevocationEpoch != candidate.RevocationEpoch || cur.ConversationID != candidate.ConversationID {
		return PersonaInstallation{}, VersionRolloutProgress{}, fmt.Errorf("%w: stale installation candidate", ErrConflict)
	}
	if err = json.Unmarshal(policyJSON, &cur.ChannelPolicy); err != nil {
		return PersonaInstallation{}, VersionRolloutProgress{}, err
	}
	if policyDigest(cur.ChannelPolicy) != candidate.PolicyDigest {
		return PersonaInstallation{}, VersionRolloutProgress{}, fmt.Errorf("%w: policy changed", ErrConflict)
	}
	var profileDigest string
	var lifecycle LifecycleState
	var reviewDigest, reviewerID, evaluationDigest, evaluationProfileDigest, evaluationSuiteDigest string
	err = tx.QueryRow(ctx, `SELECT v.content_digest,l.to_state,l.review_digest,l.reviewer_id,l.evaluation_digest,l.evaluation_profile_digest,l.evaluation_suite_digest FROM persona_versions v JOIN persona_lifecycle_events l ON l.tenant_id=v.tenant_id AND l.persona_id=v.persona_id AND l.persona_version=v.version WHERE v.tenant_id=$1 AND v.persona_id=$2 AND v.version=$3 ORDER BY l.event_sequence DESC LIMIT 1`, s.tenantID, cur.PersonaID, plan.Version).Scan(&profileDigest, &lifecycle, &reviewDigest, &reviewerID, &evaluationDigest, &evaluationProfileDigest, &evaluationSuiteDigest)
	if err != nil || lifecycle != StatePublished || profileDigest != plan.ProfileDigest || reviewDigest == "" || reviewerID == "" || evaluationDigest == "" || evaluationProfileDigest != profileDigest || evaluationSuiteDigest == "" {
		return PersonaInstallation{}, VersionRolloutProgress{}, fmt.Errorf("%w: target profile evidence", ErrConflict)
	}
	var profileOwner, businessOwner string
	if err := tx.QueryRow(ctx, `SELECT v.profile->>'owner',o.principal_id FROM persona_versions v JOIN persona_owners o ON o.tenant_id=v.tenant_id AND o.persona_id=v.persona_id AND o.owner_role='BUSINESS_OWNER' WHERE v.tenant_id=$1 AND v.persona_id=$2 AND v.version=$3 FOR SHARE OF o`, s.tenantID, cur.PersonaID, plan.Version).Scan(&profileOwner, &businessOwner); err != nil || profileOwner == "" || profileOwner != businessOwner {
		return PersonaInstallation{}, VersionRolloutProgress{}, fmt.Errorf("%w: current persona owner", ErrConflict)
	}
	if p.ApproverID == "" || actor != p.ApproverID || s.reviews == nil || s.evaluations == nil {
		return PersonaInstallation{}, VersionRolloutProgress{}, ErrPublicationEvidenceRequired
	}
	review, e := s.reviews.ResolvePersonaReview(ctx, tx, s.tenant, cur.PersonaID, plan.Version, profileDigest, plan.ReviewRef)
	if e != nil || review.ReviewID != plan.ReviewRef || review.TenantID != string(s.tenant) || review.PersonaID != cur.PersonaID || review.PersonaVersion != plan.Version || review.ProfileDigest != profileDigest || review.ReviewDigest == "" || review.ReviewerID == "" || review.ReviewerID == businessOwner || review.Permission != "persona:review" || !review.GrantCurrent || review.Decision != "APPROVE" {
		return PersonaInstallation{}, VersionRolloutProgress{}, ErrPublicationEvidenceRequired
	}
	eval, e := s.evaluations.ResolvePersonaEvaluation(ctx, tx, s.tenant, plan.EvaluationRef, cur.PersonaID, plan.Version, profileDigest)
	if e != nil || eval.RunID != plan.EvaluationRef || eval.TenantID != string(s.tenant) || eval.PersonaID != cur.PersonaID || eval.PersonaVersion != plan.Version || eval.ProfileDigest != profileDigest || eval.RunDigest == "" || eval.SuiteDigest == "" || !eval.Passed || !eval.Fresh {
		return PersonaInstallation{}, VersionRolloutProgress{}, ErrPublicationEvidenceRequired
	}
	var approvalID, approvalDigest, approvalActor string
	if err := tx.QueryRow(ctx, `SELECT approval_id,plan_digest,actor_id FROM persona_version_rollout_approval WHERE tenant_id=$1 AND rollout_id=$2`, s.tenantID, id).Scan(&approvalID, &approvalDigest, &approvalActor); err != nil || approvalDigest != planDigest || approvalActor != p.ApproverID || approvalActor != actor {
		return PersonaInstallation{}, VersionRolloutProgress{}, fmt.Errorf("%w: durable rollout approval", ErrConflict)
	}
	var scopeState string
	for _, scope := range [][2]string{{"TENANT", string(s.tenant)}, {"PERSONA", cur.PersonaID}, {"VERSION", fmt.Sprintf("%s:%d", cur.PersonaID, plan.Version)}, {"INSTALLATION", cur.InstallationID}} {
		if _, err = tx.Exec(ctx, `INSERT INTO persona_security_scope (tenant_id,scope_kind,scope_key) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, s.tenantID, scope[0], scope[1]); err != nil {
			return PersonaInstallation{}, VersionRolloutProgress{}, fmt.Errorf("agentpersonastore: initialize target security scope: %w", err)
		}
		var epoch int64
		err = tx.QueryRow(ctx, `SELECT state,epoch FROM persona_security_scope WHERE tenant_id=$1 AND scope_kind=$2 AND scope_key=$3 FOR UPDATE`, s.tenantID, scope[0], scope[1]).Scan(&scopeState, &epoch)
		if err != nil || scopeState != "ACTIVE" {
			if err == nil {
				err = ErrConflict
			}
			return PersonaInstallation{}, VersionRolloutProgress{}, fmt.Errorf("%w: target security scope %s", ErrConflict, scope[0])
		}
		if scope[0] == "INSTALLATION" {
			if _, err = tx.Exec(ctx, `UPDATE persona_security_scope SET epoch=epoch+1 WHERE tenant_id=$1 AND scope_kind=$2 AND scope_key=$3`, s.tenantID, scope[0], scope[1]); err != nil {
				return PersonaInstallation{}, VersionRolloutProgress{}, err
			}
		}
	}
	res, err := tx.Exec(ctx, `UPDATE persona_installations SET persona_version=$4,state='ACTIVE',suspension_reason='',revision=revision+1,revocation_epoch=revocation_epoch+1,rollout_preview_ref=$5,rollout_approval_ref=$6,updated_at=now() WHERE tenant_id=$1 AND installation_id=$2 AND revision=$3 AND state='ACTIVE'`, s.tenantID, target.InstallationID, candidate.Revision, plan.Version, id, approvalID)
	if err != nil || res != 1 {
		return PersonaInstallation{}, VersionRolloutProgress{}, fmt.Errorf("%w: installation CAS", ErrConflict)
	}
	next := p.Cursor + 1
	p.Cursor = next
	p.Revision++
	if next == len(plan.Candidates) {
		p.Stage = "COMPLETE"
	} else if next == plan.CanaryCount {
		p.Stage = "CANARY_COMPLETE"
	}
	if _, err = tx.Exec(ctx, `UPDATE persona_version_rollout_progress SET revision=$3,cursor=$4,stage=$5,updated_at=now() WHERE tenant_id=$1 AND rollout_id=$2 AND revision=$6`, s.tenantID, id, p.Revision, p.Cursor, p.Stage, expectedProgressRevision); err != nil {
		return PersonaInstallation{}, VersionRolloutProgress{}, err
	}
	if err = commit(ctx, tx); err != nil {
		return PersonaInstallation{}, VersionRolloutProgress{}, err
	}
	cur.PersonaVersion = plan.Version
	cur.Revision++
	cur.RevocationEpoch++
	cur.State = InstallationActive
	return cur, p, nil
}
