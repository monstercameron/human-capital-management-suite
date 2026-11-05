package application

import (
	"net/http"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// TestTodo_AGENT2_019_PopulationPreviewServed: the console's population
// preview over the served route. Choosing a population answers which skills
// its members could reach, each with the roles and organization scopes that
// still gate it per member; choosing neither a user nor a population, or
// both, is refused; nobody who is not an administrator reaches it.
func TestTodo_AGENT2_019_PopulationPreviewServed(t *testing.T) {
	s := newAgentAccessServed(t)
	const console = AgentAccessPath + "/console"
	revision := s.publishConnection()
	var preview productui.AgentAdminAccessSnapshot
	s.ok(http.MethodPost, console+"/preview", "admin-one", agentAccessPreviewBody{RevisionID: revision, Population: "employees", Label: "Workforce"}, &preview)
	if !preview.PreviewReady || preview.PreviewSubject != "Workforce" || len(preview.Preview.Connections) != 1 {
		t.Fatalf("population preview = %+v", preview.Preview)
	}
	skills := preview.Preview.Connections[0].Skills
	if len(skills) != 2 || skills[0].ID != "promotions.submit" || skills[0].Tier != "T3_SUBMIT_GOVERNED" {
		t.Fatalf("skills = %+v, want both granted skills sorted with tiers", skills)
	}
	for _, role := range s.anaRoles() {
		if !strings.Contains(skills[0].Scope, role) {
			t.Fatalf("skill scope hides role %q: %q", role, skills[0].Scope)
		}
	}
	if !strings.Contains(skills[0].Scope, servedAccessOrgScope) {
		t.Fatalf("skill scope hides the organization scope: %q", skills[0].Scope)
	}
	if len(preview.Preview.Warnings) != 0 {
		t.Fatalf("a published and approved revision warns: %v", preview.Preview.Warnings)
	}
	// A draft that grants a T3 skill warns that publishing needs a second
	// administrator, before anything is published.
	var created productui.AgentAdminAccessSnapshot
	s.ok(http.MethodPost, console+"/create", "admin-one", agentAccessCreateBody{Revision: productui.AgentConnectionRevision{
		ID: "hris", Provider: "HRIS", CredentialMode: "USER_DELEGATED",
		Grants: []productui.AgentAdminSkillGrant{{SkillID: "workers.read", Tier: "T0"}, {SkillID: "promotions.submit", Tier: "T3"}},
	}}, &created)
	s.ok(http.MethodPost, console+"/grants", "admin-one", agentAccessGrantsBody{RevisionID: "hris#2", Grants: []agentAccessGrantBody{{ID: "staff", Roles: s.anaRoles(), Population: "employees", OrganizationScopes: []string{servedAccessOrgScope}, Skills: []string{"workers.read", "promotions.submit"}}}}, &created)
	var draft productui.AgentAdminAccessSnapshot
	s.ok(http.MethodPost, console+"/preview", "admin-one", agentAccessPreviewBody{RevisionID: "hris#2", Population: "employees"}, &draft)
	if !containsWarning(draft.Preview.Warnings, "second administrator") {
		t.Fatalf("a draft T3 grant does not warn: %v", draft.Preview.Warnings)
	}
	// Without a label the subject names the population.
	var defaulted productui.AgentAdminAccessSnapshot
	s.ok(http.MethodPost, console+"/preview", "admin-one", agentAccessPreviewBody{RevisionID: revision, Population: "employees"}, &defaulted)
	if defaulted.PreviewSubject != "Population employees" {
		t.Fatalf("defaulted subject = %q", defaulted.PreviewSubject)
	}
	// A population nobody granted matches nothing, which is a valid answer.
	var empty productui.AgentAdminAccessSnapshot
	s.ok(http.MethodPost, console+"/preview", "admin-one", agentAccessPreviewBody{RevisionID: revision, Population: "partners"}, &empty)
	if !empty.PreviewReady || len(empty.Preview.Connections[0].Skills) != 0 {
		t.Fatalf("unknown population = %+v", empty.Preview)
	}
	// Exactly one of a user or a population: neither and both are refused, as
	// is the wildcard alone.
	s.refused(http.StatusBadRequest, "invalid_request", http.MethodPost, console+"/preview", "admin-one", agentAccessPreviewBody{RevisionID: revision})
	s.refused(http.StatusBadRequest, "invalid_request", http.MethodPost, console+"/preview", "admin-one", agentAccessPreviewBody{RevisionID: revision, UserID: agentTestWorker, Population: "employees"})
	s.refused(http.StatusBadRequest, "invalid_request", http.MethodPost, console+"/preview", "admin-one", agentAccessPreviewBody{RevisionID: revision, Population: "*"})
	s.refused(http.StatusNotFound, "not_found", http.MethodPost, console+"/preview", "admin-one", agentAccessPreviewBody{RevisionID: "hris#99", Population: "employees"})
	for _, token := range []string{"ana", "owner", "stranger"} {
		s.refused(http.StatusForbidden, "permission_denied", http.MethodPost, console+"/preview", token, agentAccessPreviewBody{RevisionID: revision, Population: "employees"})
	}
	s.refused(http.StatusUnauthorized, "request_denied", http.MethodPost, console+"/preview", "", agentAccessPreviewBody{RevisionID: revision, Population: "employees"})
}
