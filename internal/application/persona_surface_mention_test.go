package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentapproval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	transportdocument "github.com/monstercameron/human-capital-management-suite/internal/transport/document"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

// surfaceDirectoryFake resolves the reserved persona handles used by the
// surface tests. Unknown handles are user mentions, not personas.
type surfaceDirectoryFake struct {
	handles map[string]string
	calls   int
}

func (f *surfaceDirectoryFake) ResolveSurfacePersona(_ context.Context, tenantID string, kind chatrecipient.SurfaceKind, handle string) (string, bool, error) {
	f.calls++
	if tenantID != "tenant-a" || !kind.Valid() {
		return "", false, errors.New("unexpected surface directory input")
	}
	id, ok := f.handles[handle]
	return id, ok, nil
}

// surfaceAuthorityFake answers AGENTP-008 admission for surface channels.
// Membership is the surface's own: case participation for workflow cases,
// current read access for hub documents. Mallory is outside every surface.
type surfaceAuthorityFake struct {
	admission agentinvoke.Admission
	calls     int
}

func (f *surfaceAuthorityFake) Resolve(ctx context.Context, request agentinvoke.AdmissionRequest) (agentinvoke.Admission, error) {
	f.calls++
	if _, _, _, ok := parseSurfaceChannel(request.ConversationID); !ok {
		return agentinvoke.Admission{}, errors.New("surface authority serves surface channels only")
	}
	tuple, ok := ctx.Value(personaChatAuthorityTupleKey{}).(PersonaChatAuthorityRequest)
	if !ok || tuple.Tenant.String() != request.TenantID || tuple.InvokerID != request.InvokerID || tuple.ConversationID != request.ConversationID || !required(tuple.ThreadID) || !required(tuple.InvokingPostID) {
		return agentinvoke.Admission{}, errors.New("missing server-created surface authority tuple")
	}
	out := f.admission
	out.Persona.ID = request.PersonaID
	out.HumanMember = request.InvokerID != "mallory"
	return out, nil
}

func surfaceAdmission() agentinvoke.Admission {
	skills := agentinvoke.SkillScopes{"policy.read": {"policy:answer"}}
	return agentinvoke.Admission{
		Persona:      agentinvoke.Persona{ID: "persona-policy", Version: "v3", PinnedSkills: skills, Current: true},
		Installation: agentinvoke.Installation{ID: "placement-surface-1", Current: true, SkillCeiling: skills},
		Channel:      agentinvoke.ChannelPolicy{SkillCeiling: skills},
		Discoverable: skills, HumanMember: true, AudienceMember: true, PersonaInstalled: true,
	}
}

// surfaceSegmentFake returns the invoker-visible segments of one surface
// channel. Unknown channels carry no segments: a mention can only bind the
// viewed resource version.
type surfaceSegmentFake struct {
	segments map[string][]SurfaceSegment
	requests []SurfaceSegmentRequest
}

func (f *surfaceSegmentFake) ReadSurfaceSegments(_ context.Context, request SurfaceSegmentRequest) ([]SurfaceSegment, error) {
	f.requests = append(f.requests, request)
	if request.TenantID != "tenant-a" || request.InvokerID == "" || request.ThreadID == "" || request.InvokingCommentID == "" {
		return nil, errors.New("unexpected surface segment input")
	}
	return append([]SurfaceSegment(nil), f.segments[encodeSurfaceChannel(request.Kind, request.ResourceID, request.VersionID)]...), nil
}

type surfaceRefusalFake struct {
	refusals []agentinvoke.EphemeralRefusal
}

func (f *surfaceRefusalFake) Refuse(_ context.Context, refusal agentinvoke.EphemeralRefusal) error {
	f.refusals = append(f.refusals, refusal)
	return nil
}

type surfacePeerExtractor struct{}

func (surfacePeerExtractor) Extract(_ context.Context, request agentinvoke.PeerExtractionRequest) (agentinvoke.PeerExtraction, error) {
	return agentinvoke.PeerExtraction{SchemaID: "peer.v1", SchemaVersion: "1", SourceDigest: request.Digest, Values: map[string]string{"kind": "message"}}, nil
}

type surfaceHarness struct {
	bridge    *surfaceMentionBridge
	runs      *personaRunFake
	grants    *personaGrantFake
	refusals  *surfaceRefusalFake
	directory *surfaceDirectoryFake
	authority *surfaceAuthorityFake
	segments  *surfaceSegmentFake
}

func newSurfaceHarness(t *testing.T) (*surfaceHarness, context.Context) {
	t.Helper()
	directory := &surfaceDirectoryFake{handles: map[string]string{"policy-helper": "persona-policy", "comp-analyst": "persona-comp"}}
	authority := &surfaceAuthorityFake{admission: surfaceAdmission()}
	segments := &surfaceSegmentFake{segments: map[string][]SurfaceSegment{
		encodeSurfaceChannel(chatrecipient.SurfaceWorkflowCase, "case-1", ""): {
			{ID: "case-1/summary", AuthorID: "alice", Body: "the case summary alice wrote"},
			{ID: "case-1/note-7", AuthorID: "bob", Body: "the peer note bob wrote"},
		},
		encodeSurfaceChannel(chatrecipient.SurfaceHubDocument, "doc-9", "v12"): {
			{ID: "doc-9/v12#section-2", AuthorID: "carol", Body: "the section carol wrote"},
		},
	}}
	runs, grants, refusals := &personaRunFake{}, &personaGrantFake{}, &surfaceRefusalFake{}
	service, err := agentinvoke.NewService(agentinvoke.Config{
		Authority: authority, Grants: grants, Runs: runs,
		Repository: agentinvoke.NewMemoryRepository(), Ephemeral: refusals,
		Threads: surfaceThreadReader{segments: segments}, PeerExtractor: surfacePeerExtractor{},
	})
	if err != nil {
		t.Fatal(err)
	}
	bridge, err := newSurfaceMentionBridge(surfaceMentionBridgeConfig{Directory: directory, Resolver: service})
	if err != nil {
		t.Fatal(err)
	}
	return &surfaceHarness{bridge: bridge, runs: runs, grants: grants, refusals: refusals, directory: directory, authority: authority, segments: segments}, personaBridgeContext(t)
}

func surfaceWorkflowCommit() SurfaceCommentCommit {
	return SurfaceCommentCommit{
		TenantID: "tenant-a", Kind: chatrecipient.SurfaceWorkflowCase, ResourceID: "case-1",
		CommentID: "comment-1", ThreadID: "comment-1", AuthorID: "alice",
		Body: "@policy-helper what changed on this case?", Handles: []string{"policy-helper"},
	}
}

func surfaceDocumentCommit() SurfaceCommentCommit {
	return SurfaceCommentCommit{
		TenantID: "tenant-a", Kind: chatrecipient.SurfaceHubDocument, ResourceID: "doc-9", VersionID: "v12",
		CommentID: "comment-9", ThreadID: "comment-9", AuthorID: "alice",
		Body: "@policy-helper explain section 2", Handles: []string{"policy-helper"},
	}
}

// TestTodo_AGENTP_025 proves one invocation path for both surfaces: a mention
// committed in a workflow case comment and one in a hub document comment each
// resolve through the shared AGENTP-008 resolver, with the surface's audience,
// the viewed resource as tainted context, and AGENTP-014 approvals.
func TestTodo_AGENTP_025(t *testing.T) {
	harness, ctx := newSurfaceHarness(t)
	commits := []SurfaceCommentCommit{surfaceWorkflowCommit(), surfaceDocumentCommit()}
	for _, commit := range commits {
		invocations, err := harness.bridge.HandleCommentCommit(ctx, commit)
		if err != nil || len(invocations) != 1 {
			t.Fatalf("%s mention resolved to %+v err=%v, want one invocation", commit.Kind, invocations, err)
		}
	}
	if len(harness.runs.requests) != 2 || harness.grants.calls != 2 {
		t.Fatalf("runs=%d grants=%d, want one per surface mention", len(harness.runs.requests), harness.grants.calls)
	}
	// The same resolver admitted both: effective skills are the four-operand
	// intersection, and each run is bound to its surface channel and comment.
	for i, request := range harness.runs.requests {
		if len(request.Skills) != 1 || len(request.Skills["policy.read"]) != 1 || request.Skills["policy.read"][0] != "policy:answer" {
			t.Fatalf("effective skills = %+v, want the pinned intersection", request.Skills)
		}
		if request.Mode != agentinvoke.OnBehalfOf || request.InvokerID != "alice" || request.Grant.ID == "" {
			t.Fatalf("run is not an on-behalf-of grant: %+v", request)
		}
		kind, resource, version, ok := parseSurfaceChannel(request.ConversationID)
		if !ok || kind != commits[i].Kind || resource != commits[i].ResourceID || version != commits[i].VersionID {
			t.Fatalf("run channel %q is not the committed surface %+v", request.ConversationID, commits[i])
		}
		if request.ThreadID != commits[i].ThreadID || request.InvokingPostID != commits[i].CommentID || request.Actor.InvocationID != request.InvocationID || request.Actor.ConversationID != request.ConversationID {
			t.Fatalf("run is not bound to its surface comment: %+v", request)
		}
		// The invoker's question is the goal; the viewed resource arrives as
		// digest-bound entries, and anything others wrote is untrusted.
		if request.Context.Goal != commits[i].Body {
			t.Fatalf("run goal = %q, want the invoking question", request.Context.Goal)
		}
		invoker, untrusted := 0, 0
		for _, entry := range request.Context.Entries {
			if !strings.HasPrefix(entry.Digest, "sha256:") {
				t.Fatalf("entry is not digest-bound: %+v", entry)
			}
			if strings.Contains(entry.Digest, "changed on this case") || strings.Contains(entry.Digest, "explain section") ||
				strings.Contains(entry.Digest, "alice wrote") || strings.Contains(entry.Digest, "bob wrote") || strings.Contains(entry.Digest, "carol wrote") {
				t.Fatalf("raw text entered the bound context: %+v", entry)
			}
			switch entry.Taint {
			case agentinvoke.TaintInvokerInstruction:
				invoker++
				if entry.PostID != request.InvokingPostID {
					t.Fatalf("invoker entry is not the invoking comment: %+v", entry)
				}
			case agentinvoke.TaintUntrustedPeer:
				untrusted++
				if entry.Extraction.SourceDigest != entry.Digest {
					t.Fatalf("peer extraction is not bound to its entry: %+v", entry)
				}
			default:
				t.Fatalf("entry carries no taint: %+v", entry)
			}
		}
		if invoker == 0 || untrusted == 0 {
			t.Fatalf("context = %+v, want the invoker's question plus tainted resource entries", request.Context)
		}
	}
	if harness.runs.requests[0].ConversationID == harness.runs.requests[1].ConversationID {
		t.Fatalf("both surfaces share one channel %q", harness.runs.requests[0].ConversationID)
	}
	// Replaying either comment starts nothing more: the claim key covers the
	// surface channel and comment.
	for _, commit := range commits {
		if _, err := harness.bridge.HandleCommentCommit(ctx, commit); err != nil {
			t.Fatal(err)
		}
	}
	if len(harness.runs.requests) != 2 || harness.grants.calls != 2 {
		t.Fatalf("replayed comments started runs: runs=%d grants=%d", len(harness.runs.requests), harness.grants.calls)
	}
	// The audience for delivery is the surface's own readers, decided by the
	// one AGENTP-012 evaluator. Everyone here may read the cited field, so
	// the floor allows the public route and pins the revision the committer
	// must compare.
	authority := &surfaceFloorAuthority{
		snapshot: chatrecipient.AudienceSnapshot{TenantID: "tenant-a", Surface: chatrecipient.SurfaceRef{TenantID: "tenant-a", Kind: chatrecipient.SurfaceHubDocument, ResourceID: "doc-9", Revision: 11}, Revision: 11, CurrentMembers: []chatrecipient.AudiencePrincipal{{TenantID: "tenant-a", SubjectID: "alice"}, {TenantID: "tenant-a", SubjectID: "bob"}}, Complete: true, GuestAndExternalComplete: true},
	}
	decision := chatrecipient.EvaluateAudienceFloor(ctx, chatrecipient.AudienceFloorRequest{
		Surface:     &chatrecipient.SurfaceRef{TenantID: "tenant-a", Kind: chatrecipient.SurfaceHubDocument, ResourceID: "doc-9", Revision: 11},
		Disclosures: []chatrecipient.Disclosure{{SourceID: "doc-9", RecordID: "v12", Field: "section-2", CitationTitle: "Section 2", DataClass: dlp.ClassPublic}},
	}, authority)
	if decision.Route != chatrecipient.RoutePublic || decision.Reason != chatrecipient.AudienceFloorAllowed || decision.SnapshotRevision != 11 || decision.AudienceSize != 2 {
		t.Fatalf("surface floor decision = %+v, want the public route at revision 11", decision)
	}
	// Approvals on these surfaces are the same AGENTP-014 cards, bound to
	// the surface invocation and decided only in the task view.
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	approvals, err := agentapproval.New(func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	approval, err := approvals.Create(agentapproval.CreateRequest{
		ID: "approval-surface-1", TenantID: "tenant-a", TaskID: "task-surface-1", AssignedUserID: "alice", AgentID: "persona-policy",
		OriginChain: []agentapproval.ActorRef{{Kind: agentapproval.ActorAgent, ID: "persona-policy"}},
		Items: []agentapproval.ActionItem{{
			ID: "item-1", Kind: agentapproval.ItemGovernedIntent, Tier: agentapproval.TierSubmitGoverned,
			IntentDefinitionID: "PromoteWorker", IntentDefinitionVersion: "v3",
			Subjects:       []agentapproval.Subject{{ID: "worker-7", Kind: "worker"}},
			MaterialFields: []agentapproval.MaterialField{{Path: "job.level", Before: "L4", After: "L5"}},
			Sources:        []agentapproval.Source{{Ref: "doc-9/v12", Taint: "TRUSTED_INTERNAL"}},
			Uncertainty:    "source is current as of the task checkpoint", RiskClass: "low",
		}},
		TaskExpiresAt: now.Add(48 * time.Hour), Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	card, err := agentapproval.BuildPersonaChatCard(approval, harness.runs.requests[0].InvocationID, "alice", now)
	if err != nil || card.Digest != approval.Digest || card.InvocationID != harness.runs.requests[0].InvocationID || card.InvokerID != "alice" || card.TaskViewHref == "" {
		t.Fatalf("surface approval card = %+v err=%v", card, err)
	}
	if _, err := agentapproval.ResolvePersonaChatCardCallback(card, agentapproval.PersonaChatCardCallback{Action: "approve", ActorID: "alice", InvocationID: card.InvocationID, Now: now, Current: approval}); !errors.Is(err, agentapproval.ErrChatApprovalSurface) {
		t.Fatalf("surface card decided off the task view: %v", err)
	}
}

// surfaceFloorAuthority answers surface audience reads for the application
// surface tests through the one AGENTP-012 evaluator.
type surfaceFloorAuthority struct {
	snapshot chatrecipient.AudienceSnapshot
	denied   map[string]bool
	classErr bool
}

func (f *surfaceFloorAuthority) CurrentAudience(_ context.Context, _ chatcore.Conversation) (chatrecipient.AudienceSnapshot, error) {
	return chatrecipient.AudienceSnapshot{}, errors.New("chat audience is not consulted for a surface decision")
}

func (f *surfaceFloorAuthority) CurrentSurfaceAudience(_ context.Context, surface chatrecipient.SurfaceRef) (chatrecipient.AudienceSnapshot, error) {
	if surface.TenantID != "tenant-a" || surface.ResourceID == "" || surface.Revision == 0 || !surface.Kind.Valid() {
		return chatrecipient.AudienceSnapshot{}, errors.New("unexpected surface audience input")
	}
	return f.snapshot, nil
}

func (f *surfaceFloorAuthority) AuthorizeDisclosure(_ context.Context, principal chatrecipient.AudiencePrincipal, disclosure chatrecipient.Disclosure) error {
	if disclosure.SourceID == "" || disclosure.RecordID == "" || disclosure.Field == "" {
		return errors.New("unexpected disclosure input")
	}
	if f.denied[principal.SubjectID] {
		return errors.New("not authorized for this disclosure")
	}
	return nil
}

func (f *surfaceFloorAuthority) AllowDataClass(_ context.Context, _ chatcore.Conversation, _ dlp.DataClass) error {
	return errors.New("chat policy is not consulted for a surface decision")
}

func (f *surfaceFloorAuthority) AllowSurfaceDataClass(_ context.Context, _ chatrecipient.SurfaceRef, class dlp.DataClass) error {
	if !class.Valid() {
		return errors.New("unexpected data class input")
	}
	if f.classErr {
		return errors.New("class not allowed on this surface")
	}
	return nil
}

// TestTodo_AGENTP_025_Security proves a surface mention never reads beyond the
// invoker's view of that case or document, and never shows an answer where a
// reader could not have read it.
func TestTodo_AGENTP_025_Security(t *testing.T) {
	t.Run("a mention by someone outside the case or document is refused without a run", func(t *testing.T) {
		harness, _ := newSurfaceHarness(t)
		ctx := trust.WithPrincipal(context.Background(), mustBridgePrincipal(t, "mallory", "tenant-a"))
		commit := surfaceDocumentCommit()
		commit.AuthorID = "mallory"
		invocations, err := harness.bridge.HandleCommentCommit(ctx, commit)
		if err != nil || len(invocations) != 0 {
			t.Fatalf("outsider mention resolved to %+v err=%v", invocations, err)
		}
		if len(harness.runs.requests) != 0 || harness.grants.calls != 0 {
			t.Fatalf("outsider mention started a run")
		}
		if len(harness.segments.requests) != 0 {
			t.Fatalf("outsider mention read surface context %d times", len(harness.segments.requests))
		}
		if len(harness.refusals.refusals) != 1 || harness.refusals.refusals[0].Reason != agentinvoke.DenialNotMember {
			t.Fatalf("outsider refusal = %+v, want a typed membership refusal", harness.refusals.refusals)
		}
	})
	t.Run("a mention cannot read past the viewed version or into another resource", func(t *testing.T) {
		harness, ctx := newSurfaceHarness(t)
		// The doc-9 v12 segments exist; asking from v13 (which the invoker may
		// not have) or from another document binds no resource entries.
		for _, commit := range []SurfaceCommentCommit{
			func() SurfaceCommentCommit {
				c := surfaceDocumentCommit()
				c.CommentID, c.ThreadID, c.VersionID = "comment-13", "comment-13", "v13"
				return c
			}(),
			func() SurfaceCommentCommit {
				c := surfaceDocumentCommit()
				c.CommentID, c.ThreadID, c.ResourceID = "comment-x", "comment-x", "doc-1"
				return c
			}(),
		} {
			invocations, err := harness.bridge.HandleCommentCommit(ctx, commit)
			if err != nil || len(invocations) != 1 {
				t.Fatalf("%s/%s mention resolved to %+v err=%v", commit.ResourceID, commit.VersionID, invocations, err)
			}
		}
		if len(harness.runs.requests) != 2 {
			t.Fatalf("runs started = %d, want 2", len(harness.runs.requests))
		}
		for _, request := range harness.runs.requests {
			if len(request.Context.Entries) != 1 || request.Context.Entries[0].PostID != request.InvokingPostID || request.Context.Entries[0].Taint != agentinvoke.TaintInvokerInstruction {
				t.Fatalf("a mention bound entries from outside its viewed resource: %+v", request.Context)
			}
		}
		if len(harness.segments.requests) != 2 || harness.segments.requests[0].VersionID != "v13" || harness.segments.requests[1].ResourceID != "doc-1" {
			t.Fatalf("segment reads = %+v, want the viewed version of each mention", harness.segments.requests)
		}
	})
	t.Run("context rows that escape the invocation resource fail the run closed", func(t *testing.T) {
		harness, ctx := newSurfaceHarness(t)
		harness.segments.segments[encodeSurfaceChannel(chatrecipient.SurfaceHubDocument, "doc-9", "v12")] = []SurfaceSegment{
			{ID: "doc-1/v3#salary-table", AuthorID: "carol", Body: "foreign content"},
		}
		if _, err := harness.bridge.HandleCommentCommit(ctx, surfaceDocumentCommit()); err == nil {
			t.Fatal("context from another document was bound to the run")
		}
		if len(harness.runs.requests) != 0 {
			t.Fatalf("a run started with escaped context: %+v", harness.runs.requests)
		}
	})
	t.Run("thread rows that escape the surface channel fail the shared context check", func(t *testing.T) {
		authority := &surfaceAuthorityFake{admission: surfaceAdmission()}
		runs := &personaRunFake{}
		hostile := agentinvokeThreadReaderFunc(func(_ context.Context, request agentinvoke.ThreadReadRequest) ([]agentinvoke.ThreadPost, error) {
			return []agentinvoke.ThreadPost{{
				TenantID: "tenant-a", ConversationID: "surface:HUB_DOCUMENT:doc-1:v3", ThreadID: request.ThreadID,
				ID: "doc-1/v3#salary-table", AuthorID: "carol", Body: "foreign content",
			}}, nil
		})
		service, err := agentinvoke.NewService(agentinvoke.Config{
			Authority: authority, Grants: &personaGrantFake{}, Runs: runs,
			Repository: agentinvoke.NewMemoryRepository(), Threads: hostile, PeerExtractor: surfacePeerExtractor{},
		})
		if err != nil {
			t.Fatal(err)
		}
		channel := encodeSurfaceChannel(chatrecipient.SurfaceHubDocument, "doc-9", "v12")
		ctx := withPersonaChatAuthorityTuple(personaBridgeContext(t), "tenant-a", "alice", channel, "comment-9", "comment-9")
		post := agentinvoke.PostCommit{TenantID: "tenant-a", ConversationID: channel, ThreadID: "comment-9", PostID: "comment-9",
			AuthorID: "alice", AuthorKind: agentinvoke.HumanAuthor, Body: "@policy-helper explain section 2", New: true,
			Mentions: []agentinvoke.Mention{{Kind: agentinvoke.PersonaMention, PersonaID: "persona-policy", Canonical: true}}}
		if _, err := service.OnPostCommit(ctx, post); err == nil {
			t.Fatal("a thread row from another document passed the shared escape check")
		}
		if len(runs.requests) != 0 {
			t.Fatalf("a run started with escaped thread rows: %+v", runs.requests)
		}
	})
	t.Run("an answer one reader may not read stays private to the invoker", func(t *testing.T) {
		_, ctx := newSurfaceHarness(t)
		authority := &surfaceFloorAuthority{
			snapshot: chatrecipient.AudienceSnapshot{TenantID: "tenant-a", Surface: chatrecipient.SurfaceRef{TenantID: "tenant-a", Kind: chatrecipient.SurfaceWorkflowCase, ResourceID: "case-1", Revision: 4}, Revision: 4, CurrentMembers: []chatrecipient.AudiencePrincipal{{TenantID: "tenant-a", SubjectID: "alice"}, {TenantID: "tenant-a", SubjectID: "bob"}}, Complete: true, GuestAndExternalComplete: true},
			denied:   map[string]bool{"bob": true},
		}
		decision := chatrecipient.EvaluateAudienceFloor(ctx, chatrecipient.AudienceFloorRequest{
			Surface:     &chatrecipient.SurfaceRef{TenantID: "tenant-a", Kind: chatrecipient.SurfaceWorkflowCase, ResourceID: "case-1", Revision: 4},
			Disclosures: []chatrecipient.Disclosure{{SourceID: "case-1", RecordID: "salary", Field: "amount", CitationTitle: "Salary", DataClass: dlp.ClassCompensation}},
		}, authority)
		if decision.Route != chatrecipient.RoutePrivate || decision.Reason != chatrecipient.AudienceFloorUnauthorized {
			t.Fatalf("case floor decision = %+v, want the private route", decision)
		}
	})
	t.Run("unknown handles are user mentions and start nothing", func(t *testing.T) {
		harness, ctx := newSurfaceHarness(t)
		commit := surfaceDocumentCommit()
		commit.Handles = []string{"bob"}
		commit.Body = "@bob what do you think?"
		if invocations, err := harness.bridge.HandleCommentCommit(ctx, commit); err != nil || len(invocations) != 0 {
			t.Fatalf("user mention resolved to %+v err=%v", invocations, err)
		}
		mixed := surfaceDocumentCommit()
		mixed.CommentID, mixed.ThreadID = "comment-10", "comment-10"
		mixed.Handles = []string{"bob", "policy-helper"}
		mixed.Body = "@bob @policy-helper explain section 2"
		if invocations, err := harness.bridge.HandleCommentCommit(ctx, mixed); err != nil || len(invocations) != 1 {
			t.Fatalf("mixed mention resolved to %+v err=%v, want the one persona invocation", invocations, err)
		}
		if len(harness.runs.requests) != 1 {
			t.Fatalf("runs started = %d, want only the persona mention", len(harness.runs.requests))
		}
	})
	t.Run("resolved, orphaned and empty comments are inert", func(t *testing.T) {
		harness, ctx := newSurfaceHarness(t)
		for _, commit := range []SurfaceCommentCommit{
			func() SurfaceCommentCommit { c := surfaceDocumentCommit(); c.Resolved = true; return c }(),
			func() SurfaceCommentCommit { c := surfaceDocumentCommit(); c.Orphaned = true; return c }(),
			func() SurfaceCommentCommit { c := surfaceDocumentCommit(); c.Body = ""; return c }(),
		} {
			if _, err := harness.bridge.HandleCommentCommit(ctx, commit); !errors.Is(err, errSurfaceMentionBridge) {
				t.Fatalf("inert comment %+v error = %v, want the bridge sentinel", commit, err)
			}
		}
		if len(harness.runs.requests) != 0 || len(harness.refusals.refusals) != 0 {
			t.Fatalf("inert comments started runs (%d) or sent refusals (%d)", len(harness.runs.requests), len(harness.refusals.refusals))
		}
	})
	t.Run("a principal that does not match the author starts nothing", func(t *testing.T) {
		harness, _ := newSurfaceHarness(t)
		if _, err := harness.bridge.HandleCommentCommit(context.Background(), surfaceDocumentCommit()); !errors.Is(err, errSurfaceMentionBridge) {
			t.Fatalf("missing principal error = %v, want the bridge sentinel", err)
		}
		wrong := trust.WithPrincipal(context.Background(), mustBridgePrincipal(t, "bob", "tenant-a"))
		if _, err := harness.bridge.HandleCommentCommit(wrong, surfaceDocumentCommit()); !errors.Is(err, errSurfaceMentionBridge) {
			t.Fatalf("mismatched principal error = %v, want the bridge sentinel", err)
		}
		now := time.Date(2026, time.September, 29, 12, 0, 0, 0, time.UTC)
		machine, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant-a"), Subject: "alice", SubjectKind: trust.SubjectKindAgent, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceLow, SessionRef: "bridge-test", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "bridge-credential"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := harness.bridge.HandleCommentCommit(trust.WithPrincipal(context.Background(), machine), surfaceDocumentCommit()); !errors.Is(err, errSurfaceMentionBridge) {
			t.Fatalf("machine principal error = %v, want the bridge sentinel", err)
		}
		if len(harness.runs.requests) != 0 {
			t.Fatalf("untrusted commits started %d runs", len(harness.runs.requests))
		}
	})
}

type agentinvokeThreadReaderFunc func(context.Context, agentinvoke.ThreadReadRequest) ([]agentinvoke.ThreadPost, error)

func (f agentinvokeThreadReaderFunc) ReadThread(ctx context.Context, request agentinvoke.ThreadReadRequest) ([]agentinvoke.ThreadPost, error) {
	return f(ctx, request)
}

func TestSurfaceChannelCodec(t *testing.T) {
	workflow := encodeSurfaceChannel(chatrecipient.SurfaceWorkflowCase, "case-1", "")
	document := encodeSurfaceChannel(chatrecipient.SurfaceHubDocument, "doc-9", "v12")
	if workflow == document {
		t.Fatalf("both surfaces share one channel %q", workflow)
	}
	for _, tc := range []struct {
		channel           string
		kind              chatrecipient.SurfaceKind
		resource, version string
	}{
		{workflow, chatrecipient.SurfaceWorkflowCase, "case-1", ""},
		{document, chatrecipient.SurfaceHubDocument, "doc-9", "v12"},
	} {
		kind, resource, version, ok := parseSurfaceChannel(tc.channel)
		if !ok || kind != tc.kind || resource != tc.resource || version != tc.version {
			t.Fatalf("channel %q parsed as %s/%s/%s ok=%v", tc.channel, kind, resource, version, ok)
		}
	}
	for _, channel := range []string{"", "channel-a", "surface:", "surface:CHAT_ROOM:room", "surface:HUB_DOCUMENT:doc-9",
		"surface:WORKFLOW_CASE:case-1:v1", "surface:HUB_DOCUMENT::v12", "surface:HUB_DOCUMENT:doc-9:", "surface:WORKFLOW_CASE:",
		"surface:HUB_DOCUMENT:doc:9:v12", " surface:HUB_DOCUMENT:doc-9:v12", "surface:HUB_DOCUMENT:doc-9:v12 "} {
		if _, _, _, ok := parseSurfaceChannel(channel); ok {
			t.Fatalf("channel %q parsed, want rejection", channel)
		}
	}
}

func TestSurfaceCommitFromDocumentComment(t *testing.T) {
	root := documenthubstore.Comment{ID: "docc-1", DocumentID: "doc-9", VersionID: "v12", AuthorID: "alice", Body: "@policy-helper explain", Mentions: []string{"policy-helper"}}
	commit := surfaceCommitFromDocumentComment("tenant-a", root)
	if commit.TenantID != "tenant-a" || commit.Kind != chatrecipient.SurfaceHubDocument || commit.ResourceID != "doc-9" || commit.VersionID != "v12" ||
		commit.CommentID != "docc-1" || commit.ThreadID != "docc-1" || commit.AuthorID != "alice" || commit.Body != root.Body || len(commit.Handles) != 1 || commit.Handles[0] != "policy-helper" {
		t.Fatalf("root commit = %+v", commit)
	}
	reply := root
	reply.ID, reply.ParentID = "docc-2", "docc-1"
	if commit := surfaceCommitFromDocumentComment("tenant-a", reply); commit.CommentID != "docc-2" || commit.ThreadID != "docc-1" {
		t.Fatalf("reply commit = %+v, want the parent thread", commit)
	}
	resolved := root
	resolved.Resolved = true
	if commit := surfaceCommitFromDocumentComment("tenant-a", resolved); !commit.Resolved {
		t.Fatalf("resolved commit = %+v, want the resolved flag carried", commit)
	}
}

func TestSurfaceMentionBridgeRequiresPorts(t *testing.T) {
	if _, err := newSurfaceMentionBridge(surfaceMentionBridgeConfig{}); !errors.Is(err, errSurfaceMentionBridge) {
		t.Fatalf("missing ports error = %v, want the bridge sentinel", err)
	}
	var bridge *surfaceMentionBridge
	if _, err := bridge.HandleCommentCommit(context.Background(), surfaceDocumentCommit()); !errors.Is(err, errSurfaceMentionBridge) {
		t.Fatalf("nil bridge error = %v, want the bridge sentinel", err)
	}
}

func TestWithDocumentPersonaMentionsNilSafe(t *testing.T) {
	harness, _ := newSurfaceHarness(t)
	if got := withDocumentPersonaMentions(documentService{}, nil); got.(documentService).personaMentions != nil {
		t.Fatal("nil bridge was wired")
	}
	outer := transportDocumentServiceStub{}
	if got := withDocumentPersonaMentions(outer, harness.bridge); got != outer {
		t.Fatal("foreign service was rewired")
	}
	wired := withDocumentPersonaMentions(documentService{}, harness.bridge).(documentService)
	if wired.personaMentions != harness.bridge {
		t.Fatal("bridge was not wired")
	}
	// An unwired service admits nothing and panics nowhere.
	documentService{}.admitDocumentPersonaMentions(context.Background(), "tenant-a", documenthubstore.Comment{ID: "docc-1"})
}

type transportDocumentServiceStub struct{ transportdocument.Service }

func TestSurfaceThreadReaderRejectsNonSurfaceChannels(t *testing.T) {
	reader := surfaceThreadReader{segments: &surfaceSegmentFake{}}
	request := agentinvoke.ThreadReadRequest{TenantID: "tenant-a", ConversationID: "channel-a", ThreadID: "thread-1", InvokerID: "alice", InvokingPostID: "post-1", Limit: agentinvoke.MaxThreadPosts}
	if _, err := reader.ReadThread(context.Background(), request); err == nil {
		t.Fatal("a chat channel read through the surface reader")
	}
	var nilReader surfaceThreadReader
	surfaceRequest := request
	surfaceRequest.ConversationID = encodeSurfaceChannel(chatrecipient.SurfaceHubDocument, "doc-9", "v12")
	if _, err := nilReader.ReadThread(context.Background(), surfaceRequest); err == nil {
		t.Fatal("a reader without segments served a surface channel")
	}
}
