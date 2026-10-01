// Package bilateral evaluates the exact cross-company agreement required to
// install an agent and to deliver its output to a shared conversation.
// Callers supply current membership and grant snapshots; this package does
// not persist policy or infer consent from chat membership.
package bilateral

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	// ErrInvalid marks malformed terms or an incomplete request.
	ErrInvalid = errors.New("agent bilateral: invalid request")
	// ErrDenied marks an absent, stale, asymmetric, revoked, or expanded grant.
	ErrDenied = errors.New("agent bilateral: authorization denied")
)

// Terms bounds one host-to-consumer agreement for one agent installation.
// Egress is a set of explicitly allowed destinations and must be nonempty.
type Terms struct {
	ConversationID   string
	HostTenant       string
	ConsumerTenant   string
	InstallationID   string
	AgentID          string
	AgentVersion     string
	Purpose          string
	ProcessingRegion string
	Retention        string
	Egress           []string
	IncidentContact  string
	ExitBehavior     string
	ExpiresAt        time.Time
}

// Consent binds one company's current approval to an exact grant revision and
// canonical terms digest. A boolean acceptance flag is not sufficient proof.
type Consent struct {
	TenantID     string
	GrantVersion uint64
	TermsDigest  string
	AcceptedAt   time.Time
}

// Grant is a current, bilateral chat-share grant augmented with the agent
// installation terms that the generic conversation grant does not represent.
type Grant struct {
	ID          string
	Version     uint64
	Terms       Terms
	HostConsent Consent
	HomeConsent Consent
	RevokedAt   time.Time
}

// GrantBinding is the immutable grant identity retained by an authorized
// installation for subsequent delivery-time revalidation.
type GrantBinding struct {
	ConsumerTenant string
	GrantID        string
	GrantVersion   uint64
	TermsDigest    string
}

// Installation is the authorization result to persist with an agent install.
type Installation struct {
	ConversationID     string
	HostTenant         string
	AgentID            string
	AgentVersion       string
	InstallationID     string
	MembershipRevision uint64
	Grants             []GrantBinding
}

// InstallRequest carries the current cross-company membership and grants.
// Every foreign tenant in MemberTenants must have a matching live agreement.
type InstallRequest struct {
	ConversationID     string
	HostTenant         string
	AgentID            string
	AgentVersion       string
	InstallationID     string
	Purpose            string
	ProcessingRegion   string
	Retention          string
	Egress             []string
	IncidentContact    string
	ExitBehavior       string
	ExpiresAt          time.Time
	MemberTenants      []string
	MembershipRevision uint64
	Grants             []Grant
	Now                time.Time
}

// OutputRequest rechecks current agreements immediately before delivery.
// Membership changes require a fresh installation authorization first.
type OutputRequest struct {
	Installation              Installation
	AudienceTenants           []string
	CurrentMembershipRevision uint64
	CurrentGrants             []Grant
	Now                       time.Time
}

// AuthorizeInstall permits installation only when the host and each distinct
// foreign member tenant have approved the exact current agent terms.
func AuthorizeInstall(req InstallRequest) (Installation, error) {
	if !validInstallRequest(req) {
		return Installation{}, ErrInvalid
	}
	consumers := foreignTenants(req.MemberTenants, req.HostTenant)
	if len(consumers) == 0 || len(req.Grants) != len(consumers) {
		return Installation{}, ErrDenied
	}
	byTenant := make(map[string]Grant, len(req.Grants))
	for _, grant := range req.Grants {
		tenant := grant.Terms.ConsumerTenant
		if _, exists := byTenant[tenant]; exists {
			return Installation{}, ErrDenied
		}
		byTenant[tenant] = grant
	}
	bindings := make([]GrantBinding, 0, len(consumers))
	for _, tenant := range consumers {
		grant, ok := byTenant[tenant]
		if !ok {
			return Installation{}, ErrDenied
		}
		terms := termsFor(req, tenant)
		if !currentGrant(grant, terms, req.Now) {
			return Installation{}, ErrDenied
		}
		digest, _ := TermsDigest(terms)
		bindings = append(bindings, GrantBinding{ConsumerTenant: tenant, GrantID: grant.ID, GrantVersion: grant.Version, TermsDigest: digest})
	}
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].ConsumerTenant < bindings[j].ConsumerTenant })
	return Installation{ConversationID: req.ConversationID, HostTenant: req.HostTenant, AgentID: req.AgentID, AgentVersion: req.AgentVersion, InstallationID: req.InstallationID, MembershipRevision: req.MembershipRevision, Grants: bindings}, nil
}

// AuthorizeOutput fails closed when membership changed, the audience includes
// a tenant outside the installation, or any bilateral grant is stale,
// revoked, asymmetric, or broader/different than the installed terms.
func AuthorizeOutput(req OutputRequest) error {
	i := req.Installation
	if req.Now.IsZero() || i.MembershipRevision == 0 || req.CurrentMembershipRevision != i.MembershipRevision || strings.TrimSpace(i.ConversationID) == "" || strings.TrimSpace(i.HostTenant) == "" || strings.TrimSpace(i.InstallationID) == "" || strings.TrimSpace(i.AgentID) == "" || strings.TrimSpace(i.AgentVersion) == "" {
		return ErrDenied
	}
	bindings := make(map[string]GrantBinding, len(i.Grants))
	for _, binding := range i.Grants {
		if strings.TrimSpace(binding.ConsumerTenant) == "" || binding.ConsumerTenant == i.HostTenant || strings.TrimSpace(binding.GrantID) == "" || binding.GrantVersion == 0 || strings.TrimSpace(binding.TermsDigest) == "" {
			return ErrDenied
		}
		if _, exists := bindings[binding.ConsumerTenant]; exists {
			return ErrDenied
		}
		bindings[binding.ConsumerTenant] = binding
	}
	audience := uniqueTenants(req.AudienceTenants)
	if len(audience) != len(req.AudienceTenants) || len(audience) == 0 {
		return ErrDenied
	}
	current := make(map[string]Grant, len(req.CurrentGrants))
	for _, grant := range req.CurrentGrants {
		tenant := grant.Terms.ConsumerTenant
		if _, exists := current[tenant]; exists {
			return ErrDenied
		}
		current[tenant] = grant
	}
	used := 0
	for _, tenant := range audience {
		if tenant == i.HostTenant {
			continue
		}
		binding, ok := bindings[tenant]
		if !ok {
			return ErrDenied
		}
		grant, ok := current[tenant]
		if !ok || grant.ID != binding.GrantID || grant.Version != binding.GrantVersion {
			return ErrDenied
		}
		terms := grant.Terms
		if terms.ConversationID != i.ConversationID || terms.HostTenant != i.HostTenant || terms.ConsumerTenant != tenant || terms.InstallationID != i.InstallationID || terms.AgentID != i.AgentID || terms.AgentVersion != i.AgentVersion || !currentGrant(grant, terms, req.Now) {
			return ErrDenied
		}
		digest, err := TermsDigest(terms)
		if err != nil || digest != binding.TermsDigest {
			return ErrDenied
		}
		used++
	}
	if used == 0 || len(audience) != len(bindings)+1 || len(req.CurrentGrants) != len(bindings) {
		return ErrDenied
	}
	return nil
}

// TermsDigest returns a deterministic digest used by both consent records.
func TermsDigest(terms Terms) (string, error) {
	if err := validateTerms(terms); err != nil {
		return "", err
	}
	canonical := terms
	canonical.Egress = append([]string(nil), terms.Egress...)
	sort.Strings(canonical.Egress)
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("encode bilateral terms: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func currentGrant(grant Grant, expected Terms, now time.Time) bool {
	digest, err := TermsDigest(expected)
	if err != nil || now.IsZero() || grant.ID == "" || grant.Version == 0 || !grant.RevokedAt.IsZero() || !now.Before(expected.ExpiresAt) || !sameTerms(grant.Terms, expected) {
		return false
	}
	return grant.HostConsent.TenantID == expected.HostTenant && grant.HomeConsent.TenantID == expected.ConsumerTenant &&
		grant.HostConsent.GrantVersion == grant.Version && grant.HomeConsent.GrantVersion == grant.Version &&
		grant.HostConsent.TermsDigest == digest && grant.HomeConsent.TermsDigest == digest &&
		!grant.HostConsent.AcceptedAt.IsZero() && !grant.HomeConsent.AcceptedAt.IsZero() &&
		!grant.HostConsent.AcceptedAt.After(now) && !grant.HomeConsent.AcceptedAt.After(now)
}

func sameTerms(a, b Terms) bool {
	if a.ConversationID != b.ConversationID || a.HostTenant != b.HostTenant || a.ConsumerTenant != b.ConsumerTenant || a.InstallationID != b.InstallationID || a.AgentID != b.AgentID || a.AgentVersion != b.AgentVersion || a.Purpose != b.Purpose || a.ProcessingRegion != b.ProcessingRegion || a.Retention != b.Retention || a.IncidentContact != b.IncidentContact || a.ExitBehavior != b.ExitBehavior || !a.ExpiresAt.Equal(b.ExpiresAt) || len(a.Egress) != len(b.Egress) {
		return false
	}
	left, right := append([]string(nil), a.Egress...), append([]string(nil), b.Egress...)
	sort.Strings(left)
	sort.Strings(right)
	for n := range left {
		if left[n] != right[n] {
			return false
		}
	}
	return true
}

func validateTerms(t Terms) error {
	values := []string{t.ConversationID, t.HostTenant, t.ConsumerTenant, t.InstallationID, t.AgentID, t.AgentVersion, t.Purpose, t.ProcessingRegion, t.Retention, t.IncidentContact, t.ExitBehavior}
	for _, value := range values {
		if value == "" || strings.TrimSpace(value) != value {
			return ErrInvalid
		}
	}
	if t.HostTenant == t.ConsumerTenant || t.ExpiresAt.IsZero() || len(t.Egress) == 0 {
		return ErrInvalid
	}
	seen := make(map[string]struct{}, len(t.Egress))
	for _, destination := range t.Egress {
		if destination == "" || strings.TrimSpace(destination) != destination {
			return ErrInvalid
		}
		if _, ok := seen[destination]; ok {
			return ErrInvalid
		}
		seen[destination] = struct{}{}
	}
	return nil
}

func validInstallRequest(r InstallRequest) bool {
	if strings.TrimSpace(r.ConversationID) == "" || strings.TrimSpace(r.HostTenant) == "" || strings.TrimSpace(r.AgentID) == "" || strings.TrimSpace(r.AgentVersion) == "" || strings.TrimSpace(r.InstallationID) == "" || strings.TrimSpace(r.Purpose) == "" || strings.TrimSpace(r.ProcessingRegion) == "" || strings.TrimSpace(r.Retention) == "" || strings.TrimSpace(r.IncidentContact) == "" || strings.TrimSpace(r.ExitBehavior) == "" || r.MembershipRevision == 0 || r.Now.IsZero() || len(r.MemberTenants) == 0 {
		return false
	}
	if len(uniqueTenants(r.MemberTenants)) != len(r.MemberTenants) {
		return false
	}
	for _, tenant := range r.MemberTenants {
		if strings.TrimSpace(tenant) == "" {
			return false
		}
	}
	base := termsFor(r, "placeholder")
	return validateTerms(base) == nil && r.Now.Before(base.ExpiresAt)
}

func termsFor(r InstallRequest, consumer string) Terms {
	return Terms{ConversationID: r.ConversationID, HostTenant: r.HostTenant, ConsumerTenant: consumer, InstallationID: r.InstallationID, AgentID: r.AgentID, AgentVersion: r.AgentVersion, Purpose: r.Purpose, ProcessingRegion: r.ProcessingRegion, Retention: r.Retention, Egress: append([]string(nil), r.Egress...), IncidentContact: r.IncidentContact, ExitBehavior: r.ExitBehavior, ExpiresAt: r.ExpiresAt}
}

func foreignTenants(tenants []string, host string) []string {
	result := make([]string, 0, len(tenants))
	for _, tenant := range tenants {
		if tenant != host {
			result = append(result, tenant)
		}
	}
	sort.Strings(result)
	return result
}

func uniqueTenants(tenants []string) []string {
	result := append([]string(nil), tenants...)
	sort.Strings(result)
	unique := result[:0]
	for _, tenant := range result {
		if len(unique) == 0 || unique[len(unique)-1] != tenant {
			unique = append(unique, tenant)
		}
	}
	return unique
}
