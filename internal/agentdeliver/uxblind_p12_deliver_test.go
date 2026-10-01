package agentdeliver

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

type p12Audience struct {
	mu        sync.Mutex
	snapshots []AudienceSnapshot
	calls     int
}

func (a *p12Audience) Snapshot(context.Context, Conversation) (AudienceSnapshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	if len(a.snapshots) == 0 {
		return AudienceSnapshot{}, errors.New("missing audience snapshot")
	}
	index := a.calls - 1
	if index >= len(a.snapshots) {
		index = len(a.snapshots) - 1
	}
	return a.snapshots[index], nil
}

type p12Authorizer struct {
	mu   sync.Mutex
	deny map[string]bool
	seen []ReauthorizationRequest
}

func (a *p12Authorizer) Authorize(_ context.Context, req ReauthorizationRequest) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.seen = append(a.seen, req)
	if a.deny[req.Audience.key()+"/"+req.Material.ID] {
		return errors.New("audience member cannot read material")
	}
	return nil
}

type p12Public struct {
	mu     sync.Mutex
	posts  []PublicPost
	traces int
}

func (p *p12Public) CommitPublic(_ context.Context, post PublicPost) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.traces > 0 {
		p.traces--
		return ErrAudienceChanged
	}
	p.posts = append(p.posts, post)
	return nil
}

type p12Private struct {
	mu    sync.Mutex
	posts []PrivatePost
}

func (p *p12Private) DeliverPrivate(_ context.Context, post PrivatePost) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.posts = append(p.posts, post)
	return nil
}

func p12Conversation(kind ConversationKind) Conversation {
	return Conversation{
		TenantID: "tenant-a", ConversationID: "conversation-a", Kind: kind,
		Policy:       ChannelPolicy{AllowedClasses: []DataClass{"COMPENSATION", "PUBLIC"}},
		TenantOrigin: "https://hcm.example.test",
	}
}

func p12Result() Result {
	return Result{
		PersonaLabel: "Comp Analyst",
		Items: []ResultItem{{
			ID: "salary-record", Text: "Alice salary is $120,000.",
			Materials: []Material{
				{Kind: MaterialSource, ID: "source-payroll", DataClass: "COMPENSATION"},
				{Kind: MaterialRecord, ID: "record-alice", DataClass: "COMPENSATION"},
				{Kind: MaterialField, ID: "field-salary", DataClass: "COMPENSATION", Value: "$120,000"},
			},
			Citations: []Citation{{Title: "Alice compensation record", URL: "https://hcm.example.test/records/alice", SourceID: "citation-alice", DataClass: "COMPENSATION"}},
		}},
	}
}

func p12Service(a *p12Audience, auth *p12Authorizer, public *p12Public, private *p12Private) *Service {
	return &Service{Audience: a, Authorize: auth, Public: public, Private: private}
}

func TestTodo_AGENTP_012(t *testing.T) {
	conversation := p12Conversation(PublicChannel)
	manager := AudienceMember{TenantID: "tenant-a", SubjectID: "manager"}
	hr := AudienceMember{TenantID: "tenant-a", SubjectID: "hr"}
	futureGuest := AudienceMember{TenantID: "tenant-a", SubjectID: "future-guest", Guest: true, External: true}
	audience := &p12Audience{snapshots: []AudienceSnapshot{{Revision: 7, CurrentMembers: []AudienceMember{manager, hr}, EligibilityPopulation: []AudienceMember{futureGuest}}}}
	auth := &p12Authorizer{deny: map[string]bool{hr.key() + "/record-alice": true}}
	public, private := &p12Public{}, &p12Private{}
	got, err := p12Service(audience, auth, public, private).Deliver(context.Background(), DeliveryRequest{
		Conversation: conversation, ParentPostID: "post-question", Invoker: manager, Result: p12Result(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != DeliveryPrivate || !got.PrivatePosted || got.NeutralReceipt != "@manager asked Comp Analyst; the answer was sent privately" {
		t.Fatalf("delivery receipt = %+v", got)
	}
	if len(public.posts) != 1 || public.posts[0].Body != got.NeutralReceipt {
		t.Fatalf("neutral public receipt = %+v", public.posts)
	}
	if len(private.posts) != 1 || !private.posts[0].Ephemeral || !strings.Contains(private.posts[0].Body, "$120,000") {
		t.Fatalf("private result = %+v", private.posts)
	}
	if audience.calls != 1 {
		t.Fatalf("audience snapshots = %d, want one commit-time snapshot", audience.calls)
	}
	seen := map[string]bool{}
	for _, call := range auth.seen {
		seen[call.Audience.key()] = true
	}
	for _, member := range []AudienceMember{manager, hr, futureGuest} {
		if !seen[member.key()] {
			t.Fatalf("audience member %s was not reauthorized", member.SubjectID)
		}
	}
	if strings.Contains(public.posts[0].Body, "$120,000") {
		t.Fatal("public receipt leaked a private value")
	}
}

func TestTodo_AGENTP_012_Golden(t *testing.T) {
	conversation := p12Conversation(PublicChannel)
	manager := AudienceMember{TenantID: "tenant-a", SubjectID: "manager"}
	audience := &p12Audience{snapshots: []AudienceSnapshot{{Revision: 11, CurrentMembers: []AudienceMember{manager}}}}
	public, private := &p12Public{}, &p12Private{}
	result := Result{PersonaLabel: "Policy Helper", Items: []ResultItem{{ID: "policy", Text: "The leave policy allows 20 days."}}}
	if _, err := p12Service(audience, &p12Authorizer{}, public, private).Deliver(context.Background(), DeliveryRequest{Conversation: conversation, ParentPostID: "question", Invoker: manager, Result: result}); err != nil {
		t.Fatal(err)
	}
	if len(public.posts) != 1 || public.posts[0].Body != "The leave policy allows 20 days." || public.posts[0].ExpectedAudienceRevision != 11 {
		t.Fatalf("golden public post = %+v", public.posts)
	}
	if public.posts[0].Tier != TierCommunicate || public.posts[0].Attribution != "acting for @manager" {
		t.Fatalf("public binding = %+v", public.posts[0])
	}
	if len(private.posts) != 0 {
		t.Fatalf("public result also went private: %+v", private.posts)
	}
}

func TestTodo_AGENTP_012_Security(t *testing.T) {
	conversation := p12Conversation(PublicChannel)
	manager := AudienceMember{TenantID: "tenant-a", SubjectID: "manager"}
	audience := &p12Audience{snapshots: []AudienceSnapshot{{Revision: 1, CurrentMembers: []AudienceMember{manager}}}}
	public, private := &p12Public{}, &p12Private{}
	unsafe := Result{PersonaLabel: "Comp Analyst", Items: []ResultItem{{ID: "unsafe", Text: "safe text", Images: []Image{{URL: "https://attacker.example/image.png", AutoLoad: true}}, Embeds: []Embed{{URL: "https://attacker.example/embed"}}}}}
	_, err := p12Service(audience, &p12Authorizer{}, public, private).Deliver(context.Background(), DeliveryRequest{Conversation: conversation, ParentPostID: "question", Invoker: manager, Result: unsafe})
	if !errors.Is(err, ErrUnsafeOutput) || len(public.posts) != 0 || len(private.posts) != 0 {
		t.Fatalf("unsafe output was delivered: err=%v public=%+v private=%+v", err, public.posts, private.posts)
	}
	badLink := Result{PersonaLabel: "Policy Helper", Items: []ResultItem{{ID: "link", Text: "read this", Links: []Link{{URL: "https://attacker.example/leak", Tainted: true}}}}}
	_, err = p12Service(audience, &p12Authorizer{}, public, private).Deliver(context.Background(), DeliveryRequest{Conversation: conversation, ParentPostID: "question", Invoker: manager, Result: badLink})
	if !errors.Is(err, ErrUnsafeOutput) {
		t.Fatalf("tainted link err = %v", err)
	}
}

func TestTodo_AGENTP_012_Property(t *testing.T) {
	manager := AudienceMember{TenantID: "tenant-a", SubjectID: "manager"}
	guest := AudienceMember{TenantID: "tenant-a", SubjectID: "guest", Guest: true}
	for _, denied := range []AudienceMember{manager, guest} {
		audience := &p12Audience{snapshots: []AudienceSnapshot{{Revision: 2, CurrentMembers: []AudienceMember{manager, guest}}}}
		auth := &p12Authorizer{deny: map[string]bool{denied.key() + "/record-alice": true}}
		public, private := &p12Public{}, &p12Private{}
		_, err := p12Service(audience, auth, public, private).Deliver(context.Background(), DeliveryRequest{Conversation: p12Conversation(PublicChannel), ParentPostID: "question", Invoker: manager, Result: p12Result()})
		if denied.SubjectID == manager.SubjectID {
			if !errors.Is(err, ErrOutputDenied) || len(public.posts) != 0 || len(private.posts) != 0 {
				t.Fatalf("revoked invoker received output: err=%v public=%+v private=%+v", err, public.posts, private.posts)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if len(public.posts) != 1 || strings.Contains(public.posts[0].Body, "$120,000") {
			t.Fatalf("denied audience value reached public post for %s: %+v", denied.SubjectID, public.posts)
		}
	}
}

func TestTodo_AGENTP_012_Security_PrivateDeliveryReauthorizesInvoker(t *testing.T) {
	manager := AudienceMember{TenantID: "tenant-a", SubjectID: "manager"}
	for _, kind := range []ConversationKind{Direct, PublicChannel} {
		conversation := p12Conversation(kind)
		conversation.Policy.AlwaysPrivate = true
		public, private := &p12Public{}, &p12Private{}
		authorizer := &p12Authorizer{deny: map[string]bool{manager.key() + "/record-alice": true}}
		audience := &p12Audience{snapshots: []AudienceSnapshot{{Revision: 1, CurrentMembers: []AudienceMember{manager}}}}
		_, err := p12Service(audience, authorizer, public, private).Deliver(context.Background(), DeliveryRequest{
			Conversation: conversation, ParentPostID: "question", Invoker: manager, Result: p12Result(),
		})
		if !errors.Is(err, ErrOutputDenied) || len(public.posts) != 0 || len(private.posts) != 0 {
			t.Fatalf("private output disclosed after invoker revocation: %v %+v %+v", err, public.posts, private.posts)
		}
	}
}

func TestTodo_AGENTP_012_Security_InvalidAudienceCannotReduceTheFloor(t *testing.T) {
	manager := AudienceMember{TenantID: "tenant-a", SubjectID: "manager"}
	for _, snapshot := range []AudienceSnapshot{
		{CurrentMembers: []AudienceMember{manager}},
		{Revision: 1, CurrentMembers: []AudienceMember{manager, {TenantID: "tenant-a"}}},
		{Revision: 1, CurrentMembers: []AudienceMember{manager}, EligibilityPopulation: []AudienceMember{{SubjectID: "guest"}}},
		{Revision: 1, CurrentMembers: []AudienceMember{manager, {TenantID: "tenant-b", SubjectID: "foreign"}}},
	} {
		public, private := &p12Public{}, &p12Private{}
		audience := &p12Audience{snapshots: []AudienceSnapshot{snapshot}}
		_, err := p12Service(audience, &p12Authorizer{}, public, private).Deliver(context.Background(), DeliveryRequest{
			Conversation: p12Conversation(PublicChannel), ParentPostID: "question", Invoker: manager, Result: p12Result(),
		})
		if !errors.Is(err, ErrInvalidRequest) || len(public.posts) != 0 || len(private.posts) != 0 {
			t.Fatalf("invalid audience disclosed output: %v %+v %+v", err, public.posts, private.posts)
		}
	}
}

func TestTodo_AGENTP_013_Security_SharePreviewBindsDestinationAndPolicy(t *testing.T) {
	manager := AudienceMember{TenantID: "tenant-a", SubjectID: "manager"}
	for _, mutate := range []func(*EphemeralResult){
		func(e *EphemeralResult) { e.ParentPostID = "different-question" },
		func(e *EphemeralResult) { e.Conversation.TenantOrigin = "https://other.example.test" },
	} {
		public := &p12Public{}
		audience := &p12Audience{snapshots: []AudienceSnapshot{{Revision: 1, CurrentMembers: []AudienceMember{manager}}}}
		service := p12Service(audience, &p12Authorizer{}, public, &p12Private{})
		request := ShareRequest{Actor: manager, Ephemeral: p12Ephemeral(p12Conversation(PublicChannel), manager)}
		preview, err := service.PreviewShare(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		mutate(&request.Ephemeral)
		_, err = service.ConfirmShare(context.Background(), request, ConfirmShareRequest{Actor: manager, Preview: preview, Confirmed: true})
		if !errors.Is(err, ErrStaleSharePreview) || len(public.posts) != 0 {
			t.Fatalf("preview replayed to changed destination/policy: %v %+v", err, public.posts)
		}
	}
}

func TestTodo_AGENTP_012_Race(t *testing.T) {
	manager := AudienceMember{TenantID: "tenant-a", SubjectID: "manager"}
	audience := &p12Audience{snapshots: []AudienceSnapshot{
		{Revision: 3, CurrentMembers: []AudienceMember{manager}},
		{Revision: 4, CurrentMembers: []AudienceMember{manager, {TenantID: "tenant-a", SubjectID: "new-guest", Guest: true}}, EligibilityPopulation: nil},
	}}
	public, private := &p12Public{traces: 1}, &p12Private{}
	got, err := p12Service(audience, &p12Authorizer{}, public, private).Deliver(context.Background(), DeliveryRequest{Conversation: p12Conversation(PublicChannel), ParentPostID: "question", Invoker: manager, Result: p12Result()})
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != DeliveryPrivate || got.AudienceRevision != 4 || len(private.posts) != 1 {
		t.Fatalf("race delivery = %+v private=%+v", got, private.posts)
	}
	if len(public.posts) != 1 || public.posts[0].ExpectedAudienceRevision != 4 || strings.Contains(public.posts[0].Body, "$120,000") {
		t.Fatalf("race receipt = %+v", public.posts)
	}
}

func TestTodo_AGENTP_012_Mutation(t *testing.T) {
	manager := AudienceMember{TenantID: "tenant-a", SubjectID: "manager"}
	future := AudienceMember{TenantID: "tenant-a", SubjectID: "future", External: true}
	for _, members := range [][]AudienceMember{{manager}, {manager, future}} {
		audience := &p12Audience{snapshots: []AudienceSnapshot{{Revision: 1, CurrentMembers: members, EligibilityPopulation: []AudienceMember{future}}}}
		auth := &p12Authorizer{deny: map[string]bool{future.key() + "/record-alice": true}}
		public, private := &p12Public{}, &p12Private{}
		_, err := p12Service(audience, auth, public, private).Deliver(context.Background(), DeliveryRequest{Conversation: p12Conversation(PublicChannel), ParentPostID: "question", Invoker: manager, Result: p12Result()})
		if err != nil {
			t.Fatal(err)
		}
		if len(public.posts) != 1 || len(private.posts) != 1 || !strings.Contains(private.posts[0].Body, "$120,000") {
			t.Fatalf("removing audience term changed the privacy floor: public=%+v private=%+v", public.posts, private.posts)
		}
	}
}

func p12Ephemeral(conversation Conversation, invoker AudienceMember) EphemeralResult {
	return EphemeralResult{ID: "ephemeral-1", Conversation: conversation, ParentPostID: "question", Invoker: invoker, Result: Result{
		PersonaLabel: "Comp Analyst",
		Items: []ResultItem{
			{ID: "band-summary", Text: "The team is within the approved band."},
			{ID: "salary-record", Text: "Alice salary is $120,000.", Materials: []Material{{Kind: MaterialRecord, ID: "record-alice", DataClass: "COMPENSATION"}}, Citations: []Citation{{Title: "Alice compensation record", SourceID: "citation-alice", DataClass: "COMPENSATION"}}},
		},
	}}
}

func TestTodo_AGENTP_013(t *testing.T) {
	conversation := p12Conversation(PublicChannel)
	manager := AudienceMember{TenantID: "tenant-a", SubjectID: "manager"}
	audience := &p12Audience{snapshots: []AudienceSnapshot{{Revision: 5, CurrentMembers: []AudienceMember{manager, {TenantID: "tenant-a", SubjectID: "hr"}}}, {Revision: 5, CurrentMembers: []AudienceMember{manager, {TenantID: "tenant-a", SubjectID: "hr"}}}}}
	auth := &p12Authorizer{deny: map[string]bool{"tenant-a\x00hr/record-alice": true}}
	public := &p12Public{}
	service := p12Service(audience, auth, public, &p12Private{})
	ephemeral := p12Ephemeral(conversation, manager)
	request := ShareRequest{Actor: manager, Ephemeral: ephemeral}
	preview, err := service.PreviewShare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if preview.DroppedCount != 1 || len(preview.Items) != 2 {
		t.Fatalf("share preview = %+v", preview)
	}
	// Assert by stable item id rather than depending on a UI sort order.
	byID := map[string]ShareItemDecision{}
	for _, item := range preview.Items {
		byID[item.ItemID] = item
	}
	if !byID["band-summary"].Included || byID["salary-record"].Included || byID["salary-record"].Reason == "" {
		t.Fatalf("share decisions = %+v", byID)
	}
	outcome, err := service.ConfirmShare(context.Background(), request, ConfirmShareRequest{Actor: manager, Preview: preview, Confirmed: true})
	if err != nil || !outcome.Posted || outcome.DroppedCount != 1 {
		t.Fatalf("share outcome = %+v err=%v", outcome, err)
	}
	if len(public.posts) != 1 || public.posts[0].AuthorID != "manager" || public.posts[0].Tier != TierCommunicate || public.posts[0].Attribution != "shared from Comp Analyst" || strings.Contains(public.posts[0].Body, "$120,000") {
		t.Fatalf("shared post = %+v", public.posts)
	}
}

func TestTodo_AGENTP_013_Security(t *testing.T) {
	manager := AudienceMember{TenantID: "tenant-a", SubjectID: "manager"}
	other := AudienceMember{TenantID: "tenant-a", SubjectID: "other"}
	audience := &p12Audience{snapshots: []AudienceSnapshot{{Revision: 1, CurrentMembers: []AudienceMember{manager, other}}}}
	service := p12Service(audience, &p12Authorizer{}, &p12Public{}, &p12Private{})
	ephemeral := p12Ephemeral(p12Conversation(PublicChannel), manager)
	_, err := service.PreviewShare(context.Background(), ShareRequest{Actor: other, Ephemeral: ephemeral})
	if !errors.Is(err, ErrUnauthorizedShare) {
		t.Fatalf("other member preview err = %v", err)
	}
	preview, err := service.PreviewShare(context.Background(), ShareRequest{Actor: manager, Ephemeral: ephemeral})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.ConfirmShare(context.Background(), ShareRequest{Actor: manager, Ephemeral: ephemeral}, ConfirmShareRequest{Actor: other, Preview: preview, Confirmed: true})
	if !errors.Is(err, ErrUnauthorizedShare) {
		t.Fatalf("other member confirm err = %v", err)
	}
}

func TestTodo_AGENTP_013_Race(t *testing.T) {
	manager := AudienceMember{TenantID: "tenant-a", SubjectID: "manager"}
	guest := AudienceMember{TenantID: "tenant-a", SubjectID: "new-guest", Guest: true}
	conversation := p12Conversation(PublicChannel)
	audience := &p12Audience{snapshots: []AudienceSnapshot{
		{Revision: 1, CurrentMembers: []AudienceMember{manager}},
		{Revision: 2, CurrentMembers: []AudienceMember{manager, guest}},
	}}
	auth := &p12Authorizer{deny: map[string]bool{guest.key() + "/record-alice": true}}
	public := &p12Public{}
	service := p12Service(audience, auth, public, &p12Private{})
	request := ShareRequest{Actor: manager, Ephemeral: p12Ephemeral(conversation, manager)}
	preview, err := service.PreviewShare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := service.ConfirmShare(context.Background(), request, ConfirmShareRequest{Actor: manager, Preview: preview, Confirmed: true})
	var conflict *ShareConflictError
	if !errors.As(err, &conflict) || outcome.Preview == nil || outcome.Preview.AudienceRevision != 2 || outcome.Preview.DroppedCount != 1 || len(public.posts) != 0 {
		t.Fatalf("stale share was not re-previewed: outcome=%+v err=%v public=%+v", outcome, err, public.posts)
	}
}

func TestTodo_AGENTP_013_Browser(t *testing.T) {
	// Native component-level proof: the preview data is the browser contract
	// consumed by the Chat UI. A real browser harness is owned by the chat UI
	// lane, so this test pins the visible preview and dropped count here.
	manager := AudienceMember{TenantID: "tenant-a", SubjectID: "manager"}
	audience := &p12Audience{snapshots: []AudienceSnapshot{{Revision: 8, CurrentMembers: []AudienceMember{manager}}}}
	service := p12Service(audience, &p12Authorizer{}, &p12Public{}, &p12Private{})
	preview, err := service.PreviewShare(context.Background(), ShareRequest{Actor: manager, Ephemeral: p12Ephemeral(p12Conversation(PublicChannel), manager)})
	if err != nil {
		t.Fatal(err)
	}
	if preview.DroppedCount != 0 || preview.Rendered.Body == "" || len(preview.Items) != 2 {
		t.Fatalf("desktop share preview contract = %+v", preview)
	}
}
