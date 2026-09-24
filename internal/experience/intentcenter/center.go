// Package intentcenter builds a non-authoritative projection over owner data.
package intentcenter

import (
	"net/url"
	"strings"
	"time"
)

type Reference struct {
	IntentID       string `json:"intentId"`
	RelationshipID string `json:"relationshipId,omitempty"`
	ProposalID     string `json:"proposalId,omitempty"`
	WorkItemID     string `json:"workItemId,omitempty"`
	MessageID      string `json:"messageId,omitempty"`
	WorkflowID     string `json:"workflowId,omitempty"`
	ApprovalID     string `json:"approvalId,omitempty"`
}
type Authority struct {
	PrincipalID               string   `json:"principalId"`
	TenantID                  string   `json:"tenantId"`
	Revision                  string   `json:"revision"`
	CanView                   bool     `json:"canView"`
	Capabilities              []string `json:"capabilities"`
	VisibleFields             []string `json:"visibleFields,omitempty"`
	CanViewRestrictedEvidence bool     `json:"canViewRestrictedEvidence,omitempty"`
}
type Lifecycle struct {
	Intent   string `json:"intent"`
	Work     string `json:"work"`
	Approval string `json:"approval"`
	Delivery string `json:"delivery"`
	External string `json:"external"`
}
type SafeAction struct {
	ID         string    `json:"id"`
	Capability string    `json:"capability"`
	Label      string    `json:"label"`
	Enabled    bool      `json:"enabled"`
	Reason     string    `json:"reason,omitempty"`
	Refs       Reference `json:"refs"`
}
type RedactedValue struct {
	Redacted bool   `json:"redacted"`
	Reason   string `json:"reason"`
}
type DeepLink struct {
	Href                    string    `json:"href"`
	IntentID                string    `json:"intentId"`
	Refs                    Reference `json:"refs"`
	AuthorityRevision       string    `json:"authorityRevision"`
	RequiresReauthorization bool      `json:"requiresReauthorization"`
}
type Item struct {
	Reference
	Refs       Reference    `json:"refs"`
	Kind       string       `json:"kind"`
	Title      string       `json:"title"`
	Summary    string       `json:"summary,omitempty"`
	State      Lifecycle    `json:"state"`
	StateLabel string       `json:"stateLabel"`
	Stale      bool         `json:"stale"`
	Restricted bool         `json:"restricted,omitempty"`
	Evidence   any          `json:"evidence,omitempty"`
	Actions    []SafeAction `json:"actions"`
	DeepLink   *DeepLink    `json:"deepLink,omitempty"`
	Owner      Owner        `json:"owner"`
	UpdatedAt  string       `json:"updatedAt"`
}
type Owner struct {
	PrincipalID string `json:"principalId,omitempty"`
	TeamID      string `json:"teamId,omitempty"`
}
type SourceAction struct {
	ID         string `json:"id"`
	Capability string `json:"capability"`
	Label      string `json:"label"`
}
type SourceRecord struct {
	Reference
	Kind               string           `json:"kind"`
	Title              string           `json:"title"`
	Summary            string           `json:"summary,omitempty"`
	State              PartialLifecycle `json:"state"`
	Owner              Owner            `json:"owner,omitempty"`
	UpdatedAt          string           `json:"updatedAt"`
	Evidence           any              `json:"evidence,omitempty"`
	RestrictedEvidence bool             `json:"restrictedEvidence,omitempty"`
	Actions            []SourceAction   `json:"actions,omitempty"`
}
type PartialLifecycle struct {
	Intent   string `json:"intent"`
	Work     string `json:"work,omitempty"`
	Approval string `json:"approval,omitempty"`
	Delivery string `json:"delivery,omitempty"`
	External string `json:"external,omitempty"`
}
type TimelineSource struct {
	Reference
	EventID            string `json:"eventId"`
	OccurredAt         string `json:"occurredAt"`
	Type               string `json:"type"`
	Summary            string `json:"summary"`
	Evidence           any    `json:"evidence,omitempty"`
	RestrictedEvidence bool   `json:"restrictedEvidence,omitempty"`
}
type TimelineEvent struct {
	Reference
	EventID    string `json:"eventId"`
	OccurredAt string `json:"occurredAt"`
	Type       string `json:"type"`
	Summary    string `json:"summary"`
	Evidence   any    `json:"evidence,omitempty"`
}
type Input struct {
	Now             string
	Authority       Authority
	Records         []SourceRecord
	Timeline        []TimelineSource
	InspectIntentID string
}
type Projection struct {
	AuthorityRevision string          `json:"authorityRevision"`
	GeneratedAt       string          `json:"generatedAt"`
	Drafts            []Item          `json:"drafts"`
	Tasks             []Item          `json:"tasks"`
	Approvals         []Item          `json:"approvals"`
	Messages          []Item          `json:"messages"`
	Timeline          []TimelineEvent `json:"timeline"`
	Inspector         *Item           `json:"inspector,omitempty"`
}

var terminalStates = map[string]struct{}{"completed": {}, "cancelled": {}, "canceled": {}, "rejected": {}, "failed": {}, "closed": {}}
var uncertainStates = map[string]struct{}{"ambiguous": {}, "unknown": {}, "inconsistent": {}}

func Project(in Input) Projection {
	now := in.Now
	if now == "" {
		now = time.Now().UTC().Format(time.RFC3339Nano)
	}
	p := Projection{AuthorityRevision: in.Authority.Revision, GeneratedAt: now, Drafts: []Item{}, Tasks: []Item{}, Approvals: []Item{}, Messages: []Item{}, Timeline: []TimelineEvent{}}
	for _, record := range in.Records {
		if hidden(record.RestrictedEvidence, in.Authority) {
			continue
		}
		item := projectRecord(record, in.Authority)
		switch item.Kind {
		case "draft":
			p.Drafts = append(p.Drafts, item)
		case "task":
			p.Tasks = append(p.Tasks, item)
		case "approval":
			p.Approvals = append(p.Approvals, item)
		case "message":
			p.Messages = append(p.Messages, item)
		}
		if in.InspectIntentID != "" && item.IntentID == in.InspectIntentID && p.Inspector == nil {
			copyItem := item
			p.Inspector = &copyItem
		}
	}
	for _, event := range in.Timeline {
		if hidden(event.RestrictedEvidence, in.Authority) {
			continue
		}
		p.Timeline = append(p.Timeline, TimelineEvent{Reference: event.Reference, EventID: event.EventID, OccurredAt: event.OccurredAt, Type: event.Type, Summary: event.Summary, Evidence: redact(event.Evidence, in.Authority, event.RestrictedEvidence)})
	}
	return p
}
func ProjectIntentCenter(in Input) Projection { return Project(in) }

func projectRecord(r SourceRecord, a Authority) Item {
	s := Lifecycle{Intent: r.State.Intent, Work: defaultValue(r.State.Work, "not_started"), Approval: defaultValue(r.State.Approval, "not_required"), Delivery: defaultValue(r.State.Delivery, "not_started"), External: defaultValue(r.State.External, "not_applicable")}
	item := Item{Reference: r.Reference, Refs: r.Reference, Kind: r.Kind, Title: r.Title, Summary: r.Summary, State: s, StateLabel: strings.Join([]string{s.Intent, s.Work, s.Approval, s.Delivery, s.External}, "/"), Stale: !a.CanView, Owner: r.Owner, UpdatedAt: r.UpdatedAt, Evidence: redact(r.Evidence, a, r.RestrictedEvidence), Actions: []SafeAction{}}
	if r.RestrictedEvidence {
		item.Restricted = true
	}
	terminal := isTerminal(s.Intent) || isTerminal(s.Work)
	uncertain := isUncertain(s.Intent) || isUncertain(s.External)
	for _, action := range r.Actions {
		capable := contains(a.Capabilities, action.Capability)
		allowed := a.CanView && capable && !terminal && !(action.Capability == "intent.retry" && uncertain)
		reason := ""
		if !a.CanView || !capable {
			reason = "unauthorized"
		} else if terminal {
			reason = "terminal"
		} else if action.Capability == "intent.retry" && uncertain {
			reason = "stale"
		}
		item.Actions = append(item.Actions, SafeAction{ID: action.ID, Capability: action.Capability, Label: action.Label, Enabled: allowed, Reason: reason, Refs: r.Reference})
	}
	if link, ok := makeLink(r.Reference, a); ok {
		item.DeepLink = &link
	}
	return item
}
func hidden(restricted bool, a Authority) bool {
	return !a.CanView && restricted && !a.CanViewRestrictedEvidence
}
func redact(value any, a Authority, restricted bool) any {
	if restricted && (!a.CanView || !a.CanViewRestrictedEvidence) {
		return RedactedValue{Redacted: true, Reason: map[bool]string{true: "restricted", false: "not_authorized"}[a.CanView]}
	}
	return value
}
func makeLink(refs Reference, a Authority) (DeepLink, bool) {
	if !a.CanView || refs.IntentID == "" {
		return DeepLink{}, false
	}
	params := []string{"intent=" + url.QueryEscape(refs.IntentID), "auth=" + url.QueryEscape(a.Revision)}
	for _, pair := range []struct{ k, v string }{{"relationship", refs.RelationshipID}, {"proposal", refs.ProposalID}, {"workItem", refs.WorkItemID}, {"message", refs.MessageID}, {"approval", refs.ApprovalID}} {
		if pair.v != "" {
			params = append(params, pair.k+"="+url.QueryEscape(pair.v))
		}
	}
	return DeepLink{Href: "/intent-center/" + encodeComponent(refs.IntentID) + "?" + strings.Join(params, "&"), IntentID: refs.IntentID, Refs: refs, AuthorityRevision: a.Revision}, true
}
func encodeComponent(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-_.!~*'()", rune(c)) {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		}
	}
	return b.String()
}
func ResolveDeepLink(link DeepLink, a Authority) (DeepLink, bool) {
	if !a.CanView || a.Revision != link.AuthorityRevision {
		return DeepLink{}, false
	}
	link.RequiresReauthorization = false
	return link, true
}
func IsSafeAction(action SafeAction, a Authority) bool {
	return action.Enabled && a.CanView && contains(a.Capabilities, action.Capability) && action.Refs.IntentID != ""
}
func defaultValue(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
func isTerminal(v string) bool  { _, ok := terminalStates[v]; return ok }
func isUncertain(v string) bool { _, ok := uncertainStates[v]; return ok }
