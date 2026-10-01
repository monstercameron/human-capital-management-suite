package agentpersona

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
)

type mutablePersonaReviewAuthority struct {
	allowed bool
}

func (a *mutablePersonaReviewAuthority) CanReview(reviewer string, _ PersonaProfile) bool {
	return a != nil && a.allowed && reviewer == "steward-1"
}

type fixedPersonaEvaluationStore struct {
	record EvaluationRecord
}

func (s fixedPersonaEvaluationStore) LookupEvaluation(string) (EvaluationRecord, error) {
	return s.record, nil
}

func TestTodo_AGENTP_006_ReviewAuthorityIsRequired(t *testing.T) {
	profile, catalog := personaFixture(t)
	validator := personaValidator(catalog)
	version := mustPersonaVersion(t, profile, validator)
	manager := NewManager(Dependencies{Validator: validator})
	if err := manager.Register(version); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Review(profile.PersonaID, profile.Version, ReviewRequest{Reviewer: "steward-1", Permission: PermissionPersonaReview, Decision: "APPROVE"}); !errors.Is(err, ErrReviewRequired) {
		t.Fatalf("review without authority error = %v", err)
	}
	_, state, err := manager.Current(profile.PersonaID)
	if err != nil || state != StateDraft {
		t.Fatalf("state after unauthorized review = %q, err=%v", state, err)
	}
}

func TestTodo_AGENTP_006_ReviewPermissionMustRemainCurrent(t *testing.T) {
	profile, catalog := personaFixture(t)
	validator := personaValidator(catalog)
	version := mustPersonaVersion(t, profile, validator)
	authority := &mutablePersonaReviewAuthority{allowed: true}
	evaluations := &personaEvaluationStore{failure: errors.New("evaluation evidence unavailable")}
	manager := NewManager(Dependencies{Validator: validator, Evaluations: evaluations, Reviews: authority})
	if err := manager.Register(version); err != nil {
		t.Fatal(err)
	}
	review, err := manager.Review(profile.PersonaID, profile.Version, ReviewRequest{Reviewer: "steward-1", Permission: PermissionPersonaReview, Decision: "APPROVE"})
	if err != nil {
		t.Fatal(err)
	}
	authority.allowed = false
	if _, err := manager.Publish(profile.PersonaID, profile.Version, PublishRequest{Review: review, EvaluationDigest: "eval-run-1"}); !errors.Is(err, ErrReviewRequired) {
		t.Fatalf("publish after review permission removal error = %v", err)
	}
	_, state, err := manager.Current(profile.PersonaID)
	if err != nil || state != StateInReview {
		t.Fatalf("state after revoked reviewer permission = %q, err=%v", state, err)
	}
}

func TestTodo_AGENTP_006_RetiredSkillAfterReviewCannotPublish(t *testing.T) {
	profile, catalog := personaFixture(t)
	validator := personaValidator(catalog)
	version := mustPersonaVersion(t, profile, validator)
	evaluations := &personaEvaluationStore{failure: errors.New("evaluation evidence unavailable")}
	manager := NewManager(Dependencies{Validator: validator, Evaluations: evaluations, Reviews: personaReviewAuthority{}})
	if err := manager.Register(version); err != nil {
		t.Fatal(err)
	}
	review := mustReview(t, manager, version)
	pin := version.Profile.SkillPins[0]
	record := catalog.records[pin.Key()]
	record.Status = agentskills.StatusRetired
	catalog.records[pin.Key()] = record
	if _, err := manager.Publish(profile.PersonaID, profile.Version, PublishRequest{Review: review, EvaluationDigest: "eval-run-1"}); !errors.Is(err, ErrRetiredSkill) {
		t.Fatalf("publish with a retired pinned skill error = %v", err)
	}
	_, state, err := manager.Current(profile.PersonaID)
	if err != nil || state != StateInReview {
		t.Fatalf("state after retired skill = %q, err=%v", state, err)
	}
}

func TestTodo_AGENTP_006_RollbackRejectsMismatchedEvaluationRecord(t *testing.T) {
	profile, catalog := personaFixture(t)
	validator := personaValidator(catalog)
	version := mustPersonaVersion(t, profile, validator)
	evaluations := fixedPersonaEvaluationStore{record: EvaluationRecord{
		ProfileDigest: version.Digest,
		SuiteRef:      profile.EvalSuiteRef,
		RunDigest:     "different-run",
		Fresh:         true,
	}}
	manager := NewManager(Dependencies{Validator: validator, Evaluations: evaluations, Reviews: personaReviewAuthority{}})
	if err := manager.Register(version); err != nil {
		t.Fatal(err)
	}
	stored := manager.personas[profile.PersonaID].versions[profile.Version]
	stored.published = true
	stored.publication = Publication{ProfileDigest: version.Digest, EvaluationDigest: "pinned-run", Reviewer: "steward-1", Generation: 1}
	manager.personas[profile.PersonaID].state = StatePublished
	if _, err := manager.Rollback(profile.PersonaID, profile.Version); !errors.Is(err, ErrEvaluation) {
		t.Fatalf("rollback with mismatched evaluation record error = %v", err)
	}
	_, state, err := manager.Current(profile.PersonaID)
	if err != nil || state != StatePublished {
		t.Fatalf("state after rejected rollback = %q, err=%v", state, err)
	}
}
