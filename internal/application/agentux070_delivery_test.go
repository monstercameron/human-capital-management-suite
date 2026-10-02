package application

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

// agentUX070Room is one channel with an asker and a colleague, the asker's own
// conversation with the agent, and a real chat service over PostgreSQL. Only
// the model's answer is a fixture: the answer, its cited source, the
// audience decision on that source, the delivery and every post are real.
type agentUX070Room struct {
	t        *testing.T
	ctx      context.Context
	store    *chatstore.Adapter
	service  *chat.Service
	now      time.Time
	asker    chat.Principal
	direct   string
	channel  string
	question chat.Post
	receipts *personaSurfaceReceiptFixture
}

type agentUX070Options struct {
	kind     chat.ConversationKind
	question string
}

func newAgentUX070Room(t *testing.T, options agentUX070Options) *agentUX070Room {
	t.Helper()
	if options.kind == "" {
		options.kind = chat.PublicChannel
	}
	db := pgtest.NewEmpty(t)
	applyPersonaChatMigrations(t, db)
	raw, err := chatstore.New(context.Background(), chatstore.Config{DSN: personaChatSchemaDSN(t, db.URL, db.Schema)})
	if err != nil {
		t.Fatal(err)
	}
	store := chatstore.NewAdapter(raw)
	t.Cleanup(store.Close)
	now := time.Now().UTC()
	clock := func() time.Time { return now }
	service := chat.NewService(store, clock)
	service.SetAuthority(servedPersonaChatAuthority{})
	service.SetEphemeralStore(store)
	service.SetAgentSourceAccess(agentUX070SourceAccess{})
	room := &agentUX070Room{t: t, store: store, service: service, now: now, asker: chat.Principal{TenantID: "tenant-a", SubjectID: "owner"}, direct: "owner-policy-helper", channel: "general", receipts: &personaSurfaceReceiptFixture{}}
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "owner", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "agentux070", CredentialDigest: "agentux070", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	room.ctx = trust.WithPrincipal(context.Background(), principal)
	if _, err := service.CreateConversation(room.ctx, chat.CreateConversationRequest{Principal: room.asker, TenantID: "tenant-a", ConversationID: room.channel, Kind: options.kind, Name: "general"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AddMembership(room.ctx, chat.AddMembershipRequest{Principal: room.asker, Membership: chat.Membership{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: room.channel, SubjectID: "employee", HistoryVisibility: chat.FullHistory}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateConversation(room.ctx, chat.CreateConversationRequest{Principal: room.asker, TenantID: "tenant-a", ConversationID: room.direct, Kind: chat.Direct, Name: "Policy Helper"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AddMembership(room.ctx, chat.AddMembershipRequest{Principal: room.asker, Membership: chat.Membership{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: room.direct, SubjectID: "policy-helper", HistoryVisibility: chat.FullHistory}}); err != nil {
		t.Fatal(err)
	}
	service.SetPersonaDMResolver(agentp011PersonaDM{conversationID: room.direct})
	room.question, err = service.SendPost(room.ctx, chat.SendPostRequest{Principal: room.asker, TenantID: "tenant-a", ConversationID: room.channel, Body: options.question, IdempotencyKey: "agentux070-question"})
	if err != nil {
		t.Fatal(err)
	}
	return room
}

// agentUX070Questions reads the question back through real chat, as the run's
// thread reader does.
type agentUX070Questions struct{ room *agentUX070Room }

func (q agentUX070Questions) ReadThread(ctx context.Context, request agentinvoke.ThreadReadRequest) ([]agentinvoke.ThreadPost, error) {
	listed, err := q.room.service.ListPosts(ctx, chat.ListPostsRequest{Principal: chat.Principal{TenantID: request.TenantID, SubjectID: request.InvokerID}, TenantID: request.TenantID, ConversationID: request.ConversationID, Page: chat.Page{PageSize: 50}})
	if err != nil {
		return nil, err
	}
	var out []agentinvoke.ThreadPost
	for _, post := range listed.Posts {
		out = append(out, agentinvoke.ThreadPost{TenantID: post.TenantID, ConversationID: post.ConversationID, ThreadID: request.ThreadID, ID: post.ID, AuthorID: post.AuthorID, Body: post.Body})
	}
	return out, nil
}

// agentUX070Floor is the production floor in two pieces: the agent's own
// privacy setting, read by the same function that guards every public answer,
// and the per-member document decision.
type agentUX070Floor struct {
	profile      func(agentsecurity.FinalOutputIdentity) error
	inner        proactiveOutputFloor
	deniedReader string
}

func (f agentUX070Floor) AuthorizePersonaOutput(ctx context.Context, output agentsecurity.FinalOutputPersistence) (chat.PersonaAudienceDecision, error) {
	if err := f.profile(output.Identity()); err != nil {
		return chat.PersonaAudienceDecision{}, err
	}
	return f.inner.AuthorizePersonaOutput(ctx, output)
}

func agentUX070Profile(t *testing.T, alwaysPrivate bool) func(agentsecurity.FinalOutputIdentity) error {
	t.Helper()
	return agentUX070ProfileInChannel(t, alwaysPrivate, false)
}

// agentUX070ProfileInChannel is the agent's profile with its setting, installed
// in a channel whose administrator does or does not require private answers.
func agentUX070ProfileInChannel(t *testing.T, alwaysPrivate, channelRequires bool) func(agentsecurity.FinalOutputIdentity) error {
	t.Helper()
	profile := agentpersona.PersonaProfile{
		Manifest:  agentpersona.AgentManifestRef{ID: "agent", Version: 1, Digest: "manifest", SchemaVersion: 1},
		PersonaID: "policy-helper", Version: 1, Handle: "policy-helper", DisplayName: "Policy Helper", AvatarRef: "avatar", Purpose: "Answer policy questions", Instructions: "Answer from policy documents.", Owner: "owner", Steward: "steward", EvalSuiteRef: "eval",
		Audience:    agentpersona.Audience{Roles: []string{"ALL_MEMBERS"}, Populations: []string{"TENANT_WIDE"}, OrganizationScopes: []string{"org-a"}},
		TierCeiling: agentskills.TierRead, SkillPins: []agentskills.SkillPin{{ID: "skill.read", Version: 1, Digest: "skill-digest"}}, AlwaysPrivate: alwaysPrivate,
		ConversationKinds: []agentpersona.ConversationKind{agentpersona.ConversationDirect}, ChannelClasses: []agentpersona.ChannelClass{agentpersona.ChannelPrivate},
		EvalLimits: agentpersona.EvaluationLimits{MaxCost: 1, MaxSteps: 1, MaxLatencyMS: 1},
	}
	sealed, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	version := agentpersonastore.PersonaVersion{TenantID: values.TenantId("tenant-a"), PersonaID: "policy-helper", Version: 1, AgentVersion: "agent-v1", Profile: raw, ContentDigest: sealed.Digest}
	return func(id agentsecurity.FinalOutputIdentity) error {
		installation := agentpersonastore.ActiveInstallation{InstallationID: id.InstallationID, PersonaID: id.PersonaID, PersonaVersion: 1, ConversationID: id.ConversationID, ChannelPolicy: agentpersonastore.ChannelPolicy{AlwaysPrivate: channelRequires}}
		_, err := personaRuntimePublicProfile(version, installation, id)
		return err
	}
}

// deliver runs one answer through the delivery the served product composes: the
// current-authority wrapper that reads the question, then the real delivery over
// real chat and the real public commit, then the receipt that names the agent.
func (r *agentUX070Room) deliver(options agentUX070Delivery) (PersonaReplyDeliveryReceipt, error) {
	r.t.Helper()
	conversationID, question := options.conversation, r.question
	if conversationID == "" {
		conversationID = r.channel
	} else {
		var err error
		question, err = r.service.SendPost(r.ctx, chat.SendPostRequest{Principal: r.asker, TenantID: "tenant-a", ConversationID: conversationID, Body: "how many PTO hours carry over?", IdempotencyKey: "agentux070-direct-question"})
		if err != nil {
			r.t.Fatal(err)
		}
	}
	document := AgentAnnouncementResolvedDocument{DocumentID: "pto-policy", Version: "1", Title: "Paid time off policy", Content: "Employees carry over 40 hours.", Digest: personaRunT0ToolOutputDigest([]byte("Employees carry over 40 hours."))}
	request := AgentAnnouncementRunRequest{TenantID: "tenant-a", ConversationID: conversationID, PersonaID: "policy-helper", InstallationID: "install", OwnerID: "owner", OccurrenceID: question.ID, Documents: []AgentAnnouncementResolvedDocument{document}}
	output := proactiveSealedResult(r.t, request, "Employees carry over up to 40 hours of unused PTO.").Output
	revision, err := r.store.AudienceRevision(r.ctx, "tenant-a", r.channel)
	if err != nil {
		r.t.Fatal(err)
	}
	members := []chatrecipient.AudiencePrincipal{{TenantID: "tenant-a", SubjectID: "owner"}, {TenantID: "tenant-a", SubjectID: "employee"}}
	snapshot := chatrecipient.AudienceSnapshot{TenantID: "tenant-a", ConversationID: r.channel, Revision: revision, CurrentMembers: members, EligibilityPopulation: members, Complete: true, EligibilityComplete: true, GuestAndExternalComplete: true}
	conversation := chat.Conversation{TenantID: "tenant-a", ID: r.channel, Kind: chat.PublicChannel, Revision: 1}
	inner := proactiveOutputFloor{floor: &PersonaRuntimeAudienceFloor{Authority: proactiveAudience{snapshot: snapshot}, Documents: proactiveDocumentAuthority{denied: options.unreadableBy}}, conversation: conversation, body: "Employees carry over up to 40 hours of unused PTO."}
	floor := agentUX070Floor{profile: agentUX070ProfileInChannel(r.t, options.alwaysPrivate, options.channelPrivate), inner: inner}
	committer, err := newPersonaPublicReplyCommitter(r.store.Store, &personaPipelineCurrentReplyAuthority{runtimeReplyCurrentAuthorityFake: &runtimeReplyCurrentAuthorityFake{}, store: r.store}, privateChatGatewayVerifiedWorker(r.t, r.now), func() time.Time { return r.now })
	if err != nil {
		r.t.Fatal(err)
	}
	delivery, err := NewPersonaReplyDelivery(r.service, committer, floor)
	if err != nil {
		r.t.Fatal(err)
	}
	var questions agentinvoke.ThreadReader = agentUX070Questions{room: r}
	if options.unreadableQuestion {
		questions = agentUX070FailingQuestions{}
	}
	current := &personaRuntimeCurrentReply{next: delivery, authority: &runtimeReplyCurrentAuthorityFake{}, questions: questions}
	recorder := &personaReplyReceiptRecorder{next: current, store: r.receipts, actors: &personaReceiptActorFixture{actor: personaReplyActor{AgentID: "agent:policy-helper", Display: "Policy Helper", InvokerHandle: "owner"}}}
	documents := []agentdocref.ResolvedDocument{{Reference: agentdocref.Reference{DocumentID: "pto-policy"}, Title: "Paid time off policy"}}
	details := []PersonaReplyCitationDetail{{DocumentID: "pto-policy", VersionID: "1", Index: 1}}
	return recorder.Deliver(r.ctx, PersonaReplyDeliveryRequest{Principal: r.asker, Output: output, IdempotencyKey: output.Identity().AdmissionID, Documents: documents, CitationDetails: details})
}

type agentUX070Delivery struct {
	conversation       string
	alwaysPrivate      bool
	channelPrivate     bool
	unreadableBy       map[string]bool
	unreadableQuestion bool
}

func (r *agentUX070Room) channelPosts(subject string) []chat.Post {
	r.t.Helper()
	listed, err := r.store.ListPosts(r.ctx, chat.Principal{TenantID: "tenant-a", SubjectID: subject}, "tenant-a", r.channel, 0, chat.Page{PageSize: 20}, chat.PostWindow{})
	if err != nil {
		r.t.Fatal(err)
	}
	return agentUX070Said(listed.Posts)
}

// agentUX070Said drops the line Chat records when a member is added
// (CHATUX-021): it is not part of what was said, and these tests count the
// question and its answers.
func agentUX070Said(posts []chat.Post) []chat.Post {
	said := make([]chat.Post, 0, len(posts))
	for _, post := range posts {
		if _, joined := chat.ParseMembershipAdded(post.Body); !joined {
			said = append(said, post)
		}
	}
	return said
}

func (r *agentUX070Room) directPosts() []chat.Post {
	r.t.Helper()
	listed, err := r.store.ListPosts(r.ctx, r.asker, "tenant-a", r.direct, 0, chat.Page{PageSize: 20}, chat.PostWindow{})
	if err != nil {
		r.t.Fatal(err)
	}
	return agentUX070Said(listed.Posts)
}

func (r *agentUX070Room) cards() []chat.EphemeralPost {
	r.t.Helper()
	cards, _, err := r.store.ListEphemeral(r.ctx, r.asker, "tenant-a", r.channel, 0, 20)
	if err != nil {
		r.t.Fatal(err)
	}
	return cards
}

// assertPublic is the whole of what AGENTUX-070 asks of a public answer: one
// ordinary message from the agent, under the question, the same to everyone who
// can read the channel, with its Sources; and no card and no copy in the asker's
// own conversation with the agent.
func (r *agentUX070Room) assertPublic(receipt PersonaReplyDeliveryReceipt, err error) chat.Post {
	r.t.Helper()
	if err != nil || !receipt.Public || receipt.Private || receipt.PublicPostID == "" {
		r.t.Fatalf("answer was not posted to the channel: %+v %v", receipt, err)
	}
	var answer chat.Post
	for _, subject := range []string{"owner", "employee"} {
		posts := r.channelPosts(subject)
		if len(posts) != 2 || posts[1].ID != receipt.PublicPostID || posts[1].AuthorID != "policy-helper" || posts[1].ParentID != r.question.ID {
			r.t.Fatalf("%s did not see the agent's answer under the question: %+v", subject, posts)
		}
		if !strings.Contains(posts[1].Body, "40 hours") || !strings.Contains(posts[1].Body, "Sources") || !strings.Contains(posts[1].Body, "Paid time off policy") || strings.Contains(posts[1].Body, "chat.agent.private") {
			r.t.Fatalf("%s saw a public answer without its sources or with a private marker: %s", subject, posts[1].Body)
		}
		answer = posts[1]
	}
	if cards := r.cards(); len(cards) != 0 {
		r.t.Fatalf("a public answer also left a private card: %+v", cards)
	}
	if direct := r.directPosts(); len(direct) != 0 {
		r.t.Fatalf("a public answer was also copied into the asker's conversation with the agent: %+v", direct)
	}
	surface := &PersonaChatSurface{Chat: r.service, Receipts: r.receipts}
	for _, subject := range []string{"owner", "employee"} {
		viewer, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: subject, SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "agentux070-viewer", CredentialDigest: "agentux070-viewer", IssuedAt: r.now.Add(-time.Minute), ExpiresAt: r.now.Add(time.Hour)})
		if err != nil {
			r.t.Fatal(err)
		}
		actors, err := surface.visiblePostActors(trust.WithPrincipal(context.Background(), viewer), viewer, chat.Conversation{ID: r.channel, TenantID: "tenant-a"})
		if err != nil || len(actors) != 1 || actors[0].PostID != answer.ID || actors[0].AgentID != "agent:policy-helper" || actors[0].InvokerHandle != "owner" {
			r.t.Fatalf("%s could not tell who the agent answered for: %+v %v", subject, actors, err)
		}
	}
	return answer
}

// assertPrivate checks a private answer: the colleague sees only the question,
// the asker's card says why in one marker, and the copy saved in the asker's
// conversation with the agent carries the answer without that marker.
func (r *agentUX070Room) assertPrivate(receipt PersonaReplyDeliveryReceipt, err error, reason string) chat.EphemeralPost {
	r.t.Helper()
	if err != nil || !receipt.Private || receipt.Public || receipt.EphemeralPostID == "" {
		r.t.Fatalf("answer was not delivered privately: %+v %v", receipt, err)
	}
	if posts := r.channelPosts("employee"); len(posts) != 1 || posts[0].ID != r.question.ID {
		r.t.Fatalf("the colleague saw more than the question: %+v", posts)
	}
	cards := r.cards()
	if len(cards) != 1 || !cards[0].OnlyVisibleToYou {
		r.t.Fatalf("the asker has no private card: %+v", cards)
	}
	clean, got := chat.SplitPrivateReason(cards[0].Body)
	if got != reason || !strings.Contains(clean, "40 hours") {
		r.t.Fatalf("private card reason = %q, want %q: %s", got, reason, cards[0].Body)
	}
	direct := r.directPosts()
	if len(direct) != 1 || !strings.Contains(direct[0].Body, "40 hours") || strings.Contains(direct[0].Body, "chat.agent.private") {
		r.t.Fatalf("the saved copy is missing or carries the card's reason marker: %+v", direct)
	}
	if employeeCards, _, err := r.store.ListEphemeral(r.ctx, chat.Principal{TenantID: "tenant-a", SubjectID: "employee"}, "tenant-a", r.channel, 0, 20); err != nil || len(employeeCards) != 0 {
		r.t.Fatalf("the colleague was delivered the private card: %+v %v", employeeCards, err)
	}
	return cards[0]
}

// TestTodo_AGENTUX_070 covers each way an answer to a mention in a channel stays
// private instead of being posted to the channel: the agent is strict, a source
// is not readable by every member, the question could not be read; and a direct
// conversation is unchanged. The public case is TestAgentUXPublicAnswer_Default.
func TestTodo_AGENTUX_070(t *testing.T) {
	t.Run("private because the agent is strict", func(t *testing.T) {
		room := newAgentUX070Room(t, agentUX070Options{question: "@Policy Helper how many PTO hours carry over?"})
		receipt, err := room.deliver(agentUX070Delivery{alwaysPrivate: true})
		room.assertPrivate(receipt, err, chat.PrivateReasonAgent)
	})
	t.Run("private because a source is not readable by every member", func(t *testing.T) {
		room := newAgentUX070Room(t, agentUX070Options{question: "@Policy Helper how many PTO hours carry over?"})
		receipt, err := room.deliver(agentUX070Delivery{unreadableBy: map[string]bool{"employee": true}})
		room.assertPrivate(receipt, err, chat.PrivateReasonAudience)
	})
	t.Run("private because the question could not be read", func(t *testing.T) {
		room := newAgentUX070Room(t, agentUX070Options{question: "@Policy Helper how many PTO hours carry over?"})
		receipt, err := room.deliver(agentUX070Delivery{unreadableQuestion: true})
		room.assertPrivate(receipt, err, chat.PrivateReasonAudience)
	})
	t.Run("direct conversation is unchanged", func(t *testing.T) {
		room := newAgentUX070Room(t, agentUX070Options{question: "@Policy Helper how many PTO hours carry over?"})
		receipt, err := room.deliver(agentUX070Delivery{conversation: "owner-policy-helper"})
		if err != nil || receipt.Public || !receipt.Private {
			t.Fatalf("direct answer was %+v %v", receipt, err)
		}
		if posts := room.channelPosts("employee"); len(posts) != 1 {
			t.Fatalf("a direct answer reached the channel: %+v", posts)
		}
		// The answer is the second message of the direct conversation, with no reason line.
		if direct := room.directPosts(); len(direct) != 2 || !strings.Contains(direct[1].Body, "40 hours") || strings.Contains(direct[1].Body, "chat.agent.private") {
			t.Fatalf("direct conversation = %+v", direct)
		}
	})
}

// TestTodo_AGENTUX_070_PrivateByPhrase asks for privacy in the question, in
// each language the product speaks. The answer is the asker's alone and the card
// says that they asked.
func TestTodo_AGENTUX_070_PrivateByPhrase(t *testing.T) {
	for name, question := range map[string]string{
		"en": "@Policy Helper how many PTO hours carry over? Keep this private.",
		"de": "@Policy Helper Wie viele Urlaubsstunden verfallen nicht? Bitte privat halten.",
		"ar": "@Policy Helper كم ساعة إجازة تُرحَّل؟ اجعل هذا خاصا",
	} {
		t.Run(name, func(t *testing.T) {
			room := newAgentUX070Room(t, agentUX070Options{question: question})
			receipt, err := room.deliver(agentUX070Delivery{})
			room.assertPrivate(receipt, err, chat.PrivateReasonAsked)
		})
	}
}

// TestTodo_AGENTUX_070_ProfileGate pins the agent's own setting on the function
// the floor uses for it, for the agent's profile and for the installation.
func TestTodo_AGENTUX_070_ProfileGate(t *testing.T) {
	id := agentsecurity.FinalOutputIdentity{TenantID: "tenant-a", PersonaID: "policy-helper", PersonaVersion: "1", InstallationID: "install", ConversationID: "general"}
	if err := agentUX070Profile(t, false)(id); err != nil {
		t.Fatalf("an open agent was refused: %v", err)
	}
	err := agentUX070Profile(t, true)(id)
	if got := personaPrivateReasonOf(err); got != chat.PrivateReasonAgent {
		t.Fatalf("a strict agent's reason = %q (%v)", got, err)
	}
	// The channel's requirement is its own reason and outranks the agent's setting.
	for _, strict := range []bool{false, true} {
		err = agentUX070ProfileInChannel(t, strict, true)(id)
		if got := personaPrivateReasonOf(err); got != chat.PrivateReasonChannel {
			t.Fatalf("a channel that requires private answers (agent strict=%v) gave reason %q (%v)", strict, got, err)
		}
	}
	// Anything else that refuses a public answer is the audience check.
	if got := personaPrivateReasonOf(chat.ErrPermissionDenied); got != chat.PrivateReasonAudience {
		t.Fatalf("a plain refusal's reason = %q", got)
	}
	var _ = dlp.ClassInternal
}

// agentUX070FailingQuestions is a thread read that fails, as one does when the
// post store is unavailable.
type agentUX070FailingQuestions struct{}

func (agentUX070FailingQuestions) ReadThread(context.Context, agentinvoke.ThreadReadRequest) ([]agentinvoke.ThreadPost, error) {
	return nil, chat.ErrUnavailable
}

// TestAgentUXPublicAnswer_Default: a person mentions an agent in a channel and the
// answer is posted to the channel as one ordinary message from the agent, under
// the question, for everyone who can read it, with its Sources, attributed to the
// agent and to the person who asked; it is not also a private card or a copy in
// the asker's own conversation with the agent.
func TestAgentUXPublicAnswer_Default(t *testing.T) {
	room := newAgentUX070Room(t, agentUX070Options{question: "@Policy Helper how many PTO hours carry over?"})
	receipt, err := room.deliver(agentUX070Delivery{})
	answer := room.assertPublic(receipt, err)
	// A member who joins afterwards reads the same message.
	if _, err := room.service.AddMembership(room.ctx, chat.AddMembershipRequest{Principal: room.asker, Membership: chat.Membership{TenantID: "tenant-a", HomeTenantID: "tenant-a", ConversationID: room.channel, SubjectID: "late-joiner", HistoryVisibility: chat.FullHistory}}); err != nil {
		t.Fatal(err)
	}
	if posts := room.channelPosts("late-joiner"); len(posts) != 2 || posts[1].Body != answer.Body {
		t.Fatalf("a member added later reads a different answer: %+v", posts)
	}
}

// TestAgentUXPublicAnswer_Default_Property: for every combination of the agent's
// setting, the asker's request and the audience check, where the answer goes is
// the decision table and is never wider than the audience check allows. Public
// needs all of them to agree; otherwise the answer is the asker's alone and the
// reason is the channel's requirement first, then the agent's setting, then the
// asker's request, then the audience.
func TestAgentUXPublicAnswer_Default_Property(t *testing.T) {
	phrases := map[string]string{
		"none": "@Policy Helper how many PTO hours carry over?",
		"en":   "@Policy Helper how many PTO hours carry over? Just for me.",
		"de":   "@Policy Helper Wie viele Urlaubsstunden verfallen nicht? Nur für mich.",
		"ar":   "@Policy Helper كم ساعة إجازة تُرحَّل؟ لي وحدي",
	}
	// The channel's requirement outranks the agent's setting, which outranks the
	// asker's word, which outranks the audience check; the answer is public only
	// when none of them holds (AGENTUX-070).
	for _, channel := range []bool{false, true} {
		for _, strict := range []bool{false, true} {
			for _, unreadable := range []bool{false, true} {
				for _, language := range []string{"none", "en", "de", "ar"} {
					name := fmt.Sprintf("channel=%v/strict=%v/unreadable=%v/phrase=%s", channel, strict, unreadable, language)
					t.Run(name, func(t *testing.T) {
						room := newAgentUX070Room(t, agentUX070Options{question: phrases[language]})
						options := agentUX070Delivery{alwaysPrivate: strict, channelPrivate: channel}
						if unreadable {
							options.unreadableBy = map[string]bool{"employee": true}
						}
						receipt, err := room.deliver(options)
						switch {
						case channel:
							room.assertPrivate(receipt, err, chat.PrivateReasonChannel)
						case strict:
							room.assertPrivate(receipt, err, chat.PrivateReasonAgent)
						case language != "none":
							room.assertPrivate(receipt, err, chat.PrivateReasonAsked)
						case unreadable:
							room.assertPrivate(receipt, err, chat.PrivateReasonAudience)
						default:
							room.assertPublic(receipt, err)
						}
					})
				}
			}
		}
	}
}
