package agentpersonastore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestAgentUXSetup3_PublicationEvidenceDetails(t *testing.T) {
	tenant := values.TenantId("agentux-setup3-evidence")
	fixture := newFixture(t, tenant)
	tenantID := fixture.ids[tenant]
	profileDigest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	reviewDigest := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	runDigest := "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	suiteDigest := "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	modelDigest := "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	reviewedAt := time.Date(2026, 9, 29, 14, 30, 0, 0, time.UTC)
	evaluatedAt := reviewedAt.Add(45 * time.Minute)

	fixture.db.Exec(t, `INSERT INTO persona_versions
		(tenant_id,persona_id,version,agent_version,handle,display_name,profile,content_digest,created_at)
		VALUES ($1,'policy-helper',4,'agent.policy-helper@1','policy-helper','Policy Helper','{"owner":"walt"}'::jsonb,$2,$3)`, tenantID, profileDigest, reviewedAt.Add(-time.Hour))
	fixture.db.Exec(t, `INSERT INTO persona_review_grant
		(tenant_id,grant_id,principal_id,permission,granted_at,expires_at)
		VALUES ($1,'review-grant','curtis','persona:review',$2,$3)`, tenantID, reviewedAt.Add(-time.Hour), reviewedAt.Add(24*time.Hour))
	fixture.db.Exec(t, `INSERT INTO persona_review_decision
		(tenant_id,review_id,persona_id,persona_version,profile_digest,author_id,reviewer_id,grant_id,permission,decision,review_digest,reviewed_at,expires_at)
		VALUES ($1,'review-4','policy-helper',4,$2,'walt','curtis','review-grant','persona:review','APPROVE',$3,$4,$5)`, tenantID, profileDigest, reviewDigest, reviewedAt, reviewedAt.Add(24*time.Hour))
	fixture.db.Exec(t, `INSERT INTO persona_evaluation_evidence
		(tenant_id,run_id,persona_id,persona_version,profile_digest,suite_digest,run_digest,model_digest,passed,issued_at,expires_at,seal_key_id,seal)
		VALUES ($1,'evaluation-4','policy-helper',4,$2,$3,$4,$5,true,$6,$7,'evaluator',decode(repeat('00',64),'hex'))`, tenantID, profileDigest, suiteDigest, runDigest, modelDigest, evaluatedAt, evaluatedAt.Add(24*time.Hour))
	fixture.db.Exec(t, `INSERT INTO persona_lifecycle_events
		(tenant_id,event_id,persona_id,persona_version,from_state,to_state,reason,actor_id,occurred_at,profile_digest,review_digest,reviewer_id,evaluation_digest,evaluation_profile_digest,evaluation_suite_digest)
		VALUES ($1,'publish-4','policy-helper',4,'IN_REVIEW','PUBLISHED','reviewed','curtis',$2,$3,$4,'curtis',$5,$3,$6)`, tenantID, evaluatedAt.Add(time.Minute), profileDigest, reviewDigest, runDigest, suiteDigest)

	details, err := fixture.store(t, tenant).ReadPublicationEvidenceDetails(context.Background(), "policy-helper", 4)
	if err != nil {
		t.Fatal(err)
	}
	if details.ReviewerID != "curtis" || !details.ReviewApprovedAt.Equal(reviewedAt) || !details.EvaluationPassedAt.Equal(evaluatedAt) {
		t.Fatalf("publication evidence details = %+v", details)
	}
	if _, err := fixture.store(t, tenant).ReadPublicationEvidenceDetails(context.Background(), "policy-helper", 3); !errors.Is(err, ErrPublicationEvidenceRequired) {
		t.Fatalf("missing version evidence err=%v", err)
	}
}
