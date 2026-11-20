package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workerlifecyclestore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workerlifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Issue 3811: a new hire's private welcome. When an onboarding plan becomes
// available for a person who already has a Chat identity, the deployment's
// system sender opens a private conversation with that person alone and posts
// one card. The card holds no task and no status: each time it is drawn, the
// hire's own session asks this use case, which reads the onboarding owner and
// answers with what this person may see of their own checklist. Chat is never
// the record of what is done, never grants access, and never adds a manager or
// buddy to the conversation.

var (
	ErrOnboardingWelcomeInvalid     = errors.New("onboarding welcome: invalid request")
	ErrOnboardingWelcomeNotFound    = errors.New("onboarding welcome: card not found")
	ErrOnboardingWelcomeDenied      = errors.New("onboarding welcome: not allowed")
	ErrOnboardingWelcomeUnavailable = errors.New("onboarding welcome: unavailable")
	// ErrOnboardingWelcomeNotEligible is returned when the owner's current state
	// says there is nothing to welcome: no plan, a closed plan or a cancelled hire.
	ErrOnboardingWelcomeNotEligible = errors.New("onboarding welcome: no active onboarding plan")
	// ErrOnboardingWelcomeNoIdentity is returned when the person has no Chat
	// identity yet. Nothing is written and no identity is invented; the offer is
	// repeated once the person is provisioned.
	ErrOnboardingWelcomeNoIdentity = errors.New("onboarding welcome: the new hire has no chat identity yet")
)

// Plan states the owner can report.
const (
	OnboardingWelcomeActive    = "active"
	OnboardingWelcomeClosed    = "closed"
	OnboardingWelcomeCancelled = "cancelled"
	// OnboardingWelcomeEnded is a view-only state: the employment the card was
	// posted for is no longer the person's current onboarding (a rehire or a
	// replaced plan), or the owner no longer has the plan.
	OnboardingWelcomeEnded = "ended"
	// OnboardingWelcomeUnreachable is a view-only state: the owner could not be
	// read, so nothing is claimed about any item.
	OnboardingWelcomeUnreachable = "unavailable"
)

// The item statuses a new hire sees, in plain-word terms the page translates.
const (
	OnboardingItemConfirmed  = "confirmed"
	OnboardingItemPending    = "pending"
	OnboardingItemAttention  = "attention"
	OnboardingItemUnverified = "unverified"
	OnboardingItemOptional   = "optional"
)

// OnboardingWelcomeItem is one requirement as the owner reports it. It carries
// no evidence reference and no account of the evidence: only what kind of thing
// it is, who owns it, where it stands and the day the owner last confirmed it.
type OnboardingWelcomeItem struct {
	Ordinal     int
	Kind        string
	Owner       string
	Status      string
	ConfirmedOn string
	Required    bool
}

// OnboardingWelcomeFacts is the owner's current word on one worker's plan.
type OnboardingWelcomeFacts struct {
	Worker, Employment, Proposal values.EntityRef
	StartDate                    string
	State                        string
	Items                        []OnboardingWelcomeItem
}

// OnboardingWelcomeOwner reads the onboarding owner. It is always read in the
// caller's tenant and for exactly the worker named.
type OnboardingWelcomeOwner interface {
	Current(ctx context.Context, worker values.EntityRef) (OnboardingWelcomeFacts, error)
}

// OnboardingWelcomeIdentities joins the HR worker and the Chat person in both
// directions. It answers false rather than guessing.
type OnboardingWelcomeIdentities interface {
	ChatSubject(ctx context.Context, worker values.EntityRef) (subject string, ok bool, err error)
	Worker(ctx context.Context, tenant, subject string) (worker values.EntityRef, ok bool, err error)
}

// OnboardingWelcomePoster writes the card; chat.Service is the served one.
type OnboardingWelcomePoster interface {
	PostOnboardingCard(context.Context, chat.OnboardingCardPost) (chat.Post, error)
}

// OnboardingWelcomeChat reads a post the reader may open.
type OnboardingWelcomeChat interface {
	ReadAuthorizedReference(context.Context, chat.Principal, string, string, string) (chat.Conversation, *chat.Post, error)
}

// OnboardingWelcome is the use case behind the card and its route.
type OnboardingWelcome struct {
	Owner      OnboardingWelcomeOwner
	Identities OnboardingWelcomeIdentities
	Poster     OnboardingWelcomePoster
	Chat       OnboardingWelcomeChat
	// Sender is the system identity cards are posted as, per tenant.
	Sender func(tenant string) chat.Principal
	Now    func() time.Time
}

func (w *OnboardingWelcome) ready() bool {
	return w != nil && !isNilPersonaOutputPort(w.Owner) && !isNilPersonaOutputPort(w.Identities)
}

func (w *OnboardingWelcome) now() time.Time {
	if w.Now != nil {
		return w.Now().UTC()
	}
	return time.Now().UTC()
}

// OnboardingWelcomeKey is the stable key of one intended welcome. It binds the
// employment and the proposal that created it, never the worker alone: a person
// who leaves and returns, or holds two employments at once, gets a welcome for
// each, while a redelivered event for the same employment is the same post. A
// changed start date does not change it: the card reads the date from the owner.
func OnboardingWelcomeKey(plan OnboardingWelcomeFacts) string {
	return plan.Employment.Id + ":" + plan.Proposal.Id
}

// Offer posts the welcome for one worker's current onboarding plan, once. The
// owner's current state decides eligibility; the identity directory decides
// whom the card is for. It is safe to call again for the same plan.
func (w *OnboardingWelcome) Offer(ctx context.Context, worker values.EntityRef) (chat.Post, error) {
	if !w.ready() || isNilPersonaOutputPort(w.Poster) || w.Sender == nil {
		return chat.Post{}, ErrOnboardingWelcomeUnavailable
	}
	if worker.Validate() != nil || worker.Kind != "worker" {
		return chat.Post{}, ErrOnboardingWelcomeInvalid
	}
	facts, err := w.Owner.Current(ctx, worker)
	switch {
	case err == nil:
	case errors.Is(err, ErrOnboardingWelcomeNotFound), errors.Is(err, workerlifecyclestore.ErrNotFound):
		return chat.Post{}, ErrOnboardingWelcomeNotEligible
	default:
		return chat.Post{}, ErrOnboardingWelcomeUnavailable
	}
	if facts.Worker != worker || facts.Employment.Tenant != worker.Tenant || facts.Employment.Id == "" || facts.Proposal.Id == "" || facts.State != OnboardingWelcomeActive {
		return chat.Post{}, ErrOnboardingWelcomeNotEligible
	}
	subject, ok, err := w.Identities.ChatSubject(ctx, worker)
	if err != nil {
		return chat.Post{}, ErrOnboardingWelcomeUnavailable
	}
	if !ok || strings.TrimSpace(subject) == "" {
		return chat.Post{}, ErrOnboardingWelcomeNoIdentity
	}
	tenant := worker.Tenant.String()
	return w.Poster.PostOnboardingCard(ctx, chat.OnboardingCardPost{
		TenantID:       tenant,
		Sender:         w.Sender(tenant),
		Card:           chat.OnboardingCard{Subject: subject, Worker: worker.Id, Employment: facts.Employment.Id},
		IdempotencyKey: OnboardingWelcomeKey(facts),
	})
}

// OnboardingWelcomeItemView is one checklist line as the page draws it.
type OnboardingWelcomeItemView struct {
	Kind        string `json:"kind"`
	Owner       string `json:"owner,omitempty"`
	Status      string `json:"status"`
	ConfirmedOn string `json:"confirmed_on,omitempty"`
	Required    bool   `json:"required"`
}

// OnboardingWelcomeView is what the reader may see of one welcome card.
type OnboardingWelcomeView struct {
	PostID    string                      `json:"post_id"`
	State     string                      `json:"state"`
	StartDate string                      `json:"start_date,omitempty"`
	AsOf      string                      `json:"as_of"`
	Items     []OnboardingWelcomeItemView `json:"items,omitempty"`
}

const onboardingWelcomeMaxViews = 10

// Views reads what the person may see of each given welcome card. A card is
// shown only to the new hire it names, in a conversation they can open, and
// only while the owner's current onboarding is the very employment the card was
// posted for. Anything else is left out of the answer, indistinguishable from a
// post that does not exist.
func (w *OnboardingWelcome) Views(ctx context.Context, human *trust.Principal, conversation string, postIDs []string) (map[string]OnboardingWelcomeView, error) {
	p, err := chatWorkPrincipal(human)
	if err != nil {
		return nil, ErrOnboardingWelcomeDenied
	}
	if !w.ready() || isNilPersonaOutputPort(w.Chat) {
		return nil, ErrOnboardingWelcomeUnavailable
	}
	if strings.TrimSpace(conversation) == "" || len(postIDs) == 0 || len(postIDs) > onboardingWelcomeMaxViews {
		return nil, ErrOnboardingWelcomeInvalid
	}
	out := map[string]OnboardingWelcomeView{}
	asOf := w.now().Format(time.RFC3339)
	for _, id := range postIDs {
		if _, seen := out[id]; seen {
			continue
		}
		_, post, err := w.Chat.ReadAuthorizedReference(ctx, p, p.TenantID, conversation, id)
		switch {
		case err == nil:
		case errors.Is(err, chat.ErrNotFound), errors.Is(err, chat.ErrPermissionDenied):
			continue
		default:
			return nil, ErrOnboardingWelcomeUnavailable
		}
		if post == nil || post.ID != id || post.ConversationID != conversation || post.Deleted {
			continue
		}
		card, ok := chat.ParseOnboardingCard(post.Body)
		if !ok || card.Subject != p.SubjectID {
			continue
		}
		worker, known, err := w.Identities.Worker(ctx, p.TenantID, p.SubjectID)
		if err != nil {
			out[id] = OnboardingWelcomeView{PostID: id, State: OnboardingWelcomeUnreachable, AsOf: asOf}
			continue
		}
		if !known || worker.Id != card.Worker || worker.Tenant.String() != p.TenantID {
			continue
		}
		out[id] = w.view(ctx, id, card, worker, asOf)
	}
	return out, nil
}

func (w *OnboardingWelcome) view(ctx context.Context, id string, card chat.OnboardingCard, worker values.EntityRef, asOf string) OnboardingWelcomeView {
	facts, err := w.Owner.Current(ctx, worker)
	switch {
	case err == nil:
	case errors.Is(err, ErrOnboardingWelcomeNotFound), errors.Is(err, workerlifecyclestore.ErrNotFound):
		return OnboardingWelcomeView{PostID: id, State: OnboardingWelcomeEnded, AsOf: asOf}
	default:
		return OnboardingWelcomeView{PostID: id, State: OnboardingWelcomeUnreachable, AsOf: asOf}
	}
	if facts.Worker != worker || facts.Employment.Id != card.Employment {
		return OnboardingWelcomeView{PostID: id, State: OnboardingWelcomeEnded, AsOf: asOf}
	}
	if facts.State != OnboardingWelcomeActive {
		return OnboardingWelcomeView{PostID: id, State: facts.State, StartDate: facts.StartDate, AsOf: asOf}
	}
	view := OnboardingWelcomeView{PostID: id, State: OnboardingWelcomeActive, StartDate: facts.StartDate, AsOf: asOf}
	for _, item := range facts.Items {
		view.Items = append(view.Items, OnboardingWelcomeItemView{Kind: item.Kind, Owner: item.Owner, Status: item.Status, ConfirmedOn: item.ConfirmedOn, Required: item.Required})
	}
	return view
}

// SnapshotOnboardingWelcomeOwner reads the worker-lifecycle store. It never
// returns an evidence reference or the account of any requirement, and it
// resolves readiness again at the current date so stale evidence reads as
// unverified rather than confirmed.
type SnapshotOnboardingWelcomeOwner struct {
	Source PersonaOnboardingSnapshotSource
	Now    func() time.Time
}

func (o SnapshotOnboardingWelcomeOwner) Current(ctx context.Context, worker values.EntityRef) (OnboardingWelcomeFacts, error) {
	if isNilPersonaOutputPort(o.Source) || o.Now == nil || worker.Validate() != nil || worker.Kind != "worker" {
		return OnboardingWelcomeFacts{}, ErrOnboardingWelcomeUnavailable
	}
	snapshot, err := o.Source.Get(ctx, worker)
	if err != nil {
		if errors.Is(err, workerlifecyclestore.ErrNotFound) {
			return OnboardingWelcomeFacts{}, ErrOnboardingWelcomeNotFound
		}
		return OnboardingWelcomeFacts{}, err
	}
	if snapshot.Request.Plan.Worker != worker || snapshot.Validate() != nil {
		return OnboardingWelcomeFacts{}, ErrOnboardingWelcomeUnavailable
	}
	asOf, err := values.ParseLocalDate(o.Now().UTC().Format("2006-01-02"))
	if err != nil {
		return OnboardingWelcomeFacts{}, err
	}
	request := snapshot.Request
	request.AsOf = asOf
	readiness, err := workerlifecycle.ResolveOnboardingReadiness(request)
	if err != nil {
		return OnboardingWelcomeFacts{}, err
	}
	ordinals := map[string]int{}
	required := map[string]bool{}
	for _, r := range request.Plan.Requirements {
		ordinals[r.ID], required[r.ID] = r.Ordinal, r.Required
	}
	facts := OnboardingWelcomeFacts{Worker: worker, Employment: request.Plan.Employment, Proposal: request.Plan.Proposal, StartDate: request.Plan.EventDate.String(), State: OnboardingWelcomeActive}
	if snapshot.Tracker.Closed {
		facts.State = OnboardingWelcomeClosed
	}
	for _, result := range readiness.Results {
		item := OnboardingWelcomeItem{Ordinal: ordinals[result.RequirementID], Kind: string(result.Kind), Owner: result.Owner, Status: onboardingWelcomeStatus(result.Status), Required: required[result.RequirementID]}
		if result.Status == workerlifecycle.StatusSatisfied && result.ObservedAt.IsSet() {
			item.ConfirmedOn = result.ObservedAt.String()
		}
		facts.Items = append(facts.Items, item)
	}
	return facts, nil
}

func onboardingWelcomeStatus(s workerlifecycle.RequirementStatus) string {
	switch s {
	case workerlifecycle.StatusSatisfied:
		return OnboardingItemConfirmed
	case workerlifecycle.StatusPending:
		return OnboardingItemPending
	case workerlifecycle.StatusBlocked:
		return OnboardingItemAttention
	case workerlifecycle.StatusSkippedOptional:
		return OnboardingItemOptional
	default:
		// Unknown, stale or conflicting evidence is never reported as done.
		return OnboardingItemUnverified
	}
}
