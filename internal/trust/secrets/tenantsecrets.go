// WF-DATA-039: tenant secrets as write-only parameters bound to their
// destinations and resolved only at the outbound call.
//
// Customers bring their own API keys. The value travels one way — from the
// tenant admin's hand into the vault — and afterwards the platform shows
// only name, fingerprint and dates. A secret parameter is a reference the
// compiler allows only in a connector binding's credential slot; each
// secret is bound to declared connectors and hosts and refused anywhere
// else; resolution happens only at the outbound call after the grant check,
// and the value never enters run context, node outputs, ledger, inspector,
// designer, test evidence or logs. Rotation and revocation fail dependent
// connectors closed.
package secrets

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
)

// Tenant secret errors. All are matchable with errors.Is.
var (
	// ErrSecretRole is returned when an actor without the secret-admin
	// role attempts secret administration. The roles that set secrets
	// are distinct from workflow authors.
	ErrSecretRole = errors.New("secrets: secret administration requires the secret-admin role")
	// ErrSecretBinding is returned when a secret is resolved for a
	// connector or host it is not bound to.
	ErrSecretBinding = errors.New("secrets: secret is not bound to this connector and host")
	// ErrSecretMisplaced is returned when a secret reference flows to a
	// slot other than the credential slot, or a plain value flows into
	// the credential slot.
	ErrSecretMisplaced = errors.New("secrets: secret reference is allowed only in the connector credential slot")
	// ErrSecretInvalid is returned for malformed tenant secret input.
	ErrSecretInvalid = errors.New("secrets: invalid tenant secret")
	// ErrSecretNotFound is returned for an unknown tenant secret.
	ErrSecretNotFound = errors.New("secrets: unknown tenant secret")
)

// Secret administration roles.
const (
	// RoleSecretAdmin sets, rotates and revokes tenant secrets.
	RoleSecretAdmin = "secrets:admin"
	// RoleWorkflowAuthor authors workflows and cannot administer secrets.
	RoleWorkflowAuthor = "workflow:author"
)

// CredentialSlot is the only workflow slot a secret reference may occupy:
// a connector binding's credential slot.
const CredentialSlot = "connector.binding.credential"

// CheckSecretPlacement is the compiler rule for secret references: a
// secret reference is allowed only in the credential slot, and the
// credential slot accepts only a secret reference.
func CheckSecretPlacement(slot string, isSecretRef bool) error {
	if isSecretRef && slot == CredentialSlot {
		return nil
	}
	if isSecretRef {
		return fmt.Errorf("%w: %q", ErrSecretMisplaced, slot)
	}
	if slot == CredentialSlot {
		return fmt.Errorf("%w: plain value in %q", ErrSecretMisplaced, slot)
	}
	return nil
}

// SecretParameter is the workflow secret-reference type: identity without
// material. It binds into a connector binding's credential slot and into
// nothing else.
type SecretParameter struct {
	Tenant  string
	Name    string
	Version string
}

// Bind places the parameter into a workflow slot, refusing every slot but
// the connector binding's credential slot.
func (p SecretParameter) Bind(slot string) error {
	return CheckSecretPlacement(slot, true)
}

// ActorContext is the authenticated actor attempting an operation.
type ActorContext struct {
	Actor  string
	Roles  []string
	Tenant string
	Region string
}

func (a ActorContext) hasRole(role string) bool {
	for _, r := range a.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// SecretMaterial is the write-only input for setting a secret. The value
// exists only for the duration of the call; nothing the manager returns
// afterwards can carry it.
type SecretMaterial struct {
	Name       string
	Kind       Kind
	Value      []byte
	Connectors []string
	Hosts      []string
}

// TenantSecret is the stored tenant secret: metadata only. There is no
// field here that can hold secret material.
type TenantSecret struct {
	Tenant      string
	Name        string
	Fingerprint string
	Kind        Kind
	State       State
	Version     string
	Connectors  []string
	Hosts       []string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Reference renders the stored secret as a governable reference for the
// grant check at resolve time.
func (s TenantSecret) Reference(provider, region string) SecretReference {
	return SecretReference{
		ID:           s.Name,
		Kind:         s.Kind,
		Version:      s.Version,
		Provider:     provider,
		ProviderPath: "tenant/" + s.Tenant + "/" + s.Name + "/" + s.Version,
		Tenant:       s.Tenant,
		Region:       region,
		State:        s.State,
	}
}

// clone deep-copies the binding slices so a caller can never mutate the
// stored authorization boundary through a returned record.
func (s *TenantSecret) clone() TenantSecret {
	out := *s
	out.Connectors = append([]string(nil), s.Connectors...)
	out.Hosts = append([]string(nil), s.Hosts...)
	return out
}

// Vault is the secret-material sink on the far side of the custody
// interface. The manager hands values to it and never sees them again.
type Vault interface {
	// Seal stores one version and returns its fingerprint.
	Seal(tenant, name, version string, value []byte) (string, error)
	// Destroy removes one version so it can never resolve again.
	Destroy(tenant, name, version string) error
}

// OutboundCall describes the moment of use: the connector call that needs
// the secret, with its authenticated workload, purpose and destination.
type OutboundCall struct {
	Workload  string
	Tenant    string
	Region    string
	Purpose   string
	Connector string
	Host      string
}

// AuditRecord is the durable record of one secret administration or
// resolution. It carries the fingerprint, never the value.
type AuditRecord struct {
	Tenant      string
	Name        string
	Fingerprint string
	Action      string
	Actor       string
	Connector   string
	Host        string
	Detail      string
	At          time.Time
}

// Manager administers tenant secrets: add, rotate and revoke through a
// write-only path, and resolve only at the outbound call. It is safe for
// concurrent use.
type Manager struct {
	mu    sync.RWMutex
	vault Vault
	now   func() time.Time
	byKey map[string]*TenantSecret
	audit []AuditRecord
}

// NewManager builds a tenant secret manager over a vault.
func NewManager(vault Vault, now func() time.Time) (*Manager, error) {
	if vault == nil {
		return nil, fmt.Errorf("%w: no vault", ErrSecretInvalid)
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Manager{vault: vault, now: now, byKey: make(map[string]*TenantSecret)}, nil
}

func secretKey(tenant, name string) string { return tenant + "/" + name }

func (m *Manager) record(act ActorContext, s *TenantSecret, action, connector, host, detail string) {
	m.audit = append(m.audit, AuditRecord{
		Tenant:      s.Tenant,
		Name:        s.Name,
		Fingerprint: s.Fingerprint,
		Action:      action,
		Actor:       act.Actor,
		Connector:   connector,
		Host:        host,
		Detail:      detail,
		At:          m.now().UTC(),
	})
}

func checkAdmin(act ActorContext) error {
	if strings.TrimSpace(act.Actor) == "" || strings.TrimSpace(act.Tenant) == "" {
		return fmt.Errorf("%w: actor and tenant are required", ErrSecretInvalid)
	}
	if !act.hasRole(RoleSecretAdmin) {
		return fmt.Errorf("%w: %q", ErrSecretRole, act.Actor)
	}
	return nil
}

// checkName validates a secret name for key building: secretKey joins
// tenant and name with "/", so a "/" inside a name would collide with
// another tenant/name pair.
func checkName(name string) error {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(name) != name {
		return fmt.Errorf("%w: secret name is required", ErrSecretInvalid)
	}
	if strings.Contains(name, "/") {
		return fmt.Errorf("%w: secret name must not contain %q", ErrSecretInvalid, "/")
	}
	return nil
}

func checkBindings(connectors, hosts []string) error {
	if len(connectors) == 0 || len(hosts) == 0 {
		return fmt.Errorf("%w: at least one connector and one host binding are required", ErrSecretInvalid)
	}
	for _, b := range append(append([]string(nil), connectors...), hosts...) {
		if strings.TrimSpace(b) == "" || strings.TrimSpace(b) != b {
			return fmt.Errorf("%w: blank or padded binding", ErrSecretInvalid)
		}
	}
	return nil
}

// Set adds a tenant secret. The value is sealed into the vault inside this
// call; the returned record shows only name, fingerprint and dates.
func (m *Manager) Set(act ActorContext, mat SecretMaterial) (TenantSecret, error) {
	if err := checkAdmin(act); err != nil {
		return TenantSecret{}, err
	}
	if err := checkName(mat.Name); err != nil {
		return TenantSecret{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(mat.Value) == 0 {
		return TenantSecret{}, fmt.Errorf("%w: secret value is required", ErrSecretInvalid)
	}
	if err := checkBindings(mat.Connectors, mat.Hosts); err != nil {
		return TenantSecret{}, err
	}
	key := secretKey(act.Tenant, mat.Name)
	if _, exists := m.byKey[key]; exists {
		return TenantSecret{}, fmt.Errorf("%w: %q already exists; rotate it", ErrSecretInvalid, mat.Name)
	}
	fp, err := m.vault.Seal(act.Tenant, mat.Name, "v1", mat.Value)
	if err != nil {
		return TenantSecret{}, fmt.Errorf("%w: seal failed", ErrSecretInvalid)
	}
	now := m.now().UTC()
	s := &TenantSecret{
		Tenant:      act.Tenant,
		Name:        mat.Name,
		Fingerprint: fp,
		Kind:        mat.Kind,
		State:       Active,
		Version:     "v1",
		Connectors:  append([]string(nil), mat.Connectors...),
		Hosts:       append([]string(nil), mat.Hosts...),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	m.byKey[key] = s
	m.record(act, s, "set", "", "", "version v1 sealed")
	return s.clone(), nil
}

// Rotate seals a new version of a secret and retires the old one. The
// pre-rotation reference no longer resolves.
func (m *Manager) Rotate(act ActorContext, name string, value []byte) (TenantSecret, error) {
	if err := checkAdmin(act); err != nil {
		return TenantSecret{}, err
	}
	if err := checkName(name); err != nil {
		return TenantSecret{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(value) == 0 {
		return TenantSecret{}, fmt.Errorf("%w: secret value is required", ErrSecretInvalid)
	}
	s, ok := m.byKey[secretKey(act.Tenant, name)]
	if !ok {
		return TenantSecret{}, fmt.Errorf("%w: %q", ErrSecretNotFound, name)
	}
	if s.State == Destroyed {
		return TenantSecret{}, fmt.Errorf("%w: %q is destroyed", ErrUnusableVersion, name)
	}
	next := nextVersion(s.Version)
	fp, err := m.vault.Seal(act.Tenant, name, next, value)
	if err != nil {
		return TenantSecret{}, fmt.Errorf("%w: seal failed", ErrSecretInvalid)
	}
	old := s.Version
	if err := m.vault.Destroy(act.Tenant, name, old); err != nil {
		return TenantSecret{}, fmt.Errorf("%w: retire failed", ErrSecretInvalid)
	}
	s.Version = next
	s.Fingerprint = fp
	s.State = Active
	s.UpdatedAt = m.now().UTC()
	m.record(act, s, "rotate", "", "", "version "+old+" retired, "+next+" sealed")
	return s.clone(), nil
}

// Revoke destroys every version of a secret. Dependent connectors fail
// closed: no cached reference resolves afterwards.
func (m *Manager) Revoke(act ActorContext, name string) error {
	if err := checkAdmin(act); err != nil {
		return err
	}
	if err := checkName(name); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := secretKey(act.Tenant, name)
	s, ok := m.byKey[key]
	if !ok {
		return fmt.Errorf("%w: %q", ErrSecretNotFound, name)
	}
	if err := m.vault.Destroy(act.Tenant, name, s.Version); err != nil {
		return fmt.Errorf("%w: destroy failed", ErrSecretInvalid)
	}
	s.State = Destroyed
	s.UpdatedAt = m.now().UTC()
	m.record(act, s, "revoke", "", "", "all versions destroyed")
	return nil
}

// Get returns the stored metadata for a secret: name, fingerprint, dates,
// bindings and state. It cannot return the value.
func (m *Manager) Get(tenant, name string) (TenantSecret, error) {
	if err := checkName(name); err != nil {
		return TenantSecret{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.byKey[secretKey(tenant, name)]
	if !ok {
		return TenantSecret{}, fmt.Errorf("%w: %q", ErrSecretNotFound, name)
	}
	return s.clone(), nil
}

// Audit returns the fingerprint-only record of every administration and
// resolution, in order.
func (m *Manager) Audit() []AuditRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]AuditRecord(nil), m.audit...)
}

// ResolveAtCall resolves a secret reference at the outbound call: after
// the binding check and the grant check, and at no other time. It returns
// a bounded lease authorization, never the value.
func (m *Manager) ResolveAtCall(ref SecretReference, call OutboundCall, op custody.Operation, policy *AccessPolicy, port Port, ttl time.Duration) (Resolution, error) {
	if err := ref.Validate(); err != nil {
		return Resolution{}, err
	}
	if err := checkName(ref.ID); err != nil {
		return Resolution{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.byKey[secretKey(ref.Tenant, ref.ID)]
	if !ok {
		return Resolution{}, fmt.Errorf("%w: %s", ErrSecretNotFound, ref.ID)
	}
	if !usableState(s.State) || ref.Version != s.Version {
		return Resolution{}, fmt.Errorf("%w: %s is %s at %s", ErrUnusableVersion, ref.ID, s.State, s.Version)
	}
	if !bound(s.Connectors, call.Connector) || !bound(s.Hosts, call.Host) {
		return Resolution{}, fmt.Errorf("%w: %s for %s@%s", ErrSecretBinding, ref.ID, call.Connector, call.Host)
	}
	rctx := custody.RequestContext{
		Workload:    call.Workload,
		Tenant:      call.Tenant,
		Region:      call.Region,
		Purpose:     call.Purpose,
		Destination: call.Connector + "@" + call.Host,
	}
	resolver, err := NewResolver(policy, port, m.now)
	if err != nil {
		return Resolution{}, err
	}
	stored := s.Reference(ref.Provider, ref.Region)
	res, err := resolver.Resolve(rctx, stored, op, ttl)
	if err != nil {
		return Resolution{}, err
	}
	m.audit = append(m.audit, AuditRecord{
		Tenant:      s.Tenant,
		Name:        s.Name,
		Fingerprint: s.Fingerprint,
		Action:      "resolve",
		Actor:       call.Workload,
		Connector:   call.Connector,
		Host:        call.Host,
		Detail:      "grant " + res.GrantID + " lease " + res.LeaseID,
		At:          m.now().UTC(),
	})
	return res, nil
}

func bound(list []string, want string) bool {
	for _, b := range list {
		if b == want {
			return true
		}
	}
	return false
}

func nextVersion(v string) string {
	var n int
	if _, err := fmt.Sscanf(v, "v%d", &n); err != nil || n < 1 {
		return "v2"
	}
	return fmt.Sprintf("v%d", n+1)
}
