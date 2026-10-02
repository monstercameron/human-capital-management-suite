// Package subscription turns a source-owned, audience-scoped domain event
// projection into a candidate for the shared agent-run admission service. It
// does not read an event bus, resolve current authority, persist dedupe state,
// or start a run.
package subscription

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

var (
	// ErrInvalid identifies malformed subscription or event admission input.
	ErrInvalid = errors.New("agent event subscription: invalid input")
	// ErrInactive means the subscription or installation revision is stale.
	ErrInactive = errors.New("agent event subscription: subscription is not current and active")
	// ErrRevoked means the current installation grant is unavailable.
	ErrRevoked = errors.New("agent event subscription: current grant or installation is revoked")
	// ErrAudience means the event projection differs from the current audience grant.
	ErrAudience = errors.New("agent event subscription: audience does not match current grant")
	// ErrLoop means the cause chain contains this agent or exceeds its depth cap.
	ErrLoop = errors.New("agent event subscription: cause chain is recursive or too deep")
	// ErrDebounced means the last admitted event falls within the cooldown window.
	ErrDebounced = errors.New("agent event subscription: debounce window has not elapsed")
	// ErrBudget means the candidate exceeds the subscription's declared ceiling.
	ErrBudget = errors.New("agent event subscription: requested budget exceeds subscription ceiling")
	// ErrForbiddenField means the source projection contains a field outside its schema.
	ErrForbiddenField = errors.New("agent event subscription: source projection contains an undeclared field")
	// ErrProjectionInvalid means the source projection cannot be safely filtered.
	ErrProjectionInvalid = errors.New("agent event subscription: source projection is invalid")
)

// FieldRule is the source owner's declaration for one field in an event
// schema. Classification is an ordinal disclosure level: PUBLIC, INTERNAL,
// CONFIDENTIAL, RESTRICTED. Unknown levels are rejected.
type FieldRule struct {
	Name           string
	Classification string
}

// ProjectedField is a source-owner-produced value with the audiences allowed
// to see it. The subscription package never accepts a raw domain event body.
type ProjectedField struct {
	Name           string
	Value          string
	Classification string
	AudienceIDs    []string
}

// EventProjection is a typed, already source-authorized event view. Schema
// and Audience must come from the owning domain's projection API.
type EventProjection struct {
	ID             string
	TenantID       string
	LegalEntity    string
	Kind           string
	SchemaVersion  string
	OccurredAt     time.Time
	CauseID        string
	CauseDepth     uint16
	OriginAgentIDs []string
	Audience       agentrun.AudienceScope
	Schema         []FieldRule
	Fields         []ProjectedField
}

// Subscription is the current resolved version and grant snapshot supplied
// by the owning installation and authorization services. It is a predicate
// input, not proof for downstream admission; AGENT-015 must recheck authority.
type Subscription struct {
	ID                     string
	TenantID               string
	Revision               uint64
	CurrentRevision        uint64
	State                  string
	Agent                  agentrun.VersionRef
	InstallationID         string
	InstallationRevision   uint64
	CurrentInstallRevision uint64
	InstallationActive     bool
	GrantActive            bool
	GrantRevision          uint64
	CurrentGrantRevision   uint64
	Purpose                string
	SponsorID              string
	AgentPrincipalID       string
	Audience               agentrun.AudienceScope
	EventKinds             []string
	FieldsByKind           map[string][]string
	MaximumClassification  string
	Debounce               time.Duration
	MaxCauseDepth          uint16
	BudgetCeiling          agentrun.Budget
}

// EvaluateInput carries one current-policy snapshot and an event projection.
// At and LastAdmittedAt are supplied by the caller; this pure package never
// reads a clock or stores a debounce reservation.
type EvaluateInput struct {
	Subscription   Subscription
	Event          EventProjection
	At             time.Time
	LastAdmittedAt time.Time
	Deadline       time.Time
	Budget         agentrun.Budget
}

// Candidate is safe to hand to AGENT-015 and the later context builder. Its
// run request contains only references and digests; Fields is audience- and
// subscription-filtered source content for a bounded context snapshot.
type Candidate struct {
	Request agentrun.Request
	Fields  []ProjectedField
}

// Evaluate validates current subscription predicates, filters the source
// projection, and builds the stable event source identity used by durable
// AGENT-015 deduplication. Current authority and atomic debounce admission
// remain the responsibility of the owning services.
func Evaluate(input EvaluateInput) (Candidate, error) {
	sub, event := input.Subscription, input.Event
	if err := validateIdentity(input); err != nil {
		return Candidate{}, err
	}
	if sub.State != "ACTIVE" || sub.Revision == 0 || sub.Revision != sub.CurrentRevision ||
		sub.InstallationRevision == 0 || sub.InstallationRevision != sub.CurrentInstallRevision {
		return Candidate{}, ErrInactive
	}
	if !sub.InstallationActive || !sub.GrantActive || sub.GrantRevision == 0 || sub.GrantRevision != sub.CurrentGrantRevision {
		return Candidate{}, ErrRevoked
	}
	if event.TenantID != sub.TenantID || event.Audience != sub.Audience {
		return Candidate{}, ErrAudience
	}
	if !slices.Contains(sub.EventKinds, event.Kind) {
		return Candidate{}, fmt.Errorf("%w: event kind is not subscribed", ErrInvalid)
	}
	if sub.MaxCauseDepth == 0 || event.CauseDepth >= sub.MaxCauseDepth || slices.Contains(event.OriginAgentIDs, sub.Agent.AgentID) {
		return Candidate{}, ErrLoop
	}
	if input.LastAdmittedAt.After(input.At) || (sub.Debounce > 0 && !input.LastAdmittedAt.IsZero() && input.At.Sub(input.LastAdmittedAt) < sub.Debounce) {
		return Candidate{}, ErrDebounced
	}
	if !withinBudget(input.Budget, sub.BudgetCeiling) {
		return Candidate{}, ErrBudget
	}
	fields, err := projectFields(event, sub)
	if err != nil {
		return Candidate{}, err
	}
	digest, err := projectionDigest(event, fields)
	if err != nil {
		return Candidate{}, err
	}
	key := sourceKey(sub.TenantID, sub.ID, sub.Revision, event.ID)
	causeID := event.CauseID
	if causeID == "" {
		causeID = event.ID
	}
	request := agentrun.Request{
		Source:         agentrun.SourceIdentity{TenantID: sub.TenantID, Kind: agentrun.SourceEvent, Key: key, Ref: sub.ID},
		LegalEntity:    event.LegalEntity,
		Agent:          sub.Agent,
		InstallationID: sub.InstallationID,
		Principal:      agentrun.PrincipalChain{Mode: agentrun.ModeSponsored, AgentPrincipalID: sub.AgentPrincipalID, SponsorID: sub.SponsorID},
		Purpose:        sub.Purpose,
		Audience:       sub.Audience,
		Context:        agentrun.ContextScope{ID: "domain-event:" + event.ID, SnapshotID: event.Kind + ":" + event.SchemaVersion, Digest: digest},
		Deadline:       input.Deadline,
		Budget:         input.Budget,
		CauseID:        causeID,
	}
	return Candidate{Request: request, Fields: fields}, nil
}

func validateIdentity(input EvaluateInput) error {
	sub, event := input.Subscription, input.Event
	for _, field := range []struct {
		value string
		limit int
	}{
		{sub.ID, 256}, {sub.TenantID, 256}, {sub.Agent.AgentID, 256}, {sub.Agent.Version, 256},
		{sub.InstallationID, 256}, {sub.Purpose, 4096}, {sub.SponsorID, 256}, {sub.AgentPrincipalID, 256},
		{event.ID, 200}, {event.TenantID, 256}, {event.LegalEntity, 256}, {event.Kind, 128}, {event.SchemaVersion, 64},
		{sub.Audience.ID, 256}, {sub.Audience.SnapshotID, 256}, {event.Audience.ID, 256}, {event.Audience.SnapshotID, 256},
	} {
		if !cleanRequired(field.value, field.limit) {
			return ErrInvalid
		}
	}
	if event.CauseID != "" && !cleanRequired(event.CauseID, 512) {
		return ErrInvalid
	}
	for _, origin := range event.OriginAgentIDs {
		if !cleanRequired(origin, 256) {
			return ErrInvalid
		}
	}
	if !validDigest(sub.Agent.Digest) || !validDigest(sub.Audience.Digest) ||
		!validDigest(event.Audience.Digest) || event.OccurredAt.IsZero() || input.At.IsZero() || event.OccurredAt.After(input.At) ||
		input.Deadline.IsZero() || !input.Deadline.After(input.At) ||
		sub.Debounce < 0 || sub.MaxCauseDepth == 0 || input.Budget.MaxCostMicros == 0 ||
		input.Budget.MaxInputTokens == 0 || input.Budget.MaxOutputTokens == 0 {
		return ErrInvalid
	}
	if len(event.Schema) == 0 || len(event.Schema) > 256 || len(event.Fields) == 0 || len(event.Fields) > 128 {
		return ErrProjectionInvalid
	}
	return nil
}

func projectFields(event EventProjection, sub Subscription) ([]ProjectedField, error) {
	rules := make(map[string]string, len(event.Schema))
	for _, rule := range event.Schema {
		if !validFieldName(rule.Name) || classification(rule.Classification) == 0 {
			return nil, ErrProjectionInvalid
		}
		if _, exists := rules[rule.Name]; exists {
			return nil, ErrProjectionInvalid
		}
		rules[rule.Name] = rule.Classification
	}
	allowed := make(map[string]struct{}, len(sub.FieldsByKind[event.Kind]))
	for _, name := range sub.FieldsByKind[event.Kind] {
		if !validFieldName(name) {
			return nil, ErrInvalid
		}
		if _, declared := rules[name]; !declared {
			return nil, ErrProjectionInvalid
		}
		allowed[name] = struct{}{}
	}
	maxClass := classification(sub.MaximumClassification)
	if maxClass == 0 {
		return nil, ErrInvalid
	}
	seen := make(map[string]struct{}, len(event.Fields))
	out := make([]ProjectedField, 0, len(event.Fields))
	for _, field := range event.Fields {
		ruleClass, declared := rules[field.Name]
		if !declared || ruleClass != field.Classification {
			return nil, ErrForbiddenField
		}
		if _, exists := seen[field.Name]; exists || len(field.Value) > 8192 {
			return nil, ErrProjectionInvalid
		}
		seen[field.Name] = struct{}{}
		if _, requested := allowed[field.Name]; !requested || classification(field.Classification) > maxClass ||
			!slices.Contains(field.AudienceIDs, sub.Audience.ID) {
			continue
		}
		copyField := field
		copyField.AudienceIDs = []string{sub.Audience.ID}
		out = append(out, copyField)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: no source fields remain after filtering", ErrProjectionInvalid)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func projectionDigest(event EventProjection, fields []ProjectedField) (string, error) {
	canonical := struct {
		TenantID      string
		EventID       string
		Kind          string
		SchemaVersion string
		Audience      agentrun.AudienceScope
		Fields        []ProjectedField
	}{event.TenantID, event.ID, event.Kind, event.SchemaVersion, event.Audience, fields}
	b, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("%w: encode projection digest: %v", ErrProjectionInvalid, err)
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func sourceKey(tenant, subscription string, revision uint64, eventID string) string {
	data, _ := json.Marshal(struct {
		Domain       string
		Tenant       string
		Subscription string
		Revision     uint64
		Event        string
	}{"hcmnext.agentsystem.domain-event-source/v1", tenant, subscription, revision, eventID})
	sum := sha256.Sum256(data)
	return "domain-event:" + hex.EncodeToString(sum[:])
}

func withinBudget(requested, ceiling agentrun.Budget) bool {
	return ceiling.MaxCostMicros > 0 && ceiling.MaxInputTokens > 0 && ceiling.MaxOutputTokens > 0 &&
		requested.MaxCostMicros <= ceiling.MaxCostMicros && requested.MaxInputTokens <= ceiling.MaxInputTokens &&
		requested.MaxOutputTokens <= ceiling.MaxOutputTokens
}

func validDigest(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, char := range value[len("sha256:"):] {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func cleanRequired(value string, max int) bool {
	return value != "" && len(value) <= max && strings.TrimSpace(value) == value
}

func validFieldName(name string) bool {
	if name == "" || len(name) > 128 || strings.TrimSpace(name) != name {
		return false
	}
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

func classification(value string) int {
	switch value {
	case "PUBLIC":
		return 1
	case "INTERNAL":
		return 2
	case "CONFIDENTIAL":
		return 3
	case "RESTRICTED":
		return 4
	default:
		return 0
	}
}
