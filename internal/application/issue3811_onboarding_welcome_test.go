package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workerlifecyclestore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type welcomeIdentities struct {
	subject string
	known   bool
	err     error
	worker  values.EntityRef
}

func (i welcomeIdentities) ChatSubject(context.Context, values.EntityRef) (string, bool, error) {
	return i.subject, i.known, i.err
}
func (i welcomeIdentities) Worker(_ context.Context, _, subject string) (values.EntityRef, bool, error) {
	if i.err != nil {
		return values.EntityRef{}, false, i.err
	}
	return i.worker, i.known && subject == i.subject, nil
}

type welcomePoster struct {
	posts []chat.OnboardingCardPost
	seen  map[string]chat.Post
}

func (p *welcomePoster) PostOnboardingCard(_ context.Context, r chat.OnboardingCardPost) (chat.Post, error) {
	if p.seen == nil {
		p.seen = map[string]chat.Post{}
	}
	if post, ok := p.seen[r.IdempotencyKey]; ok {
		return post, nil
	}
	p.posts = append(p.posts, r)
	body, err := chat.OnboardingCardBody(r.Card)
	if err != nil {
		return chat.Post{}, err
	}
	post := chat.Post{ID: "post-" + r.IdempotencyKey, ConversationID: "dm", Body: body}
	p.seen[r.IdempotencyKey] = post
	return post, nil
}

type welcomeChat struct {
	post *chat.Post
	err  error
}

func (c welcomeChat) ReadAuthorizedReference(_ context.Context, _ chat.Principal, _, conversation, id string) (chat.Conversation, *chat.Post, error) {
	if c.err != nil {
		return chat.Conversation{}, nil, c.err
	}
	return chat.Conversation{ID: conversation}, c.post, nil
}

func welcomeHuman(t *testing.T, subject string) *trust.Principal {
	t.Helper()
	at := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: subject, SubjectKind: trust.SubjectKindHuman, Purposes: []string{"chat"}, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session", IssuedAt: at, ExpiresAt: at.Add(time.Hour), CredentialDigest: "credential"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

type welcomeFixture struct {
	welcome *OnboardingWelcome
	source  *nativeOnboardingSource
	poster  *welcomePoster
	worker  values.EntityRef
}

func newWelcomeFixture(t *testing.T) welcomeFixture {
	t.Helper()
	_, source, _, _, _ := nativeOnboardingFixture(t)
	worker := source.snapshot.Request.Plan.Worker
	at := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	poster := &welcomePoster{}
	w := &OnboardingWelcome{
		Owner:      SnapshotOnboardingWelcomeOwner{Source: source, Now: func() time.Time { return at }},
		Identities: welcomeIdentities{subject: "taylor", known: true, worker: worker},
		Poster:     poster,
		Sender:     func(tenant string) chat.Principal { return chat.Principal{TenantID: tenant, SubjectID: "system"} },
		Now:        func() time.Time { return at },
	}
	return welcomeFixture{welcome: w, source: source, poster: poster, worker: worker}
}

func TestIssue3811_OfferPostsOneCardPerEmploymentAndReplayIsTheSamePost(t *testing.T) {
	f := newWelcomeFixture(t)
	first, err := f.welcome.Offer(context.Background(), f.worker)
	if err != nil {
		t.Fatal(err)
	}
	again, err := f.welcome.Offer(context.Background(), f.worker)
	if err != nil || again.ID != first.ID || len(f.poster.posts) != 1 {
		t.Fatalf("replay = %+v, %v with %d posts", again, err, len(f.poster.posts))
	}
	got := f.poster.posts[0]
	plan := f.source.snapshot.Request.Plan
	if got.Card.Subject != "taylor" || got.Card.Employment != plan.Employment.Id || got.Card.Worker != f.worker.Id || got.IdempotencyKey != plan.Employment.Id+":"+plan.Proposal.Id {
		t.Fatalf("card = %+v", got)
	}
	// A rehire is a new employment of the same worker: it must not be
	// suppressed by the first welcome.
	f.source.snapshot.Request.Plan.Employment = values.EntityRef{Tenant: "tenant-a", Kind: "employment", Id: "second-employment"}
	f.source.snapshot.Request.Plan.CanonicalDigest = ""
	rehired := f.welcome
	rehired.Owner = welcomeStaticOwner{facts: OnboardingWelcomeFacts{Worker: f.worker, Employment: f.source.snapshot.Request.Plan.Employment, Proposal: plan.Proposal, State: OnboardingWelcomeActive}}
	if _, err := rehired.Offer(context.Background(), f.worker); err != nil || len(f.poster.posts) != 2 {
		t.Fatalf("rehire offer: %v with %d posts", err, len(f.poster.posts))
	}
}

type welcomeStaticOwner struct {
	facts OnboardingWelcomeFacts
	err   error
}

func (o welcomeStaticOwner) Current(context.Context, values.EntityRef) (OnboardingWelcomeFacts, error) {
	return o.facts, o.err
}

func TestIssue3811_OfferWritesNothingWhenThereIsNothingToWelcome(t *testing.T) {
	f := newWelcomeFixture(t)
	active := OnboardingWelcomeFacts{Worker: f.worker, Employment: f.source.snapshot.Request.Plan.Employment, Proposal: f.source.snapshot.Request.Plan.Proposal, State: OnboardingWelcomeActive}
	for name, tc := range map[string]struct {
		owner      OnboardingWelcomeOwner
		identities OnboardingWelcomeIdentities
		want       error
	}{
		"cancelled hire":     {welcomeStaticOwner{facts: withState(active, OnboardingWelcomeCancelled)}, nil, ErrOnboardingWelcomeNotEligible},
		"closed plan":        {welcomeStaticOwner{facts: withState(active, OnboardingWelcomeClosed)}, nil, ErrOnboardingWelcomeNotEligible},
		"no plan":            {welcomeStaticOwner{err: workerlifecyclestore.ErrNotFound}, nil, ErrOnboardingWelcomeNotEligible},
		"owner unreachable":  {welcomeStaticOwner{err: errors.New("down")}, nil, ErrOnboardingWelcomeUnavailable},
		"another worker":     {welcomeStaticOwner{facts: withWorker(active, values.EntityRef{Tenant: "tenant-a", Kind: "worker", Id: "other"})}, nil, ErrOnboardingWelcomeNotEligible},
		"no chat identity":   {nil, welcomeIdentities{known: false, worker: f.worker}, ErrOnboardingWelcomeNoIdentity},
		"identity directory": {nil, welcomeIdentities{err: errors.New("down")}, ErrOnboardingWelcomeUnavailable},
	} {
		w := *f.welcome
		if tc.owner != nil {
			w.Owner = tc.owner
		}
		if tc.identities != nil {
			w.Identities = tc.identities
		}
		if _, err := w.Offer(context.Background(), f.worker); !errors.Is(err, tc.want) || len(f.poster.posts) != 0 {
			t.Errorf("%s: err %v, posts %d, want %v and none", name, err, len(f.poster.posts), tc.want)
		}
	}
}

func withState(f OnboardingWelcomeFacts, s string) OnboardingWelcomeFacts { f.State = s; return f }
func withWorker(f OnboardingWelcomeFacts, w values.EntityRef) OnboardingWelcomeFacts {
	f.Worker = w
	return f
}

func welcomePost(f welcomeFixture, subject string) *chat.Post {
	plan := f.source.snapshot.Request.Plan
	body, _ := chat.OnboardingCardBody(chat.OnboardingCard{Subject: subject, Worker: f.worker.Id, Employment: plan.Employment.Id})
	return &chat.Post{ID: "p1", ConversationID: "dm", Body: body}
}

// The checklist is the owner's: statuses come from the lifecycle source, an
// unverified item is never shown as done, and protected evidence never reaches
// the page.
func TestIssue3811_ViewsReadTheChecklistFromTheOwnerAndLeakNothing_Security(t *testing.T) {
	f := newWelcomeFixture(t)
	f.welcome.Chat = welcomeChat{post: welcomePost(f, "taylor")}
	views, err := f.welcome.Views(context.Background(), welcomeHuman(t, "taylor"), "dm", []string{"p1"})
	if err != nil {
		t.Fatal(err)
	}
	view, ok := views["p1"]
	if !ok || view.State != OnboardingWelcomeActive || len(view.Items) != 1 {
		t.Fatalf("view = %+v", views)
	}
	item := view.Items[0]
	if item.Kind != "IDENTITY" || item.Owner != "people" || item.Status != OnboardingItemConfirmed || item.ConfirmedOn != "2026-09-30" || !item.Required {
		t.Fatalf("item = %+v", item)
	}
	raw, _ := json.Marshal(views)
	for _, secret := range []string{"passport", "evidence", "REDACTED"} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("the view carries %q: %s", secret, raw)
		}
	}
	// Evidence that has gone stale is the owner's "unverified", not "done".
	stale := newWelcomeFixture(t)
	stale.welcome.Chat = welcomeChat{post: welcomePost(stale, "taylor")}
	stale.welcome.Owner = SnapshotOnboardingWelcomeOwner{Source: stale.source, Now: func() time.Time { return time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC) }}
	views, err = stale.welcome.Views(context.Background(), welcomeHuman(t, "taylor"), "dm", []string{"p1"})
	if err != nil || views["p1"].Items[0].Status != OnboardingItemUnverified || views["p1"].Items[0].ConfirmedOn != "" {
		t.Fatalf("stale view = %+v, %v", views, err)
	}
}

// Only the new hire the card names sees it; a manager, a buddy or another
// person who somehow reads the post gets no view, and a refusal reads the same
// as a post that does not exist.
func TestIssue3811_ViewsAreOnlyForTheNewHire_Security(t *testing.T) {
	f := newWelcomeFixture(t)
	f.welcome.Chat = welcomeChat{post: welcomePost(f, "taylor")}
	views, err := f.welcome.Views(context.Background(), welcomeHuman(t, "manager"), "dm", []string{"p1"})
	if err != nil || len(views) != 0 {
		t.Fatalf("another person got %+v, %v", views, err)
	}
	f.welcome.Chat = welcomeChat{err: chat.ErrPermissionDenied}
	views, err = f.welcome.Views(context.Background(), welcomeHuman(t, "taylor"), "dm", []string{"p1"})
	if err != nil || len(views) != 0 {
		t.Fatalf("a refused read got %+v, %v", views, err)
	}
	// The card names a worker the identity directory does not join to this person.
	f.welcome.Chat = welcomeChat{post: welcomePost(f, "taylor")}
	f.welcome.Identities = welcomeIdentities{subject: "taylor", known: true, worker: values.EntityRef{Tenant: "tenant-a", Kind: "worker", Id: "someone-else"}}
	views, err = f.welcome.Views(context.Background(), welcomeHuman(t, "taylor"), "dm", []string{"p1"})
	if err != nil || len(views) != 0 {
		t.Fatalf("a mismatched identity got %+v, %v", views, err)
	}
	// The directory joins this person to a worker of another tenant: no view,
	// and the owner is never asked about that worker.
	f.welcome.Identities = welcomeIdentities{subject: "taylor", known: true, worker: values.EntityRef{Tenant: "tenant-b", Kind: "worker", Id: f.worker.Id}}
	f.welcome.Owner = welcomeStaticOwner{err: errors.New("must not be read")}
	views, err = f.welcome.Views(context.Background(), welcomeHuman(t, "taylor"), "dm", []string{"p1"})
	if err != nil || len(views) != 0 {
		t.Fatalf("a cross-tenant worker got %+v, %v", views, err)
	}
	// A deleted post, or a post that is not the one asked for, shows nothing.
	f.welcome.Identities = welcomeIdentities{subject: "taylor", known: true, worker: f.worker}
	deleted := welcomePost(f, "taylor")
	deleted.Deleted = true
	f.welcome.Chat = welcomeChat{post: deleted}
	if views, err = f.welcome.Views(context.Background(), welcomeHuman(t, "taylor"), "dm", []string{"p1"}); err != nil || len(views) != 0 {
		t.Fatalf("a deleted card got %+v, %v", views, err)
	}
	other := welcomePost(f, "taylor")
	other.ID = "p2"
	f.welcome.Chat = welcomeChat{post: other}
	if views, err = f.welcome.Views(context.Background(), welcomeHuman(t, "taylor"), "dm", []string{"p1"}); err != nil || len(views) != 0 {
		t.Fatalf("a different post got %+v, %v", views, err)
	}
	// An agent never reads a welcome through the person's route.
	agent, _ := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "bot", SubjectKind: trust.SubjectKindAgent, Purposes: []string{"chat"}, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "s", IssuedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour), CredentialDigest: "c"})
	if _, err := f.welcome.Views(context.Background(), agent, "dm", []string{"p1"}); !errors.Is(err, ErrOnboardingWelcomeDenied) {
		t.Fatalf("agent err = %v", err)
	}
}

// A card for an earlier employment, a cancelled hire or a source that cannot be
// reached says so; it never shows the new plan's items or a stale "done".
func TestIssue3811_ViewsStateWhatChangedAndWhatCannotBeRead(t *testing.T) {
	f := newWelcomeFixture(t)
	f.welcome.Chat = welcomeChat{post: welcomePost(f, "taylor")}
	plan := f.source.snapshot.Request.Plan
	active := OnboardingWelcomeFacts{Worker: f.worker, Employment: plan.Employment, Proposal: plan.Proposal, StartDate: "2026-10-05", State: OnboardingWelcomeActive, Items: []OnboardingWelcomeItem{{Kind: "ACCESS", Status: OnboardingItemPending}}}
	later := values.EntityRef{Tenant: "tenant-a", Kind: "employment", Id: "rehire"}
	for name, tc := range map[string]struct {
		owner     welcomeStaticOwner
		wantState string
		wantItems int
	}{
		"start date changed":   {welcomeStaticOwner{facts: active}, OnboardingWelcomeActive, 1},
		"rehire replaced plan": {welcomeStaticOwner{facts: withEmployment(active, later)}, OnboardingWelcomeEnded, 0},
		"hire cancelled":       {welcomeStaticOwner{facts: withState(active, OnboardingWelcomeCancelled)}, OnboardingWelcomeCancelled, 0},
		"plan gone":            {welcomeStaticOwner{err: ErrOnboardingWelcomeNotFound}, OnboardingWelcomeEnded, 0},
		"owner unreachable":    {welcomeStaticOwner{err: errors.New("down")}, OnboardingWelcomeUnreachable, 0},
	} {
		f.welcome.Owner = tc.owner
		views, err := f.welcome.Views(context.Background(), welcomeHuman(t, "taylor"), "dm", []string{"p1"})
		got := views["p1"]
		if err != nil || got.State != tc.wantState || len(got.Items) != tc.wantItems {
			t.Errorf("%s: view %+v, err %v", name, got, err)
		}
	}
	f.welcome.Owner = welcomeStaticOwner{err: errors.New("down")}
	if v, _ := f.welcome.Views(context.Background(), welcomeHuman(t, "taylor"), "dm", []string{"p1"}); v["p1"].StartDate != "" {
		t.Errorf("an unreachable owner must not leave a start date behind: %+v", v["p1"])
	}
}

func withEmployment(f OnboardingWelcomeFacts, e values.EntityRef) OnboardingWelcomeFacts {
	f.Employment = e
	return f
}

func TestIssue3811_ViewsRefuseBadRequests(t *testing.T) {
	f := newWelcomeFixture(t)
	f.welcome.Chat = welcomeChat{post: welcomePost(f, "taylor")}
	human := welcomeHuman(t, "taylor")
	if _, err := f.welcome.Views(context.Background(), human, "", []string{"p1"}); !errors.Is(err, ErrOnboardingWelcomeInvalid) {
		t.Errorf("no conversation: %v", err)
	}
	if _, err := f.welcome.Views(context.Background(), human, "dm", nil); !errors.Is(err, ErrOnboardingWelcomeInvalid) {
		t.Errorf("no posts: %v", err)
	}
	if _, err := f.welcome.Views(context.Background(), nil, "dm", []string{"p1"}); !errors.Is(err, ErrOnboardingWelcomeDenied) {
		t.Errorf("no person: %v", err)
	}
	var unwired *OnboardingWelcome
	if _, err := unwired.Views(context.Background(), human, "dm", []string{"p1"}); !errors.Is(err, ErrOnboardingWelcomeUnavailable) {
		t.Errorf("unwired: %v", err)
	}
}
