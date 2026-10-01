// Package handle owns the tenant-local namespace used by persona chat
// identities.  It deliberately stores the existing chatapps.Agent value
// rather than inventing another agent identity record.
package handle

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatapps"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

var (
	ErrInvalid           = errors.New("agentpersona handle: invalid identity")
	ErrConflict          = errors.New("agentpersona handle: namespace collision")
	ErrAlreadyRegistered = errors.New("agentpersona handle: already registered")
	ErrNotFound          = errors.New("agentpersona handle: not found")
	ErrRetired           = errors.New("agentpersona handle: persona retired")
)

// Status is the registration state of a persona identity. Retired identities
// remain in the namespace forever and can never be registered again.
type Status string

const (
	Active  Status = "ACTIVE"
	Retired Status = "RETIRED"
)

// Surface identifies a chat projection that must carry the same agent badge.
type Surface string

const (
	Post         Surface = "post"
	MentionChip  Surface = "mention_chip"
	Notification Surface = "notification"
	SearchResult Surface = "search_result"
)

// PersonRegistration reserves a human handle and display name in the same
// namespace as personas. A later rename is checked atomically by RegisterPerson.
type PersonRegistration struct {
	Tenant      string
	PersonID    string
	Handle      string
	DisplayName string
}

// PersonaRegistration binds a persona profile to the visible agent identity
// created by CHAT-043. Agent is a chatapps.Agent, not a human membership.
type PersonaRegistration struct {
	Tenant      string
	PersonaID   string
	Handle      string
	DisplayName string
	Agent       chatapps.Agent
}

// AgentBadge is immutable identity data for a persona projection. The
// invoker-specific ActingFor value is generated for each projection, while
// the agent and persona identifiers remain stable across surfaces.
type AgentBadge struct {
	IsAgent   bool   `json:"is_agent"`
	PersonaID string `json:"persona_id"`
	AgentID   string `json:"agent_id"`
	Label     string `json:"label"`
	ActingFor string `json:"acting_for"`
}

// SurfaceProjection is the minimum identity envelope consumed by posts,
// mention chips, notifications and search results.
type SurfaceProjection struct {
	Surface  Surface         `json:"surface"`
	Identity PersonaIdentity `json:"identity"`
	Badge    AgentBadge      `json:"badge"`
}

// PersonaIdentity is the durable chat identity projection. The Agent field is
// the CHAT-043 identity record, so this package cannot accidentally create a
// second membership-like identity table.
type PersonaIdentity struct {
	Tenant      string         `json:"tenant"`
	PersonaID   string         `json:"persona_id"`
	Handle      string         `json:"handle"`
	DisplayName string         `json:"display_name"`
	Agent       chatapps.Agent `json:"agent"`
	Status      Status         `json:"status"`
}

// Collision explains which existing namespace claim blocked a registration.
// It unwraps to ErrConflict so callers can use errors.Is.
type Collision struct {
	Tenant        string
	Candidate     string
	ExistingKind  string
	ExistingID    string
	ExistingValue string
}

func (e *Collision) Error() string {
	return fmt.Sprintf("%v: %q conflicts with %s %q (%s)", ErrConflict, e.Candidate, e.ExistingKind, e.ExistingValue, e.ExistingID)
}

func (e *Collision) Unwrap() error { return ErrConflict }

type claim struct {
	kind  string
	id    string
	raw   string
	owner string
}

type Registry struct {
	mu       sync.RWMutex
	claims   map[string]map[string]claim
	people   map[string]map[string]PersonRegistration
	personas map[string]map[string]PersonaIdentity
}

// NewRegistry constructs an empty, concurrency-safe namespace registry.
func NewRegistry() *Registry {
	return &Registry{
		claims:   make(map[string]map[string]claim),
		people:   make(map[string]map[string]PersonRegistration),
		personas: make(map[string]map[string]PersonaIdentity),
	}
}

// CanonicalHandle returns the case-folded handle used for storage and display
// links. An optional leading @ is presentation syntax and is removed.
func CanonicalHandle(raw string) (string, error) {
	if !utf8.ValidString(raw) {
		return "", ErrInvalid
	}
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "@")
	if s == "" || len([]rune(s)) > 64 {
		return "", ErrInvalid
	}
	for i, r := range []rune(s) {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '.') || (i == 0 && !unicode.IsLetter(r) && !unicode.IsDigit(r)) {
			return "", ErrInvalid
		}
	}
	runes := []rune(s)
	last := runes[len(runes)-1]
	if !unicode.IsLetter(last) && !unicode.IsDigit(last) {
		return "", ErrInvalid
	}
	return cases.Fold().String(norm.NFKC.String(s)), nil
}

// ConfusableSkeleton produces a conservative, case-folded skeleton. It uses
// NFKD to collapse compatibility forms and accents, removes separators, and
// maps common Latin/Cyrillic/Greek look-alikes. This is intentionally stricter
// than visual font matching: false positives are safer than impersonation.
func ConfusableSkeleton(raw string) (string, error) {
	if !utf8.ValidString(raw) {
		return "", ErrInvalid
	}
	s := cases.Fold().String(norm.NFKD.String(strings.TrimSpace(raw)))
	var b strings.Builder
	for _, r := range s {
		if unicode.Is(unicode.Mn, r) || unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		if mapped, ok := confusableRune[r]; ok {
			r = mapped
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "", ErrInvalid
	}
	return b.String(), nil
}

// RegisterPerson puts a human handle and display name into the shared
// namespace. Re-registering the same person is a rename; all checks happen
// before the old claims are replaced.
func (r *Registry) RegisterPerson(in PersonRegistration) error {
	if r == nil || strings.TrimSpace(in.Tenant) == "" || strings.TrimSpace(in.PersonID) == "" {
		return ErrInvalid
	}
	tenant := strings.TrimSpace(in.Tenant)
	handle, err := CanonicalHandle(in.Handle)
	if err != nil {
		return err
	}
	claims, err := candidateClaims(handle, in.DisplayName, "person", in.PersonID)
	if err != nil {
		return err
	}
	owner := "person:" + in.PersonID
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkClaims(tenant, claims, owner); err != nil {
		return err
	}
	if _, ok := r.people[tenant][in.PersonID]; ok {
		r.removeOwner(tenant, owner)
	}
	r.putClaims(tenant, claims, owner)
	if r.people[tenant] == nil {
		r.people[tenant] = make(map[string]PersonRegistration)
	}
	in.Handle = handle
	in.Tenant = tenant
	r.people[tenant][in.PersonID] = in
	return nil
}

// RegisterPersona reserves a handle permanently and binds it to the existing
// CHAT-043 agent identity. A retired or duplicate persona is never replaced.
func (r *Registry) RegisterPersona(in PersonaRegistration) (PersonaIdentity, error) {
	if r == nil || strings.TrimSpace(in.Tenant) == "" || strings.TrimSpace(in.PersonaID) == "" ||
		strings.TrimSpace(in.Agent.ID) == "" || strings.TrimSpace(in.Agent.InstallationID) == "" || in.Agent.Status != chatapps.Active {
		return PersonaIdentity{}, ErrInvalid
	}
	tenant := strings.TrimSpace(in.Tenant)
	handle, err := CanonicalHandle(in.Handle)
	if err != nil {
		return PersonaIdentity{}, err
	}
	// CHAT-043 supplies the agent's visible display name. Reserve it as well as
	// the persona-profile name because either may be rendered by a chat surface.
	claims, err := candidateClaimsWithNames(handle, []string{in.DisplayName, in.Agent.DisplayName}, "persona", in.PersonaID)
	if err != nil {
		return PersonaIdentity{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.personas[tenant] != nil {
		if _, ok := r.personas[tenant][in.PersonaID]; ok {
			return PersonaIdentity{}, ErrAlreadyRegistered
		}
	}
	owner := "persona:" + in.PersonaID
	if err := r.checkClaims(tenant, claims, owner); err != nil {
		return PersonaIdentity{}, err
	}
	if r.personas[tenant] == nil {
		r.personas[tenant] = make(map[string]PersonaIdentity)
	}
	in.Tenant = tenant
	in.Handle = handle
	identity := PersonaIdentity{Tenant: in.Tenant, PersonaID: in.PersonaID, Handle: handle, DisplayName: strings.TrimSpace(in.DisplayName), Agent: cloneAgent(in.Agent), Status: Active}
	r.putClaims(tenant, claims, owner)
	r.personas[tenant][in.PersonaID] = identity
	return cloneIdentity(identity), nil
}

// RetirePersona keeps all of the persona's claims as tombstones, preventing a
// different persona (or a human) from reusing a retired identity.
func (r *Registry) RetirePersona(tenant, personaID string) error {
	if r == nil || strings.TrimSpace(tenant) == "" || strings.TrimSpace(personaID) == "" {
		return ErrInvalid
	}
	tenant = strings.TrimSpace(tenant)
	r.mu.Lock()
	defer r.mu.Unlock()
	identity, ok := r.personas[tenant][personaID]
	if !ok {
		return ErrNotFound
	}
	if identity.Status == Retired {
		return ErrRetired
	}
	identity.Status = Retired
	r.personas[tenant][personaID] = identity
	return nil
}

// Persona returns a copy of the registered identity, including its status.
func (r *Registry) Persona(tenant, personaID string) (PersonaIdentity, error) {
	if r == nil {
		return PersonaIdentity{}, ErrInvalid
	}
	tenant = strings.TrimSpace(tenant)
	r.mu.RLock()
	defer r.mu.RUnlock()
	identity, ok := r.personas[tenant][personaID]
	if !ok {
		return PersonaIdentity{}, ErrNotFound
	}
	return cloneIdentity(identity), nil
}

// CheckPerson reports whether a proposed human rename would fit the shared
// namespace without changing it.
func (r *Registry) CheckPerson(in PersonRegistration) error {
	if r == nil || strings.TrimSpace(in.Tenant) == "" || strings.TrimSpace(in.PersonID) == "" {
		return ErrInvalid
	}
	tenant := strings.TrimSpace(in.Tenant)
	handle, err := CanonicalHandle(in.Handle)
	if err != nil {
		return err
	}
	claims, err := candidateClaims(handle, in.DisplayName, "person", in.PersonID)
	if err != nil {
		return err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.checkClaims(tenant, claims, "person:"+in.PersonID)
}

// Project makes the agent badge mandatory for every supported chat surface.
func (p PersonaIdentity) Project(surface Surface, invoker string) (SurfaceProjection, error) {
	if surface != Post && surface != MentionChip && surface != Notification && surface != SearchResult {
		return SurfaceProjection{}, ErrInvalid
	}
	badge, err := p.BadgeFor(invoker)
	if err != nil {
		return SurfaceProjection{}, err
	}
	return SurfaceProjection{Surface: surface, Identity: cloneIdentity(p), Badge: badge}, nil
}

// BadgeFor returns the permanent agent marker and the current invoker label.
func (p PersonaIdentity) BadgeFor(invoker string) (AgentBadge, error) {
	if p.Status != Active || strings.TrimSpace(p.PersonaID) == "" || strings.TrimSpace(p.Agent.ID) == "" {
		if p.Status == Retired {
			return AgentBadge{}, ErrRetired
		}
		return AgentBadge{}, ErrInvalid
	}
	canonical, err := CanonicalHandle(invoker)
	if err != nil {
		return AgentBadge{}, err
	}
	return AgentBadge{IsAgent: true, PersonaID: p.PersonaID, AgentID: p.Agent.ID, Label: "Agent", ActingFor: "acting for @" + canonical}, nil
}

func candidateClaims(handle, display, kind, id string) (map[string]claim, error) {
	return candidateClaimsWithNames(handle, []string{display}, kind, id)
}

func candidateClaimsWithNames(handle string, displays []string, kind, id string) (map[string]claim, error) {
	values := append([]string{handle}, displays...)
	out := make(map[string]claim, len(values))
	for _, raw := range values {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return nil, ErrInvalid
		}
		skeleton, err := ConfusableSkeleton(raw)
		if err != nil {
			return nil, err
		}
		if _, ok := out[skeleton]; ok {
			continue
		}
		claimKind := "display_name"
		if raw == handle {
			claimKind = "handle"
		}
		out[skeleton] = claim{kind: claimKind, id: id, raw: raw}
	}
	return out, nil
}

func (r *Registry) checkClaims(tenant string, candidates map[string]claim, owner string) error {
	for skeleton, candidate := range candidates {
		if existing, ok := r.claims[tenant][skeleton]; ok && existing.owner != owner {
			return &Collision{Tenant: tenant, Candidate: candidate.raw, ExistingKind: existing.kind, ExistingID: existing.id, ExistingValue: existing.raw}
		}
	}
	return nil
}

func (r *Registry) putClaims(tenant string, candidates map[string]claim, owner string) {
	if r.claims[tenant] == nil {
		r.claims[tenant] = make(map[string]claim)
	}
	for skeleton, candidate := range candidates {
		candidate.owner = owner
		r.claims[tenant][skeleton] = candidate
	}
}

func (r *Registry) removeOwner(tenant, owner string) {
	for skeleton, existing := range r.claims[tenant] {
		if existing.owner == owner {
			delete(r.claims[tenant], skeleton)
		}
	}
}

func cloneAgent(agent chatapps.Agent) chatapps.Agent {
	agent.Capabilities = append([]string(nil), agent.Capabilities...)
	return agent
}

func cloneIdentity(identity PersonaIdentity) PersonaIdentity {
	identity.Agent = cloneAgent(identity.Agent)
	return identity
}

// Common cross-script confusables used in handles and display names. The
// corpus is intentionally explicit and deterministic so it can be extended
// without changing normalization semantics.
var confusableRune = map[rune]rune{
	'а': 'a', 'А': 'a', 'ɑ': 'a', 'α': 'a', 'Α': 'a',
	'е': 'e', 'Е': 'e', 'ε': 'e', 'Ε': 'e',
	'і': 'i', 'І': 'i', 'ι': 'i', 'Ι': 'i', 'ӏ': 'i',
	'о': 'o', 'О': 'o', 'ο': 'o', 'Ο': 'o', 'օ': 'o',
	'р': 'p', 'Р': 'p', 'ρ': 'p', 'Ρ': 'p',
	'с': 'c', 'С': 'c', 'ϲ': 'c',
	'х': 'x', 'Х': 'x', 'χ': 'x', 'Χ': 'x',
	'у': 'y', 'У': 'y', 'υ': 'y', 'Υ': 'y',
	'к': 'k', 'К': 'k', 'κ': 'k', 'Κ': 'k',
	'м': 'm', 'М': 'm', 'μ': 'm', 'Μ': 'm',
	'т': 't', 'Т': 't', 'τ': 't', 'Τ': 't',
	'н': 'h', 'Н': 'h', 'ν': 'n', 'Ν': 'n',
	'в': 'b', 'В': 'b', 'β': 'b', 'Β': 'b',
	'ѕ': 's', 'Ѕ': 's', 'ζ': 'z', 'Ζ': 'z',
	'ј': 'j', 'Ј': 'j', 'գ': 'g',
	'0': 'o', '1': 'l', '3': 'e', '4': 'a', '5': 's', '6': 'g', '7': 't', '8': 'b', '9': 'g',
}
