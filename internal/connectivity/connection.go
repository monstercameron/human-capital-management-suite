package connectivity

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// LifecycleState is a connection's position in its governed lifecycle.
//
// The names follow planning/data/models/connectivity-access-content.md. The
// shorthand used in planning prose maps onto them directly: "diagnosed" is
// VALIDATING then READY, "enabled" is ACTIVE, and "retired" is REVOKED.
type LifecycleState string

// The lifecycle states.
const (
	// StateDraft is a configured but never validated connection. It can reach
	// nothing external.
	StateDraft LifecycleState = "DRAFT"
	// StateValidating is a connection undergoing credential, scope and
	// reachability diagnosis (INTG-003).
	StateValidating LifecycleState = "VALIDATING"
	// StateReady is a diagnosed connection that has not been enabled.
	StateReady LifecycleState = "READY"
	// StateActive is an enabled connection that observation runs may use.
	StateActive LifecycleState = "ACTIVE"
	// StateDegraded is an active connection whose health probes are failing.
	// It still serves reads; the caller decides whether to trust them.
	StateDegraded LifecycleState = "DEGRADED"
	// StateSuspended is an operator-paused connection.
	StateSuspended LifecycleState = "SUSPENDED"
	// StateQuarantined is a connection held out of use pending review, e.g.
	// after schema drift or an authority dispute.
	StateQuarantined LifecycleState = "QUARANTINED"
	// StateRevoked is terminal. A revoked connection is never usable again;
	// a replacement is a new connection with a new id.
	StateRevoked LifecycleState = "REVOKED"
)

// LifecycleStates returns every state in canonical order.
func LifecycleStates() []LifecycleState {
	return []LifecycleState{
		StateDraft, StateValidating, StateReady, StateActive,
		StateDegraded, StateSuspended, StateQuarantined, StateRevoked,
	}
}

// Valid reports whether s is a declared state.
func (s LifecycleState) Valid() bool {
	for _, known := range LifecycleStates() {
		if s == known {
			return true
		}
	}
	return false
}

// Terminal reports whether no transition leaves s.
func (s LifecycleState) Terminal() bool { return s == StateRevoked }

// Usable reports whether an observation run may read through a connection in
// this state. DEGRADED is usable on purpose: a degraded connection still
// returns real observations, and refusing to read would turn a health signal
// into an outage.
func (s LifecycleState) Usable() bool {
	return s == StateActive || s == StateDegraded
}

func (s LifecycleState) String() string { return string(s) }

// legalTransitions is the explicit legality table. It is a table rather than a
// switch so that it can be enumerated: a test walks every (from, to) pair and
// asserts the implementation agrees with exactly this.
//
// Two properties are deliberate. Resuming a suspended connection returns it to
// READY, not straight to ACTIVE, so that re-enabling is an explicit decision
// with its own evidence. And REVOKED has no outgoing edges at all: a revoked
// credential must never be resurrected by a state change.
var legalTransitions = map[LifecycleState][]LifecycleState{
	StateDraft:       {StateValidating, StateRevoked},
	StateValidating:  {StateDraft, StateReady, StateQuarantined, StateRevoked},
	StateReady:       {StateValidating, StateActive, StateSuspended, StateQuarantined, StateRevoked},
	StateActive:      {StateDegraded, StateSuspended, StateQuarantined, StateRevoked},
	StateDegraded:    {StateActive, StateSuspended, StateQuarantined, StateRevoked},
	StateSuspended:   {StateReady, StateQuarantined, StateRevoked},
	StateQuarantined: {StateValidating, StateRevoked},
	StateRevoked:     nil,
}

// LegalTransitions returns the states reachable from s, ascending.
func LegalTransitions(s LifecycleState) []LifecycleState {
	out := append([]LifecycleState(nil), legalTransitions[s]...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// IsLegalTransition reports whether from -> to is permitted. A self-transition
// is never legal: recording "it stayed ACTIVE" as a transition would put
// evidence in the history for something that did not happen.
func IsLegalTransition(from, to LifecycleState) bool {
	for _, allowed := range legalTransitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

// CredentialRef is an opaque reference to credential material held elsewhere.
//
// The material itself never enters this process. The type exists so that a
// caller cannot accidentally pass a token where a reference belongs: parsing
// rejects anything carrying inline secret material, and the zero value is not
// usable.
type CredentialRef struct {
	scheme string
	path   string
}

// Credential reference schemes this plane accepts.
var credentialSchemes = map[string]bool{
	"secretref": true,
	"vault":     true,
	"kms":       true,
}

// maxCredentialSegment bounds one path segment. Real secret-store paths are
// short names; an inline JWT, PEM body or base64 blob is not, so a long
// segment is rejected as probable secret material rather than a reference.
const maxCredentialSegment = 64

// ParseCredentialRef parses "scheme://path" into an opaque reference.
//
// It rejects, in order: an unknown scheme, an empty path, a userinfo section
// (which is how "https://user:password@host" smuggles a secret), a query or
// fragment (which is how "?token=..." does), any character outside a
// deliberately narrow set, and any path segment long enough to be the secret
// itself rather than a name for one.
func ParseCredentialRef(s string) (CredentialRef, error) {
	const op = "connectivity.ParseCredentialRef"
	scheme, rest, found := strings.Cut(s, "://")
	if !found {
		return CredentialRef{}, newError(op, ErrCredential,
			"credential reference must be scheme://path")
	}
	if !credentialSchemes[scheme] {
		return CredentialRef{}, newError(op, ErrCredential,
			"credential scheme %q is not an accepted secret-store scheme", scheme)
	}
	switch {
	case rest == "":
		return CredentialRef{}, newError(op, ErrCredential, "credential reference has no path")
	case strings.ContainsAny(rest, "@"):
		return CredentialRef{}, newError(op, ErrCredential,
			"credential reference carries a userinfo section; inline credentials are never accepted")
	case strings.ContainsAny(rest, "?#="):
		return CredentialRef{}, newError(op, ErrCredential,
			"credential reference carries query or fragment material; inline credentials are never accepted")
	}
	for _, segment := range strings.Split(rest, "/") {
		if segment == "" {
			return CredentialRef{}, newError(op, ErrCredential,
				"credential reference has an empty path segment")
		}
		if len(segment) > maxCredentialSegment {
			return CredentialRef{}, newError(op, ErrCredential,
				"credential reference segment is %d bytes; a reference names a secret, it does not carry one",
				len(segment))
		}
		// A secret-store path segment is a name. Two dots in one segment is
		// the shape of a JWT, and "eyJ" is the base64 of a JSON object's
		// opening brace, which is how every JWT and most opaque tokens start.
		if strings.Count(segment, ".") > 1 || strings.HasPrefix(segment, "eyJ") {
			return CredentialRef{}, newError(op, ErrCredential,
				"credential reference segment is token-shaped; a reference names a secret, it does not carry one")
		}
		for _, r := range segment {
			if !isCredentialPathRune(r) {
				return CredentialRef{}, newError(op, ErrCredential,
					"credential reference contains a character outside [A-Za-z0-9._-]")
			}
		}
	}
	return CredentialRef{scheme: scheme, path: rest}, nil
}

func isCredentialPathRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	case r == '.' || r == '_' || r == '-':
		return true
	default:
		return false
	}
}

// IsZero reports whether the reference is unset.
func (c CredentialRef) IsZero() bool { return c.scheme == "" }

// String returns the reference. It is safe to log: by construction it names a
// secret rather than containing one.
func (c CredentialRef) String() string {
	if c.IsZero() {
		return ""
	}
	return c.scheme + "://" + c.path
}

// Environment separates a connection's deployment context. A sandbox
// connection must never be readable as if it were production data.
type Environment string

// The environments.
const (
	EnvironmentSandbox    Environment = "SANDBOX"
	EnvironmentProduction Environment = "PRODUCTION"
)

// Valid reports whether e is a declared environment.
func (e Environment) Valid() bool {
	return e == EnvironmentSandbox || e == EnvironmentProduction
}

// EndpointPolicy bounds where a connection may talk to. It is part of the
// connection rather than deployment configuration because "this tenant's
// connector may only reach these hosts" is a tenant-visible governance fact.
type EndpointPolicy struct {
	// AllowedHosts is the exact set of hosts the connector may reach.
	AllowedHosts []string
	// RequireTLS states that plaintext transport is refused.
	RequireTLS bool
	// EgressProfile names the governed egress path, e.g. "cell-egress/us".
	EgressProfile string
}

// Validate reports whether the endpoint policy can bound an external call.
func (p EndpointPolicy) Validate() error {
	const op = "connectivity.EndpointPolicy.Validate"
	switch {
	case len(p.AllowedHosts) == 0:
		return newError(op, ErrInvalid, "endpoint policy allows no hosts")
	case !p.RequireTLS:
		return newError(op, ErrInvalid, "endpoint policy must require TLS")
	case strings.TrimSpace(p.EgressProfile) == "":
		return newError(op, ErrInvalid, "endpoint policy names no egress profile")
	}
	for _, h := range p.AllowedHosts {
		if strings.TrimSpace(h) == "" {
			return newError(op, ErrInvalid, "endpoint policy has an empty host")
		}
	}
	return nil
}

// TransitionEvidence is what justifies one lifecycle change. Every transition
// carries it: a lifecycle history without evidence is a list of assertions
// nobody can audit.
type TransitionEvidence struct {
	// Reason is the human-readable cause.
	Reason string
	// ActorRef identifies who or what decided, e.g. "user:ops@harborcare" or
	// "system:health-probe".
	ActorRef string
	// EvidenceRef points at the durable artifact: a diagnosis run, an approval
	// record, an incident.
	EvidenceRef string
	// OccurredAt is when the decision was made.
	OccurredAt time.Time
}

// Validate reports whether the evidence can justify a transition.
func (e TransitionEvidence) Validate() error {
	const op = "connectivity.TransitionEvidence.Validate"
	switch {
	case strings.TrimSpace(e.Reason) == "":
		return newError(op, ErrInvalid, "transition evidence has no reason")
	case strings.TrimSpace(e.ActorRef) == "":
		return newError(op, ErrInvalid, "transition evidence has no actor")
	case strings.TrimSpace(e.EvidenceRef) == "":
		return newError(op, ErrInvalid, "transition evidence has no evidence ref")
	case e.OccurredAt.IsZero():
		return newError(op, ErrInvalid, "transition evidence has no occurrence time")
	}
	return nil
}

// Transition is one recorded lifecycle change.
type Transition struct {
	Sequence uint64
	From     LifecycleState
	To       LifecycleState
	Evidence TransitionEvidence
}

// ConnectionSpec is everything a caller supplies to create a connection. It
// has no State field: a connection is always created in DRAFT, because a
// caller that could name its own starting state could skip diagnosis.
type ConnectionSpec struct {
	ConnectionID     string
	TenantID         string
	OrgID            string
	SystemID         string
	Environment      Environment
	Residency        string
	ConnectorID      string
	ConnectorVersion Version
	AuthMode         AuthMode
	CredentialRef    CredentialRef
	Scopes           []string
	EndpointPolicy   EndpointPolicy
	// Capabilities the connection claims. Must be a subset of the definition's.
	Capabilities []Capability
	// Bounds the connection honours. Must narrow the definition's.
	Bounds Bounds
	// CreatedAt is the drafting instant, supplied rather than read from the
	// wall clock so creation is a pure function of its inputs.
	CreatedAt time.Time
}

// ConnectorConnection is one tenant's bound instance of a definition version.
//
// It is a mutable aggregate guarded by its own mutex: lifecycle transitions
// arrive from operators, health probes and diagnosis runs concurrently, and
// the legality check and the state write must be one step.
type ConnectorConnection struct {
	mu sync.RWMutex

	spec       ConnectionSpec
	definition ConnectorDefinition
	state      LifecycleState
	version    uint64
	history    []Transition
}

// NewConnection drafts a connection against a published definition version.
//
// It refuses a connection that broadens the definition: an extra capability, a
// looser bound, an auth mode the definition does not offer. A connection is a
// narrowing of a published surface, never an extension of one.
func NewConnection(pub Publication, spec ConnectionSpec) (*ConnectorConnection, error) {
	const op = "connectivity.NewConnection"
	def := pub.Definition
	switch {
	case strings.TrimSpace(spec.ConnectionID) == "":
		return nil, newError(op, ErrInvalid, "connection has no id")
	case strings.TrimSpace(spec.TenantID) == "":
		return nil, newError(op, ErrInvalid, "connection has no tenant")
	case strings.TrimSpace(spec.OrgID) == "":
		return nil, newError(op, ErrInvalid, "connection has no organization")
	case strings.TrimSpace(spec.SystemID) == "":
		return nil, newError(op, ErrInvalid, "connection names no external system")
	case !spec.Environment.Valid():
		return nil, newError(op, ErrInvalid, "connection has no environment")
	case strings.TrimSpace(spec.Residency) == "":
		return nil, newError(op, ErrInvalid, "connection has no residency profile")
	case spec.CreatedAt.IsZero():
		return nil, newError(op, ErrInvalid, "connection has no creation time")
	case spec.ConnectorID != def.ConnectorID:
		return nil, newError(op, ErrInvalid,
			"connection names connector %q but was drafted against %q", spec.ConnectorID, def.ConnectorID)
	case spec.ConnectorVersion != def.Version:
		return nil, newError(op, ErrInvalid,
			"connection names version %s but was drafted against %s", spec.ConnectorVersion, def.Version)
	case spec.CredentialRef.IsZero():
		return nil, newError(op, ErrCredential, "connection has no credential reference")
	case len(spec.Scopes) == 0:
		return nil, newError(op, ErrInvalid, "connection declares no scopes")
	}
	if !supportsAuthMode(def, spec.AuthMode) {
		return nil, newError(op, ErrCapabilityBroadened,
			"connection requests auth mode %q which connector %s %s does not publish",
			string(spec.AuthMode), def.ConnectorID, def.Version)
	}
	if err := spec.EndpointPolicy.Validate(); err != nil {
		return nil, err
	}
	if len(spec.Capabilities) == 0 {
		return nil, newError(op, ErrInvalid, "connection claims no capabilities")
	}
	for _, c := range spec.Capabilities {
		if !def.Supports(c) {
			return nil, newError(op, ErrCapabilityBroadened,
				"connection claims capability %q which connector %s %s does not publish",
				c.String(), def.ConnectorID, def.Version)
		}
	}
	if err := spec.Bounds.Validate(); err != nil {
		return nil, err
	}
	if !spec.Bounds.Narrows(def.Bounds) {
		return nil, newError(op, ErrCapabilityBroadened,
			"connection bounds are looser than connector %s %s publishes", def.ConnectorID, def.Version)
	}

	normalized := spec
	normalized.Scopes = append([]string(nil), spec.Scopes...)
	sort.Strings(normalized.Scopes)
	normalized.Capabilities = append([]Capability(nil), spec.Capabilities...)
	normalized.CreatedAt = spec.CreatedAt.UTC()

	return &ConnectorConnection{
		spec:       normalized,
		definition: def,
		state:      StateDraft,
		version:    1,
	}, nil
}

func supportsAuthMode(def ConnectorDefinition, mode AuthMode) bool {
	for _, m := range def.AuthModes {
		if m == mode {
			return true
		}
	}
	return false
}

// ID returns the connection id.
func (c *ConnectorConnection) ID() string { return c.spec.ConnectionID }

// TenantID returns the owning tenant.
func (c *ConnectorConnection) TenantID() string { return c.spec.TenantID }

// OrgID returns the owning organization.
func (c *ConnectorConnection) OrgID() string { return c.spec.OrgID }

// SystemID returns the external system identity.
func (c *ConnectorConnection) SystemID() string { return c.spec.SystemID }

// ConnectorID returns the definition identity this connection is bound to.
func (c *ConnectorConnection) ConnectorID() string { return c.definition.ConnectorID }

// ConnectorVersion returns the pinned definition version.
func (c *ConnectorConnection) ConnectorVersion() Version { return c.definition.Version }

// Environment returns the deployment context.
func (c *ConnectorConnection) Environment() Environment { return c.spec.Environment }

// Residency returns the residency profile.
func (c *ConnectorConnection) Residency() string { return c.spec.Residency }

// AuthMode returns the connection's authentication mode.
func (c *ConnectorConnection) AuthMode() AuthMode { return c.spec.AuthMode }

// CredentialRef returns the opaque credential reference. There is no accessor
// for credential material because no credential material is held.
func (c *ConnectorConnection) CredentialRef() CredentialRef { return c.spec.CredentialRef }

// Scopes returns the granted scopes, ascending.
func (c *ConnectorConnection) Scopes() []string { return append([]string(nil), c.spec.Scopes...) }

// EndpointPolicy returns the egress bound.
func (c *ConnectorConnection) EndpointPolicy() EndpointPolicy { return c.spec.EndpointPolicy }

// Bounds returns the connection's read envelope.
func (c *ConnectorConnection) Bounds() Bounds { return c.spec.Bounds }

// Capabilities returns the capabilities this connection claims.
func (c *ConnectorConnection) Capabilities() []Capability {
	return append([]Capability(nil), c.spec.Capabilities...)
}

// Supports reports whether the connection claims the capability.
func (c *ConnectorConnection) Supports(want Capability) bool {
	for _, have := range c.spec.Capabilities {
		if have == want {
			return true
		}
	}
	return false
}

// State returns the current lifecycle state.
func (c *ConnectorConnection) State() LifecycleState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

// StateVersion returns the optimistic-concurrency version, incremented on
// every accepted transition.
func (c *ConnectorConnection) StateVersion() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.version
}

// History returns the recorded transitions in order.
func (c *ConnectorConnection) History() []Transition {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]Transition(nil), c.history...)
}

// Usable reports whether an observation run may read through this connection
// right now.
func (c *ConnectorConnection) Usable() bool { return c.State().Usable() }

// Transition moves the connection to a new state.
//
// It fails when the legality table forbids the edge, when the evidence is
// incomplete, or when the connection is already terminal. There is no forced
// variant: a lifecycle that can be overridden is not a lifecycle.
func (c *ConnectorConnection) Transition(to LifecycleState, ev TransitionEvidence) error {
	const op = "connectivity.ConnectorConnection.Transition"
	if !to.Valid() {
		return newError(op, ErrInvalid, "unknown lifecycle state %q", string(to))
	}
	if err := ev.Validate(); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state.Terminal() {
		return newError(op, ErrIllegalTransition,
			"connection %s is %s, which is terminal", c.spec.ConnectionID, c.state)
	}
	if !IsLegalTransition(c.state, to) {
		return newError(op, ErrIllegalTransition,
			"connection %s cannot move %s -> %s", c.spec.ConnectionID, c.state, to)
	}
	c.history = append(c.history, Transition{
		Sequence: uint64(len(c.history)) + 1,
		From:     c.state,
		To:       to,
		Evidence: TransitionEvidence{
			Reason:      ev.Reason,
			ActorRef:    ev.ActorRef,
			EvidenceRef: ev.EvidenceRef,
			OccurredAt:  ev.OccurredAt.UTC(),
		},
	})
	c.state = to
	c.version++
	return nil
}

// RequireUsable reports why the connection may not be read through, or nil.
func (c *ConnectorConnection) RequireUsable() error {
	const op = "connectivity.ConnectorConnection.RequireUsable"
	state := c.State()
	if state.Usable() {
		return nil
	}
	return newError(op, ErrPermission,
		"connection %s is %s; only ACTIVE or DEGRADED connections may be read", c.spec.ConnectionID, state)
}

// String renders the connection for logs. It names the credential reference,
// never any credential material, and there is no other rendering: a caller
// that formats a connection cannot accidentally reach past this.
func (c *ConnectorConnection) String() string {
	return "connection " + c.spec.ConnectionID +
		" tenant=" + c.spec.TenantID +
		" connector=" + c.definition.ConnectorID + "@" + c.definition.Version.String() +
		" env=" + string(c.spec.Environment) +
		" state=" + string(c.State()) +
		" credential=" + c.spec.CredentialRef.String()
}
