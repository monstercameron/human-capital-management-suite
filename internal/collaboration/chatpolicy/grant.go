package chatpolicy

import (
	"strings"
	"time"
)

// ConversationGrantTerms is the bounded, host-proposed share that a consumer
// administrator must accept. Scope is currently limited to one conversation;
// a conversation grant cannot silently grow into a tenant-wide grant.
type ConversationGrantTerms struct {
	ConversationID string
	HostTenant     string
	ConsumerTenant string
	Scope          string
	Classification string
	Residency      string
	ExpiresAt      time.Time
}

// Validate requires every part of a conversation share to be explicit. In
// particular, zero expiry and blank classification or residency are not
// treated as wildcards.
func (t ConversationGrantTerms) Validate(at time.Time) error {
	if at.IsZero() || !grantTerm(t.ConversationID) || !grantTerm(t.HostTenant) ||
		!grantTerm(t.ConsumerTenant) || t.HostTenant == t.ConsumerTenant ||
		t.Scope != "conversation" || !grantTerm(t.Classification) ||
		!grantTerm(t.Residency) || t.ExpiresAt.IsZero() || !at.Before(t.ExpiresAt) {
		return ErrInvalidInput
	}
	return nil
}

// ProposeConversationGrant creates only the host-proposed half. It binds the
// exact terms supplied for the conversation and always requires a finite
// expiry. Consumer consent is recorded separately by AcceptConversationGrant.
func ProposeConversationGrant(id string, terms ConversationGrantTerms, version uint64, at time.Time) (Grant, error) {
	if !grantTerm(id) || version == 0 || terms.Validate(at) != nil {
		return Grant{}, ErrInvalidInput
	}
	return Grant{
		ID: id, ConversationID: terms.ConversationID, HostTenant: terms.HostTenant,
		ConsumerTenant: terms.ConsumerTenant, Version: version, Scope: terms.Scope,
		Classification: terms.Classification, Residency: terms.Residency,
		Proposed: true, AcceptedByHost: true, ExpiresAt: terms.ExpiresAt,
	}, nil
}

// AcceptConversationGrant records consent only for the named consumer and only
// while the proposal remains bounded and unexpired.
func AcceptConversationGrant(g Grant, consumerTenant string, at time.Time) (Grant, error) {
	if err := (ConversationGrantTerms{
		ConversationID: g.ConversationID, HostTenant: g.HostTenant,
		ConsumerTenant: g.ConsumerTenant, Scope: g.Scope,
		Classification: g.Classification, Residency: g.Residency,
		ExpiresAt: g.ExpiresAt,
	}).Validate(at); err != nil || g.ID == "" || g.Version == 0 || !g.Proposed ||
		!g.AcceptedByHost || g.AcceptedByConsumer || !g.RevokedAt.IsZero() ||
		consumerTenant != g.ConsumerTenant {
		return Grant{}, ErrInvalidInput
	}
	return AcceptGrant(g, consumerTenant, at)
}

// ConversationGrantCurrent checks both consent and exact term binding. It is
// suitable at read and delivery boundaries where a broader or incomplete
// stored record must fail closed.
func ConversationGrantCurrent(g Grant, terms ConversationGrantTerms, at time.Time) bool {
	return terms.Validate(at) == nil && g.Current(at, terms.ConversationID, terms.HostTenant, terms.ConsumerTenant) &&
		g.Scope == terms.Scope && g.Classification == terms.Classification &&
		g.Residency == terms.Residency && g.ExpiresAt.Equal(terms.ExpiresAt)
}

func grantTerm(value string) bool {
	return value != "" && strings.TrimSpace(value) == value
}
