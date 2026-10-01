package agentpersona

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
)

type tupleGrantFixture struct {
	grant SkillAudienceGrant
	err   error
	ok    bool
}

func (f tupleGrantFixture) AudienceGrant(agentskills.SkillPin) (SkillAudienceGrant, error) {
	return f.grant, f.err
}

func (f tupleGrantFixture) AllowsAudience(agentskills.SkillPin, Audience) (bool, error) {
	return f.ok, f.err
}

func TestTodo_AGENTP_018_GrantTupleMatcherPreventsCartesianWidening(t *testing.T) {
	profile, catalog := personaFixture(t)
	matcher := tupleGrantFixture{ok: false, grant: SkillAudienceGrant{
		Roles: []string{"manager", "auditor"}, Populations: []string{"employees", "contractors"}, OrganizationScopes: []string{"org-west", "org-east"},
	}}
	validator := personaValidator(catalog)
	validator.Grants = matcher
	profile.Audience = Audience{
		Roles: []string{"manager", "auditor"}, Populations: []string{"employees", "contractors"}, OrganizationScopes: []string{"org-west", "org-east"},
	}
	if _, err := validator.Validate(profile); !errors.Is(err, ErrAudience) {
		t.Fatalf("tuple-incomplete audience error = %v; want ErrAudience", err)
	}

	matcher.ok = true
	validator.Grants = matcher
	if _, err := validator.Validate(profile); err != nil {
		t.Fatalf("tuple-covered audience rejected: %v", err)
	}
}
