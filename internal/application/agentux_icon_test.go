package application

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentinvocationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func agentIconApplicationFixture(t *testing.T) (*agentpersonastore.Store, *agentpersonastore.TenantStore, context.Context, time.Time, *pgtest.DB) {
	t.Helper()
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	if err := agentpersonastore.MigrateIcons(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant(tenant_id) VALUES($1)`, id)
	conn := db.NewConn(t)
	if _, err := conn.Exec(ctx, "SET ROLE "+agentstore.AppRole); err != nil {
		t.Fatal(err)
	}
	store, err := agentpersonastore.New(conn, func(tenant values.TenantId) uuid.UUID {
		if tenant == "tenant-a" {
			return id
		}
		return uuid.Nil
	})
	if err != nil {
		t.Fatal(err)
	}
	scoped, err := store.Scoped("tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	ctx = trust.WithPrincipal(ctx, personaDraftPrincipal(t, "tenant-a", "user:owner", now))
	return store, scoped, ctx, now, db
}

func agentIconCreateApplicationDraft(t *testing.T, store PersonaDraftStore, ctx context.Context, now time.Time, id string) agentpersona.PersonaVersion {
	t.Helper()
	profile := personaDraftProfile()
	profile.PersonaID = id
	profile.DisplayName = "Policy Helper"
	sealed, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	service := &PersonaAdminDraftService{Store: store, Authorizer: &personaCreateAuthorizerFake{allowedTenant: "tenant-a"}, Profiles: personaProfileBuilderFake{}, Clock: personaDraftClockFake{now: now}}
	if receipt, err := service.CreateDraft(ctx, PersonaDraftRequest{Version: sealed, BusinessOwnerID: profile.Owner, TechnicalStewardID: profile.Steward}); err != nil || receipt.PersonaID != id {
		t.Fatal("draft", receipt, err)
	}
	return sealed
}

func TestAgentUXIcon_Generated(t *testing.T) {
	store, scoped, ctx, now, _ := agentIconApplicationFixture(t)
	agentIconCreateApplicationDraft(t, scoped, ctx, now, "direct")
	agentIconCreateApplicationDraft(t, personaAdminDraftStoreAdapter{store: store}, ctx, now, "new-agent")
	var first agenticon.Value
	for _, id := range []string{"direct", "new-agent"} {
		icon, err := scoped.GetIcon(ctx, id)
		if err != nil || !icon.Value.Valid() || icon.Revision != 1 {
			t.Fatal(id, icon, err)
		}
		if id == "direct" {
			if icon.Value.Glyph != "book" {
				t.Fatal("first policy agent lost its semantic glyph", icon)
			}
			first = icon.Value
		} else if icon.Value == first {
			t.Fatal("second creation path reused the first agent's identity", icon)
		}
	}
	// The template creation path delegates through the same authorized service.
	instructions := "Answer only using approved policy."
	manifest := personaStarterManifest(instructions)
	drafts := &PersonaAdminDraftService{Store: personaAdminDraftStoreAdapter{store: store}, Authorizer: &personaCreateAuthorizerFake{allowedTenant: "tenant-a"}, Profiles: &personaStarterProfileBuilderSpy{}, Clock: personaDraftClockFake{now: now}}
	builder := &PersonaStarterDraftBuilder{Drafts: drafts, Manifests: &personaStarterManifestFake{manifest: manifest}, Instructions: &personaStarterInstructionsFake{text: instructions}}
	req := validPersonaStarterRequest(manifest)
	req.PersonaID = "template"
	if _, err := builder.CreateDraft(ctx, req); err != nil {
		t.Fatal(err)
	}
	if icon, err := scoped.GetIcon(ctx, "template"); err != nil || !icon.Value.Valid() || icon.Revision != 1 {
		t.Fatal("template icon", icon, err)
	}
	fake := &personaDraftStoreFake{}
	agentIconCreateApplicationDraft(t, fake, ctx, now, "port-fixture")
	if fake.calls != 1 {
		t.Fatal("legacy port delegation", fake.calls)
	}
}

func TestAgentUXIcon_Generated_Integration(t *testing.T) {
	store, scoped, ctx, now, db := agentIconApplicationFixture(t)
	agentIconCreateApplicationDraft(t, scoped, ctx, now, "commands")
	surface := &AgentIconSurface{Store: store, Now: func() time.Time { return now }}
	command := transport.AgentIconCommand{PersonaID: "commands", ExpectedRevision: 1, Action: "shuffle"}
	preview, err := surface.PreviewAgentIcon(ctx, command)
	if err != nil || preview.Revision != 1 {
		t.Fatal("preview", preview, err)
	}
	changed, err := surface.ChangeAgentIcon(ctx, command)
	if err != nil || changed.Revision != 2 || changed.Icon != preview.Icon {
		t.Fatal("command", changed, err)
	}
	if _, err = surface.ChangeAgentIcon(ctx, command); !errors.Is(err, transport.ErrAgentIconConflict) {
		t.Fatal("replayed command", err)
	}
	command.ExpectedRevision = 2
	command.Action = "regenerate"
	if reply, err := surface.ChangeAgentIcon(ctx, command); err != nil || reply.Icon.Glyph != "book" || reply.Revision != 3 {
		t.Fatal("regenerate", reply, err)
	}
	command.ExpectedRevision = 3
	command.Action = "reset"
	if reply, err := surface.ChangeAgentIcon(ctx, command); err != nil || reply.Revision != 4 {
		t.Fatal("reset", reply, err)
	}
	var count int
	if err = db.SQL.QueryRow(`SELECT count(*) FROM persona_icon_events WHERE persona_id='commands'`).Scan(&count); err != nil || count != 4 {
		t.Fatal("audit", count, err)
	}
	if count, err := scoped.BackfillIcons(ctx, "preparation", now); err != nil || count != 0 {
		t.Fatal("created agents already have icons", count, err)
	}
	if !(LocalAgentDemoSummary{IconsSet: 3}).Changed() || (LocalAgentDemoSummary{}).Changed() {
		t.Fatal("backfill receipt change detection")
	}
}

func TestAgentUXIcon_Generated_Security(t *testing.T) {
	store, scoped, ctx, now, _ := agentIconApplicationFixture(t)
	agentIconCreateApplicationDraft(t, scoped, ctx, now, "secure")
	surface := &AgentIconSurface{Store: store, Now: func() time.Time { return now }}
	command := transport.AgentIconCommand{PersonaID: "secure", ExpectedRevision: 1, Action: "shuffle"}
	for _, tc := range []struct {
		ctx  context.Context
		want error
	}{{context.Background(), transport.ErrAgentIconUnauthenticated}, {nil, transport.ErrAgentIconUnauthenticated}, {trust.WithPrincipal(context.Background(), personaDraftPrincipal(t, "tenant-a", "intruder", now)), transport.ErrAgentIconDenied}, {trust.WithPrincipal(context.Background(), personaDraftPrincipal(t, "tenant-b", "user:owner", now)), transport.ErrAgentIconDenied}} {
		if _, err := surface.ChangeAgentIcon(tc.ctx, command); !errors.Is(err, tc.want) {
			t.Fatal("admission", err, tc.want)
		}
	}
	surface.Now = func() time.Time { return now.Add(2 * time.Hour) }
	if _, err := surface.ChangeAgentIcon(ctx, command); !errors.Is(err, transport.ErrAgentIconUnauthenticated) {
		t.Fatal("expired principal", err)
	}
	surface.Now = func() time.Time { return now }
	command.Action = "upload"
	if _, err := surface.ChangeAgentIcon(ctx, command); !errors.Is(err, transport.ErrAgentIconInvalid) {
		t.Fatal("invalid action", err)
	}
	if current, err := scoped.GetIcon(ctx, "secure"); err != nil || current.Revision != 1 {
		t.Fatal("rejected command mutated state", current, err)
	}
	if err := (AgentIconRoleAdministrator{}).AuthorizeAgentIconAdministrator(ctx, "tenant-a", "intruder"); !errors.Is(err, agentpersonastore.ErrIconDenied) {
		t.Fatal("forged admin", err)
	}
	if err := (AgentIconRoleAdministrator{}).AuthorizeAgentIconAdministrator(ctx, "tenant-a", "user:owner"); !errors.Is(err, agentpersonastore.ErrIconDenied) {
		t.Fatal("unwired admin", err)
	}
	adminContext, admin := personaCatalogRoleContext(t)
	adminContext = trust.WithPrincipal(adminContext, admin)
	roles := &personaCatalogRoleStore{snapshot: roleaccess.Snapshot{PagePermissions: []roleaccess.PagePermission{{RoleID: "hcm_admin", PageID: string(productui.PagePersonaAdmin), View: true, Update: true}}}}
	authority := AgentIconRoleAdministrator{Roles: roles}
	if err := authority.AuthorizeAgentIconAdministrator(adminContext, admin.Tenant(), admin.Subject()); err != nil {
		t.Fatal("current administrator", err)
	}
	roles.snapshot.PagePermissions = nil
	if err := authority.AuthorizeAgentIconAdministrator(adminContext, admin.Tenant(), admin.Subject()); !errors.Is(err, agentpersonastore.ErrIconDenied) || roles.calls != 2 {
		t.Fatal("revoked administrator", err, roles.calls)
	}
	if _, err := (&AgentIconSurface{}).ChangeAgentIcon(ctx, command); !errors.Is(err, transport.ErrAgentIconUnavailable) {
		t.Fatal("unwired surface", err)
	}
	if agentIconError(errors.New("secret database details")) != transport.ErrAgentIconUnavailable {
		t.Fatal("internal error leaked")
	}
	if err := agentIconError(agentpersonastore.ErrNotFound); !errors.Is(err, transport.ErrAgentIconDenied) {
		t.Fatal("identity existence leaked")
	}
}

func TestAgentUXIcon_Generated_Property(t *testing.T) {
	store, scoped, ctx, now, _ := agentIconApplicationFixture(t)
	sealed := agentIconCreateApplicationDraft(t, scoped, ctx, now, "projection")
	base := catalogVersions{{Profile: sealed, Lifecycle: agentpersona.StateDraft, Owner: sealed.Profile.Owner, Steward: sealed.Profile.Steward}}
	reader := AgentIconCatalogVersions{Base: base, Store: store}
	versions, err := reader.ListPersonaCatalogVersions(ctx, "tenant-a")
	icon, iconErr := scoped.GetIcon(ctx, "projection")
	if err != nil || iconErr != nil || len(versions) != 1 || versions[0].Icon != icon.Value || versions[0].IconRevision != 1 {
		t.Fatal("catalog projection", versions, err, iconErr)
	}
	if base[0].Icon.Valid() {
		t.Fatal("decorator mutated base")
	}
	svc := &PersonaAdminCatalogService{Versions: reader, Installations: catalogInstalls{}, Targets: catalogTargets{}, Skills: agentIconCatalogSkills{}, Grants: &catalogGrants{allowed: true}, Authorizer: &catalogAuth{}}
	snapshot, err := svc.Snapshot(ctx, productui.PersonaAdminSnapshotRequest{TenantID: "tenant-a", Principal: "user:owner"})
	if err != nil || len(snapshot.Personas) != 1 || snapshot.Personas[0].Icon != icon.Value || snapshot.Personas[0].IconRevision != 1 {
		t.Fatal("page catalog icon fields", snapshot, err)
	}
	if history, err := reader.ListPersonaAdminVersionHistory(ctx, "tenant-a"); err != nil || len(history) != 0 {
		t.Fatal("absent history", history, err)
	}
	if _, err := reader.ListPersonaCatalogVersions(ctx, "tenant-b"); !errors.Is(err, ErrPersonaCatalogDenied) {
		t.Fatal("tenant binding", err)
	}
	if _, err := (AgentIconCatalogVersions{}).ListPersonaCatalogVersions(ctx, "tenant-a"); !errors.Is(err, ErrPersonaCatalogDenied) {
		t.Fatal("unwired reader", err)
	}
	projection := AgentIconProjection{Store: store}
	mention := chatui.ResolvedPersonaMention{Reference: chatui.ChatReference{Kind: "AGENT_MENTION", TenantID: "tenant-a", ID: "agent:projection", Display: "Policy Helper"}}
	if err := projection.Mention(ctx, "tenant-a", "projection", &mention); err != nil || mention.Icon != icon.Value {
		t.Fatal("mention projection", mention, err)
	}
	actor := chatui.PersonaActor{Trusted: true, PersonaID: "projection", AgentID: "agent:projection"}
	if err := projection.Actor(ctx, "tenant-a", &actor); err != nil || actor.Icon != icon.Value || actor.IconRevision != 1 {
		t.Fatal("message author projection", actor, err)
	}
	agent := productui.AgentSummary{ID: "projection", Name: "Policy Helper"}
	if err := projection.Agent(ctx, "tenant-a", "projection", &agent); err != nil || agent.Icon != icon.Value {
		t.Fatal("personal agent projection", agent, err)
	}
	if err := projection.Actor(ctx, "tenant-a", &chatui.PersonaActor{PersonaID: "projection"}); !errors.Is(err, ErrPersonaCatalogDenied) {
		t.Fatal("untrusted actor", err)
	}
	if err := projection.Mention(ctx, "tenant-b", "projection", &mention); !errors.Is(err, ErrPersonaCatalogDenied) {
		t.Fatal("cross tenant mention", err)
	}
	if err := projection.Agent(context.Background(), "tenant-a", "projection", &agent); !errors.Is(err, ErrPersonaCatalogDenied) {
		t.Fatal("missing auth", err)
	}
}

func TestAgentUXIcon_Generated_Browser(t *testing.T) {
	store, scoped, _, now, _ := agentIconApplicationFixture(t)
	base, ctx, room, _, _ := personaSurfaceFixture(t)
	principal, _ := trust.FromContext(ctx)
	profiles, err := base.Personas.ListAvailable(ctx, principal)
	if err != nil {
		t.Fatal(err)
	}
	// Persist the same admitted persona whose canonical chat reference is in the fixture.
	profile := profiles[0].Profile
	profileRaw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	row := agentpersonastore.PersonaVersion{TenantID: "tenant-a", PersonaID: profile.PersonaID, Version: int64(profile.Version), AgentVersion: "agent-v1", Handle: profile.Handle, DisplayName: profile.DisplayName, Profile: profileRaw, ContentDigest: profiles[0].Digest, CreatedAt: now}
	owner := agentpersonastore.PersonaOwner{TenantID: "tenant-a", PersonaID: profile.PersonaID, Role: agentpersonastore.BusinessOwner, PrincipalID: profile.Owner}
	steward := agentpersonastore.PersonaOwner{TenantID: "tenant-a", PersonaID: profile.PersonaID, Role: agentpersonastore.TechnicalSteward, PrincipalID: profile.Steward}
	if err := scoped.CreateDraftWithIcon(ctx, row, owner, steward, "user-a", now); err != nil {
		t.Fatal(err)
	}
	room.posts = []chat.Post{{ID: "answer", TenantID: "tenant-a", ConversationID: "channel-a", AuthorID: profile.PersonaID, Revision: 1}}
	base.Receipts = &personaSurfaceReceiptFixture{receipts: []agentinvocationstore.ReplyReceipt{{TenantID: "tenant-a", ConversationID: "channel-a", InvokerID: "user-a", PersonaID: profile.PersonaID, AgentID: "agent:coach", Display: profile.DisplayName, PublicPostID: "answer", PersonaVersion: "1"}}}
	source := AgentIconChatDirectory{Base: base, Store: store}
	directory, err := source.DirectoryAgentIcons(ctx, "channel-a")
	if err != nil || len(directory.Personas) != 1 || !directory.Personas[0].Icon.Valid() || len(directory.PostActors) != 1 || directory.PostActors[0].Icon != directory.Personas[0].Icon {
		t.Fatal("chat directory", directory, err)
	}
	w := httptest.NewRecorder()
	request := httptest.NewRequest("GET", personachat.Path+"?conversation_id=channel-a", nil).WithContext(ctx)
	transport.AgentIconDirectoryHandler{Source: source}.ServeHTTP(w, request)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"icon_revision":1`) || strings.Contains(w.Body.String(), "instructions") || strings.Contains(w.Body.String(), "secret-comp") {
		t.Fatal("directory payload", w.Code, w.Body.String())
	}
	admission, bearer := integrate1Admission(t, "tenant-a", "user-a", base.Now)
	handler := OverlayPersonaChatSurface(http.NotFoundHandler(), base, admission, PersonaChatBrowserOptions{Icons: store})
	w = httptest.NewRecorder()
	request = httptest.NewRequest("GET", personachat.Path+"?conversation_id=channel-a", nil)
	request.Header.Set("Authorization", bearer)
	handler.ServeHTTP(w, request)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"icon_revision":1`) {
		t.Fatal("served icon directory decorator missing", w.Code, w.Body)
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", personachat.Path+"?conversation_id=channel-a", nil))
	if w.Code != 401 {
		t.Fatal("unadmitted icon directory", w.Code)
	}
	markup, err := ui.RenderToString(agenticon.Node(directory.Personas[0].Icon))
	if err != nil || !strings.Contains(markup, `aria-hidden="true"`) || !strings.Contains(markup, "var(--hcm-color-") {
		t.Fatal("SSR icon", markup, err)
	}
	if _, err := source.DirectoryAgentIcons(context.Background(), "channel-a"); !errors.Is(err, personachat.ErrUnauthenticated) {
		t.Fatal("directory authentication", err)
	}
	room.members = nil
	if _, err := source.DirectoryAgentIcons(ctx, "channel-a"); !errors.Is(err, personachat.ErrDenied) {
		t.Fatal("revoked room still projects icons", err)
	}
}

type agentIconCatalogSkills struct{}

func (agentIconCatalogSkills) ResolvePin(pin agentskills.SkillPin) (agentskills.SkillRecord, error) {
	return agentskills.SkillRecord{Definition: agentskills.SkillDefinition{ID: pin.ID, Version: pin.Version, Description: "Read policy", SideEffectTier: agentskills.TierRead}, Digest: pin.Digest, Status: agentskills.StatusActive}, nil
}
