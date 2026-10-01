package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatadmission"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatstream"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestTodo_AGENTP_011_EphemeralProjection(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	request := chatcore.WatchConversationRequest{
		Principal: chatcore.Principal{TenantID: "home", SubjectID: "alice"},
		TenantID:  "host", ConversationID: "shared-room",
	}
	post := chatcore.EphemeralPost{
		ID: "ephemeral-1", TenantID: "host", ConversationID: "shared-room", ThreadID: "root-post",
		RecipientHomeTenantID: "home", RecipientSubjectID: "alice", Body: "private answer",
		OnlyVisibleToYou: true, CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		DurableCopyConversationID: "alice-persona-dm", DurableCopyPostID: "dm-post-1", ThreadLink: "/chat/share/opaque", Sequence: 41,
	}
	payload, err := json.Marshal(post)
	if err != nil {
		t.Fatal(err)
	}
	event := chatstream.Event{TenantID: "host", ConversationID: "shared-room", Sequence: 41,
		RecipientHomeTenantID: "home", RecipientSubjectID: "alice", Ephemeral: true, ExpiresAt: post.ExpiresAt, Payload: payload}
	projected, err := projectChatStreamEvent(context.Background(), request, event, now)
	if err != nil {
		t.Fatalf("recipient projection: %v", err)
	}
	if projected.Event != (chatcore.ConversationEvent{}) || projected.EphemeralDelivery == nil {
		t.Fatalf("ephemeral event was not projected separately: %+v", projected)
	}
	want := chatcore.EphemeralDelivery{ID: post.ID, ThreadID: post.ThreadID, Body: post.Body,
		OnlyVisibleToYou: true, CreatedAt: post.CreatedAt, ExpiresAt: post.ExpiresAt, ThreadLink: post.ThreadLink}
	if *projected.EphemeralDelivery != want {
		t.Fatalf("delivery=%+v want=%+v", *projected.EphemeralDelivery, want)
	}
	encoded, err := json.Marshal(projected)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"alice-persona-dm", "dm-post-1", "RecipientSubjectID", "RecipientHomeTenantID", "alice"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("projected payload contains sensitive field %q: %s", forbidden, encoded)
		}
	}
}

type agentp011PersonaResolver struct {
	conversation string
	calls        int
}

func (r *agentp011PersonaResolver) ResolvePersonaDM(context.Context, chatcore.Principal, string) (string, error) {
	r.calls++
	return r.conversation, nil
}

type agentp011EphemeralService struct {
	chatcore.ConversationService
	got      chatcore.SendEphemeralPostRequest
	probe    func() []error
	probeErr error
}

func (s *agentp011EphemeralService) SendEphemeralPost(_ context.Context, request chatcore.SendEphemeralPostRequest) (chatcore.EphemeralPost, error) {
	s.got = request
	if s.probe != nil {
		for _, err := range s.probe() {
			if !errors.Is(err, chatadmission.ErrOverloaded) {
				s.probeErr = err
				if err == nil {
					s.probeErr = errors.New("admission probe succeeded")
				}
			}
		}
	}
	return chatcore.EphemeralPost{ID: "e"}, nil
}

func TestTodo_AGENTP_011_PrivateSendUsesResolvedDMAndFailsClosed(t *testing.T) {
	inner := &agentp011EphemeralService{}
	request := chatcore.SendEphemeralPostRequest{Principal: chatcore.Principal{TenantID: "tenant", SubjectID: "alice"},
		TenantID: "tenant", ConversationID: "shared-room", DurableCopyConversationID: "attacker-selected-dm", Body: "private"}
	withoutResolver := &streamingChatService{ConversationService: inner}
	if _, err := withoutResolver.SendEphemeralPost(context.Background(), request); !errors.Is(err, chatcore.ErrUnavailable) {
		t.Fatalf("missing canonical resolver error=%v; want fail-closed unavailable", err)
	}
	if inner.got.DurableCopyConversationID != "" {
		t.Fatal("private content reached the service without canonical DM resolution")
	}

	resolver := &agentp011PersonaResolver{conversation: "alice-persona-dm"}
	config := runtimeConfig("agentp011-private-send")
	config.Budgets = chatadmission.Config{TenantConcurrent: 4, ConversationConcurrent: 1, SendConcurrent: 4, WatchConcurrent: 2}
	runtime, err := NewChatStreamRuntime(config)
	if err != nil {
		t.Fatal(err)
	}
	inner.probe = func() []error {
		var results []error
		for _, conversation := range []string{"shared-room", "alice-persona-dm"} {
			lease, probeErr := runtime.AcquireSend(context.Background(), "tenant", conversation)
			if lease != nil {
				_ = lease.Release()
			}
			results = append(results, probeErr)
		}
		return results
	}
	service := &streamingChatService{ConversationService: inner, runtime: runtime, personaDM: resolver}
	if _, err := service.SendEphemeralPost(context.Background(), request); err != nil {
		t.Fatalf("private send with canonical resolver: %v", err)
	}
	if resolver.calls != 1 || inner.got.DurableCopyConversationID != "alice-persona-dm" {
		t.Fatalf("request hint selected durable route: calls=%d request=%+v", resolver.calls, inner.got)
	}
	if inner.probeErr != nil {
		t.Fatalf("source and canonical DM admission were not both held: %v", inner.probeErr)
	}
}

func TestTodo_AGENTP_011_EphemeralProjectionFailsClosed(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	request := chatcore.WatchConversationRequest{Principal: chatcore.Principal{TenantID: "home", SubjectID: "alice"}, TenantID: "host", ConversationID: "room"}
	post := chatcore.EphemeralPost{ID: "e1", TenantID: "host", ConversationID: "room", ThreadID: "root",
		RecipientHomeTenantID: "home", RecipientSubjectID: "alice", Body: "private", OnlyVisibleToYou: true,
		CreatedAt: now, ExpiresAt: now.Add(time.Minute), Sequence: 8}
	payload, err := json.Marshal(post)
	if err != nil {
		t.Fatal(err)
	}
	base := chatstream.Event{TenantID: "host", ConversationID: "room", Sequence: 8, RecipientHomeTenantID: "home", RecipientSubjectID: "alice", Ephemeral: true,
		ExpiresAt: post.ExpiresAt, Payload: payload}
	machine, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "home", Subject: "alice", SubjectKind: trust.SubjectKindAgent,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial,
		SessionRef: "projection-test", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "test-digest"})
	if err != nil {
		t.Fatal(err)
	}
	machineCtx := trust.WithPrincipal(context.Background(), machine)

	cases := []struct {
		name    string
		request chatcore.WatchConversationRequest
		event   chatstream.Event
		ctx     context.Context
		now     time.Time
		wantErr error
	}{
		{name: "wrong event tenant", request: request, event: alteredEphemeralEvent(base, func(e *chatstream.Event) { e.TenantID = "other" }), ctx: context.Background(), now: now},
		{name: "wrong event conversation", request: request, event: alteredEphemeralEvent(base, func(e *chatstream.Event) { e.ConversationID = "other" }), ctx: context.Background(), now: now},
		{name: "wrong recipient subject", request: request, event: alteredEphemeralEvent(base, func(e *chatstream.Event) { e.RecipientSubjectID = "bob" }), ctx: context.Background(), now: now},
		{name: "payload recipient mismatch", request: request, event: alteredEphemeralPayload(t, base, func(p *chatcore.EphemeralPost) { p.RecipientSubjectID = "bob" }), ctx: context.Background(), now: now},
		{name: "not private", request: request, event: alteredEphemeralPayload(t, base, func(p *chatcore.EphemeralPost) { p.OnlyVisibleToYou = false }), ctx: context.Background(), now: now},
		{name: "expired before output", request: request, event: alteredEphemeralPayload(t, base, func(p *chatcore.EphemeralPost) { p.ExpiresAt = now.Add(-time.Second) }), ctx: context.Background(), now: now, wantErr: chatcore.ErrEphemeralExpired},
		{name: "expired event envelope", request: request, event: alteredEphemeralEvent(base, func(e *chatstream.Event) { e.ExpiresAt = now.Add(-time.Second) }), ctx: context.Background(), now: now, wantErr: chatcore.ErrEphemeralExpired},
		{name: "machine recipient denied", request: request, event: base, ctx: machineCtx, now: now},
		{name: "malformed payload", request: request, event: alteredEphemeralEvent(base, func(e *chatstream.Event) { e.Payload = []byte("{") }), ctx: context.Background(), now: now, wantErr: chatcore.ErrUnavailable},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := projectChatStreamEvent(test.ctx, test.request, test.event, test.now); err == nil {
				t.Fatal("invalid ephemeral stream event was accepted")
			} else if test.wantErr != nil && !errors.Is(err, test.wantErr) {
				t.Fatalf("projection error=%v; want %v", err, test.wantErr)
			} else if test.wantErr == nil && !errors.Is(err, chatcore.ErrPermissionDenied) {
				t.Fatalf("projection error=%v; want permission denied", err)
			}
		})
	}
}

func alteredEphemeralEvent(event chatstream.Event, alter func(*chatstream.Event)) chatstream.Event {
	alter(&event)
	return event
}

func alteredEphemeralPayload(t *testing.T, event chatstream.Event, alter func(*chatcore.EphemeralPost)) chatstream.Event {
	t.Helper()
	var post chatcore.EphemeralPost
	if err := json.Unmarshal(event.Payload, &post); err != nil {
		t.Fatalf("decode test fixture: %v", err)
	}
	alter(&post)
	payload, err := json.Marshal(post)
	if err != nil {
		t.Fatalf("encode test fixture: %v", err)
	}
	event.Payload = payload
	event.ExpiresAt = post.ExpiresAt
	return event
}
