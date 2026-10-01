// Package schedule owns the pure publication contract for tenant-scoped
// trigger definitions. It validates source syntax and governance bindings,
// then produces immutable, content-addressed publication records. Dispatch,
// persistence adapters, and intent execution remain outside this package.
package schedule

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/cycle"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const (
	legacyIntentSchemaVersion = 1
	schemaVersion             = 2
)

// PublisherIntentTypeID and PublisherIntentVersion identify the intent whose
// governed operation publishes trigger definitions.
const PublisherIntentTypeID = "hcmnext.scheduling.publish_triggers"

const PublisherIntentVersion uint32 = 1

var PublisherIntentRef = intent.Ref{TypeID: PublisherIntentTypeID, Version: PublisherIntentVersion}

// Version reports this engine's canonical contract version.
func Version() int { return schemaVersion }

var (
	ErrRejected           = errors.New("schedule: trigger definition rejected")
	ErrIdentity           = errors.New("schedule: trigger identity is required")
	ErrTenant             = errors.New("schedule: tenant is required")
	ErrTarget             = errors.New("schedule: target intent is invalid")
	ErrInputTemplate      = errors.New("schedule: input template digest is required")
	ErrPurpose            = errors.New("schedule: purpose is required")
	ErrOwner              = errors.New("schedule: owner is required")
	ErrSource             = errors.New("schedule: exactly one valid trigger source is required")
	ErrCron               = errors.New("schedule: cron expression is invalid")
	ErrCalendar           = errors.New("schedule: calendar source is invalid")
	ErrEvent              = errors.New("schedule: event filter is invalid")
	ErrOverlap            = errors.New("schedule: overlap policy is required")
	ErrStorm              = errors.New("schedule: storm policy is required")
	ErrExecutionContract  = errors.New("schedule: execution mode contract is invalid")
	ErrUnauthorizedTarget = errors.New("schedule: target intent is not authorized")
	ErrPurposeMismatch    = errors.New("schedule: target does not declare the trigger purpose")
	ErrRecursion          = errors.New("schedule: recursive trigger is refused")
	ErrRevisionConflict   = errors.New("schedule: published revision is immutable")
	ErrNoStore            = errors.New("schedule: publication store is required")
	ErrActivation         = errors.New("schedule: activation is invalid")
	ErrUnknownRevision    = errors.New("schedule: trigger revision is unknown")
	ErrTargetKind         = errors.New("schedule: trigger target kind is invalid")
	ErrAgentRunTarget     = errors.New("schedule: agent-run target is invalid")
)

// Error is a typed refusal. Field names the contract field that caused it and
// Code is stable for transport and policy callers.
type Error struct {
	Code   string
	Field  string
	Detail string
	Cause  error
}

func (e *Error) Error() string {
	field := ""
	if e.Field != "" {
		field = " [" + e.Field + "]"
	}
	msg := fmt.Sprintf("schedule: %s%s", e.Code, field)
	if e.Detail != "" {
		msg += ": " + e.Detail
	}
	return msg
}

func (e *Error) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrRejected}
	}
	return []error{ErrRejected, e.Cause}
}

func refuse(code, field string, cause error, format string, args ...any) error {
	return &Error{Code: code, Field: field, Cause: cause, Detail: fmt.Sprintf(format, args...)}
}

// CodeOf returns the stable refusal code, or an empty string for another
// error type.
func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// FieldOf returns the offending field path, or an empty string for another
// error type.
func FieldOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Field
	}
	return ""
}

// SourceKind is the closed vocabulary of trigger source shapes.
type SourceKind string

const (
	SourceUnspecified SourceKind = ""
	SourceCron        SourceKind = "CRON"
	SourceCalendar    SourceKind = "CALENDAR"
	SourceEvent       SourceKind = "EVENT"
)

func (k SourceKind) Valid() bool {
	return k == SourceCron || k == SourceCalendar || k == SourceEvent
}

// OverlapPolicy controls what a scheduler does when an earlier firing is
// still outstanding.
type OverlapPolicy string

const (
	OverlapUnspecified OverlapPolicy = ""
	OverlapSkip        OverlapPolicy = "SKIP"
	OverlapQueue       OverlapPolicy = "QUEUE"
	OverlapRefuse      OverlapPolicy = "REFUSE"
)

func (p OverlapPolicy) Valid() bool {
	return p == OverlapSkip || p == OverlapQueue || p == OverlapRefuse
}

// StormPolicy bounds trigger firings in a deterministic window. A zero jitter
// is valid and means that no jitter is permitted; the policy itself is still
// mandatory because MaxFiringsPerWindow and Window must be declared.
type StormPolicy struct {
	MaxFiringsPerWindow uint32
	Window              time.Duration
	JitterBound         time.Duration
}

func (p StormPolicy) Validate() error {
	if p.MaxFiringsPerWindow == 0 {
		return refuse("MISSING_STORM_POLICY", "storm_policy.max_firings_per_window", ErrStorm,
			"maximum firings per window must be positive")
	}
	if p.Window <= 0 {
		return refuse("INVALID_STORM_POLICY", "storm_policy.window", ErrStorm,
			"window must be positive")
	}
	if p.JitterBound < 0 || p.JitterBound > p.Window {
		return refuse("INVALID_STORM_POLICY", "storm_policy.jitter_bound", ErrStorm,
			"jitter bound must be non-negative and no greater than the window")
	}
	return nil
}

// IntentRef identifies an exact versioned intent definition.
type IntentRef = intent.Ref

// TargetKind is the closed set of target contracts that a published trigger
// can name. The empty value remains the legacy INTENT spelling.
type TargetKind string

const (
	TargetIntent   TargetKind = "INTENT"
	TargetAgentRun TargetKind = "AGENT_RUN"
)

// AgentVersionRef pins one exact published manifest. A zero/empty version is
// never a request to resolve the latest agent.
type AgentVersionRef struct {
	ID      string
	Version string
	Digest  string
}

// AgentRunBudget contains strict positive upper bounds carried to admission.
type AgentRunBudget struct {
	MaxCostMicros   uint64
	MaxInputTokens  uint64
	MaxOutputTokens uint64
}

// AgentRunDestination pins the approved output audience and membership
// snapshot selected by the schedule publisher.
type AgentRunDestination struct {
	AudienceID         string
	AudienceSnapshotID string
	AudienceDigest     string
}

// AgentRunTarget is the immutable, authority-neutral portion of a scheduled
// agent request. Current grants and installation authority are rechecked when
// the occurrence is admitted; this value never contains a delegated token.
type AgentRunTarget struct {
	Agent       AgentVersionRef
	SponsorID   string
	Purpose     string
	Budget      AgentRunBudget
	Destination AgentRunDestination
}

func (t AgentRunTarget) validate() error {
	version, versionErr := strconv.ParseUint(t.Agent.Version, 10, 64)
	if strings.TrimSpace(t.Agent.ID) == "" || versionErr != nil || version == 0 || strconv.FormatUint(version, 10) != t.Agent.Version || !looksLikeDigest(t.Agent.Digest) {
		return refuse("INVALID_AGENT_VERSION", "target.agent", ErrAgentRunTarget,
			"agent id, exact version, and manifest digest are required")
	}
	if strings.TrimSpace(t.SponsorID) == "" {
		return refuse("MISSING_AGENT_SPONSOR", "target.sponsor_id", ErrAgentRunTarget, "sponsor id is required")
	}
	if strings.TrimSpace(t.Purpose) == "" {
		return refuse("MISSING_AGENT_PURPOSE", "target.purpose", ErrAgentRunTarget, "purpose is required")
	}
	if t.Budget.MaxCostMicros == 0 || t.Budget.MaxInputTokens == 0 || t.Budget.MaxOutputTokens == 0 {
		return refuse("INVALID_AGENT_BUDGET", "target.budget", ErrAgentRunTarget, "all budget ceilings must be positive")
	}
	if strings.TrimSpace(t.Destination.AudienceID) == "" || strings.TrimSpace(t.Destination.AudienceSnapshotID) == "" || !looksLikeDigest(t.Destination.AudienceDigest) {
		return refuse("INVALID_AGENT_DESTINATION", "target.destination", ErrAgentRunTarget,
			"audience id, membership snapshot, and digest are required")
	}
	return nil
}

func (d TriggerDefinition) targetKind() TargetKind {
	if d.TargetKind == "" {
		return TargetIntent
	}
	return d.TargetKind
}

// AuthorizedTarget is the caller-supplied target allowlist entry. The
// scheduler never discovers targets or purposes by name; both are supplied by
// the caller and checked against the definition being published.
type AuthorizedTarget struct {
	Ref          IntentRef
	Purposes     []string
	AllowedModes []intent.Mode
	// AgentRun is an exact target authorization for AGENT_RUN publications.
	// It does not authorize other versions, sponsors, purposes, budgets, or
	// destinations.
	AgentRun *AgentRunTarget
	// PublisherRef names the intent that owns trigger publication. It is an
	// authorization-context field, not part of target identity.
	PublisherRef IntentRef
}

// TargetAllowlist is a readable alias for the authorization input to Publish.
type TargetAllowlist = []AuthorizedTarget

// CronSource is a five-field cron source.
type CronSource struct {
	Expression string
}

// CalendarSource pins a tenant calendar revision and the cycle cutoff rule it
// is to resolve. The source is a reference only; resolving a future occurrence
// is a separate pure scheduler operation.
type CalendarSource struct {
	CalendarRef values.CalendarRef
	// TenantCalendarRef and CutoffRule are compatibility spellings for callers
	// using the model's longer entity names. When set, they must agree with the
	// shorter fields above.
	TenantCalendarRef values.CalendarRef
	Cutoff            cycle.CutoffRule
	CutoffRule        cycle.CutoffRule
}

// EventType is a closed event vocabulary. TRIGGER_FIRED is intentionally part
// of the vocabulary so recursion can be refused explicitly rather than by a
// string convention outside the package.
type EventType string

const (
	EventIntentCreated     EventType = "INTENT_CREATED"
	EventIntentCompleted   EventType = "INTENT_COMPLETED"
	EventIntentFailed      EventType = "INTENT_FAILED"
	EventIntentCancelled   EventType = "INTENT_CANCELLED"
	EventCalendarCutoff    EventType = "CALENDAR_CUTOFF_REACHED"
	EventDeadlineDue       EventType = "DEADLINE_DUE"
	EventThresholdBreached EventType = "THRESHOLD_BREACHED"
	EventHealthSignal      EventType = "HEALTH_SIGNAL"
	EventTriggerFired      EventType = "TRIGGER_FIRED"
	EventScheduleFired     EventType = EventTriggerFired
)

var eventVocabulary = map[EventType]struct{}{
	EventIntentCreated:     {},
	EventIntentCompleted:   {},
	EventIntentFailed:      {},
	EventIntentCancelled:   {},
	EventCalendarCutoff:    {},
	EventDeadlineDue:       {},
	EventThresholdBreached: {},
	EventHealthSignal:      {},
	EventTriggerFired:      {},
}

// EventFilter selects one event vocabulary member. Attributes are exact
// normalized key/value predicates; they are optional, but when present their
// map order is excluded from identity by sorted canonical encoding.
type EventFilter struct {
	EventType     EventType
	SchemaVersion string
	Attributes    map[string]string
}

// TriggerSource is the discriminated union of the three supported source
// shapes. Exactly the branch named by Kind may be populated.
type TriggerSource struct {
	Kind     SourceKind
	Cron     CronSource
	Calendar CalendarSource
	Event    EventFilter
}

// Source is a short alias used by callers that prefer the model's entity name.
type Source = TriggerSource

// TriggerDefinition is one immutable logical trigger revision. Version is a
// publication revision, while Target.Ref.Version and CalendarRef.Version pin
// the definitions the trigger consumes.
type TriggerDefinition struct {
	ID                   string
	Version              string
	TenantID             string
	Target               IntentRef
	TargetKind           TargetKind
	AgentRun             *AgentRunTarget
	InputTemplateDigest  string
	Purpose              string
	Owner                string
	Source               TriggerSource
	Overlap              OverlapPolicy
	OverlapPolicy        OverlapPolicy
	Storm                StormPolicy
	StormPolicy          StormPolicy
	ExecutionMode        intent.Mode
	ExecutionEnvironment intent.Environment
}

// Validate checks intrinsic trigger structure. Cross-definition authorization
// is checked by Publish because it requires the caller's allowlist.
func (d TriggerDefinition) Validate() error {
	if strings.TrimSpace(d.ID) == "" || strings.TrimSpace(d.Version) == "" {
		return refuse("INVALID_IDENTITY", "identity", ErrIdentity, "id and version are required")
	}
	if strings.TrimSpace(d.TenantID) == "" {
		return refuse("MISSING_TENANT", "tenant_id", ErrTenant, "tenant id is required")
	}
	switch d.targetKind() {
	case TargetIntent:
		if d.AgentRun != nil {
			return refuse("INVALID_TARGET_KIND", "target_kind", ErrTargetKind, "intent target cannot carry AGENT_RUN fields")
		}
		if err := d.Target.Validate(); err != nil {
			return refuse("INVALID_TARGET", "target", ErrTarget, "%v", err)
		}
	case TargetAgentRun:
		if d.Target != (IntentRef{}) || d.AgentRun == nil {
			return refuse("INVALID_AGENT_TARGET", "target", ErrAgentRunTarget, "AGENT_RUN requires only its explicit target payload")
		}
		if err := d.AgentRun.validate(); err != nil {
			return err
		}
		if d.AgentRun.Purpose != d.Purpose {
			return refuse("AGENT_PURPOSE_MISMATCH", "target.purpose", ErrAgentRunTarget, "agent-run purpose must equal trigger purpose")
		}
	default:
		return refuse("INVALID_TARGET_KIND", "target_kind", ErrTargetKind, "target kind %q is not supported", d.TargetKind)
	}
	if !looksLikeDigest(d.InputTemplateDigest) {
		return refuse("INVALID_INPUT_TEMPLATE", "input_template_digest", ErrInputTemplate,
			"input template digest must be a non-empty sha256 digest")
	}
	if strings.TrimSpace(d.Purpose) == "" {
		return refuse("MISSING_PURPOSE", "purpose", ErrPurpose, "purpose is required")
	}
	if strings.TrimSpace(d.Owner) == "" {
		return refuse("MISSING_OWNER", "owner", ErrOwner, "owner is required")
	}
	if err := d.Source.Validate(); err != nil {
		return err
	}
	overlap, err := d.overlapPolicy()
	if err != nil {
		return err
	}
	storm, err := d.stormPolicy()
	if err != nil {
		return err
	}
	if !overlap.Valid() {
		return refuse("MISSING_OVERLAP_POLICY", "overlap_policy", ErrOverlap,
			"overlap policy must be SKIP, QUEUE, or REFUSE")
	}
	if err := storm.Validate(); err != nil {
		return err
	}
	if !d.ExecutionMode.Valid() {
		return refuse("INVALID_EXECUTION_CONTRACT", "execution_mode", ErrExecutionContract,
			"execution mode is not declared")
	}
	if !d.ExecutionEnvironment.Valid() {
		return refuse("INVALID_EXECUTION_CONTRACT", "execution_environment", ErrExecutionContract,
			"execution environment is not declared")
	}
	if _, err := intent.ModeContractFor(d.ExecutionMode, d.ExecutionEnvironment); err != nil {
		return refuse("INVALID_EXECUTION_CONTRACT", "execution_mode", ErrExecutionContract, "%v", err)
	}
	return nil
}

func (d TriggerDefinition) overlapPolicy() (OverlapPolicy, error) {
	if d.Overlap != OverlapUnspecified && d.OverlapPolicy != OverlapUnspecified && d.Overlap != d.OverlapPolicy {
		return OverlapUnspecified, refuse("CONFLICTING_OVERLAP_POLICY", "overlap_policy", ErrOverlap, "Overlap and OverlapPolicy disagree")
	}
	if d.Overlap != OverlapUnspecified {
		return d.Overlap, nil
	}
	return d.OverlapPolicy, nil
}

func (d TriggerDefinition) stormPolicy() (StormPolicy, error) {
	if d.Storm != (StormPolicy{}) && d.StormPolicy != (StormPolicy{}) && d.Storm != d.StormPolicy {
		return StormPolicy{}, refuse("CONFLICTING_STORM_POLICY", "storm_policy", ErrStorm, "Storm and StormPolicy disagree")
	}
	if d.Storm != (StormPolicy{}) {
		return d.Storm, nil
	}
	return d.StormPolicy, nil
}

func (s TriggerSource) Validate() error {
	switch s.Kind {
	case SourceCron:
		if !isZeroCalendar(s.Calendar) || !isZeroEvent(s.Event) {
			return refuse("INVALID_SOURCE", "source", ErrSource, "CRON source carries another source branch")
		}
		if _, err := ParseCron(s.Cron.Expression); err != nil {
			return refuse("INVALID_CRON", "source.cron.expression", ErrCron, "%v", err)
		}
	case SourceCalendar:
		if strings.TrimSpace(s.Cron.Expression) != "" || !isZeroEvent(s.Event) {
			return refuse("INVALID_SOURCE", "source", ErrSource, "CALENDAR source carries another source branch")
		}
		calendarRef, cutoff, err := s.Calendar.normalized()
		if err != nil {
			return err
		}
		if err := calendarRef.Validate(); err != nil {
			return refuse("INVALID_CALENDAR_REF", "source.calendar.calendar_ref", ErrCalendar, "%v", err)
		}
		if err := cutoff.Validate(); err != nil {
			return refuse("INVALID_CUTOFF_RULE", "source.calendar.cutoff", ErrCalendar, "%v", err)
		}
	case SourceEvent:
		if strings.TrimSpace(s.Cron.Expression) != "" || !isZeroCalendar(s.Calendar) {
			return refuse("INVALID_SOURCE", "source", ErrSource, "EVENT source carries another source branch")
		}
		if _, ok := eventVocabulary[s.Event.EventType]; !ok {
			return refuse("UNKNOWN_EVENT", "source.event.event_type", ErrEvent,
				"event %q is outside the closed vocabulary", s.Event.EventType)
		}
		for key := range s.Event.Attributes {
			if strings.TrimSpace(key) == "" {
				return refuse("INVALID_EVENT_FILTER", "source.event.attributes", ErrEvent, "attribute key is empty")
			}
		}
	default:
		return refuse("MISSING_SOURCE", "source.kind", ErrSource, "source kind is CRON, CALENDAR, or EVENT")
	}
	return nil
}

func isZeroCalendar(c CalendarSource) bool {
	return c.CalendarRef == (values.CalendarRef{}) && c.TenantCalendarRef == (values.CalendarRef{}) && c.Cutoff == (cycle.CutoffRule{}) && c.CutoffRule == (cycle.CutoffRule{})
}
func isZeroEvent(e EventFilter) bool {
	return e.EventType == "" && e.SchemaVersion == "" && len(e.Attributes) == 0
}

func (c CalendarSource) normalized() (values.CalendarRef, cycle.CutoffRule, error) {
	if c.CalendarRef != (values.CalendarRef{}) && c.TenantCalendarRef != (values.CalendarRef{}) && c.CalendarRef != c.TenantCalendarRef {
		return values.CalendarRef{}, cycle.CutoffRule{}, refuse("CONFLICTING_CALENDAR_REF", "source.calendar.calendar_ref", ErrCalendar, "CalendarRef and TenantCalendarRef disagree")
	}
	ref := c.CalendarRef
	if ref == (values.CalendarRef{}) {
		ref = c.TenantCalendarRef
	}
	if c.Cutoff != (cycle.CutoffRule{}) && c.CutoffRule != (cycle.CutoffRule{}) && c.Cutoff != c.CutoffRule {
		return values.CalendarRef{}, cycle.CutoffRule{}, refuse("CONFLICTING_CUTOFF_RULE", "source.calendar.cutoff", ErrCalendar, "Cutoff and CutoffRule disagree")
	}
	rule := c.Cutoff
	if rule == (cycle.CutoffRule{}) {
		rule = c.CutoffRule
	}
	return ref, rule, nil
}

func looksLikeDigest(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) == len("sha256:")+64 && strings.HasPrefix(s, "sha256:") {
		for _, r := range s[len("sha256:"):] {
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
				return false
			}
		}
		return true
	}
	return false
}

// ValidateTargetAuthorization applies the caller's allowlist and the
// target-declared purpose/mode boundary.
func (d TriggerDefinition) ValidateTargetAuthorization(targets []AuthorizedTarget) error {
	if d.targetKind() == TargetAgentRun {
		if d.AgentRun == nil {
			return refuse("INVALID_AGENT_TARGET", "target", ErrAgentRunTarget, "AGENT_RUN payload is required")
		}
		for _, target := range targets {
			if target.AgentRun != nil && *target.AgentRun == *d.AgentRun {
				return nil
			}
		}
		return refuse("UNAUTHORIZED_AGENT_TARGET", "target", ErrUnauthorizedTarget,
			"exact agent version, sponsor, purpose, budget, and destination are absent from the caller's allowlist")
	}
	if d.targetKind() != TargetIntent {
		return refuse("INVALID_TARGET_KIND", "target_kind", ErrTargetKind, "target kind %q is not supported", d.TargetKind)
	}
	for _, target := range targets {
		if target.Ref != d.Target {
			continue
		}
		purpose := false
		for _, declared := range target.Purposes {
			if declared == d.Purpose {
				purpose = true
				break
			}
		}
		if !purpose {
			return refuse("PURPOSE_MISMATCH", "purpose", ErrPurposeMismatch,
				"target %s does not declare purpose %q", d.Target, d.Purpose)
		}
		if len(target.AllowedModes) > 0 {
			allowed := false
			for _, mode := range target.AllowedModes {
				if mode == d.ExecutionMode {
					allowed = true
					break
				}
			}
			if !allowed {
				return refuse("UNAUTHORIZED_TARGET_MODE", "execution_mode", ErrUnauthorizedTarget,
					"target %s does not allow execution mode %s", d.Target, d.ExecutionMode)
			}
		}
		return nil
	}
	return refuse("UNAUTHORIZED_TARGET", "target", ErrUnauthorizedTarget,
		"target %s is absent from the caller's allowlist", d.Target)
}

// ValidateRecursion refuses both direct publication recursion and event
// recursion. The publisher intent is an input to this cross-boundary check,
// never inferred from owner or target names.
func (d TriggerDefinition) ValidateRecursion(publisherIntent IntentRef) error {
	if d.targetKind() == TargetIntent && (d.Target == publisherIntent || d.Target == PublisherIntentRef) {
		return refuse("RECURSION_REFUSED", "target", ErrRecursion,
			"target %s is the intent that publishes triggers", d.Target)
	}
	if d.Source.Kind == SourceEvent && d.Source.Event.EventType == EventTriggerFired {
		return refuse("RECURSION_REFUSED", "source.event.event_type", ErrRecursion,
			"event filter matches trigger firing events")
	}
	return nil
}

// PublishedTrigger is a publication receipt. Definition and CanonicalBytes
// are defensively copied at publication and on every registry read. Digest is
// recomputable through Verify; callers cannot change the stored record by
// mutating their input definition or returned slices/maps.
type PublishedTrigger struct {
	Definition     TriggerDefinition
	CanonicalBytes []byte
	Digest         string
}

// Ref returns the exact published trigger revision.
func (p PublishedTrigger) Ref() TriggerRef {
	return TriggerRef{TenantID: p.Definition.TenantID, ID: p.Definition.ID, Version: p.Definition.Version}
}

// CanonicalDigest returns the publication digest.
func (p PublishedTrigger) CanonicalDigest() string { return p.Digest }

// Canonical returns a defensive copy of the bytes used to compute Digest.
func (p PublishedTrigger) Canonical() []byte { return append([]byte(nil), p.CanonicalBytes...) }

// Verify checks that the receipt's public content still matches its digest.
func (p PublishedTrigger) Verify() error {
	canonical, err := canonicalDefinition(p.Definition)
	if err != nil {
		return err
	}
	if canonicalbytes.Digest(canonical) != p.Digest || !equalBytes(canonical, p.CanonicalBytes) {
		return refuse("RECORD_MUTATED", "published_trigger", ErrRejected,
			"published trigger %s no longer matches its canonical digest", p.Ref())
	}
	return nil
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func cloneDefinition(d TriggerDefinition) TriggerDefinition {
	c := d
	c.Source.Event.Attributes = cloneStringMap(d.Source.Event.Attributes)
	if d.AgentRun != nil {
		target := *d.AgentRun
		c.AgentRun = &target
	}
	return c
}

func clonePublished(p PublishedTrigger) PublishedTrigger {
	p.Definition = cloneDefinition(p.Definition)
	p.CanonicalBytes = append([]byte(nil), p.CanonicalBytes...)
	return p
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func canonicalDefinition(d TriggerDefinition) ([]byte, error) {
	overlap, err := d.overlapPolicy()
	if err != nil {
		return nil, err
	}
	storm, err := d.stormPolicy()
	if err != nil {
		return nil, err
	}
	canonicalVersion := schemaVersion
	if d.targetKind() == TargetIntent {
		canonicalVersion = legacyIntentSchemaVersion
	}
	w := canonicalbytes.New("hcmnext.engines.schedule.TriggerDefinition", canonicalVersion).
		String("id", d.ID).
		String("version", d.Version).
		String("tenant_id", d.TenantID).
		String("target_type_id", d.Target.TypeID).
		Int("target_version", int64(d.Target.Version)).
		String("input_template_digest", d.InputTemplateDigest).
		String("purpose", d.Purpose).
		String("owner", d.Owner).
		String("source_kind", string(d.Source.Kind))
	w.String("overlap_policy", string(overlap)).
		Int("storm_max_firings_per_window", int64(storm.MaxFiringsPerWindow)).
		Int("storm_window_nanos", int64(storm.Window)).
		Int("storm_jitter_bound_nanos", int64(storm.JitterBound)).
		String("execution_mode", d.ExecutionMode.String()).
		String("execution_environment", d.ExecutionEnvironment.String())
	if d.targetKind() == TargetAgentRun {
		target := d.AgentRun
		if target == nil {
			return nil, refuse("INVALID_AGENT_TARGET", "target", ErrAgentRunTarget, "AGENT_RUN payload is required")
		}
		w.String("target_kind", string(TargetAgentRun)).
			String("agent_id", target.Agent.ID).
			String("agent_version", target.Agent.Version).
			String("agent_manifest_digest", target.Agent.Digest).
			String("agent_sponsor_id", target.SponsorID).
			String("agent_purpose", target.Purpose).
			String("agent_budget_max_cost_micros", strconv.FormatUint(target.Budget.MaxCostMicros, 10)).
			String("agent_budget_max_input_tokens", strconv.FormatUint(target.Budget.MaxInputTokens, 10)).
			String("agent_budget_max_output_tokens", strconv.FormatUint(target.Budget.MaxOutputTokens, 10)).
			String("agent_destination_audience_id", target.Destination.AudienceID).
			String("agent_destination_snapshot_id", target.Destination.AudienceSnapshotID).
			String("agent_destination_digest", target.Destination.AudienceDigest)
	}
	switch d.Source.Kind {
	case SourceCron:
		parsed, err := ParseCron(d.Source.Cron.Expression)
		if err != nil {
			return nil, err
		}
		w.String("cron_expression", parsed.String())
	case SourceCalendar:
		calendarRef, cutoff, err := d.Source.Calendar.normalized()
		if err != nil {
			return nil, err
		}
		w.String("calendar_ref", calendarRef.Ref).
			String("calendar_version", calendarRef.Version).
			String("cutoff_phase_id", cutoff.PhaseID).
			String("cutoff_nominal_date", cutoff.NominalDate.String()).
			String("cutoff_nominal_time", cutoff.NominalTime.String()).
			String("cutoff_jurisdiction", cutoff.JurisdictionRef).
			String("cutoff_adjustment", string(cutoff.Adjustment))
	case SourceEvent:
		w.String("event_type", string(d.Source.Event.EventType)).
			String("event_schema_version", d.Source.Event.SchemaVersion)
		keys := make([]string, 0, len(d.Source.Event.Attributes))
		for key := range d.Source.Event.Attributes {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		w.Count("event_attributes", len(keys))
		for _, key := range keys {
			w.String("event_attribute_key", key).String("event_attribute_value", d.Source.Event.Attributes[key])
		}
	}
	return w.Bytes()
}

// Digest returns the canonical digest of a valid definition without publishing
// it. It is useful for input pinning and golden fixtures.
func (d TriggerDefinition) Digest() string {
	if d.Validate() != nil {
		return ""
	}
	b, err := canonicalDefinition(d)
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}

// Explain is a deterministic audit explanation of the definition's bindings.
func (d TriggerDefinition) Explain() string {
	return fmt.Sprintf("trigger %s/%s for tenant %q targets %s for purpose %q from %s; overlap=%s storm=%d/%s jitter<=%s; runs under %s/%s; canonical=%s",
		d.ID, d.Version, d.TenantID, d.targetDescription(), d.Purpose, d.Source.Kind,
		d.Overlap, d.Storm.MaxFiringsPerWindow, d.Storm.Window, d.Storm.JitterBound,
		d.ExecutionMode, d.ExecutionEnvironment, d.Digest())
}

func (d TriggerDefinition) targetDescription() string {
	if d.targetKind() == TargetAgentRun && d.AgentRun != nil {
		return fmt.Sprintf("AGENT_RUN %s@%s", d.AgentRun.Agent.ID, d.AgentRun.Agent.Version)
	}
	return d.Target.String()
}

// Explain returns the immutable receipt's explanation.
func (p PublishedTrigger) Explain() string { return p.Definition.Explain() + "; published=" + p.Digest }

// TriggerRef identifies one tenant trigger publication.
type TriggerRef struct {
	TenantID string
	ID       string
	Version  string
}

func (r TriggerRef) String() string { return r.TenantID + "/" + r.ID + "/" + r.Version }

// Store is the publication persistence port. The package ships Registry as a
// concurrency-safe in-memory adapter; durable adapters can implement this port
// without importing data packages into the engine.
type Store interface {
	Put(PublishedTrigger) error
	Get(TriggerRef) (PublishedTrigger, bool, error)
}

// Registry is an in-memory immutable publication store. Publish is atomic for
// a revision key: identical concurrent calls return the same bytes, while a
// changed body under an existing revision is refused.
type Registry struct {
	mu      sync.RWMutex
	records map[TriggerRef]PublishedTrigger
	active  map[string]TriggerRef
	history map[string][]ActivationRecord
}

var _ Store = (*Registry)(nil)

// NewRegistry returns an empty publication registry.
func NewRegistry() *Registry {
	return &Registry{records: make(map[TriggerRef]PublishedTrigger), active: make(map[string]TriggerRef), history: make(map[string][]ActivationRecord)}
}

func (r *Registry) Put(p PublishedTrigger) error {
	if r == nil {
		return ErrNoStore
	}
	ref := p.Ref()
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.records[ref]; ok {
		if existing.Digest != p.Digest || !equalBytes(existing.CanonicalBytes, p.CanonicalBytes) {
			return refuse("REVISION_CONFLICT", "version", ErrRevisionConflict,
				"revision %s is already published with different content", ref)
		}
		return nil
	}
	r.records[ref] = clonePublished(p)
	return nil
}

func (r *Registry) Get(ref TriggerRef) (PublishedTrigger, bool, error) {
	if r == nil {
		return PublishedTrigger{}, false, ErrNoStore
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.records[ref]
	if !ok {
		return PublishedTrigger{}, false, nil
	}
	return clonePublished(p), true, nil
}

func publishValidated(def TriggerDefinition, targets []AuthorizedTarget) (PublishedTrigger, error) {
	if err := def.Validate(); err != nil {
		return PublishedTrigger{}, err
	}
	if err := def.ValidateTargetAuthorization(targets); err != nil {
		return PublishedTrigger{}, err
	}
	// The publisher intent is a caller-supplied authorization boundary when the
	// caller has one; otherwise the package's published publisher identity is
	// used.
	publisher := publisherRef(targets)
	if err := def.ValidateRecursion(publisher); err != nil {
		return PublishedTrigger{}, err
	}
	canonDef := cloneDefinition(def)
	if canonDef.Source.Kind == SourceCron {
		parsed, _ := ParseCron(canonDef.Source.Cron.Expression)
		canonDef.Source.Cron.Expression = parsed.String()
	}
	canonical, err := canonicalDefinition(canonDef)
	if err != nil {
		return PublishedTrigger{}, err
	}
	return PublishedTrigger{Definition: canonDef, CanonicalBytes: canonical, Digest: canonicalbytes.Digest(canonical)}, nil
}

// publisherRef extracts the declared trigger-publishing intent from the
// allowlist boundary. The first entry's optional PublisherRef is used when
// supplied; see AuthorizedTarget.PublisherRef's compatibility field below.
func publisherRef(targets []AuthorizedTarget) IntentRef {
	for _, target := range targets {
		if target.PublisherRef != (IntentRef{}) {
			return target.PublisherRef
		}
	}
	return PublisherIntentRef
}

// Publish validates and records one immutable trigger revision. The caller's
// allowlist is also the explicit recursion boundary: set PublisherRef on one
// entry to the intent that is publishing trigger definitions.
func Publish(store Store, def TriggerDefinition, targets []AuthorizedTarget) (PublishedTrigger, error) {
	if store == nil {
		return PublishedTrigger{}, ErrNoStore
	}
	if r, ok := store.(*Registry); ok {
		return r.Publish(def, targets)
	}
	p, err := publishValidated(def, targets)
	if err != nil {
		return PublishedTrigger{}, err
	}
	if existing, found, err := store.Get(p.Ref()); err != nil {
		return PublishedTrigger{}, err
	} else if found {
		if existing.Digest == p.Digest && equalBytes(existing.CanonicalBytes, p.CanonicalBytes) {
			return existing, nil
		}
		return PublishedTrigger{}, refuse("REVISION_CONFLICT", "version", ErrRevisionConflict,
			"revision %s is already published with different content", p.Ref())
	}
	if err := store.Put(p); err != nil {
		return PublishedTrigger{}, err
	}
	return clonePublished(p), nil
}

// Publish is the method-shaped form used by callers that own a Registry.
func (r *Registry) Publish(def TriggerDefinition, targets []AuthorizedTarget) (PublishedTrigger, error) {
	if r == nil {
		return PublishedTrigger{}, ErrNoStore
	}
	p, err := publishValidated(def, targets)
	if err != nil {
		return PublishedTrigger{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, found := r.records[p.Ref()]; found {
		if existing.Digest == p.Digest && equalBytes(existing.CanonicalBytes, p.CanonicalBytes) {
			return clonePublished(existing), nil
		}
		return PublishedTrigger{}, refuse("REVISION_CONFLICT", "version", ErrRevisionConflict,
			"revision %s is already published with different content", p.Ref())
	}
	r.records[p.Ref()] = clonePublished(p)
	return clonePublished(p), nil
}

// ActivationEvidence is supplied by the separate governed activation step.
type ActivationEvidence struct {
	ActivatedBy string
	Authority   string
	Reason      string
	ActivatedAt time.Time
}

// ActivationRecord is append-only evidence that a published revision became
// active. Publishing never silently activates a revision.
type ActivationRecord struct {
	Ref         TriggerRef
	ActivatedBy string
	Authority   string
	Reason      string
	ActivatedAt time.Time
}

// Activate records a previously published revision as active.
func (r *Registry) Activate(ref TriggerRef, evidence ActivationEvidence) (ActivationRecord, error) {
	if r == nil {
		return ActivationRecord{}, ErrNoStore
	}
	if strings.TrimSpace(evidence.ActivatedBy) == "" {
		return ActivationRecord{}, refuse("MISSING_ACTIVATOR", "activated_by", ErrActivation, "activating principal is required")
	}
	if evidence.ActivatedAt.IsZero() {
		return ActivationRecord{}, refuse("MISSING_ACTIVATION_TIME", "activated_at", ErrActivation, "activation time is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.records[ref]; !ok {
		return ActivationRecord{}, refuse("UNKNOWN_REVISION", "version", ErrUnknownRevision, "revision %s is not published", ref)
	}
	rec := ActivationRecord{Ref: ref, ActivatedBy: evidence.ActivatedBy, Authority: evidence.Authority, Reason: evidence.Reason, ActivatedAt: evidence.ActivatedAt}
	r.history[ref.TenantID+"\x00"+ref.ID] = append(r.history[ref.TenantID+"\x00"+ref.ID], rec)
	r.active[ref.TenantID+"\x00"+ref.ID] = ref
	return rec, nil
}

// Active returns the most recently activated revision for a trigger id.
func (r *Registry) Active(tenantID, id string) (PublishedTrigger, bool) {
	if r == nil {
		return PublishedTrigger{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	ref, ok := r.active[tenantID+"\x00"+id]
	if !ok {
		return PublishedTrigger{}, false
	}
	p, ok := r.records[ref]
	return clonePublished(p), ok
}

// ListActivations returns append-only activation evidence oldest first.
func (r *Registry) ListActivations(tenantID, id string) []ActivationRecord {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	h := r.history[tenantID+"\x00"+id]
	return append([]ActivationRecord(nil), h...)
}

// ParseCron parses the supported five-field cron grammar. Supported terms are
// *, integers, integer ranges, optional /positive-step suffixes, and comma
// lists. Names, macros, seconds, year fields, L/W/#/? and other extensions are
// deliberately rejected.
type CronExpression struct {
	Expression string
	fields     [5]cronField
}

type cronField struct {
	min, max int
	terms    []cronTerm
}

type cronTerm struct {
	start, end, step int
	star             bool
}

func (c CronExpression) String() string { return c.Expression }

// ParseCron validates and returns a canonical-whitespace five-field cron
// expression.
func ParseCron(expression string) (CronExpression, error) {
	parts := strings.Fields(expression)
	if len(parts) != 5 {
		return CronExpression{}, fmt.Errorf("want exactly five fields")
	}
	ranges := [5][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 7}}
	var out CronExpression
	for i, part := range parts {
		field, err := parseCronField(part, ranges[i][0], ranges[i][1])
		if err != nil {
			return CronExpression{}, fmt.Errorf("field %d: %w", i+1, err)
		}
		out.fields[i] = field
	}
	out.Expression = strings.Join(parts, " ")
	return out, nil
}

// ValidateCron is the error-only convenience form of ParseCron.
func ValidateCron(expression string) error {
	_, err := ParseCron(expression)
	return err
}

func parseCronField(text string, min, max int) (cronField, error) {
	if text == "" {
		return cronField{}, errors.New("empty field")
	}
	var field cronField
	field.min, field.max = min, max
	for _, listTerm := range strings.Split(text, ",") {
		if listTerm == "" {
			return cronField{}, errors.New("empty list term")
		}
		base, stepText, hasStep := strings.Cut(listTerm, "/")
		if hasStep {
			if stepText == "" || strings.Contains(stepText, "/") {
				return cronField{}, errors.New("step must be one positive integer")
			}
		}
		step := 1
		if hasStep {
			var err error
			step, err = parseCronInteger(stepText)
			if err != nil || step <= 0 {
				return cronField{}, errors.New("step must be positive")
			}
		}
		switch {
		case base == "*":
			field.terms = append(field.terms, cronTerm{start: min, end: max, step: step, star: true})
		case strings.Contains(base, "-"):
			loText, hiText, ok := strings.Cut(base, "-")
			if !ok || loText == "" || hiText == "" || strings.Contains(hiText, "-") {
				return cronField{}, errors.New("range must be low-high")
			}
			lo, errLo := parseCronInteger(loText)
			hi, errHi := parseCronInteger(hiText)
			if errLo != nil || errHi != nil || lo < min || hi > max || lo > hi {
				return cronField{}, fmt.Errorf("range must be within %d-%d", min, max)
			}
			field.terms = append(field.terms, cronTerm{start: lo, end: hi, step: step})
		default:
			value, err := parseCronInteger(base)
			if err != nil || value < min || value > max {
				return cronField{}, fmt.Errorf("value must be within %d-%d", min, max)
			}
			field.terms = append(field.terms, cronTerm{start: value, end: value, step: step})
		}
	}
	return field, nil
}

func parseCronInteger(s string) (int, error) {
	if s == "" || (len(s) > 1 && s[0] == '0') {
		return 0, errors.New("integer is not canonical")
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errors.New("integer contains a non-digit")
		}
	}
	return strconv.Atoi(s)
}

// Explain describes the canonicalization and safety boundary of this engine.
func Explain() string {
	return fmt.Sprintf("schedule v%d: immutable tenant-scoped CRON/CALENDAR/EVENT definitions; target, input template, purpose and source revisions are canonicalbytes-pinned; overlap and storm policies are mandatory; publication is allowlist- and recursion-checked; activation is separate", schemaVersion)
}
