package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

type personaAcceptedClassFake struct {
	calls int
	post  chat.Post
	err   error
	check func(chat.Post)
}

func (f *personaAcceptedClassFake) ClassifyAcceptedHumanPost(_ context.Context, post chat.Post) error {
	f.calls++
	f.post = post
	if f.check != nil {
		f.check(post)
	}
	return f.err
}

type personaClassRoomFake struct {
	room  chat.Conversation
	err   error
	calls int
}

func (f *personaClassRoomFake) GetConversation(_ context.Context, r chat.GetConversationRequest) (chat.Conversation, error) {
	f.calls++
	if r.TenantID != "tenant-a" || r.ConversationID != "channel-a" || r.Principal.SubjectID != "alice" {
		return chat.Conversation{}, chat.ErrPermissionDenied
	}
	return f.room, f.err
}
func personaClassifiedPost() chat.Post {
	return chat.Post{ID: "post-1", TenantID: "tenant-a", ConversationID: "channel-a", AuthorID: "alice", AuthorHomeTenantID: "tenant-a", Revision: 1, Body: "Stored canonical policy question", References: personaSendRequest().References}
}
func personaClassifiedRoom(kind chat.ConversationKind) *personaClassRoomFake {
	return &personaClassRoomFake{room: chat.Conversation{ID: "channel-a", TenantID: "tenant-a", Kind: kind, Revision: 1}}
}

func TestTodo_AGENTP_012_ClassifiedHumanWriter_Handoff(t *testing.T) {
	for _, kind := range []chat.ConversationKind{chat.PublicChannel, chat.PrivateChannel, chat.Direct, chat.Group} {
		for _, failure := range []bool{false, true} {
			t.Run(string(kind)+map[bool]string{true: " failed", false: " accepted"}[failure], func(t *testing.T) {
				service, inner, refs, runs, grants, failures, ctx := personaInvocationFixture(t, trust.SubjectKindHuman, personaAdmission(), nil)
				inner.post = personaClassifiedPost()
				classifier := &personaAcceptedClassFake{check: func(post chat.Post) {
					if inner.calls != 1 || refs.calls != 0 || grants.calls != 0 || len(runs.requests) != 0 || post.Body != inner.post.Body || post.Body == personaSendRequest().Body {
						t.Fatalf("classification not between exact durable write and admission: %+v", post)
					}
				}}
				if failure {
					classifier.err = errors.New("classifier unavailable")
				}
				ports := personaInvocationServedPorts{chat: inner, conversations: personaClassifiedRoom(kind)}
				wrapped, err := ports.ClassifiedHumanWriter(classifier)
				if err != nil {
					t.Fatal(err)
				}
				if ports.chat != inner {
					t.Fatal("wrapping mutated original writer")
				}
				service.chat = wrapped
				post, err := service.SendPost(ctx, personaSendRequest())
				if err != nil || post.ID != inner.post.ID || post.Body != inner.post.Body || classifier.calls != 1 {
					t.Fatalf("durable response post=%+v err=%v classifier=%d", post, err, classifier.calls)
				}
				if failure {
					if refs.calls != 0 || grants.calls != 0 || len(runs.requests) != 0 || len(failures.errs) != 1 || !errors.Is(failures.errs[0], ErrPersonaHumanPostClassificationFailed) || !errors.Is(failures.errs[0], classifier.err) {
						t.Fatalf("failed classification reached admission refs=%d grants=%d runs=%d failures=%v", refs.calls, grants.calls, len(runs.requests), failures.errs)
					}
				} else if refs.calls != 1 || grants.calls != 1 || len(runs.requests) != 1 || len(failures.errs) != 0 {
					t.Fatalf("classified handoff refs=%d grants=%d runs=%d failures=%v", refs.calls, grants.calls, len(runs.requests), failures.errs)
				}
			})
		}
	}
}

func TestTodo_AGENTP_012_ClassifiedHumanWriter_Security(t *testing.T) {
	_, _, _, _, _, _, ctx := personaInvocationFixture(t, trust.SubjectKindHuman, personaAdmission(), nil)
	for _, tc := range []struct {
		name     string
		change   func(*chat.Post)
		kind     chat.ConversationKind
		roomErr  error
		archived bool
	}{
		{name: "foreign tenant", change: func(p *chat.Post) { p.TenantID = "foreign" }},
		{name: "foreign home", change: func(p *chat.Post) { p.AuthorHomeTenantID = "foreign" }},
		{name: "another room", change: func(p *chat.Post) { p.ConversationID = "another" }},
		{name: "another author", change: func(p *chat.Post) { p.AuthorID = "bob" }},
		{name: "missing id", change: func(p *chat.Post) { p.ID = "" }},
		{name: "reader unavailable", roomErr: errors.New("reader unavailable")},
		{name: "archived room", archived: true},
		{name: "unknown kind", kind: "UNKNOWN"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inner := &personaChatWriterFake{post: personaClassifiedPost()}
			if tc.change != nil {
				tc.change(&inner.post)
			}
			// Empty ID must be returned literally, not repaired by the shared fake.
			writer := personaClassLiteralWriter{post: inner.post}
			room := personaClassifiedRoom(chat.PublicChannel)
			room.err = tc.roomErr
			room.room.Archived = tc.archived
			if tc.kind != "" {
				room.room.Kind = tc.kind
			}
			classifier := &personaAcceptedClassFake{}
			wrapped, err := NewClassifiedPersonaHumanPostWriter(writer, room, classifier)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = wrapped.SendPost(ctx, personaSendRequest()); err == nil || classifier.calls != 0 {
				t.Fatalf("forged source classified err=%v calls=%d", err, classifier.calls)
			}
		})
	}
	for _, kind := range []trust.SubjectKind{trust.SubjectKindAgent, trust.SubjectKindHuman} {
		_, inner, _, _, _, _, kindCtx := personaInvocationFixture(t, kind, personaAdmission(), nil)
		classifier := &personaAcceptedClassFake{}
		wrapped, _ := NewClassifiedPersonaHumanPostWriter(inner, personaClassifiedRoom(chat.PublicChannel), classifier)
		request := personaSendRequest()
		if kind == trust.SubjectKindHuman {
			request.Principal.SubjectID = "forged"
		}
		if _, err := wrapped.SendPost(kindCtx, request); !errors.Is(err, chat.ErrPermissionDenied) || inner.calls != 0 || classifier.calls != 0 {
			t.Fatalf("invalid identity side effect err=%v writes=%d classify=%d", err, inner.calls, classifier.calls)
		}
	}
	if _, err := NewClassifiedPersonaHumanPostWriter(nil, nil, nil); !errors.Is(err, errPersonaInvocationServedPorts) {
		t.Fatal(err)
	}
	var nilClassifier *personaAcceptedClassFake
	if _, err := NewClassifiedPersonaHumanPostWriter(&personaChatWriterFake{}, personaClassifiedRoom(chat.PublicChannel), nilClassifier); !errors.Is(err, errPersonaInvocationServedPorts) {
		t.Fatal(err)
	}
	var nilWriter *ClassifiedPersonaHumanPostWriter
	if _, err := nilWriter.SendPost(ctx, personaSendRequest()); !errors.Is(err, errPersonaInvocationServedPorts) {
		t.Fatal(err)
	}
}

type personaClassLiteralWriter struct {
	post chat.Post
	err  error
}

func (w personaClassLiteralWriter) SendPost(context.Context, chat.SendPostRequest) (chat.Post, error) {
	return w.post, w.err
}

func TestTodo_AGENTP_012_ClassifiedHumanWriter_Ineligible(t *testing.T) {
	_, _, _, _, _, _, ctx := personaInvocationFixture(t, trust.SubjectKindHuman, personaAdmission(), nil)
	for _, mutate := range []func(*chat.Post){func(p *chat.Post) { p.Revision = 2 }, func(p *chat.Post) { p.Deleted = true }, func(p *chat.Post) { p.SourceAttribution = &chat.SourceAttribution{} }} {
		post := personaClassifiedPost()
		mutate(&post)
		classifier := &personaAcceptedClassFake{}
		room := personaClassifiedRoom(chat.PublicChannel)
		wrapped, _ := NewClassifiedPersonaHumanPostWriter(personaClassLiteralWriter{post: post}, room, classifier)
		got, err := wrapped.SendPost(ctx, personaSendRequest())
		if err != nil || got.ID != post.ID || classifier.calls != 0 || room.calls != 0 {
			t.Fatalf("ineligible post classification err=%v calls=%d room=%d", err, classifier.calls, room.calls)
		}
	}
	failure := errors.New("write rejected")
	classifier := &personaAcceptedClassFake{}
	room := personaClassifiedRoom(chat.PublicChannel)
	wrapped, _ := NewClassifiedPersonaHumanPostWriter(personaClassLiteralWriter{err: failure}, room, classifier)
	if _, err := wrapped.SendPost(ctx, personaSendRequest()); !errors.Is(err, failure) || classifier.calls != 0 || room.calls != 0 {
		t.Fatalf("failed write reached classification: %v", err)
	}
	var absent *PersonaHumanPostClassificationError
	if !errors.Is(absent, ErrPersonaHumanPostClassificationFailed) || absent.Error() == "" {
		t.Fatal("typed sentinel lost")
	}
}

func TestTodo_AGENTP_012_ClassifiedHumanWriter_Integration(t *testing.T) {
	db := pgtest.NewEmpty(t)
	applyPersonaChatMigrations(t, db)
	raw, err := chatstore.New(context.Background(), chatstore.Config{DSN: personaChatSchemaDSN(t, db.URL, db.Schema)})
	if err != nil {
		t.Fatal(err)
	}
	store := chatstore.NewAdapter(raw)
	t.Cleanup(store.Close)
	service := chat.NewService(store, func() time.Time { return time.Now().UTC() })
	service.SetAuthority(servedPersonaChatAuthority{})
	classifier, err := NewLocalDevPersonaPostClassifier(raw, true)
	if err != nil {
		t.Fatal(err)
	}
	wrapped, err := NewClassifiedPersonaHumanPostWriter(servedPersonaPostWriter{service: service}, service, classifier)
	if err != nil {
		t.Fatal(err)
	}
	ctx := personaLocalDevClassContext(t, "ironridge-demo", "alice")
	principal := chat.Principal{TenantID: "ironridge-demo", SubjectID: "alice"}
	authority := NewPersonaPublicAudienceAuthority(raw)
	for _, kind := range []chat.ConversationKind{chat.PublicChannel, chat.Direct} {
		t.Run(string(kind), func(t *testing.T) {
			room := "exact-" + string(kind)
			if _, err := service.CreateConversation(ctx, chat.CreateConversationRequest{Principal: principal, TenantID: principal.TenantID, ConversationID: room, Kind: kind, Name: "source test"}); err != nil {
				t.Fatal(err)
			}
			request := chat.SendPostRequest{Principal: principal, TenantID: principal.TenantID, ConversationID: room, Body: "How do I request vacation?", IdempotencyKey: room}
			post, err := wrapped.SendPost(ctx, request)
			if err != nil || post.ID == "" {
				t.Fatalf("accepted/classified post=%+v err=%v", post, err)
			}
			sum := sha256.Sum256([]byte(post.Body))
			digest := "sha256:" + hex.EncodeToString(sum[:])
			class, err := authority.PersonaPublicChatDisclosureClass(ctx, principal.TenantID, room, post.ID, digest)
			if err != nil || class != dlp.ClassInternal {
				t.Fatalf("source owner evidence class=%s err=%v", class, err)
			}
			before, err := store.AudienceRevision(ctx, principal.TenantID, room)
			if err != nil {
				t.Fatal(err)
			}
			postReplay, err := wrapped.SendPost(ctx, request)
			if err != nil || postReplay.ID != post.ID {
				t.Fatalf("durable replay=%+v err=%v", postReplay, err)
			}
			after, err := store.AudienceRevision(ctx, principal.TenantID, room)
			if err != nil || before != after {
				t.Fatalf("classification replay invalidated admitted audience before=%d after=%d err=%v", before, after, err)
			}
			outsider := personaRunAudienceContext(t, principal.TenantID, "bob")
			if _, err := authority.PersonaPublicChatDisclosureClass(outsider, principal.TenantID, room, post.ID, digest); !errors.Is(err, ErrPersonaAudienceFloorUnavailable) {
				t.Fatalf("nonmember source class read=%v", err)
			}
		})
	}
}
