package application

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	transportdocument "github.com/monstercameron/human-capital-management-suite/internal/transport/document"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type agentdocPersonaReaderFake struct {
	rows          []transportdocument.Preview
	err           error
	tenant, actor string
	ids           []string
	calls         int
}

func (f *agentdocPersonaReaderFake) GetDocumentPreviews(_ context.Context, tenant, actor string, ids []string) ([]transportdocument.Preview, error) {
	f.calls++
	f.tenant, f.actor, f.ids = tenant, actor, append([]string(nil), ids...)
	return append([]transportdocument.Preview(nil), f.rows...), f.err
}

func agentdocReference(id, label string) agentdocref.Reference {
	return agentdocref.Reference{DocumentID: id, VersionMode: agentdocref.ModePinned, PinnedVersion: 4, SectionAnchor: "policy", Label: label}
}

func TestTodo_AGENTDOC_002_PersonaAdminCommands(t *testing.T) {
	ctx, _ := personaAdminCommandContext(t)
	refs := []agentdocref.Reference{agentdocReference("doc-123e4567-e89b-42d3-a456-426614174000", "Leave policy")}
	reader := &agentdocPersonaReaderFake{rows: []transportdocument.Preview{{DocumentID: refs[0].DocumentID, Title: "Leave policy", Readable: true}}}
	authorizer := &personaAdminCommandAuthFake{}
	executor := &personaAdminCommandExecutorFake{}
	factory, err := NewPersonaAdminCommandFactoryWithDocumentReader(personaAdminCommandCatalogFake{}, authorizer, executor, reader)
	if err != nil {
		t.Fatal(err)
	}
	request := productui.PersonaAdminCommandRequest{Action: "CREATE_VERSION", PersonaID: "persona-a", StarterID: "hcmnext.persona_template.policy_helper", StarterVersion: 1, Handle: "policy-helper", AllowedChannels: []string{"PRIVATE"}}
	if err := factory.ExecutePersonaAdminCommandWithDocumentReferences(ctx, request, refs); err != nil {
		t.Fatalf("create version with readable document = %v", err)
	}
	edit := executor.command.StarterVersionEdit
	if authorizer.calls != 1 || reader.calls != 1 || executor.calls != 1 || reader.tenant != "tenant-a" || reader.actor == "" || edit == nil || !reflect.DeepEqual(edit.DocumentReferences, refs) {
		t.Fatalf("command flow: authorizer=%+v reader=%+v executor=%+v", authorizer, reader, executor)
	}
	refs[0].Label = "caller mutation"
	if edit.DocumentReferences[0].Label != "Leave policy" {
		t.Fatal("command references alias HTTP-owned memory")
	}
}

func TestTodo_AGENTDOC_002_SecurityPersonaAdminCommands(t *testing.T) {
	ctx, _ := personaAdminCommandContext(t)
	ref := agentdocReference("doc-123e4567-e89b-42d3-a456-426614174000", "Restricted policy")
	reader := &agentdocPersonaReaderFake{rows: []transportdocument.Preview{{DocumentID: ref.DocumentID}}}
	authorizer := &personaAdminCommandAuthFake{}
	executor := &personaAdminCommandExecutorFake{}
	factory, err := NewPersonaAdminCommandFactoryWithDocumentReader(personaAdminCommandCatalogFake{}, authorizer, executor, reader)
	if err != nil {
		t.Fatal(err)
	}
	request := productui.PersonaAdminCommandRequest{Action: "CREATE_VERSION", PersonaID: "persona-a", StarterID: "hcmnext.persona_template.policy_helper", StarterVersion: 1}
	err = factory.ExecutePersonaAdminCommandWithDocumentReferences(ctx, request, []agentdocref.Reference{ref})
	if !errors.Is(err, ErrPersonaDocumentUnreadable) || personaAdminCommandCode(err) != personaAdminCodeDocumentUnreadable {
		t.Fatalf("unreadable error = %v, code %q", err, personaAdminCommandCode(err))
	}
	if authorizer.calls != 1 || reader.calls != 1 || executor.calls != 0 {
		t.Fatalf("unreadable document crossed lifecycle boundary: auth=%d read=%d execute=%d", authorizer.calls, reader.calls, executor.calls)
	}

	plainAuthorizer := &personaAdminCommandAuthFake{}
	plainExecutor := &personaAdminCommandExecutorFake{}
	plainFactory, err := NewPersonaAdminCommandFactory(personaAdminCommandCatalogFake{}, plainAuthorizer, plainExecutor)
	if err != nil {
		t.Fatal(err)
	}
	invalid := ref
	invalid.Label = "Policy https://example.invalid"
	err = plainFactory.Execute(ctx, PersonaAdminCommand{Action: PersonaAdminCreateVersion, PersonaID: "persona-a", StarterVersionEdit: &PersonaStarterVersionRequest{StarterID: "hcmnext.persona_template.policy_helper", StarterVersion: 1, DocumentReferences: []agentdocref.Reference{invalid}}})
	if !errors.Is(err, agentdocref.ErrLabelURL) || personaAdminCommandCode(err) != personaAdminCodeInvalid || plainExecutor.calls != 0 {
		t.Fatalf("validation without document reader = %v, code %q, execute=%d", err, personaAdminCommandCode(err), plainExecutor.calls)
	}
}

func TestTodo_AGENTDOC_002_PersonaStarterVersion(t *testing.T) {
	instructions := "Answer tenant-wide policy questions only from approved records and cite support."
	manifest := personaStarterManifest(instructions)
	starter, _ := agenttemplate.PersonaStarterFor("hcmnext.persona_template.policy_helper", 1)
	currentProfile := personaStarterProfile(starter, validPersonaStarterRequest(manifest), manifest, instructions)
	currentProfile.DocumentReferences = []agentdocref.Reference{agentdocReference("doc-123e4567-e89b-42d3-a456-426614174000", "Old policy")}
	current, err := (&personaStarterProfileBuilderSpy{}).Build(currentProfile)
	if err != nil {
		t.Fatal(err)
	}
	builder := &PersonaStarterDraftBuilder{Drafts: &PersonaAdminDraftService{Profiles: &personaStarterProfileBuilderSpy{}}, Manifests: &personaStarterManifestFake{manifest: manifest}, Instructions: &personaStarterInstructionsFake{text: instructions}}
	kept, err := builder.BuildVersion(context.Background(), current, PersonaStarterVersionRequest{StarterID: starter.ID, StarterVersion: starter.Version, Version: 2})
	if err != nil || !reflect.DeepEqual(kept.Profile.DocumentReferences, current.Profile.DocumentReferences) {
		t.Fatalf("omitted references were not preserved: %#v, %v", kept.Profile.DocumentReferences, err)
	}
	cleared, err := builder.BuildVersion(context.Background(), current, PersonaStarterVersionRequest{StarterID: starter.ID, StarterVersion: starter.Version, Version: 2, DocumentReferences: []agentdocref.Reference{}})
	if err != nil || len(cleared.Profile.DocumentReferences) != 0 {
		t.Fatalf("explicit empty references did not clear: %#v, %v", cleared.Profile.DocumentReferences, err)
	}
	replacement := []agentdocref.Reference{agentdocReference("doc-223e4567-e89b-42d3-a456-426614174000", "New policy")}
	replaced, err := builder.BuildVersion(context.Background(), current, PersonaStarterVersionRequest{StarterID: starter.ID, StarterVersion: starter.Version, Version: 2, DocumentReferences: replacement})
	if err != nil || !reflect.DeepEqual(replaced.Profile.DocumentReferences, replacement) {
		t.Fatalf("replacement references = %#v, %v", replaced.Profile.DocumentReferences, err)
	}
}

func TestTodo_AGENTDOC_008_PersonaAdminCommands(t *testing.T) {
	ctx, _ := personaAdminCommandContext(t)
	ref := agentdocReference("doc-123e4567-e89b-42d3-a456-426614174000", "Travel policy")
	reader := &agentdocPersonaReaderFake{rows: []transportdocument.Preview{{DocumentID: ref.DocumentID, Title: "Travel policy", Readable: true}}}
	authorizer := &personaAdminCommandAuthFake{}
	executor := &personaAdminCommandExecutorFake{}
	factory, err := NewPersonaAdminCommandFactoryWithDocumentReader(personaAdminCommandCatalogFake{}, authorizer, executor, reader)
	if err != nil {
		t.Fatal(err)
	}
	guidance := "Follow {{doc:" + ref.DocumentID + "}}."
	draft := productui.PersonaAdminCommandRequest{Action: "CREATE_DRAFT", PersonaID: "persona-new", StarterID: "hcmnext.persona_template.policy_helper", StarterVersion: 1, AvatarRef: "avatar:policy", OrganizationScopes: []string{"org-a"}, ManifestID: "manifest-policy", BusinessOwnerID: "owner", TechnicalStewardID: "steward", Instructions: &guidance}
	if err := factory.ExecutePersonaAdminCommandWithDocumentReferences(ctx, draft, []agentdocref.Reference{ref}); err != nil {
		t.Fatalf("CREATE_DRAFT guidance = %v", err)
	}
	if executor.command.Starter == nil || executor.command.Starter.Guidance != guidance || !reflect.DeepEqual(executor.command.Starter.DocumentReferences, []agentdocref.Reference{ref}) {
		t.Fatalf("draft mapping = %+v", executor.command.Starter)
	}

	version := productui.PersonaAdminCommandRequest{Action: "CREATE_VERSION", PersonaID: "persona-a", StarterID: "hcmnext.persona_template.policy_helper", StarterVersion: 1, Handle: "policy-helper", AllowedChannels: []string{"PRIVATE"}, Instructions: &guidance}
	if err := factory.ExecutePersonaAdminCommandWithDocumentReferences(ctx, version, []agentdocref.Reference{ref}); err != nil {
		t.Fatalf("CREATE_VERSION guidance = %v", err)
	}
	if executor.command.StarterVersionEdit == nil || executor.command.StarterVersionEdit.Guidance == nil || *executor.command.StarterVersionEdit.Guidance != guidance {
		t.Fatalf("version mapping = %+v", executor.command.StarterVersionEdit)
	}
}

func TestTodo_AGENTDOC_008_CreateDraftStoresGuidance(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	instructions := "Answer policy questions only from approved records."
	manifest := personaStarterManifest(instructions)
	ref := agentdocReference("doc-123e4567-e89b-42d3-a456-426614174000", "Travel policy")
	guidance := "Follow {{doc:" + ref.DocumentID + "}}."
	store := &personaDraftStoreFake{}
	drafts := &PersonaAdminDraftService{Store: store, Authorizer: &personaCreateAuthorizerFake{allowedTenant: "tenant-a"}, Profiles: &personaStarterProfileBuilderSpy{}, Clock: personaDraftClockFake{now}}
	builder := &PersonaStarterDraftBuilder{Drafts: drafts, Manifests: &personaStarterManifestFake{manifest: manifest}, Instructions: &personaStarterInstructionsFake{text: instructions}}
	request := validPersonaStarterRequest(manifest)
	request.Guidance = guidance
	request.DocumentReferences = []agentdocref.Reference{ref}
	ctx := trust.WithPrincipal(context.Background(), personaDraftPrincipal(t, "tenant-a", "user:creator", now))
	if _, err := builder.CreateDraft(ctx, request); err != nil {
		t.Fatal(err)
	}
	var persisted agentpersona.PersonaProfile
	if err := json.Unmarshal(store.version.Profile, &persisted); err != nil {
		t.Fatal(err)
	}
	if store.calls != 1 || persisted.Guidance != guidance || !reflect.DeepEqual(persisted.DocumentReferences, request.DocumentReferences) || persisted.Instructions != instructions {
		t.Fatalf("persisted draft profile = %+v, calls=%d", persisted, store.calls)
	}
}

func TestTodo_AGENTDOC_008_SecurityPersonaAdminCommands(t *testing.T) {
	ctx, _ := personaAdminCommandContext(t)
	ref := agentdocReference("doc-123e4567-e89b-42d3-a456-426614174000", "Travel policy")
	authorizer := &personaAdminCommandAuthFake{}
	executor := &personaAdminCommandExecutorFake{}
	factory, err := NewPersonaAdminCommandFactory(personaAdminCommandCatalogFake{}, authorizer, executor)
	if err != nil {
		t.Fatal(err)
	}
	guidance := "Follow {{doc:doc-not-listed}}."
	request := productui.PersonaAdminCommandRequest{Action: "CREATE_VERSION", PersonaID: "persona-a", StarterID: "hcmnext.persona_template.policy_helper", StarterVersion: 1, AllowedChannels: []string{"PRIVATE"}, Instructions: &guidance}
	err = factory.ExecutePersonaAdminCommandWithDocumentReferences(ctx, request, []agentdocref.Reference{ref})
	var validation *agentdocref.GuidanceValidationError
	if !errors.Is(err, agentdocref.ErrUnknownInstructionDocumentToken) || !errors.As(err, &validation) || !strings.Contains(validation.Error(), "These instructions mention a document that is not in the list below: doc-not-listed") || executor.calls != 0 {
		t.Fatalf("unknown guidance token crossed command boundary: err=%v calls=%d", err, executor.calls)
	}
	draft := productui.PersonaAdminCommandRequest{Action: "CREATE_DRAFT", PersonaID: "persona-new", StarterID: "hcmnext.persona_template.policy_helper", StarterVersion: 1, AvatarRef: "avatar:policy", OrganizationScopes: []string{"org-a"}, ManifestID: "manifest-policy", BusinessOwnerID: "owner", TechnicalStewardID: "steward", Instructions: &guidance}
	err = factory.ExecutePersonaAdminCommand(ctx, draft)
	validation = nil
	if !errors.Is(err, agentdocref.ErrUnknownInstructionDocumentToken) || !errors.As(err, &validation) || executor.calls != 0 {
		t.Fatalf("draft guidance token without references crossed command boundary: err=%v calls=%d", err, executor.calls)
	}
}

func TestTodo_AGENTDOC_008_PersonaStarterVersionGuidancePresence(t *testing.T) {
	instructions := "Answer tenant-wide policy questions only from approved records and cite support."
	manifest := personaStarterManifest(instructions)
	starter, _ := agenttemplate.PersonaStarterFor("hcmnext.persona_template.policy_helper", 1)
	currentProfile := personaStarterProfile(starter, validPersonaStarterRequest(manifest), manifest, instructions)
	currentProfile.Guidance = "Keep this guidance."
	current, err := (&personaStarterProfileBuilderSpy{}).Build(currentProfile)
	if err != nil {
		t.Fatal(err)
	}
	builder := &PersonaStarterDraftBuilder{Drafts: &PersonaAdminDraftService{Profiles: &personaStarterProfileBuilderSpy{}}, Manifests: &personaStarterManifestFake{manifest: manifest}, Instructions: &personaStarterInstructionsFake{text: instructions}}
	kept, err := builder.BuildVersion(context.Background(), current, PersonaStarterVersionRequest{StarterID: starter.ID, StarterVersion: starter.Version, Version: 2})
	if err != nil || kept.Profile.Guidance != current.Profile.Guidance {
		t.Fatalf("omitted guidance = %q, %v", kept.Profile.Guidance, err)
	}
	empty := ""
	cleared, err := builder.BuildVersion(context.Background(), current, PersonaStarterVersionRequest{StarterID: starter.ID, StarterVersion: starter.Version, Version: 2, Guidance: &empty})
	if err != nil || cleared.Profile.Guidance != "" {
		t.Fatalf("cleared guidance = %q, %v", cleared.Profile.Guidance, err)
	}
	replacement := "Use approved sources."
	changed, err := builder.BuildVersion(context.Background(), current, PersonaStarterVersionRequest{StarterID: starter.ID, StarterVersion: starter.Version, Version: 2, Guidance: &replacement})
	if err != nil || changed.Profile.Guidance != replacement || changed.Digest == kept.Digest {
		t.Fatalf("changed guidance = %+v, %v", changed, err)
	}
}

func TestTodo_AGENTDOC_002_PersonaAdminCatalog(t *testing.T) {
	ctx, _ := catalogContext(t)
	starter, _ := agenttemplate.PersonaStarterFor("hcmnext.persona_template.policy_helper", 1)
	refs := []agentdocref.Reference{
		agentdocReference("doc-123e4567-e89b-42d3-a456-426614174000", "Readable label"),
		{DocumentID: "doc-223e4567-e89b-42d3-a456-426614174000", VersionMode: agentdocref.ModeLatestPublished, Label: "Hidden label"},
	}
	profile := agentpersona.PersonaProfile{Manifest: agentpersona.AgentManifestRef{ID: "agent", Version: 1, Digest: "manifest", SchemaVersion: 1}, PersonaID: "persona-a", Version: 1, Handle: "persona", DisplayName: "Persona", AvatarRef: "avatar", Purpose: "Help safely", Audience: agentpersona.Audience{Roles: []string{"member"}, Populations: []string{"staff"}, OrganizationScopes: []string{"org-a"}}, SkillPins: []agentskills.SkillPin{{ID: "skill.read", Version: 1, Digest: "skill-digest"}}, TierCeiling: agentskills.TierRead, ConversationKinds: []agentpersona.ConversationKind{agentpersona.ConversationDirect}, ChannelClasses: []agentpersona.ChannelClass{agentpersona.ChannelPrivate}, Instructions: "Answer only with approved records.", DocumentReferences: refs, Owner: "owner", Steward: "steward", EvalSuiteRef: starter.EvaluationSuite, EvalLimits: agentpersona.EvaluationLimits{MaxCost: 1, MaxSteps: 1, MaxLatencyMS: 1}}
	version, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	reader := &agentdocPersonaReaderFake{rows: []transportdocument.Preview{{DocumentID: refs[0].DocumentID, Title: "Current readable title", OwnerName: "People Operations", Readable: true}, {DocumentID: refs[1].DocumentID}}}
	svc := &PersonaAdminCatalogService{Versions: catalogVersions{{Profile: version, Lifecycle: agentpersona.StateDraft, Owner: "owner", Steward: "steward"}}, Installations: catalogInstalls{}, Targets: catalogTargets{}, Skills: catalogSkills{}, Grants: &catalogGrants{}, Authorizer: &catalogAuth{}, Documents: reader}
	snapshot, err := svc.Snapshot(ctx, productui.PersonaAdminSnapshotRequest{})
	if err != nil {
		t.Fatal(err)
	}
	got := snapshot.Personas[0].DocumentReferences
	if len(got) != 2 || !got[0].Readable || got[0].Title != "Current readable title" || got[0].Location != "People Operations" || got[1].Readable || got[1].Title != "" || got[1].Label != "Hidden label" || got[1].Location != "" {
		t.Fatalf("document reference projection = %#v", got)
	}
}

func TestTodo_AGENTDOC_002_PersonaAdminDocumentReaderBinding(t *testing.T) {
	reader := &agentdocPersonaReaderFake{}
	service := &PersonaAdminCatalogService{}
	factory := &PersonaAdminCommandFactory{}
	wiring := &personaServeWiring{
		adminCatalog: readOnlyPersonaAdminCatalog{service: service},
		adminFactory: factory,
	}

	if err := wiring.bindAdminDocumentReader(reader); err != nil {
		t.Fatalf("bind document reader: %v", err)
	}
	if service.Documents != reader {
		t.Fatal("catalog did not receive the document reader")
	}
	if factory.documents != reader {
		t.Fatal("command factory did not receive the document reader")
	}
}
