package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// surfaceProdCatalogFake is the published-persona catalog behind the
// store-backed surface directory tests.
type surfaceProdCatalogFake struct {
	rows []surfacePublishedPersona
	err  error
}

func (f *surfaceProdCatalogFake) ListSurfacePublishedPersonas(_ context.Context, tenant string) ([]surfacePublishedPersona, error) {
	if tenant != "tenant-a" {
		return nil, errors.New("unexpected surface catalog tenant")
	}
	if f.err != nil {
		return nil, f.err
	}
	return append([]surfacePublishedPersona(nil), f.rows...), nil
}

func TestSurfaceProductionDirectory(t *testing.T) {
	ctx := context.Background()
	catalog := &surfaceProdCatalogFake{rows: []surfacePublishedPersona{
		{PersonaID: "persona-policy", Version: 3, Handle: "policy-helper"},
		{PersonaID: "persona-policy", Version: 2, Handle: "policy-helper"},
		{PersonaID: "persona-comp", Version: 1, Handle: "comp-analyst"},
	}}
	directory, err := newStoreSurfacePersonaDirectory(catalog)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []chatrecipient.SurfaceKind{chatrecipient.SurfaceWorkflowCase, chatrecipient.SurfaceHubDocument} {
		id, ok, err := directory.ResolveSurfacePersona(ctx, "tenant-a", kind, "policy-helper")
		if err != nil || !ok || id != "persona-policy" {
			t.Fatalf("%s policy-helper = %q,%v,%v, want persona-policy", kind, id, ok, err)
		}
		id, ok, err = directory.ResolveSurfacePersona(ctx, "tenant-a", kind, "comp-analyst")
		if err != nil || !ok || id != "persona-comp" {
			t.Fatalf("%s comp-analyst = %q,%v,%v, want persona-comp", kind, id, ok, err)
		}
		// Unknown handles are user mentions, not personas: no error, no id.
		if id, ok, err := directory.ResolveSurfacePersona(ctx, "tenant-a", kind, "bob"); err != nil || ok || id != "" {
			t.Fatalf("%s bob = %q,%v,%v, want a user mention", kind, id, ok, err)
		}
		// Handles are exact identifiers: no case folding, no display matching.
		if id, ok, err := directory.ResolveSurfacePersona(ctx, "tenant-a", kind, "Policy-Helper"); err != nil || ok || id != "" {
			t.Fatalf("%s Policy-Helper = %q,%v,%v, want a user mention", kind, id, ok, err)
		}
		if id, ok, err := directory.ResolveSurfacePersona(ctx, "tenant-a", kind, ""); err != nil || ok || id != "" {
			t.Fatalf("%s empty handle = %q,%v,%v, want a user mention", kind, id, ok, err)
		}
	}
	if _, _, err := directory.ResolveSurfacePersona(ctx, "tenant-a", "CHAT", "policy-helper"); err == nil {
		t.Fatal("unknown surface kind resolved a persona")
	}
	if _, _, err := directory.ResolveSurfacePersona(ctx, "", chatrecipient.SurfaceHubDocument, "policy-helper"); err == nil {
		t.Fatal("blank tenant resolved a persona")
	}
	broken, err := newStoreSurfacePersonaDirectory(&surfaceProdCatalogFake{err: errors.New("store down")})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := broken.ResolveSurfacePersona(ctx, "tenant-a", chatrecipient.SurfaceHubDocument, "policy-helper"); err == nil {
		t.Fatal("catalog failure resolved a persona")
	}
	// An ambiguous handle names no one: two personas sharing one handle fail
	// closed instead of invoking either.
	ambiguous, err := newStoreSurfacePersonaDirectory(&surfaceProdCatalogFake{rows: []surfacePublishedPersona{
		{PersonaID: "persona-one", Version: 1, Handle: "helper"},
		{PersonaID: "persona-two", Version: 1, Handle: "helper"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if id, ok, err := ambiguous.ResolveSurfacePersona(ctx, "tenant-a", chatrecipient.SurfaceHubDocument, "helper"); err == nil || ok || id != "" {
		t.Fatalf("ambiguous helper = %q,%v,%v, want a fail-closed error", id, ok, err)
	}
	if _, err := newStoreSurfacePersonaDirectory(nil); err == nil {
		t.Fatal("nil catalog composed a directory")
	}
}

// surfaceProdContentFake is the hub read behind the store-backed segment
// reader tests. Unknown versions carry no segments.
type surfaceProdContentFake struct {
	versions map[string]surfaceVersionContent
	comments map[string][]surfaceVersionComment
	denied   map[string]bool
}

func (f *surfaceProdContentFake) ReadSurfaceVersion(_ context.Context, tenant, docID, versionID, invokerID string) (surfaceVersionContent, error) {
	if tenant != "tenant-a" || docID == "" || versionID == "" || invokerID == "" {
		return surfaceVersionContent{}, errors.New("unexpected surface version input")
	}
	if f.denied[docID] {
		return surfaceVersionContent{}, documenthubstore.ErrDenied
	}
	version, ok := f.versions[docID+"/"+versionID]
	if !ok {
		return surfaceVersionContent{}, documenthubstore.ErrDenied
	}
	return version, nil
}

func (f *surfaceProdContentFake) ListSurfaceVersionComments(_ context.Context, tenant, docID, versionID, invokerID string) ([]surfaceVersionComment, error) {
	if tenant != "tenant-a" || docID == "" || versionID == "" || invokerID == "" {
		return nil, errors.New("unexpected surface comment input")
	}
	if f.denied[docID] {
		return nil, documenthubstore.ErrDenied
	}
	return append([]surfaceVersionComment(nil), f.comments[docID+"/"+versionID]...), nil
}

func surfaceProdContent() *surfaceProdContentFake {
	return &surfaceProdContentFake{
		versions: map[string]surfaceVersionContent{
			"doc-9/v12": {AuthorID: "carol", Title: "Leave policy", Markdown: "# Leave policy\n\nSubmit requests two weeks ahead.\n\n## Carry-over\n\nUnused days carry over.\n\n## Notice\n\nManagers approve sooner.\n"},
		},
		comments: map[string][]surfaceVersionComment{
			"doc-9/v12": {
				{ID: "docc-1", AuthorID: "alice", Body: "@policy-helper how much notice?"},
				{ID: "docc-2", AuthorID: "bob", Body: "Two weeks, per the policy."},
			},
		},
		denied: map[string]bool{},
	}
}

func TestSurfaceProductionSegments(t *testing.T) {
	ctx := context.Background()
	reader, err := newStoreSurfaceSegmentReader(surfaceProdContent())
	if err != nil {
		t.Fatal(err)
	}
	segments, err := reader.ReadSurfaceSegments(ctx, SurfaceSegmentRequest{
		TenantID: "tenant-a", Kind: chatrecipient.SurfaceHubDocument, ResourceID: "doc-9", VersionID: "v12",
		ThreadID: "docc-1", InvokerID: "alice", InvokingCommentID: "docc-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Three content sections plus the two version-thread comments.
	if len(segments) != 5 {
		t.Fatalf("segments = %+v, want 3 sections and 2 comments", segments)
	}
	for _, segment := range segments {
		if !surfaceSegmentBelongs(segment.ID, chatrecipient.SurfaceHubDocument, "doc-9", "v12") {
			t.Fatalf("segment %q escaped the viewed version", segment.ID)
		}
		if segment.AuthorID == "" || strings.TrimSpace(segment.Body) == "" {
			t.Fatalf("segment is incomplete: %+v", segment)
		}
	}
	if segments[0].AuthorID != "carol" || !strings.Contains(segments[0].Body, "two weeks ahead") {
		t.Fatalf("first section = %+v, want the preamble by the version author", segments[0])
	}
	if !strings.Contains(segments[1].Body, "carry over") || !strings.Contains(segments[2].Body, "approve sooner") {
		t.Fatalf("sections = %+v, want one segment per heading", segments[:3])
	}
	if segments[3].ID != "doc-9/v12/comment-docc-1" || segments[3].AuthorID != "alice" {
		t.Fatalf("comment segment = %+v, want alice's invoking comment", segments[3])
	}
	// The limit binds the read: an invoker asking with a limit of two sees
	// two segments, and a non-positive limit falls back to the shared bound.
	capped, err := reader.ReadSurfaceSegments(ctx, SurfaceSegmentRequest{
		TenantID: "tenant-a", Kind: chatrecipient.SurfaceHubDocument, ResourceID: "doc-9", VersionID: "v12",
		ThreadID: "docc-1", InvokerID: "alice", InvokingCommentID: "docc-1", Limit: 2,
	})
	if err != nil || len(capped) != 2 {
		t.Fatalf("capped segments = %+v err=%v, want 2", capped, err)
	}
	// A reader without access reads nothing: the denial surfaces as an
	// error, never as an empty context that would silently drop grounding.
	denied, err := newStoreSurfaceSegmentReader(&surfaceProdContentFake{denied: map[string]bool{"doc-9": true}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := denied.ReadSurfaceSegments(ctx, SurfaceSegmentRequest{
		TenantID: "tenant-a", Kind: chatrecipient.SurfaceHubDocument, ResourceID: "doc-9", VersionID: "v12",
		ThreadID: "docc-1", InvokerID: "mallory", InvokingCommentID: "docc-1",
	}); err == nil {
		t.Fatal("a reader without access read segments")
	}
	// Workflow cases keep generic support but bind no store content: no
	// case comment surface exists in-tree, so the invoker's question stands
	// alone rather than binding another surface's rows.
	workflow, err := reader.ReadSurfaceSegments(ctx, SurfaceSegmentRequest{
		TenantID: "tenant-a", Kind: chatrecipient.SurfaceWorkflowCase, ResourceID: "case-1",
		ThreadID: "comment-1", InvokerID: "alice", InvokingCommentID: "comment-1",
	})
	if err != nil || len(workflow) != 0 {
		t.Fatalf("workflow segments = %+v err=%v, want none", workflow, err)
	}
	for name, request := range map[string]SurfaceSegmentRequest{
		"blank tenant":    {Kind: chatrecipient.SurfaceHubDocument, ResourceID: "doc-9", VersionID: "v12", ThreadID: "docc-1", InvokerID: "alice", InvokingCommentID: "docc-1"},
		"unknown kind":    {TenantID: "tenant-a", Kind: "CHAT", ResourceID: "doc-9", VersionID: "v12", ThreadID: "docc-1", InvokerID: "alice", InvokingCommentID: "docc-1"},
		"blank resource":  {TenantID: "tenant-a", Kind: chatrecipient.SurfaceHubDocument, VersionID: "v12", ThreadID: "docc-1", InvokerID: "alice", InvokingCommentID: "docc-1"},
		"missing version": {TenantID: "tenant-a", Kind: chatrecipient.SurfaceHubDocument, ResourceID: "doc-9", ThreadID: "docc-1", InvokerID: "alice", InvokingCommentID: "docc-1"},
		"blank invoker":   {TenantID: "tenant-a", Kind: chatrecipient.SurfaceHubDocument, ResourceID: "doc-9", VersionID: "v12", ThreadID: "docc-1", InvokingCommentID: "docc-1"},
		"blank thread":    {TenantID: "tenant-a", Kind: chatrecipient.SurfaceHubDocument, ResourceID: "doc-9", VersionID: "v12", InvokerID: "alice", InvokingCommentID: "docc-1"},
		"blank comment":   {TenantID: "tenant-a", Kind: chatrecipient.SurfaceHubDocument, ResourceID: "doc-9", VersionID: "v12", ThreadID: "docc-1", InvokerID: "alice"},
	} {
		if _, err := reader.ReadSurfaceSegments(ctx, request); err == nil {
			t.Fatalf("%s read segments", name)
		}
	}
	if _, err := newStoreSurfaceSegmentReader(nil); err == nil {
		t.Fatal("nil document content composed a segment reader")
	}
}

func TestSplitSurfaceMarkdownSections(t *testing.T) {
	sections := splitSurfaceMarkdownSections("# Title\n\nFirst.\n\n## Second\n\nMore.\n\n### Nested\n\nDeep.\n")
	if len(sections) != 3 || !strings.Contains(sections[0], "First.") || !strings.Contains(sections[1], "More.") || !strings.Contains(sections[2], "Deep.") {
		t.Fatalf("sections = %q, want preamble plus one segment per heading", sections)
	}
	if sections := splitSurfaceMarkdownSections("  \n\n"); len(sections) != 0 {
		t.Fatalf("blank markdown split into %q", sections)
	}
	if sections := splitSurfaceMarkdownSections("Just prose, no headings."); len(sections) != 1 || sections[0] != "Just prose, no headings." {
		t.Fatalf("prose split into %q", sections)
	}
}

// surfaceProdPublishedFake is the current-published-persona read behind the
// surface authority tests.
type surfaceProdPublishedFake struct {
	versions map[string]agentpersonastore.PersonaVersion
	err      error
}

func (f *surfaceProdPublishedFake) ReadSurfacePublishedPersona(_ context.Context, tenant, personaID string) (agentpersonastore.PersonaVersion, error) {
	if tenant != "tenant-a" || personaID == "" {
		return agentpersonastore.PersonaVersion{}, errors.New("unexpected surface persona input")
	}
	if f.err != nil {
		return agentpersonastore.PersonaVersion{}, f.err
	}
	version, ok := f.versions[personaID]
	if !ok {
		return agentpersonastore.PersonaVersion{}, errors.New("surface persona: not published")
	}
	return version, nil
}

// surfaceProdMembershipFake answers surface membership: alice reads doc-9
// and participates in nothing else; mallory is outside every surface.
type surfaceProdMembershipFake struct{ err error }

func (f *surfaceProdMembershipFake) SurfaceMember(_ context.Context, tenant string, kind chatrecipient.SurfaceKind, resource, version, subject string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	if tenant != "tenant-a" || !kind.Valid() || resource == "" || subject == "" {
		return false, errors.New("unexpected surface membership input")
	}
	if kind == chatrecipient.SurfaceHubDocument {
		return resource == "doc-9" && version == "v12" && subject == "alice", nil
	}
	return false, nil
}

// surfaceProdSkillCatalogFake is the shared skill catalog behind the surface
// authority tests: one current T0 read skill.
type surfaceProdSkillCatalogFake struct{ record agentskills.SkillRecord }

func surfaceProdSkillRecord() agentskills.SkillRecord {
	return agentskills.SkillRecord{
		Definition: agentskills.SkillDefinition{ID: "skill.read", Version: 1, Description: "Read records", SideEffectTier: agentskills.TierRead, DataClassesRead: []string{"WORKFORCE"}},
		Digest:     "skill-digest", Status: agentskills.StatusActive,
		ResolvedOperations: []agentskills.ResolvedOperation{{Capability: capability.Record{Definition: capability.Definition{AuthZScopeRef: "worker"}}, HasCapability: true}},
	}
}

func (f *surfaceProdSkillCatalogFake) List() []agentskills.SkillRecord {
	return []agentskills.SkillRecord{f.record}
}

func (f *surfaceProdSkillCatalogFake) ResolvePin(pin agentskills.SkillPin) (agentskills.SkillRecord, error) {
	if pin.ID != f.record.Definition.ID || pin.Version != f.record.Definition.Version || pin.Digest != f.record.Digest {
		return agentskills.SkillRecord{}, errors.New("surface test catalog: unknown pin")
	}
	return f.record, nil
}

func surfaceProdPurposeContext(t *testing.T, subject string) context.Context {
	t.Helper()
	now := time.Date(2026, time.September, 29, 12, 0, 0, 0, time.UTC)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant-a"), Subject: subject, SubjectKind: trust.SubjectKindHuman, Purposes: []string{personaChatReplyPurpose}, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceLow, SessionRef: "surface-production-test", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "surface-production"})
	if err != nil {
		t.Fatal(err)
	}
	return trust.WithPrincipal(context.Background(), principal)
}

func surfaceProdAuthoritySource(t *testing.T, published *surfaceProdPublishedFake, members *surfaceProdMembershipFake) *surfacePersonaAuthoritySource {
	t.Helper()
	version, _ := authorityProfile(t)
	if published == nil {
		published = &surfaceProdPublishedFake{versions: map[string]agentpersonastore.PersonaVersion{"persona-a": version}}
	}
	if members == nil {
		members = &surfaceProdMembershipFake{}
	}
	scopes, err := NewPersonaAuthorityScopeResolver(&surfaceProdSkillCatalogFake{record: surfaceProdSkillRecord()})
	if err != nil {
		t.Fatal(err)
	}
	source, err := newSurfacePersonaAuthoritySource(published, members, scopes, &surfaceProdSkillCatalogFake{record: surfaceProdSkillRecord()})
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func TestSurfaceProductionAuthority(t *testing.T) {
	channel := encodeSurfaceChannel(chatrecipient.SurfaceHubDocument, "doc-9", "v12")
	source := surfaceProdAuthoritySource(t, nil, nil)
	invoker := TrustedInvoker{ID: "alice", TenantID: "tenant-a", HumanMember: true, AudienceMember: true, Discoverable: agentinvoke.SkillScopes{"skill.read": {"worker"}}}
	admission, err := source.ResolvePersonaAuthority(surfaceProdPurposeContext(t, "alice"),
		agentinvoke.AdmissionRequest{TenantID: "tenant-a", ConversationID: channel, InvokerID: "alice", PersonaID: "persona-a"}, invoker)
	if err != nil {
		t.Fatalf("surface admission: %v", err)
	}
	// One published version, one deterministic surface installation, and the
	// persona's own pinned ceiling resolved through the shared catalog.
	if admission.Persona.ID != "persona-a" || admission.Persona.Version != "2" || !admission.Persona.Current ||
		!admission.Installation.Current || !admission.PersonaInstalled || !admission.HumanMember || !admission.AudienceMember {
		t.Fatalf("admission = %+v, want a current member admission", admission)
	}
	if admission.Persona.InstallationID == "" || admission.Persona.InstallationID != admission.Installation.ID {
		t.Fatalf("admission = %+v, want one bound installation", admission)
	}
	want := agentinvoke.SkillScopes{"skill.read": {"worker"}}
	for name, scopes := range map[string]agentinvoke.SkillScopes{"persona": admission.Persona.PinnedSkills, "installation": admission.Installation.SkillCeiling, "channel": admission.Channel.SkillCeiling} {
		if len(scopes) != 1 || len(scopes["skill.read"]) != 1 || scopes["skill.read"][0] != "worker" {
			t.Fatalf("%s scopes = %+v, want %+v", name, scopes, want)
		}
	}
	again, err := source.ResolvePersonaAuthority(surfaceProdPurposeContext(t, "alice"),
		agentinvoke.AdmissionRequest{TenantID: "tenant-a", ConversationID: channel, InvokerID: "alice", PersonaID: "persona-a"}, invoker)
	if err != nil || again.Installation.ID != admission.Installation.ID {
		t.Fatalf("second admission = %+v err=%v, want the same installation binding", again, err)
	}
	other, err := source.ResolvePersonaAuthority(surfaceProdPurposeContext(t, "alice"),
		agentinvoke.AdmissionRequest{TenantID: "tenant-a", ConversationID: encodeSurfaceChannel(chatrecipient.SurfaceHubDocument, "doc-9", "v13"), InvokerID: "alice", PersonaID: "persona-a"}, invoker)
	if err == nil || other.Installation.ID == admission.Installation.ID {
		t.Fatalf("another version admitted with the same installation: %+v err=%v", other, err)
	}
}

func TestSurfaceProductionAuthorityFailsClosed(t *testing.T) {
	channel := encodeSurfaceChannel(chatrecipient.SurfaceHubDocument, "doc-9", "v12")
	version, _ := authorityProfile(t)
	invoker := TrustedInvoker{ID: "alice", TenantID: "tenant-a", HumanMember: true, AudienceMember: true, Discoverable: agentinvoke.SkillScopes{"skill.read": {"worker"}}}
	request := agentinvoke.AdmissionRequest{TenantID: "tenant-a", ConversationID: channel, InvokerID: "alice", PersonaID: "persona-a"}

	// Chat channels are not surfaces: the shared chat authority stays the
	// only path that admits them.
	chatSource := surfaceProdAuthoritySource(t, nil, nil)
	chatRequest := request
	chatRequest.ConversationID = "room-a"
	if _, err := chatSource.ResolvePersonaAuthority(surfaceProdPurposeContext(t, "alice"), chatRequest, invoker); err == nil {
		t.Fatal("a chat channel was admitted by the surface authority")
	}
	// A reader outside the document is not a member of its surface.
	if _, err := chatSource.ResolvePersonaAuthority(surfaceProdPurposeContext(t, "mallory"),
		agentinvoke.AdmissionRequest{TenantID: "tenant-a", ConversationID: channel, InvokerID: "mallory", PersonaID: "persona-a"},
		TrustedInvoker{ID: "mallory", TenantID: "tenant-a", Discoverable: invoker.Discoverable}); err == nil {
		t.Fatal("an outsider was admitted to the document surface")
	}
	// Workflow cases admit nothing until their surface exists in-tree.
	workflowRequest := request
	workflowRequest.ConversationID = encodeSurfaceChannel(chatrecipient.SurfaceWorkflowCase, "case-1", "")
	if _, err := chatSource.ResolvePersonaAuthority(surfaceProdPurposeContext(t, "alice"), workflowRequest, invoker); err == nil {
		t.Fatal("a workflow case was admitted with no case surface in-tree")
	}
	// An unpublished persona admits nothing.
	unpublished := surfaceProdAuthoritySource(t, &surfaceProdPublishedFake{versions: map[string]agentpersonastore.PersonaVersion{}}, nil)
	if _, err := unpublished.ResolvePersonaAuthority(surfaceProdPurposeContext(t, "alice"), request, invoker); err == nil {
		t.Fatal("an unpublished persona was admitted")
	}
	// A tampered profile never matches its sealed digest.
	tamperedVersion := version
	tamperedVersion.ContentDigest = "sha256:forged"
	tampered := surfaceProdAuthoritySource(t, &surfaceProdPublishedFake{versions: map[string]agentpersonastore.PersonaVersion{"persona-a": tamperedVersion}}, nil)
	if _, err := tampered.ResolvePersonaAuthority(surfaceProdPurposeContext(t, "alice"), request, invoker); err == nil {
		t.Fatal("a tampered profile was admitted")
	}
	// A mismatched principal or invoker binding admits nothing.
	if _, err := chatSource.ResolvePersonaAuthority(surfaceProdPurposeContext(t, "alice"), request, TrustedInvoker{ID: "bob", TenantID: "tenant-a", Discoverable: invoker.Discoverable}); err == nil {
		t.Fatal("a mismatched invoker binding was admitted")
	}
	if _, err := chatSource.ResolvePersonaAuthority(context.Background(), request, invoker); err == nil {
		t.Fatal("a missing principal was admitted")
	}
	// A membership read failure denies rather than admitting blind.
	outage := surfaceProdAuthoritySource(t, nil, &surfaceProdMembershipFake{err: errors.New("store down")})
	if _, err := outage.ResolvePersonaAuthority(surfaceProdPurposeContext(t, "alice"), request, invoker); err == nil {
		t.Fatal("a membership outage admitted a mention")
	}
	// Every port is required.
	scopes, err := NewPersonaAuthorityScopeResolver(&surfaceProdSkillCatalogFake{record: surfaceProdSkillRecord()})
	if err != nil {
		t.Fatal(err)
	}
	published := &surfaceProdPublishedFake{versions: map[string]agentpersonastore.PersonaVersion{"persona-a": version}}
	members := &surfaceProdMembershipFake{}
	catalog := &surfaceProdSkillCatalogFake{record: surfaceProdSkillRecord()}
	for name, build := range map[string]func() (*surfacePersonaAuthoritySource, error){
		"nil personas": func() (*surfacePersonaAuthoritySource, error) {
			return newSurfacePersonaAuthoritySource(nil, members, scopes, catalog)
		},
		"nil members": func() (*surfacePersonaAuthoritySource, error) {
			return newSurfacePersonaAuthoritySource(published, nil, scopes, catalog)
		},
		"nil scopes": func() (*surfacePersonaAuthoritySource, error) {
			return newSurfacePersonaAuthoritySource(published, members, nil, catalog)
		},
		"nil catalog": func() (*surfacePersonaAuthoritySource, error) {
			return newSurfacePersonaAuthoritySource(published, members, scopes, nil)
		},
	} {
		if _, err := build(); err == nil {
			t.Fatalf("%s composed a surface authority source", name)
		}
	}
}

// surfaceProdInvokerSkillsFake answers invoker skill discovery for the
// surface identity tests.
type surfaceProdInvokerSkillsFake struct {
	scopes agentinvoke.SkillScopes
	err    error
}

func (f *surfaceProdInvokerSkillsFake) ResolveInvokerSkills(_ context.Context, principal *trust.Principal, purpose string) (agentinvoke.SkillScopes, error) {
	if principal == nil || purpose != personaChatReplyPurpose {
		return nil, errors.New("unexpected surface skill input")
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.scopes.Clone(), nil
}

func TestSurfaceProductionIdentity(t *testing.T) {
	identity, err := NewServerTrustedInvokerSource(TrustedInvokerSourceConfig{
		Membership: surfaceMembershipSource{members: &surfaceProdMembershipFake{}},
		Skills:     &surfaceProdInvokerSkillsFake{scopes: agentinvoke.SkillScopes{"skill.read": {"worker"}}},
		Purpose:    personaChatReplyPurpose,
	})
	if err != nil {
		t.Fatal(err)
	}
	channel := encodeSurfaceChannel(chatrecipient.SurfaceHubDocument, "doc-9", "v12")
	bound := withPersonaChatAuthorityTuple(surfaceProdPurposeContext(t, "alice"), "tenant-a", "alice", channel, "docc-1", "docc-1")
	trusted, err := identity.ResolveTrustedInvoker(bound, "tenant-a", "alice")
	if err != nil {
		t.Fatalf("surface member identity: %v", err)
	}
	if !trusted.HumanMember || !trusted.AudienceMember || len(trusted.Discoverable) != 1 {
		t.Fatalf("trusted invoker = %+v, want a member with discovered skills", trusted)
	}
	// Membership is bound to the committed surface channel: another version,
	// another resource, or no tuple at all is not a member.
	for name, ctx := range map[string]context.Context{
		"another version":  withPersonaChatAuthorityTuple(surfaceProdPurposeContext(t, "alice"), "tenant-a", "alice", encodeSurfaceChannel(chatrecipient.SurfaceHubDocument, "doc-9", "v13"), "docc-1", "docc-1"),
		"another surface":  withPersonaChatAuthorityTuple(surfaceProdPurposeContext(t, "alice"), "tenant-a", "alice", encodeSurfaceChannel(chatrecipient.SurfaceWorkflowCase, "case-1", ""), "comment-1", "comment-1"),
		"chat channel":     withPersonaChatAuthorityTuple(surfaceProdPurposeContext(t, "alice"), "tenant-a", "alice", "room-a", "post-1", "post-1"),
		"unbound":          surfaceProdPurposeContext(t, "alice"),
		"outsider channel": withPersonaChatAuthorityTuple(surfaceProdPurposeContext(t, "mallory"), "tenant-a", "mallory", channel, "docc-1", "docc-1"),
	} {
		subject := "alice"
		if name == "outsider channel" {
			subject = "mallory"
		}
		if _, err := identity.ResolveTrustedInvoker(ctx, "tenant-a", subject); err == nil {
			t.Fatalf("%s resolved a trusted invoker", name)
		}
	}
}

func TestSurfaceProductionServedCompositionFailsClosed(t *testing.T) {
	if _, err := ComposeServedSurfaceMentions(SurfaceServeDependencies{}); !errors.Is(err, errSurfaceMentionProduction) {
		t.Fatalf("empty served dependencies error = %v, want the production sentinel", err)
	}
	// A skill source without a gate or discovery context composes nothing,
	// even when every other dependency is present.
	deps := SurfaceServeDependencies{
		Personas: &agentpersonastore.Store{}, Documents: &documenthubstore.Store{},
		AgentStore: &agentstore.Store{}, Pool: &pgxadapter.Pool{},
		TenantUUID: func(values.TenantId) uuid.UUID { return uuid.New() },
		Skills:     &AgentSkillSource{}, Catalog: &surfaceProdSkillCatalogFake{record: surfaceProdSkillRecord()},
		Runs: &personaRunFake{}, Peer: surfacePeerExtractor{}, Now: time.Now,
	}
	if _, err := ComposeServedSurfaceMentions(deps); !errors.Is(err, errSurfaceMentionProduction) {
		t.Fatalf("gateless skill source error = %v, want the production sentinel", err)
	}
}

func TestSurfaceProductionPeerPortLeavesChatCompositionUnchanged(t *testing.T) {
	// The surface peer port rides the shared production config but satisfies
	// nothing on its own: chat composition still refuses an otherwise empty
	// config, and still ignores the port when set.
	if _, err := NewDatabasePersonaInvocationProductionRuntime(PersonaInvocationProductionConfig{Peer: surfacePeerExtractor{}}); !errors.Is(err, errPersonaInvocationProductionComposition) {
		t.Fatalf("peer-only config error = %v, want the production composition sentinel", err)
	}
}

func TestSurfaceProductionCompositionFailsClosed(t *testing.T) {
	personas := &agentpersonastore.Store{}
	documents := &documenthubstore.Store{}
	catalog := &surfaceProdSkillCatalogFake{record: surfaceProdSkillRecord()}
	discover := &surfaceProdDiscoverFake{records: []agentskills.SkillRecord{surfaceProdSkillRecord()}}
	grants := &personaGrantFake{}
	runs := &personaRunFake{}
	repository := agentinvoke.NewMemoryRepository()
	full := SurfaceMentionProductionConfig{
		Personas: personas, Documents: documents, Catalog: catalog, Skills: discover,
		Grants: grants, Runs: runs, Repository: repository, Peer: surfacePeerExtractor{},
	}
	if _, err := ComposeSurfaceMentionBridge(full); err != nil {
		t.Fatalf("full surface composition: %v", err)
	}
	without := func(mutate func(*SurfaceMentionProductionConfig)) SurfaceMentionProductionConfig {
		cfg := full
		mutate(&cfg)
		return cfg
	}
	for name, cfg := range map[string]SurfaceMentionProductionConfig{
		"no persona store":  without(func(c *SurfaceMentionProductionConfig) { c.Personas = nil }),
		"no document store": without(func(c *SurfaceMentionProductionConfig) { c.Documents = nil }),
		"no catalog":        without(func(c *SurfaceMentionProductionConfig) { c.Catalog = nil }),
		"no skills":         without(func(c *SurfaceMentionProductionConfig) { c.Skills = nil }),
		"no grants":         without(func(c *SurfaceMentionProductionConfig) { c.Grants = nil }),
		"no runs":           without(func(c *SurfaceMentionProductionConfig) { c.Runs = nil }),
		"no repository":     without(func(c *SurfaceMentionProductionConfig) { c.Repository = nil }),
		"no peer extractor": without(func(c *SurfaceMentionProductionConfig) { c.Peer = nil }),
	} {
		if _, err := ComposeSurfaceMentionBridge(cfg); err == nil {
			t.Fatalf("%s composed a surface bridge", name)
		}
	}
}

// TestSurfaceProductionBridgeAdmitsDocumentMention proves the composed
// production bridge end to end against real stores: a reader's @handle
// resolves through the published catalog, admits through the surface
// authority, and starts one run bound to the viewed document version with
// the version's sections as tainted context.
func TestSurfaceProductionBridgeAdmitsDocumentMention(t *testing.T) {
	ctx := context.Background()
	personas, tenant := surfaceProdPersonaStore(t)
	sealed, _ := authorityProfile(t)
	sealed.TenantID = tenant
	sealed.Handle = "policy-helper"
	sealed.DisplayName = "Policy Helper"
	scoped, err := personas.Scoped(tenant)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	owner := agentpersonastore.PersonaOwner{TenantID: tenant, PersonaID: sealed.PersonaID, Role: agentpersonastore.BusinessOwner, PrincipalID: "owner"}
	steward := agentpersonastore.PersonaOwner{TenantID: tenant, PersonaID: sealed.PersonaID, Role: agentpersonastore.TechnicalSteward, PrincipalID: "user:technical-steward"}
	if err := scoped.CreateDraft(ctx, sealed, owner, steward, "user:creator", at); err != nil {
		t.Fatal(err)
	}
	if err := scoped.AppendLifecycle(ctx, agentpersonastore.LifecycleEvent{TenantID: tenant, EventID: "persona-a-review", PersonaID: sealed.PersonaID, PersonaVersion: sealed.Version, From: agentpersonastore.StateDraft, To: agentpersonastore.StateInReview, Reason: "reviewed change", ActorID: "user:reviewer", OccurredAt: at}); err != nil {
		t.Fatal(err)
	}
	if err := scoped.Publish(ctx, agentpersonastore.LifecycleEvent{TenantID: tenant, EventID: "persona-a-publish", PersonaID: sealed.PersonaID, PersonaVersion: sealed.Version, From: agentpersonastore.StateInReview, To: agentpersonastore.StatePublished, Reason: "published", ActorID: "user:reviewer", OccurredAt: at}, agentpersonastore.PublicationEvidence{ReviewID: "review-persona-a", EvaluationRunID: "eval-persona-a"}); err != nil {
		t.Fatal(err)
	}

	svc := documentServiceFixture(t)
	const readerID = "alice"
	id, versionID, err := svc.CreateDocument(ctx, tenant.String(), "owner", "Leave policy", "# Leave policy\n\nSubmit requests two weeks ahead.\n\n## Carry-over\n\nUnused days carry over.\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ShareDocument(ctx, tenant.String(), "owner", id, readerID, ""); err != nil {
		t.Fatal(err)
	}

	runs, grants := &personaRunFake{}, &personaGrantFake{}
	bridge, err := ComposeSurfaceMentionBridge(SurfaceMentionProductionConfig{
		Personas: personas, Documents: svc.store,
		Catalog: &surfaceProdSkillCatalogFake{record: surfaceProdSkillRecord()},
		Skills:  &surfaceProdDiscoverFake{records: []agentskills.SkillRecord{surfaceProdSkillRecord()}},
		Grants:  grants, Runs: runs, Repository: agentinvoke.NewMemoryRepository(), Peer: surfacePeerExtractor{},
	})
	if err != nil {
		t.Fatal(err)
	}
	invocations, err := bridge.HandleCommentCommit(surfaceProdPurposeContext(t, readerID), SurfaceCommentCommit{
		TenantID: tenant.String(), Kind: chatrecipient.SurfaceHubDocument, ResourceID: id, VersionID: versionID,
		CommentID: "docc-1", ThreadID: "docc-1", AuthorID: readerID,
		Body: "@policy-helper how much notice?", Handles: []string{"policy-helper"},
	})
	if err != nil || len(invocations) != 1 {
		t.Fatalf("production mention resolved to %+v err=%v, want one invocation", invocations, err)
	}
	if len(runs.requests) != 1 || grants.calls != 1 {
		t.Fatalf("runs=%d grants=%d, want one run through the shared resolver", len(runs.requests), grants.calls)
	}
	run := runs.requests[0]
	kind, resource, version, ok := parseSurfaceChannel(run.ConversationID)
	if !ok || kind != chatrecipient.SurfaceHubDocument || resource != id || version != versionID {
		t.Fatalf("run channel %q is not the viewed document version", run.ConversationID)
	}
	if run.PersonaID != "persona-a" || run.PersonaVersion != "2" || run.InvokerID != readerID || run.Mode != agentinvoke.OnBehalfOf {
		t.Fatalf("run is not the reader's on-behalf-of invocation: %+v", run)
	}
	if len(run.Skills) != 1 || run.Skills["skill.read"][0] != "worker" {
		t.Fatalf("effective skills = %+v, want the pinned intersection", run.Skills)
	}
	invoker, untrusted := 0, 0
	for _, entry := range run.Context.Entries {
		switch entry.Taint {
		case agentinvoke.TaintInvokerInstruction:
			invoker++
		case agentinvoke.TaintUntrustedPeer:
			untrusted++
			if entry.Extraction.SourceDigest != entry.Digest {
				t.Fatalf("peer extraction is not bound to its entry: %+v", entry)
			}
		default:
			t.Fatalf("entry carries no taint: %+v", entry)
		}
	}
	if invoker != 1 || untrusted != 2 {
		t.Fatalf("context = %+v, want the question plus the 2 tainted version sections", run.Context)
	}
	// A mention by an outsider admits nothing and starts nothing.
	outsider, err := bridge.HandleCommentCommit(surfaceProdPurposeContext(t, "mallory"), SurfaceCommentCommit{
		TenantID: tenant.String(), Kind: chatrecipient.SurfaceHubDocument, ResourceID: id, VersionID: versionID,
		CommentID: "docc-9", ThreadID: "docc-9", AuthorID: "mallory",
		Body: "@policy-helper how much notice?", Handles: []string{"policy-helper"},
	})
	if err == nil || len(outsider) != 0 || len(runs.requests) != 1 {
		t.Fatalf("outsider mention resolved to %+v err=%v runs=%d", outsider, err, len(runs.requests))
	}
}

// surfaceProdDiscoverFake answers tenant skill discovery for the surface
// composition tests.
type surfaceProdDiscoverFake struct {
	records []agentskills.SkillRecord
	err     error
}

func (f *surfaceProdDiscoverFake) Discover(_ context.Context, principal *trust.Principal, purpose string) ([]agentskills.SkillRecord, error) {
	if principal == nil || purpose != personaChatReplyPurpose {
		return nil, errors.New("unexpected surface discovery input")
	}
	if f.err != nil {
		return nil, f.err
	}
	return append([]agentskills.SkillRecord(nil), f.records...), nil
}

// surfaceProdReviewSource and surfaceProdEvaluationSource attest the fixture
// publications used by the store-backed production tests.
type surfaceProdReviewSource struct{}

func (surfaceProdReviewSource) ResolvePersonaReview(_ context.Context, _ dbport.Tx, tenant values.TenantId, personaID string, version int64, digest, reviewID string) (agentpersonastore.VerifiedReview, error) {
	return agentpersonastore.VerifiedReview{ReviewID: reviewID, TenantID: tenant.String(), PersonaID: personaID, ReviewerID: "user:reviewer", Permission: "persona:review", Decision: "APPROVE", PersonaVersion: version, ProfileDigest: digest, ReviewDigest: "review:" + reviewID, GrantCurrent: true}, nil
}

type surfaceProdEvaluationSource struct{}

func (surfaceProdEvaluationSource) ResolvePersonaEvaluation(_ context.Context, _ dbport.Tx, tenant values.TenantId, runID, personaID string, version int64, digest string) (agentpersonastore.VerifiedEvaluation, error) {
	return agentpersonastore.VerifiedEvaluation{RunID: runID, TenantID: tenant.String(), PersonaID: personaID, PersonaVersion: version, ProfileDigest: digest, SuiteDigest: "suite:persona", RunDigest: "run:" + runID, Passed: true, Fresh: true}, nil
}

func surfaceProdPersonaStore(t *testing.T) (*agentpersonastore.Store, values.TenantId) {
	t.Helper()
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatalf("apply isolated agent migrations: %v", err)
	}
	tenant := values.TenantId("tenant-a")
	tenantUUID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id) VALUES ($1)`, tenantUUID)
	conn := db.NewConn(t)
	if _, err := conn.Exec(ctx, "SET ROLE "+agentstore.AppRole); err != nil {
		t.Fatal(err)
	}
	store, err := agentpersonastore.NewWithPublicationAuthorities(conn, func(key values.TenantId) uuid.UUID {
		if key != tenant {
			return uuid.Nil
		}
		return tenantUUID
	}, surfaceProdReviewSource{}, surfaceProdEvaluationSource{})
	if err != nil {
		t.Fatal(err)
	}
	return store, tenant
}

func surfaceProdPublish(t *testing.T, store *agentpersonastore.Store, tenant values.TenantId, personaID, handle string, version int64, publish bool) agentpersonastore.PersonaVersion {
	t.Helper()
	ctx := context.Background()
	profile, err := json.Marshal(map[string]string{"owner": "user:business-owner", "handle": handle})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	row := agentpersonastore.PersonaVersion{TenantID: tenant, PersonaID: personaID, Version: version, AgentVersion: "agent-v1", Handle: handle, DisplayName: handle, Profile: profile, ContentDigest: "sha256:" + personaID, CreatedAt: at}
	scoped, err := store.Scoped(tenant)
	if err != nil {
		t.Fatal(err)
	}
	owner := agentpersonastore.PersonaOwner{TenantID: tenant, PersonaID: personaID, Role: agentpersonastore.BusinessOwner, PrincipalID: "user:business-owner"}
	steward := agentpersonastore.PersonaOwner{TenantID: tenant, PersonaID: personaID, Role: agentpersonastore.TechnicalSteward, PrincipalID: "user:technical-steward"}
	if err := scoped.CreateDraft(ctx, row, owner, steward, "user:creator", at); err != nil {
		t.Fatal(err)
	}
	if !publish {
		return row
	}
	if err := scoped.AppendLifecycle(ctx, agentpersonastore.LifecycleEvent{TenantID: tenant, EventID: personaID + "-review", PersonaID: personaID, PersonaVersion: version, From: agentpersonastore.StateDraft, To: agentpersonastore.StateInReview, Reason: "reviewed change", ActorID: "user:reviewer", OccurredAt: at}); err != nil {
		t.Fatal(err)
	}
	if err := scoped.Publish(ctx, agentpersonastore.LifecycleEvent{TenantID: tenant, EventID: personaID + "-publish", PersonaID: personaID, PersonaVersion: version, From: agentpersonastore.StateInReview, To: agentpersonastore.StatePublished, Reason: "published", ActorID: "user:reviewer", OccurredAt: at}, agentpersonastore.PublicationEvidence{ReviewID: "review-" + personaID, EvaluationRunID: "eval-" + personaID}); err != nil {
		t.Fatal(err)
	}
	return row
}

func TestSurfaceProductionStores(t *testing.T) {
	ctx := context.Background()
	personas, tenant := surfaceProdPersonaStore(t)
	surfaceProdPublish(t, personas, tenant, "persona-policy", "policy-helper", 3, true)
	surfaceProdPublish(t, personas, tenant, "persona-draft", "draft-helper", 1, false)

	catalog, err := newSurfacePersonaStoreCatalog(personas)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := catalog.ListSurfacePublishedPersonas(ctx, tenant.String())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].PersonaID != "persona-policy" || rows[0].Handle != "policy-helper" || rows[0].Version != 3 {
		t.Fatalf("published personas = %+v, want only the published policy helper", rows)
	}
	directory, err := newStoreSurfacePersonaDirectory(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if id, ok, err := directory.ResolveSurfacePersona(ctx, tenant.String(), chatrecipient.SurfaceHubDocument, "policy-helper"); err != nil || !ok || id != "persona-policy" {
		t.Fatalf("policy-helper = %q,%v,%v, want persona-policy", id, ok, err)
	}
	if id, ok, err := directory.ResolveSurfacePersona(ctx, tenant.String(), chatrecipient.SurfaceHubDocument, "draft-helper"); err != nil || ok || id != "" {
		t.Fatalf("draft-helper = %q,%v,%v, want a user mention", id, ok, err)
	}
	reader, err := newSurfacePersonaStoreReader(personas)
	if err != nil {
		t.Fatal(err)
	}
	published, err := reader.ReadSurfacePublishedPersona(ctx, tenant.String(), "persona-policy")
	if err != nil || published.Version != 3 || published.Handle != "policy-helper" {
		t.Fatalf("published persona-policy = %+v err=%v", published, err)
	}
	if _, err := reader.ReadSurfacePublishedPersona(ctx, tenant.String(), "persona-draft"); err == nil {
		t.Fatal("a draft persona read as published")
	}
	if _, err := reader.ReadSurfacePublishedPersona(ctx, tenant.String(), "persona-missing"); err == nil {
		t.Fatal("a missing persona read as published")
	}

	// The document half reads through the same grants the hub enforces: a
	// reader sees the version's sections and thread, an outsider sees an
	// error, and membership follows the grant.
	svc := documentServiceFixture(t)
	const owner, readerID = "owner", "alice"
	id, versionID, err := svc.CreateDocument(ctx, tenant.String(), owner, "Leave policy", "# Leave policy\n\nSubmit requests two weeks ahead.\n\n## Carry-over\n\nUnused days carry over.\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ShareDocument(ctx, tenant.String(), owner, id, readerID, ""); err != nil {
		t.Fatal(err)
	}
	ownerCtx := trust.WithPrincipal(ctx, mustBridgePrincipal(t, owner, tenant.String()))
	if _, err := svc.AddDocumentComment(ownerCtx, tenant.String(), owner, id, versionID, "Managers approve sooner.", nil, ""); err != nil {
		t.Fatal(err)
	}
	content, err := newSurfaceDocumentStoreContent(svc.store)
	if err != nil {
		t.Fatal(err)
	}
	segments, err := newStoreSurfaceSegmentReader(content)
	if err != nil {
		t.Fatal(err)
	}
	thread, err := svc.store.ListComments(ctx, tenant.String(), id, versionID, "person", readerID)
	if err != nil || len(thread) != 1 {
		t.Fatalf("thread = %+v err=%v, want the owner's comment", thread, err)
	}
	read, err := segments.ReadSurfaceSegments(ctx, SurfaceSegmentRequest{
		TenantID: tenant.String(), Kind: chatrecipient.SurfaceHubDocument, ResourceID: id, VersionID: versionID,
		ThreadID: thread[0].ID, InvokerID: readerID, InvokingCommentID: thread[0].ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(read) != 3 {
		t.Fatalf("segments = %+v, want 2 sections and 1 comment", read)
	}
	for _, segment := range read {
		if !surfaceSegmentBelongs(segment.ID, chatrecipient.SurfaceHubDocument, id, versionID) {
			t.Fatalf("segment %q escaped the viewed version", segment.ID)
		}
	}
	if _, err := segments.ReadSurfaceSegments(ctx, SurfaceSegmentRequest{
		TenantID: tenant.String(), Kind: chatrecipient.SurfaceHubDocument, ResourceID: id, VersionID: versionID,
		ThreadID: thread[0].ID, InvokerID: "mallory", InvokingCommentID: thread[0].ID,
	}); !errors.Is(err, documenthubstore.ErrDenied) {
		t.Fatalf("outsider segments error = %v, want the hub denial", err)
	}
	membership, err := newSurfaceDocumentMembership(svc.store)
	if err != nil {
		t.Fatal(err)
	}
	if member, err := membership.SurfaceMember(ctx, tenant.String(), chatrecipient.SurfaceHubDocument, id, versionID, readerID); err != nil || !member {
		t.Fatalf("reader member=%v err=%v, want a member", member, err)
	}
	if member, err := membership.SurfaceMember(ctx, tenant.String(), chatrecipient.SurfaceHubDocument, id, versionID, "mallory"); err != nil || member {
		t.Fatalf("outsider member=%v err=%v, want a non-member", member, err)
	}
	if member, err := membership.SurfaceMember(ctx, tenant.String(), chatrecipient.SurfaceWorkflowCase, "case-1", "", readerID); err != nil || member {
		t.Fatalf("workflow member=%v err=%v, want a non-member until the case surface exists", member, err)
	}
}
