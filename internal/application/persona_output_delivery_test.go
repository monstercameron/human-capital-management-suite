package application

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdeliver"
)

type personaOutputSourceFake struct {
	result agentdeliver.Result
	err    error
	mu     sync.Mutex
	seen   []PersonaOutputRequest
}

func (f *personaOutputSourceFake) LoadPersonaOutput(_ context.Context, request PersonaOutputRequest) (agentdeliver.Result, error) {
	f.mu.Lock()
	f.seen = append(f.seen, request)
	f.mu.Unlock()
	return f.result, f.err
}

type personaOutputPolicyFake struct {
	conversation agentdeliver.Conversation
	err          error
}

func (f personaOutputPolicyFake) CurrentPersonaOutputPolicy(context.Context, string, string) (agentdeliver.Conversation, error) {
	return f.conversation, f.err
}

type personaOutputAudienceFake struct{ snapshot agentdeliver.AudienceSnapshot }

func (f personaOutputAudienceFake) Snapshot(context.Context, agentdeliver.Conversation) (agentdeliver.AudienceSnapshot, error) {
	return f.snapshot, nil
}

type personaOutputAuthorizerFake struct {
	deny bool
	mu   sync.Mutex
	seen int
}

func (f *personaOutputAuthorizerFake) Authorize(context.Context, agentdeliver.ReauthorizationRequest) error {
	f.mu.Lock()
	f.seen++
	f.mu.Unlock()
	if f.deny {
		return errors.New("current grant denied")
	}
	return nil
}

type personaOutputPublicFake struct {
	mu       sync.Mutex
	posts    []agentdeliver.PublicPost
	audience bool
}

func (f *personaOutputPublicFake) CommitPublic(_ context.Context, post agentdeliver.PublicPost) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.audience {
		f.audience = false
		return agentdeliver.ErrAudienceChanged
	}
	f.posts = append(f.posts, post)
	return nil
}

type personaOutputPrivateFake struct {
	mu    sync.Mutex
	posts []agentdeliver.PrivatePost
}

func (f *personaOutputPrivateFake) DeliverPrivate(_ context.Context, post agentdeliver.PrivatePost) error {
	f.mu.Lock()
	f.posts = append(f.posts, post)
	f.mu.Unlock()
	return nil
}

func personaOutputFixture(deny bool) (*PersonaOutputDelivery, *personaOutputSourceFake, *personaOutputPublicFake, *personaOutputPrivateFake, *personaOutputAuthorizerFake, error) {
	source := &personaOutputSourceFake{result: agentdeliver.Result{PersonaLabel: "Comp Analyst", Items: []agentdeliver.ResultItem{{ID: "answer", Text: "The policy allows 20 days.", Materials: []agentdeliver.Material{{Kind: agentdeliver.MaterialRecord, ID: "policy-1", DataClass: "POLICY"}}}}}}
	public := &personaOutputPublicFake{}
	private := &personaOutputPrivateFake{}
	authorizer := &personaOutputAuthorizerFake{deny: deny}
	conversation := agentdeliver.Conversation{TenantID: "tenant-a", ConversationID: "room-a", Kind: agentdeliver.PublicChannel, Policy: agentdeliver.ChannelPolicy{AllowedClasses: []agentdeliver.DataClass{"POLICY"}}}
	gate := &agentdeliver.Service{Audience: personaOutputAudienceFake{snapshot: agentdeliver.AudienceSnapshot{Revision: 7, CurrentMembers: []agentdeliver.AudienceMember{{TenantID: "tenant-a", SubjectID: "alice"}}, EligibilityPopulation: []agentdeliver.AudienceMember{{TenantID: "tenant-a", SubjectID: "guest", Guest: true, External: true}}}}, Authorize: authorizer, Public: public, Private: private}
	delivery, err := NewPersonaOutputDelivery(PersonaOutputDeliveryConfig{Source: source, Policy: personaOutputPolicyFake{conversation: conversation}, Service: gate})
	return delivery, source, public, private, authorizer, err
}

func personaOutputRequest() PersonaOutputRequest {
	return PersonaOutputRequest{TenantID: "tenant-a", ConversationID: "room-a", ParentPostID: "question-1", OutputID: "output-1", Invoker: agentdeliver.AudienceMember{TenantID: "tenant-a", SubjectID: "alice"}}
}

func TestPersonaOutputDelivery_PublicWhenCurrentAudienceCanRead(t *testing.T) {
	delivery, source, public, private, authorizer, err := personaOutputFixture(false)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := delivery.Deliver(context.Background(), personaOutputRequest())
	if err != nil || !receipt.PublicPosted || receipt.Mode != agentdeliver.DeliveryPublic {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
	if len(public.posts) != 1 || len(private.posts) != 0 || authorizer.seen != 2 {
		t.Fatalf("public=%+v private=%+v auth=%d", public.posts, private.posts, authorizer.seen)
	}
	if len(source.seen) != 1 || source.seen[0].OutputID != "output-1" {
		t.Fatalf("source request=%+v", source.seen)
	}
}

func TestPersonaOutputDelivery_DeniedAudienceGetsNeutralReceiptAndPrivateCopy(t *testing.T) {
	delivery, _, public, private, _, err := personaOutputFixture(true)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := delivery.Deliver(context.Background(), personaOutputRequest())
	if err != nil || !receipt.PrivatePosted || receipt.NeutralReceipt == "" {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
	if len(public.posts) != 1 || public.posts[0].Body != receipt.NeutralReceipt || len(private.posts) != 1 {
		t.Fatalf("public=%+v private=%+v", public.posts, private.posts)
	}
	if public.posts[0].Body == private.posts[0].Body {
		t.Fatal("neutral receipt exposed the private answer")
	}
}

func TestPersonaOutputDelivery_FailsClosedBeforeAnyEffect(t *testing.T) {
	delivery, _, public, private, _, err := personaOutputFixture(false)
	if err != nil {
		t.Fatal(err)
	}
	request := personaOutputRequest()
	request.ConversationID = "other-room"
	if _, err := delivery.Deliver(context.Background(), request); !errors.Is(err, agentdeliver.ErrInvalidRequest) {
		t.Fatalf("mismatched policy err=%v", err)
	}
	if len(public.posts) != 0 || len(private.posts) != 0 {
		t.Fatal("mismatched current policy caused an effect")
	}

	if _, err := NewPersonaOutputDelivery(PersonaOutputDeliveryConfig{}); !errors.Is(err, ErrPersonaOutputDeliveryUnavailable) {
		t.Fatalf("missing composition err=%v", err)
	}
}

func TestPersonaOutputDelivery_AudienceRaceFallsBackPrivately(t *testing.T) {
	delivery, _, public, private, _, err := personaOutputFixture(false)
	if err != nil {
		t.Fatal(err)
	}
	public.audience = true
	receipt, err := delivery.Deliver(context.Background(), personaOutputRequest())
	if err != nil || !receipt.PrivatePosted || len(private.posts) != 1 {
		t.Fatalf("race receipt=%+v private=%+v err=%v", receipt, private.posts, err)
	}
	if len(public.posts) != 1 || public.posts[0].Body != receipt.NeutralReceipt {
		t.Fatalf("race public=%+v receipt=%q", public.posts, receipt.NeutralReceipt)
	}
}

func TestPersonaOutputDelivery_ConcurrentDeliveries(t *testing.T) {
	delivery, _, public, _, _, err := personaOutputFixture(false)
	if err != nil {
		t.Fatal(err)
	}
	const calls = 8
	errs := make(chan error, calls)
	var wg sync.WaitGroup
	for i := 0; i < calls; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, callErr := delivery.Deliver(context.Background(), personaOutputRequest())
			errs <- callErr
		}()
	}
	wg.Wait()
	close(errs)
	for callErr := range errs {
		if callErr != nil {
			t.Fatal(callErr)
		}
	}
	if len(public.posts) != calls {
		t.Fatalf("public posts=%d want=%d", len(public.posts), calls)
	}
}
