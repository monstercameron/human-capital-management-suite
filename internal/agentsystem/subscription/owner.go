package subscription

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

var ErrRevision = errors.New("agent subscription: stale revision")

// Definition binds a reviewed event declaration to one installation. Policy
// snapshots inside Declaration are resolved afresh before evaluating events.
type Definition struct {
	Declaration Subscription
	OwnerID     string
	Revision    uint64
	State       string
	RunTimeout  time.Duration
}
type Actor struct{ TenantID, UserID string }
type Audit struct {
	TenantID, SubscriptionID, ActorID, Action string
	Revision                                  uint64
	At                                        time.Time
}
type Delivery struct {
	TenantID, Key, SubscriptionID string
	Revision                      uint64
	Candidate                     Candidate
	At                            time.Time
}
type Store interface {
	LoadSubscription(context.Context, string, string) (Definition, bool, error)
	SaveSubscription(context.Context, Definition, uint64, Audit) error
	ReserveEvent(context.Context, Delivery, time.Duration) (Delivery, bool, error)
	GetEvent(context.Context, string, string) (Delivery, bool, error)
	PendingEvents(context.Context, string, int) ([]Delivery, error)
	AcknowledgeEvent(context.Context, string, string, agentrun.Record) error
}

// SourceOwner must resolve an audience-filtered projection from the domain's
// durable source. Entry points supply an event ID, never event payload fields.
type SourceOwner interface {
	Project(context.Context, string, string, agentrun.AudienceScope) (EventProjection, error)
	ValidateClasses(context.Context, Definition) error
}

// GovernedSourceOwner additionally reauthorizes the native projection using
// the exact current subscription sponsor and disclosure ceiling. Production
// composition requires this port; a declaration is a lookup request, not proof.
type GovernedSourceOwner interface {
	SourceOwner
	ProjectForSubscription(context.Context,Definition,Subscription,string)(EventProjection,error)
}
type Policy interface {
	Authorize(context.Context, Actor, string, Definition) error
	Resolve(context.Context, Definition) (Subscription, error)
}
type Inbox interface {
	Admit(context.Context, agentrun.Request) (agentrun.Record, bool, error)
}
type Owner struct {
	store  Store
	source SourceOwner
	policy Policy
	inbox  Inbox
}

func NewOwner(store Store, source SourceOwner, policy Policy, inbox Inbox) (*Owner, error) {
	if store == nil || source == nil || policy == nil || inbox == nil {
		return nil, ErrInvalid
	}
	return &Owner{store, source, policy, inbox}, nil
}
func ValidateDefinition(d Definition) error {
	s := d.Declaration
	if !cleanRequired(s.ID, 256) || !cleanRequired(s.TenantID, 256) || !cleanRequired(d.OwnerID, 256) || d.RunTimeout <= 0 || d.RunTimeout > 24*time.Hour || len(s.EventKinds) == 0 || len(s.EventKinds) > 64 || s.Debounce < 0 || s.MaxCauseDepth == 0 || !validDigest(s.Agent.Digest) || !validDigest(s.Audience.Digest) || s.BudgetCeiling.MaxCostMicros == 0 || s.BudgetCeiling.MaxInputTokens == 0 || s.BudgetCeiling.MaxOutputTokens == 0 {
		return ErrInvalid
	}
	for _, value := range []string{s.Agent.AgentID, s.Agent.Version, s.InstallationID, s.Purpose, s.SponsorID, s.AgentPrincipalID, s.Audience.ID, s.Audience.SnapshotID} {
		if !cleanRequired(value, 256) {
			return ErrInvalid
		}
	}
	if d.State != "DRAFT" && d.State != "ACTIVE" && d.State != "PAUSED" && d.State != "RETIRED" {
		return ErrInvalid
	}
	return nil
}
func (o *Owner) Save(ctx context.Context, actor Actor, d Definition, expected uint64, action string, at time.Time) (Definition, error) {
	if o == nil || actor.TenantID != d.Declaration.TenantID || actor.UserID == "" || at.IsZero() {
		return Definition{}, ErrInvalid
	}
	current, found, err := o.store.LoadSubscription(ctx, actor.TenantID, d.Declaration.ID)
	if err != nil {
		return Definition{}, err
	}
	if (found && current.Revision != expected) || (!found && expected != 0) {
		return Definition{}, ErrRevision
	}
	switch action {
	case "DRAFT":
		if actor.UserID != d.OwnerID || found && current.OwnerID != d.OwnerID {
			return Definition{}, ErrRevoked
		}
		d.State = "DRAFT"
	case "PUBLISH":
		if !found || current.State != "DRAFT" || actor.UserID == current.OwnerID {
			return Definition{}, ErrRevoked
		}
		d = current
		d.State = "ACTIVE"
	case "PAUSE":
		if !found || current.State != "ACTIVE" {
			return Definition{}, ErrInactive
		}
		d = current
		d.State = "PAUSED"
	case "RESUME":
		if !found || current.State != "PAUSED" {
			return Definition{}, ErrInactive
		}
		d = current
		d.State = "ACTIVE"
	case "RETIRE":
		if !found || current.State == "RETIRED" {
			return Definition{}, ErrInactive
		}
		d = current
		d.State = "RETIRED"
	default:
		return Definition{}, ErrInvalid
	}
	d.Revision = expected + 1
	d.Declaration.Revision = d.Revision
	d.Declaration.CurrentRevision = d.Revision
	d.Declaration.State = d.State
	if err := ValidateDefinition(d); err != nil {
		return Definition{}, err
	}
	if err := o.policy.Authorize(ctx, actor, action, d); err != nil {
		return Definition{}, err
	}
	if err := o.source.ValidateClasses(ctx, d); err != nil {
		return Definition{}, err
	}
	if err := o.store.SaveSubscription(ctx, d, expected, Audit{actor.TenantID, d.Declaration.ID, actor.UserID, action, d.Revision, at.UTC()}); err != nil {
		return Definition{}, err
	}
	return d, nil
}
func (o *Owner) Ingest(ctx context.Context, tenant, id, eventID string, at time.Time) (Delivery, bool, error) {
	d, found, err := o.store.LoadSubscription(ctx, tenant, id)
	if err != nil {
		return Delivery{}, false, err
	}
	if !found || d.State != "ACTIVE" {
		return Delivery{}, false, ErrInactive
	}
	// Source identity is evaluated before reserving debounce. Existing durable
	// events are replayed with their original projection and request deadline.
	sub, err := o.policy.Resolve(ctx, d)
	if err != nil {
		return Delivery{}, false, err
	}
	if !resolvedMatches(d, sub) {
		return Delivery{}, false, ErrRevoked
	}
	key := sourceKey(tenant, id, d.Revision, eventID)
	if prior, found, err := o.store.GetEvent(ctx, tenant, key); err != nil {
		return Delivery{}, false, err
	} else if found {
		return prior, false, nil
	}
	var event EventProjection
	if source,ok:=o.source.(GovernedSourceOwner);ok {
		event,err=source.ProjectForSubscription(ctx,d,sub,eventID)
	}else{
		event,err=o.source.Project(ctx,tenant,eventID,sub.Audience)
	}
	if err != nil {
		return Delivery{}, false, err
	}
	if event.ID != eventID {
		return Delivery{}, false, ErrProjectionInvalid
	}
	candidate, err := Evaluate(EvaluateInput{Subscription: sub, Event: event, At: at, Deadline: at.Add(d.RunTimeout), Budget: sub.BudgetCeiling})
	if err != nil {
		return Delivery{}, false, err
	}
	candidate.Request.Source.Ref = key
	delivery := Delivery{tenant, key, id, d.Revision, candidate, at.UTC()}
	return o.store.ReserveEvent(ctx, delivery, sub.Debounce)
}
func (o *Owner) CheckRequest(ctx context.Context, r agentrun.Request) error {
	event, found, err := o.store.GetEvent(ctx, r.Source.TenantID, r.Source.Ref)
	if err != nil {
		return err
	}
	if !found || r.Source.Kind != agentrun.SourceEvent || event.Key != r.Source.Key {
		return ErrInvalid
	}
	got, e1 := agentrun.AdmissionRequestDigest(r)
	want, e2 := agentrun.AdmissionRequestDigest(event.Candidate.Request)
	if e1 != nil || e2 != nil || got != want {
		return ErrInvalid
	}
	d, found, err := o.store.LoadSubscription(ctx, r.Source.TenantID, event.SubscriptionID)
	if err != nil {
		return err
	}
	if !found || d.State != "ACTIVE" || d.Revision != event.Revision {
		return ErrInactive
	}
	resolved, err := o.policy.Resolve(ctx, d)
	if err != nil {
		return err
	}
	if !resolvedMatches(d, resolved) || resolved.InstallationID != r.InstallationID || resolved.SponsorID != r.Principal.SponsorID || resolved.AgentPrincipalID != r.Principal.AgentPrincipalID || resolved.Purpose != r.Purpose || !withinBudget(r.Budget, resolved.BudgetCeiling) {
		return ErrRevoked
	}
	if resolved.Revision != event.Revision || resolved.CurrentRevision != event.Revision || !resolved.InstallationActive || !resolved.GrantActive || resolved.GrantRevision != resolved.CurrentGrantRevision || resolved.Audience != r.Audience || resolved.Agent != r.Agent {
		return ErrRevoked
	}
	return nil
}
func resolvedMatches(d Definition, s Subscription) bool {
	declared := d.Declaration
	return s.ID == declared.ID && s.TenantID == declared.TenantID && s.Revision == d.Revision && s.CurrentRevision == d.Revision && s.State == d.State && s.Agent == declared.Agent && s.InstallationID == declared.InstallationID && s.Purpose == declared.Purpose && s.SponsorID == declared.SponsorID && s.AgentPrincipalID == declared.AgentPrincipalID && s.Audience == declared.Audience && reflect.DeepEqual(s.EventKinds, declared.EventKinds) && reflect.DeepEqual(s.FieldsByKind, declared.FieldsByKind) && s.MaximumClassification == declared.MaximumClassification && s.Debounce == declared.Debounce && s.MaxCauseDepth == declared.MaxCauseDepth && withinBudget(s.BudgetCeiling, declared.BudgetCeiling)
}
func (o *Owner) ResolveSourceKey(ctx context.Context, r agentrun.Request) (string, error) {
	event, found, err := o.store.GetEvent(ctx, r.Source.TenantID, r.Source.Ref)
	if err != nil {
		return "", err
	}
	if !found || r.Source.Kind != agentrun.SourceEvent {
		return "", ErrInvalid
	}
	return event.Key, nil
}
func (o *Owner) Replay(ctx context.Context, tenant string, limit int) (int, error) {
	events, err := o.store.PendingEvents(ctx, tenant, limit)
	if err != nil {
		return 0, err
	}
	done := 0
	var failures []error
	for _, event := range events {
		if err := o.CheckRequest(ctx, event.Candidate.Request); err != nil {
			failures = append(failures, err)
			continue
		}
		record, _, err := o.inbox.Admit(ctx, event.Candidate.Request)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if err := agentrun.ValidateAdmissionRecord(record); err != nil || record.Request.Source != event.Candidate.Request.Source {
			failures = append(failures, fmt.Errorf("%w: inbox record differs", ErrInvalid))
			continue
		}
		if err := o.store.AcknowledgeEvent(ctx, tenant, event.Key, record); err != nil {
			failures = append(failures, err)
			continue
		}
		done++
	}
	return done, errors.Join(failures...)
}
