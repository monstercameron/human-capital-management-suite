package agentgate

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

func projectionRequest(f gateFixture) DiscoveryRequest {
	return DiscoveryRequest{
		User: f.user, Purpose: testPurpose, Subjects: []Subject{f.subject},
		Fields: []authz.FieldID{authz.FieldWorkerNumber}, At: f.now,
	}
}

func publishDisjointSkill(t *testing.T, f gateFixture) agentskills.SkillKey {
	t.Helper()
	def := agentskills.SkillDefinition{
		ID: "hcmnext.skill.other_state", Version: 1, Owner: "people", Description: "Read another state.",
		InputSchema: []byte(`{"type":"object"}`), OutputSchema: []byte(`{"type":"object"}`),
		Operations:     []agentskills.OperationRef{{Kind: agentskills.OperationCapability, Capability: testCapability().Key()}},
		SideEffectTier: agentskills.TierRead, RequiredPurposes: []string{testPurpose}, IdempotencyRule: "read-only", CostClass: "LOW",
	}
	if err := f.skills.Publish(def); err != nil {
		t.Fatalf("publish disjoint skill: %v", err)
	}
	return def.Key()
}

func TestTodo_AGENT2_005_SkillProjection(t *testing.T) {
	f := newGateFixture(t)
	projection, err := f.gate.ProjectSkillAuthorization(context.Background(), projectionRequest(f), f.key)
	if err != nil {
		t.Fatalf("project skill authorization: %v", err)
	}
	if projection.Skill.Definition.Key() != f.key || projection.Grant.ID != "grant-manager" || projection.Purpose != testPurpose {
		t.Fatalf("projection identity = %+v", projection)
	}
	if len(projection.Capabilities) != 1 || projection.Capabilities[0].Capability != testCapability().Key() {
		t.Fatalf("projection capabilities = %+v", projection.Capabilities)
	}
	if len(projection.Capabilities[0].Subjects) != 1 || projection.Capabilities[0].Subjects[0].Fields[authz.FieldWorkerNumber] != authz.EffectAllow {
		t.Fatalf("projection field ruling = %+v", projection.Capabilities[0].Subjects)
	}
	if len(f.pdp.calls) != 1 || f.pdp.calls[0].Skill != f.key || f.pdp.calls[0].Purpose != testPurpose {
		t.Fatalf("projection PDP calls = %+v", f.pdp.calls)
	}
}

func TestTodo_AGENT2_005_SkillProjectionSecurity(t *testing.T) {
	cases := []struct {
		name string
		edit func(*gateFixture, *DiscoveryRequest, agentskills.SkillKey)
		key  func(gateFixture, agentskills.SkillKey) agentskills.SkillKey
		want DenialCode
	}{
		{
			name: "disjoint skill grant is evaluated independently",
			edit: func(f *gateFixture, _ *DiscoveryRequest, other agentskills.SkillKey) {
				f.grants.grants = append(f.grants.grants, SkillGrant{ID: "grant-other", Tenant: testTenant, Skill: other, Roles: []string{"employee"}, Population: "managers", OrganizationScopes: []string{testOrg}, Purposes: []string{testPurpose}})
			},
			key: func(f gateFixture, _ agentskills.SkillKey) agentskills.SkillKey {
				return publishDisjointSkill(t, f)
			},
			want: DenyRole,
		},
		{
			name: "revoked grant is rechecked",
			edit: func(f *gateFixture, _ *DiscoveryRequest, _ agentskills.SkillKey) {
				f.grants.grants[0].Roles = []string{"employee"}
			},
			key:  func(_ gateFixture, key agentskills.SkillKey) agentskills.SkillKey { return key },
			want: DenyRole,
		},
		{
			name: "consent is rechecked",
			edit: func(f *gateFixture, _ *DiscoveryRequest, _ agentskills.SkillKey) {
				f.grants.grants[0].ConsentRequired = true
			},
			key:  func(_ gateFixture, key agentskills.SkillKey) agentskills.SkillKey { return key },
			want: DenyConsent,
		},
		{
			name: "field ruling is required",
			edit: func(f *gateFixture, req *DiscoveryRequest, _ agentskills.SkillKey) {
				req.Fields = []authz.FieldID{authz.FieldBaseSalary}
				f.pdp.decision.Subjects[0].Fields = map[authz.FieldID]authz.Effect{authz.FieldBaseSalary: authz.EffectDenied}
			},
			key:  func(_ gateFixture, key agentskills.SkillKey) agentskills.SkillKey { return key },
			want: DenyField,
		},
		{
			name: "purpose remains bound to the skill call",
			edit: func(_ *gateFixture, req *DiscoveryRequest, _ agentskills.SkillKey) { req.Purpose = "workforce:export" },
			key:  func(_ gateFixture, key agentskills.SkillKey) agentskills.SkillKey { return key },
			want: DenyPurpose,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newGateFixture(t)
			req := projectionRequest(f)
			other := agentskills.SkillKey{}
			if tc.name == "disjoint skill grant is evaluated independently" {
				other = agentskills.SkillKey{ID: "hcmnext.skill.other_state", Version: 1}
			}
			tc.edit(&f, &req, other)
			key := tc.key(f, f.key)
			if _, err := f.gate.ProjectSkillAuthorization(context.Background(), req, key); deniedCode(t, err) != tc.want {
				t.Fatalf("projection error = %v, want %s", err, tc.want)
			}
		})
	}
}

func TestTodo_AGENT2_005_SkillProjectionRejectsWildcards(t *testing.T) {
	f := newGateFixture(t)
	req := projectionRequest(f)
	req.Subjects = nil
	if _, err := f.gate.ProjectSkillAuthorization(context.Background(), req, f.key); deniedCode(t, err) != DenyInvalid {
		t.Fatalf("empty subject projection error = %v", err)
	}
	req = projectionRequest(f)
	req.Fields = nil
	if _, err := f.gate.ProjectSkillAuthorization(context.Background(), req, f.key); deniedCode(t, err) != DenyInvalid {
		t.Fatalf("empty field projection error = %v", err)
	}
}
