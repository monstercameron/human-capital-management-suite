package agentaccess

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/agentconnect"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

type fakeAuthorizer map[string]bool

func (a fakeAuthorizer) CanAdminister(tenant, actor string) bool { return a[tenant+"/"+actor] }

type fakePublisher struct {
	published []string
	failWith  error
}

func (p *fakePublisher) Publish(revision Revision) error {
	if p.failWith != nil {
		return p.failWith
	}
	p.published = append(p.published, revision.ID())
	return nil
}

type fakeSnapshots struct{}

func (fakeSnapshots) Tools(tenant, snapshotID string) ([]MCPTool, error) {
	if tenant != "tenant-a" || snapshotID != "snap-1" {
		return nil, ErrInvalid
	}
	return []MCPTool{{Name: "workers.read", ReadOnly: true}, {Name: "workers.update"}}, nil
}

func newConsoleFixture(t *testing.T) (*Console, *fakePublisher) {
	t.Helper()
	publisher := &fakePublisher{}
	clock := time.Unix(1000, 0)
	console, err := NewConsole(fakeAuthorizer{"tenant-a/admin-one": true, "tenant-a/admin-two": true, "tenant-b/admin-one": true}, publisher, fakeSnapshots{}, func() time.Time { clock = clock.Add(time.Second); return clock })
	if err != nil {
		t.Fatal(err)
	}
	return console, publisher
}

func managersGrant(skills ...string) agentconnect.GrantScope {
	return agentconnect.GrantScope{ID: "managers", Roles: []string{"manager"}, Population: "employees", OrganizationScopes: []string{"org-a"}, Skills: skills}
}

func readDraft() Revision {
	return Revision{ConnectionID: "hris", Provider: "HRIS", CredentialMode: agentconnect.UserDelegated,
		Skills: []Skill{{ID: "workers.read", Tier: agentconnect.TierT0}, {ID: "promotions.submit", Tier: agentconnect.TierT3}},
		Grants: []agentconnect.GrantScope{managersGrant("workers.read")}}
}

// The console creates revisions, imports a snapshot, assigns tiers and
// grants, previews a person's effective skills, and publishes only what
// separation of duties allows.
func TestTodo_AGENT2_019(t *testing.T) {
	console, publisher := newConsoleFixture(t)
	draft, err := console.Create("tenant-a", "admin-one", readDraft())
	if err != nil || draft.Number != 1 || draft.Status != StatusDraft {
		t.Fatalf("create = %+v, %v", draft, err)
	}
	if draft.RequiresSecondAdmin() {
		t.Fatal("a draft whose T3 skill is granted to nobody needs no second administrator")
	}
	// Publishing a read-only revision needs no second administrator.
	if _, err := console.Publish("tenant-a", "admin-one", draft.ID()); err != nil {
		t.Fatalf("publish a read-only revision = %v", err)
	}

	// A second revision imports a tool snapshot: the unclassified tool comes in
	// as T4, the read-only one as T0.
	second, err := console.Create("tenant-a", "admin-one", Revision{ConnectionID: "hris", Provider: "HRIS", CredentialMode: agentconnect.UserDelegated})
	if err != nil || second.Number != 2 {
		t.Fatalf("second revision = %+v, %v", second, err)
	}
	second, err = console.ImportSnapshot("tenant-a", "admin-one", "hris", "snap-1")
	if err != nil {
		t.Fatal(err)
	}
	tiers := map[string]agentconnect.SideEffectTier{}
	for _, skill := range second.Skills {
		tiers[skill.ID] = skill.Tier
	}
	if tiers["workers.read"] != agentconnect.TierT0 || tiers["workers.update"] != agentconnect.TierT4 || second.MCPSnapshotID != "snap-1" {
		t.Fatalf("imported tiers = %v", tiers)
	}
	second, err = console.SetTier("tenant-a", "admin-one", second.ID(), "workers.update", agentconnect.TierT1)
	if err != nil || second.Skills[1].Tier != agentconnect.TierT1 && second.Skills[0].Tier != agentconnect.TierT1 {
		t.Fatalf("set tier = %+v, %v", second, err)
	}

	// The preview answers what a given person's agent can do, before publishing.
	subject := agentconnect.UserContext{TenantID: "tenant-a", UserID: "ana", Roles: []string{"manager"}, Population: "employees", OrganizationScopes: []string{"org-a"}}
	_, err = console.Create("tenant-a", "admin-one", Revision{ConnectionID: "hris", Provider: "HRIS", CredentialMode: agentconnect.UserDelegated, Skills: second.Skills, Grants: []agentconnect.GrantScope{managersGrant("workers.read", "workers.update")}})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := console.Preview("tenant-a", "admin-one", "hris#3", subject)
	if err != nil || len(preview.Skills) != 2 || len(preview.Warnings) != 0 {
		t.Fatalf("preview = %+v, %v", preview, err)
	}
	outsider := subject
	outsider.Roles = []string{"employee"}
	preview, err = console.Preview("tenant-a", "admin-one", "hris#3", outsider)
	if err != nil || len(preview.Skills) != 0 {
		t.Fatalf("a person the grant does not name sees skills: %+v, %v", preview, err)
	}
	if len(publisher.published) != 1 {
		t.Fatalf("published = %v", publisher.published)
	}
}

// T3 and T4 grants and brokered credentials cannot be published without a
// different administrator's step-up approval of the exact content.
func TestTodo_AGENT2_019_Security(t *testing.T) {
	console, publisher := newConsoleFixture(t)
	risky := readDraft()
	risky.Grants = []agentconnect.GrantScope{managersGrant("workers.read", "promotions.submit")}
	draft, err := console.Create("tenant-a", "admin-one", risky)
	if err != nil || !draft.RequiresSecondAdmin() {
		t.Fatalf("a T3 grant must need a second administrator: %+v, %v", draft, err)
	}
	id := draft.ID()
	if _, err := console.Publish("tenant-a", "admin-one", id); !errors.Is(err, ErrSeparation) {
		t.Fatalf("publish without approval = %v", err)
	}
	if _, err := console.Approve("tenant-a", "admin-two", id, true); !errors.Is(err, ErrWrongState) {
		t.Fatalf("approving before it is requested = %v", err)
	}
	if _, err := console.RequestApproval("tenant-a", "admin-one", id); err != nil {
		t.Fatal(err)
	}
	if _, err := console.Approve("tenant-a", "admin-one", id, true); !errors.Is(err, ErrSeparation) {
		t.Fatalf("the author approved their own revision = %v", err)
	}
	if _, err := console.Approve("tenant-a", "admin-two", id, false); !errors.Is(err, ErrSeparation) {
		t.Fatalf("approval without step-up = %v", err)
	}
	if _, err := console.Publish("tenant-a", "admin-one", id); !errors.Is(err, ErrSeparation) {
		t.Fatalf("publish while approval is pending = %v", err)
	}
	// Changing the content after the request drops the request: the approval
	// the second administrator gives next cannot be for the old content.
	if _, err := console.SetTier("tenant-a", "admin-one", id, "promotions.submit", agentconnect.TierT4); err != nil {
		t.Fatal(err)
	}
	if _, err := console.Approve("tenant-a", "admin-two", id, true); !errors.Is(err, ErrWrongState) {
		t.Fatalf("approving an edited revision that was not re-requested = %v", err)
	}
	if _, err := console.RequestApproval("tenant-a", "admin-one", id); err != nil {
		t.Fatal(err)
	}
	if _, err := console.Approve("tenant-a", "admin-two", id, true); err != nil {
		t.Fatal(err)
	}
	if _, err := console.Publish("tenant-a", "admin-one", id); err != nil {
		t.Fatalf("publish after a second administrator's step-up approval = %v", err)
	}
	if len(publisher.published) != 1 {
		t.Fatalf("published = %v", publisher.published)
	}

	// Brokered credentials need the same, even with only reads.
	brokered := readDraft()
	brokered.ConnectionID, brokered.CredentialMode = "payroll", agentconnect.Brokered
	b, err := console.Create("tenant-a", "admin-one", brokered)
	if err != nil || !b.RequiresSecondAdmin() {
		t.Fatalf("brokered = %+v, %v", b, err)
	}
	if _, err := console.Publish("tenant-a", "admin-one", b.ID()); !errors.Is(err, ErrSeparation) {
		t.Fatalf("brokered publish without approval = %v", err)
	}

	// Someone who is not an administrator, or administers another tenant, can
	// do nothing and see nothing.
	for _, actor := range []string{"ana", "admin-two-of-another-tenant"} {
		if _, err := console.Create("tenant-a", actor, readDraft()); !errors.Is(err, ErrDenied) {
			t.Fatalf("%s created a revision: %v", actor, err)
		}
		if _, err := console.Revisions("tenant-a", actor); !errors.Is(err, ErrDenied) {
			t.Fatalf("%s listed revisions: %v", actor, err)
		}
	}
	if list, err := console.Revisions("tenant-b", "admin-one"); err != nil || len(list) != 0 {
		t.Fatalf("another tenant's administrator sees %v, %v", list, err)
	}
	if _, err := console.Preview("tenant-a", "admin-one", id, agentconnect.UserContext{TenantID: "tenant-b", UserID: "x"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("previewing a person of another tenant = %v", err)
	}
	// An imported tool never widens access by itself.
	imported, _ := console.Create("tenant-a", "admin-one", Revision{ConnectionID: "chat", CredentialMode: agentconnect.UserDelegated})
	imported, err = console.ImportSnapshot("tenant-a", "admin-one", "chat", "snap-1")
	if err != nil || len(imported.Grants) != 0 {
		t.Fatalf("an import added grants: %+v, %v", imported, err)
	}
	// Every state change is audited with who did it.
	trail := console.AuditTrail("tenant-a")
	actions := map[string]bool{}
	for _, event := range trail {
		actions[event.Action+"/"+event.Actor] = true
	}
	for _, want := range []string{"create/admin-one", "request-approval/admin-one", "approve/admin-two", "publish/admin-one"} {
		if !actions[want] {
			t.Errorf("audit trail lacks %q: %v", want, trail)
		}
	}
}

// Publication is revisioned and reversible: rolling back publishes the
// earlier content again as the newest revision and keeps the history.
func TestTodo_AGENT2_019_Golden(t *testing.T) {
	console, publisher := newConsoleFixture(t)
	first, _ := console.Create("tenant-a", "admin-one", readDraft())
	if _, err := console.Publish("tenant-a", "admin-one", first.ID()); err != nil {
		t.Fatal(err)
	}
	next := readDraft()
	next.Skills = append(next.Skills, Skill{ID: "workers.search", Tier: agentconnect.TierT0})
	second, _ := console.Create("tenant-a", "admin-one", next)
	if _, err := console.Publish("tenant-a", "admin-one", second.ID()); err != nil {
		t.Fatal(err)
	}
	rolled, err := console.Rollback("tenant-a", "admin-one", first.ID())
	if err != nil {
		t.Fatal(err)
	}
	if rolled.Number != 3 || len(rolled.Skills) != 2 || rolled.Status != StatusPublished {
		t.Fatalf("rollback = %+v", rolled)
	}
	list, _ := console.Revisions("tenant-a", "admin-one")
	var got []string
	for _, revision := range list {
		got = append(got, revision.ID()+"="+revision.Status)
	}
	want := "hris#3=PUBLISHED hris#2=SUPERSEDED hris#1=SUPERSEDED"
	if strings.Join(got, " ") != want {
		t.Fatalf("history = %q, want %q", strings.Join(got, " "), want)
	}
	if strings.Join(publisher.published, " ") != "hris#1 hris#2 hris#3" {
		t.Fatalf("publisher saw %v", publisher.published)
	}
	// Only a superseded revision can be rolled back to; the live one cannot.
	if _, err := console.Rollback("tenant-a", "admin-one", "hris#3"); !errors.Is(err, ErrWrongState) {
		t.Fatalf("rolling back to the live revision = %v", err)
	}
	// A publisher that fails leaves the live revision as it was.
	publisher.failWith = errors.New("registry unavailable")
	failing, _ := console.Create("tenant-a", "admin-one", readDraft())
	if _, err := console.Publish("tenant-a", "admin-one", failing.ID()); err == nil {
		t.Fatal("publish succeeded although the registry refused it")
	}
	list, _ = console.Revisions("tenant-a", "admin-one")
	if list[1].Status != StatusPublished && list[0].Status != StatusDraft {
		t.Fatalf("a failed publish changed the live revision: %v", list)
	}
}

// The page: a high-impact revision shows the second-administrator warning and
// no Publish button until it is approved; the preview for a chosen person is
// drawn from the service.
func TestTodo_AGENT2_019_Browser(t *testing.T) {
	console, _ := newConsoleFixture(t)
	risky := readDraft()
	risky.Grants = []agentconnect.GrantScope{managersGrant("workers.read", "promotions.submit")}
	draft, _ := console.Create("tenant-a", "admin-one", risky)
	session := console.For("tenant-a", "admin-one")
	subject := agentconnect.UserContext{TenantID: "tenant-a", UserID: "ana", Roles: []string{"manager"}, Population: "employees", OrganizationScopes: []string{"org-a"}}

	render := func(locale string) string {
		snapshot, err := session.Snapshot("Ana Flores", &subject, draft.ID())
		if err != nil {
			t.Fatal(err)
		}
		markup, err := ui.RenderToString(productui.AgentAdminAccessPage(productui.AgentAdminAccessPageProps{I18nProps: productui.I18nProps{Locale: productui.ResolveProductLocale(locale)}, State: productui.AgentAccessStateReady, Snapshot: snapshot, Client: session}))
		if err != nil {
			t.Fatal(err)
		}
		return strings.NewReplacer("&#39;", "'", "&amp;", "&").Replace(markup)
	}
	markup := render("en-US")
	for _, want := range []string{"Agent connections and grants", "HRIS", `data-approval-required="true"`, "Second-admin approval required", "Effective access preview", "Ana Flores", "promotions.submit", "Request second-admin approval"} {
		if !strings.Contains(markup, want) {
			t.Errorf("console missing %q:\n%s", want, markup)
		}
	}
	if strings.Contains(markup, "Publish revision") {
		t.Fatalf("a high-impact revision offered Publish before approval:\n%s", markup)
	}
	if _, err := console.RequestApproval("tenant-a", "admin-one", draft.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := console.Approve("tenant-a", "admin-two", draft.ID(), true); err != nil {
		t.Fatal(err)
	}
	if markup = render("de-DE"); strings.Contains(markup, "Agent connections and grants") || strings.Contains(markup, "Publish revision") {
		t.Fatalf("English text leaked into the German console:\n%s", markup)
	}
	if markup = render("en-US"); !strings.Contains(markup, "Publish revision") {
		t.Fatalf("an approved revision offers no Publish button:\n%s", markup)
	}
}
