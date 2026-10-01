package application

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

func TestTodo_AGENTP_008_RuntimeCompositionRequiresProductionOwners(t *testing.T) {
	if _, err := composePersonaRuntimeDependencies(context.Background(), personaRuntimeCompositionInput{}); !errors.Is(err, errPersonaInvocationProductionComposition) {
		t.Fatalf("incomplete runtime err=%v", err)
	}
	reader := personaRuntimeManifestReader{tenant: "tenant-a"}
	if reader.TenantID() != "tenant-a" {
		t.Fatalf("tenant reader escaped scope: %q", reader.TenantID())
	}
	if err := BindPersonaRuntimeTools(&PersonaInvocationProductionConfig{}, nil); !errors.Is(err, ErrPersonaRunExecutorUnavailable) {
		t.Fatalf("missing tool authority err=%v", err)
	}
}

func TestTodo_AGENTP_012_RuntimePublicReplyRespectsImmutableProfilePrivacy(t *testing.T) {
	version, profile := authorityProfile(t)
	installation := agentpersonastore.ActiveInstallation{PersonaID: version.PersonaID, PersonaVersion: version.Version, InstallationID: "install-a", ConversationID: "room-a"}
	id := agentsecurity.FinalOutputIdentity{TenantID: version.TenantID.String(), PersonaID: version.PersonaID, PersonaVersion: "2", InstallationID: "install-a", ConversationID: "room-a"}
	if got, err := personaRuntimePublicProfile(version, installation, id); err != nil || got.PersonaID != id.PersonaID {
		t.Fatalf("current public profile=%+v err=%v", got, err)
	}
	installation.ChannelPolicy.AlwaysPrivate = true
	if _, err := personaRuntimePublicProfile(version, installation, id); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("private installation allowed public: %v", err)
	}
	installation.ChannelPolicy.AlwaysPrivate = false
	profile.AlwaysPrivate = true
	sealed, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	version.Profile, err = json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	version.ContentDigest = sealed.Digest
	if _, err := personaRuntimePublicProfile(version, installation, id); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("private profile allowed public: %v", err)
	}
	profile.AlwaysPrivate = false
	version.Profile, _ = json.Marshal(profile)
	if _, err := personaRuntimePublicProfile(version, installation, id); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("tampered privacy digest allowed public: %v", err)
	}
	id.TenantID = "foreign"
	if _, err := personaRuntimePublicProfile(version, installation, id); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("foreign profile allowed public: %v", err)
	}
}

type runtimeCurrentOwnerFake struct {
	authorityCalls, threadCalls int
	snapshot                    agentrun.AuthoritySnapshot
	posts                       []agentinvoke.ThreadPost
	err                         error
}

func (f *runtimeCurrentOwnerFake) VerifyAdmission(context.Context, agentrun.Request) (agentrun.AuthoritySnapshot, error) {
	f.authorityCalls++
	return f.snapshot, f.err
}
func (f *runtimeCurrentOwnerFake) ReadThread(context.Context, agentinvoke.ThreadReadRequest) ([]agentinvoke.ThreadPost, error) {
	f.threadCalls++
	return f.posts, f.err
}

func TestTodo_AGENTP_008_RuntimeCurrentOwnerForwardsWithoutManufacturedAuthority(t *testing.T) {
	ctx := context.Background()
	owner := &runtimeCurrentOwnerFake{snapshot: agentrun.AuthoritySnapshot{PolicyDigest: "current-owner"}, posts: []agentinvoke.ThreadPost{{ID: "post-a"}}}
	bound := &personaRuntimeCurrentOwners{authority: owner, threads: owner}
	got, err := bound.VerifyAdmission(ctx, agentrun.Request{})
	if err != nil || got.PolicyDigest != "current-owner" || owner.authorityCalls != 1 {
		t.Fatalf("authority=%+v calls=%d err=%v", got, owner.authorityCalls, err)
	}
	posts, err := bound.ReadThread(ctx, agentinvoke.ThreadReadRequest{})
	if err != nil || len(posts) != 1 || posts[0].ID != "post-a" || owner.threadCalls != 1 {
		t.Fatalf("posts=%+v calls=%d err=%v", posts, owner.threadCalls, err)
	}
	owner.err = errors.New("owner revoked")
	if _, err := bound.VerifyAdmission(ctx, agentrun.Request{}); !errors.Is(err, owner.err) {
		t.Fatalf("revoked authority swallowed: %v", err)
	}
	if _, err := bound.ReadThread(ctx, agentinvoke.ThreadReadRequest{}); !errors.Is(err, owner.err) {
		t.Fatalf("revoked source swallowed: %v", err)
	}
	if _, err := (&personaRuntimeCurrentOwners{}).VerifyAdmission(ctx, agentrun.Request{}); !errors.Is(err, errPersonaInvocationProductionComposition) {
		t.Fatalf("unbound current authority: %v", err)
	}
	if _, err := (&personaRuntimeCurrentOwners{}).ReadThread(ctx, agentinvoke.ThreadReadRequest{}); !errors.Is(err, errPersonaInvocationProductionComposition) {
		t.Fatalf("unbound current source: %v", err)
	}
}

type runtimeChatClassFake struct {
	humanCalls, nativeCalls int
	class                   dlp.DataClass
	err                     error
}

type runtimeReplyClassFake struct {
	class        dlp.DataClass
	err          error
	tenant, body string
}

func (f *runtimeReplyClassFake) ClassifyPersonaReplyText(_ context.Context, tenant, body string) (dlp.DataClass, error) {
	f.tenant, f.body = tenant, body
	return f.class, f.err
}

func TestTodo_AGENTP_012_RuntimePublicBodyCannotInheritSourceClassification(t *testing.T) {
	ctx := context.Background()
	owner := &runtimeReplyClassFake{class: dlp.ClassInternal}
	class, err := personaRuntimePublicBodyClass(ctx, owner, "tenant-a", "Exact sealed narrative")
	if err != nil || class != dlp.ClassInternal || owner.tenant != "tenant-a" || owner.body != "Exact sealed narrative" {
		t.Fatalf("body owner not consulted for exact bytes: owner=%+v class=%q err=%v", owner, class, err)
	}
	for _, restricted := range []dlp.DataClass{dlp.ClassPII, dlp.ClassCompensation, dlp.ClassMedical, dlp.ClassBank, dlp.ClassCase, dlp.ClassSpecialCategory, ""} {
		owner.class = restricted
		if _, err := personaRuntimePublicBodyClass(ctx, owner, "tenant-a", "Exact sealed narrative"); !errors.Is(err, chat.ErrPermissionDenied) {
			t.Fatalf("generated narrative class %q inherited public source permission: %v", restricted, err)
		}
	}
	owner.class, owner.err = dlp.ClassPublic, errors.New("body classification unavailable")
	if _, err := personaRuntimePublicBodyClass(ctx, owner, "tenant-a", "Exact sealed narrative"); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("classifier failure allowed public reply: %v", err)
	}
	if _, err := personaRuntimePublicBodyClass(ctx, nil, "tenant-a", "Exact sealed narrative"); !errors.Is(err, ErrPersonaAudienceFloorUnavailable) {
		t.Fatalf("missing body classifier allowed public reply: %v", err)
	}
	if _, err := personaRuntimePublicBodyClass(ctx, owner, "tenant-a", " "); !errors.Is(err, ErrPersonaAudienceFloorUnavailable) {
		t.Fatalf("empty body allowed: %v", err)
	}
}

func (f *runtimeChatClassFake) PersonaPublicChatDisclosureClass(context.Context, string, string, string, string) (dlp.DataClass, error) {
	f.humanCalls++
	return f.class, f.err
}
func (f *runtimeChatClassFake) PublicChatDisclosureClass(ctx context.Context, _, _, _, _ string) (dlp.DataClass, error) {
	f.nativeCalls++
	if _, ok := trust.FromContext(ctx); ok {
		return "", errors.New("workload classification manufactured a human principal")
	}
	return f.class, f.err
}

func TestTodo_AGENTP_008_RuntimeBackgroundClassificationRequiresCurrentReadableSource(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	req := foregroundAuthorityRequest(now)
	body := "Current source body"
	digest := personaRunBytesDigest([]byte(body))
	owner := &runtimeCurrentOwnerFake{posts: []agentinvoke.ThreadPost{{ID: "post-a", TenantID: req.Source.TenantID, ConversationID: req.Audience.ID, Body: body}}}
	classes := &runtimeChatClassFake{class: dlp.ClassInternal}
	bound := &personaRuntimeCurrentOwners{authority: owner, threads: owner, classes: classes, classStore: classes}
	ctx := WithPersonaBackgroundAdmission(context.Background(), agentrun.Record{Request: req})
	got, err := bound.PersonaPublicChatDisclosureClass(ctx, req.Source.TenantID, req.Audience.ID, "post-a", digest)
	if err != nil || got != dlp.ClassInternal || classes.nativeCalls != 1 || classes.humanCalls != 0 || owner.authorityCalls != 1 || owner.threadCalls != 1 {
		t.Fatalf("class=%q owner=%+v classes=%+v err=%v", got, owner, classes, err)
	}
	for _, tc := range []struct{ name, tenant, room, post, digest string }{
		{"foreign tenant", "other", req.Audience.ID, "post-a", digest},
		{"foreign room", req.Source.TenantID, "other", "post-a", digest},
		{"unreadable post", req.Source.TenantID, req.Audience.ID, "other", digest},
		{"changed source", req.Source.TenantID, req.Audience.ID, "post-a", personaRunBytesDigest([]byte("changed"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := classes.nativeCalls
			if _, err := bound.PersonaPublicChatDisclosureClass(ctx, tc.tenant, tc.room, tc.post, tc.digest); !errors.Is(err, ErrPersonaAudienceFloorUnavailable) || classes.nativeCalls != before {
				t.Fatalf("denied source reached classifier: calls=%d err=%v", classes.nativeCalls, err)
			}
		})
	}
	if _, err := bound.PersonaPublicChatDisclosureClass(context.Background(), req.Source.TenantID, req.Audience.ID, "post-a", digest); !errors.Is(err, ErrPersonaAudienceFloorUnavailable) {
		t.Fatalf("missing current workload admission allowed: %v", err)
	}
	owner.err = errors.New("invoker access revoked")
	before := classes.nativeCalls
	if _, err := bound.PersonaPublicChatDisclosureClass(ctx, req.Source.TenantID, req.Audience.ID, "post-a", digest); !errors.Is(err, owner.err) || classes.nativeCalls != before {
		t.Fatalf("revoked admission reached classifier: %v", err)
	}
	owner.err = nil
	classes.err = errors.New("source classification revoked")
	if _, err := bound.PersonaPublicChatDisclosureClass(ctx, req.Source.TenantID, req.Audience.ID, "post-a", digest); !errors.Is(err, classes.err) {
		t.Fatalf("native classification denial swallowed: %v", err)
	}
	classes.err = nil
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "user-1", SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "session-a",
		IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "test:verified-human"})
	if err != nil {
		t.Fatal(err)
	}
	human := trust.WithPrincipal(context.Background(), principal)
	before = classes.nativeCalls
	if _, err := bound.PersonaPublicChatDisclosureClass(human, "tenant-a", req.Audience.ID, "post-a", digest); err != nil || classes.humanCalls != 1 || classes.nativeCalls != before {
		t.Fatalf("human source owner bypassed: %+v err=%v", classes, err)
	}
}

func TestTodo_AGENTP_008_RuntimeGrantBindsPersonaVersionSeparatelyFromManifest(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	request := foregroundAuthorityRequest(now)
	grant := foregroundPositiveGrant(now)
	if request.Persona.Version == request.Agent.Version {
		t.Fatal("fixture must exercise distinct persona and manifest versions")
	}
	if !personaForegroundGrantEnvelope(grant, request, now, grant.RevocationEpoch) {
		t.Fatal("exact persona grant refused")
	}
	grant.AgentVersion = request.Agent.Version
	if personaForegroundGrantEnvelope(grant, request, now, grant.RevocationEpoch) {
		t.Fatal("manifest version accepted as persona delegation version")
	}
}

type runtimeSecurityLeaseStoreFake struct {
	lease agentstore.PersonaSecurityLease
	issue agentstore.PersonaSecurityLeaseRequest
	err   error
	calls int
}

func (s *runtimeSecurityLeaseStoreFake) IssuePersonaSecurityLease(_ context.Context, req agentstore.PersonaSecurityLeaseRequest) (agentstore.PersonaSecurityLease, error) {
	s.calls++
	s.issue = req
	return s.lease, s.err
}
func (s *runtimeSecurityLeaseStoreFake) ResolveActivePersonaSecurityLease(context.Context, uuid.UUID, string, time.Time) (agentstore.PersonaSecurityLease, error) {
	return s.lease, s.err
}

func TestTodo_AGENTP_016_RuntimeIssuesDurableLeaseBeforeLocalBinding(t *testing.T) {
	at := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	tenant := uuid.New()
	record, run := personaRunSecurityEvidenceFixture()
	store := &runtimeSecurityLeaseStoreFake{lease: personaRunSecurityLeaseFixture(tenant, at)}
	switchBoard := agentsecurity.NewKillSwitch()
	resolver := &personaRuntimeSecurityLeases{store: store, switchBoard: switchBoard, tenantUUID: func(values.TenantId) uuid.UUID { return tenant }, now: func() time.Time { return at }}
	lease, err := resolver.ResolvePersonaRunSecurityLease(context.Background(), record, run)
	if err != nil || lease != "lease-a" || store.issue.TenantID != tenant || store.issue.AdmissionID != record.ID || !store.issue.ExpiresAt.Equal(record.Request.Deadline) {
		t.Fatalf("lease=%q issue=%+v err=%v", lease, store.issue, err)
	}
	fence := agentsecurity.NewPersonaRunFence(switchBoard)
	if err := fence.Bind(agentsecurity.PersonaRunID(run.ID), lease); err != nil {
		t.Fatalf("durably issued lease not locally bindable: %v", err)
	}
	if _, err := resolver.ResolvePersonaRunSecurityLease(context.Background(), record, run); err != nil {
		t.Fatalf("exact replay denied: %v", err)
	}
	store.err = agentstore.ErrPersonaSecurityLeaseRevoked
	if _, err := resolver.ResolvePersonaRunSecurityLease(context.Background(), record, run); !errors.Is(err, agentstore.ErrPersonaSecurityLeaseRevoked) {
		t.Fatalf("revoked store bypassed by local lease: %v", err)
	}
	run.TenantID = "foreign"
	before := store.calls
	if _, err := resolver.ResolvePersonaRunSecurityLease(context.Background(), record, run); !errors.Is(err, errPersonaRunSecurityLeaseResolver) || store.calls != before {
		t.Fatalf("foreign run reached issuer calls=%d err=%v", store.calls, err)
	}
}

func TestTodo_AGENTP_012_RuntimeOutputRequiresSealedCurrentAuthority(t *testing.T) {
	record, run := personaRunSecurityEvidenceFixture()
	if _, err := (&personaRuntimeOutputAuthority{}).ResolvePersonaRunChatReplyAuthority(context.Background(), record, run); !errors.Is(err, ErrPersonaRunOutputValidatorUnavailable) {
		t.Fatalf("missing output owner accepted: %v", err)
	}
	if _, err := (&PersonaRuntimeAudienceFloor{}).AuthorizePersonaOutput(context.Background(), agentsecurity.FinalOutputPersistence{}); !errors.Is(err, ErrPersonaAudienceFloorUnavailable) {
		t.Fatalf("unsealed output reached audience authority: %v", err)
	}
	if _, err := (&personaRuntimeSecurityLeases{}).ResolvePersonaRunSecurityLease(context.Background(), record, runstate.Run{}); !errors.Is(err, errPersonaRunSecurityLeaseResolver) {
		t.Fatalf("missing run accepted: %v", err)
	}
}
