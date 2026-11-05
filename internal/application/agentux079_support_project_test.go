package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/application/projectservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// agentUX079FakeProjectProvision is the project database behind proof 2: it
// remembers one project per ID, so a second preparation replays the first.
type agentUX079FakeProjectProvision struct {
	projects map[string]projectservice.ProjectRecord
	creates  int
}

func (f *agentUX079FakeProjectProvision) CreateProject(_ context.Context, p projectservice.ProjectRecord, actor, key string) (projectservice.ProjectRecord, error) {
	if f.projects == nil {
		f.projects = map[string]projectservice.ProjectRecord{}
	}
	if existing, ok := f.projects[p.ID]; ok {
		return existing, nil
	}
	if p.ID == "" || p.TenantID == "" || actor == "" || key == "" {
		return projectservice.ProjectRecord{}, errors.New("invalid project")
	}
	f.projects[p.ID] = p
	f.creates++
	return p, nil
}

func (f *agentUX079FakeProjectProvision) GetProject(_ context.Context, tenant, project string) (projectservice.ProjectRecord, error) {
	if p, ok := f.projects[project]; ok && p.TenantID == tenant {
		return p, nil
	}
	return projectservice.ProjectRecord{}, errAgentUX079FakeProjectNotFound
}

var errAgentUX079FakeProjectNotFound = errors.New("project not found")

// agentUX079FakeGrantLedger is the support grant authority ledger behind proof
// 2, with the store's revision rule: the first grant is revision 1 and a
// repeat of revision 1 is a replay.
type agentUX079FakeGrantLedger struct {
	allowed map[string]bool
	appends int
}

func (f *agentUX079FakeGrantLedger) SupportProjectCreateAllowed(_ context.Context, _ uuid.UUID, subject, project string) (bool, error) {
	return f.allowed[subject+"\x00"+project], nil
}

func (f *agentUX079FakeGrantLedger) AppendSupportProjectGrant(_ context.Context, r agentstore.SupportProjectGrant) error {
	if f.allowed == nil {
		f.allowed = map[string]bool{}
	}
	key := r.ServiceSubject + "\x00" + r.ProjectID
	if f.allowed[key] || r.Revision != 1 {
		return agentstore.ErrSupportInboxReplay
	}
	f.allowed[key] = r.Active
	f.appends++
	return nil
}

// agentUX079FakeProjectResolver is the served project state behind proof 2.
type agentUX079FakeProjectResolver struct {
	info AgentUX079SupportProjectInfo
	err  error
}

func (f agentUX079FakeProjectResolver) ResolveSupportProject(_ context.Context, tenant string) (AgentUX079SupportProjectInfo, error) {
	if f.err != nil || tenant == "" {
		return AgentUX079SupportProjectInfo{}, f.err
	}
	return f.info, nil
}

func agentUX079TestDeps(projects *agentUX079FakeProjectProvision, grants *agentUX079FakeGrantLedger, now time.Time) AgentUX079SupportProjectDeps {
	return AgentUX079SupportProjectDeps{
		Projects: projects, Grants: grants,
		TenantUUID: func(values.TenantId) uuid.UUID { return uuid.NewSHA1(uuid.NameSpaceOID, []byte("agentux079-test")) },
		Tenant:     localAgentDemoTenant, Admin: localAgentDemoAdmin,
		Now: func() time.Time { return now },
	}
}

// TestAgentUX079_SupportProjectPrepared: preparation creates the "Customer
// support" project and the service grant once; a second run changes nothing.
func TestAgentUX079_SupportProjectPrepared(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	projects, grants := &agentUX079FakeProjectProvision{}, &agentUX079FakeGrantLedger{}
	receipt, err := ensureAgentUX079SupportProject(context.Background(), agentUX079TestDeps(projects, grants, now))
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Checked || !receipt.Created || !receipt.GrantRecorded || receipt.Reason != "" {
		t.Fatalf("first prepare = %+v, want checked and created with no reason", receipt)
	}
	stored, err := projects.GetProject(context.Background(), localAgentDemoTenant, AgentUX079SupportProjectID)
	if err != nil || stored.Name != AgentUX079SupportProjectName || stored.OwnerID != localAgentDemoAdmin {
		t.Fatalf("stored project = %+v, err = %v, want the Customer support project owned by the demo administrator", stored, err)
	}
	allowed, err := grants.SupportProjectCreateAllowed(context.Background(), uuid.New(), AgentUX079SupportServiceSubject, AgentUX079SupportProjectID)
	if err != nil || !allowed {
		t.Fatalf("grant allowed = %t, err = %v, want the service allowed to create tickets", allowed, err)
	}
	rerun, err := ensureAgentUX079SupportProject(context.Background(), agentUX079TestDeps(projects, grants, now))
	if err != nil {
		t.Fatal(err)
	}
	if !rerun.Checked || rerun.Created || rerun.GrantRecorded || rerun.Reason != "" {
		t.Fatalf("second prepare = %+v, want checked with no change", rerun)
	}
	if projects.creates != 1 || grants.appends != 1 {
		t.Fatalf("creates = %d, appends = %d, want one of each", projects.creates, grants.appends)
	}
}

// TestAgentUX079_SupportProjectUnconfigured: without a project database the
// preparation records the plain reason and fails nothing.
func TestAgentUX079_SupportProjectUnconfigured(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	grants := &agentUX079FakeGrantLedger{}
	deps := agentUX079TestDeps(nil, grants, now)
	deps.Projects = nil
	receipt, err := ensureAgentUX079SupportProject(context.Background(), deps)
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Checked || receipt.Created || receipt.GrantRecorded || receipt.Reason == "" {
		t.Fatalf("unconfigured prepare = %+v, want checked with a plain reason and no change", receipt)
	}
	if !strings.Contains(receipt.Reason, "project database is not configured") {
		t.Fatalf("reason %q does not say the project database is not configured", receipt.Reason)
	}
	if grants.appends != 0 {
		t.Fatalf("appends = %d, want no grant without a project database", grants.appends)
	}
}

// TestAgentUX079_SupportTargetsResolve: the served target source resolves the
// prepared project, the incident channel, the grant and a fresh service
// identity; anything missing or revoked is denied, and a missing project
// database says so plainly.
func TestAgentUX079_SupportTargetsResolve(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	tenantID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("agentux079-targets"))
	newTargets := func(resolver AgentUX079SupportProjectResolver, grants *agentUX079FakeGrantLedger) AgentUX079SupportTargets {
		return AgentUX079SupportTargets{
			TenantUUID: func(values.TenantId) uuid.UUID { return tenantID },
			Projects:   resolver, Grants: grants,
			Now: func() time.Time { return now },
		}
	}
	resolver := agentUX079FakeProjectResolver{info: AgentUX079SupportProjectInfo{ProjectID: AgentUX079SupportProjectID, InitialStatusID: "support_new", WorkflowRevision: 1}}
	grants := &agentUX079FakeGrantLedger{allowed: map[string]bool{AgentUX079SupportServiceSubject + "\x00" + AgentUX079SupportProjectID: true}}

	targets, err := newTargets(resolver, grants).SupportTargets(context.Background(), localAgentDemoTenant)
	if err != nil {
		t.Fatal(err)
	}
	if targets.TenantID != tenantID || targets.ProjectID != AgentUX079SupportProjectID || targets.InitialStatusID != "support_new" || targets.WorkflowRevision != 1 {
		t.Fatalf("targets = %+v, want the prepared project and its workflow", targets)
	}
	if want := localDevPersonaDemoConversationID(localAgentDemoTenant, localAgentDemoSupportRoom); targets.ConversationID != want {
		t.Fatalf("conversation = %q, want %q", targets.ConversationID, want)
	}
	principal := targets.Principal
	if principal == nil || principal.SubjectKind() != trust.SubjectKindService || principal.Subject() != AgentUX079SupportServiceSubject || !principal.AuthorizesPurpose("customer-support") || !now.Before(principal.ExpiresAt()) {
		t.Fatalf("principal = %+v, want a fresh service identity for customer-support", principal)
	}

	revoked := &agentUX079FakeGrantLedger{allowed: map[string]bool{}}
	if _, err := newTargets(resolver, revoked).SupportTargets(context.Background(), localAgentDemoTenant); !errors.Is(err, ErrAgentUXDemoSupportDenied) {
		t.Fatalf("revoked grant err = %v, want denied", err)
	}
	broken := agentUX079FakeProjectResolver{err: errors.New("project store unavailable")}
	if _, err := newTargets(broken, grants).SupportTargets(context.Background(), localAgentDemoTenant); !errors.Is(err, ErrAgentUXDemoSupportDenied) {
		t.Fatalf("broken project store err = %v, want denied", err)
	}
	if _, err := newTargets(resolver, grants).SupportTargets(context.Background(), "another-tenant"); !errors.Is(err, ErrAgentUXDemoSupportDenied) {
		t.Fatalf("other tenant err = %v, want denied", err)
	}
	unconfigured := newTargets(resolver, grants)
	unconfigured.Projects = nil
	err = nil
	if _, err = unconfigured.SupportTargets(context.Background(), localAgentDemoTenant); !errors.Is(err, ErrAgentUX079SupportProjectUnavailable) || !strings.Contains(err.Error(), "project database is not configured") {
		t.Fatalf("unconfigured err = %v, want the plain-words project-database reason", err)
	}
}
