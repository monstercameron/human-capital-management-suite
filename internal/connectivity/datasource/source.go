// Package datasource is the tenant data-source connector fabric
// (WF-DATA-015). Production code opens tenant-scoped Connectors through the
// Registry; adapter packages (for example internal/connectivity/mongo) turn
// a validated Source into that contract. Credential material is never
// stored on a Source: it travels from the secret store to the adapter at
// Open time only.
package datasource

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Connector errors. All Open/Query/Write failures wrap one of these
// sentinels; none of the messages carries credential material.
var (
	ErrUnknownAdapter        = errors.New("datasource: unknown adapter")
	ErrDuplicateAdapter      = errors.New("datasource: adapter already registered")
	ErrDuplicateSource       = errors.New("datasource: source already registered")
	ErrUnknownSource         = errors.New("datasource: unknown source")
	ErrInvalidSource         = errors.New("datasource: invalid source")
	ErrInsecureTransport     = errors.New("datasource: TLS is required")
	ErrEgressDenied          = errors.New("datasource: egress denied")
	ErrSecretResolution      = errors.New("datasource: secret resolution failed")
	ErrReadOnly              = errors.New("datasource: source is read-only")
	ErrCollectionNotWritable = errors.New("datasource: collection is not writable")
	ErrLimitExceeded         = errors.New("datasource: limit exceeded")
	ErrBatchTooLarge         = errors.New("datasource: write batch too large")
	ErrInvalidQuery          = errors.New("datasource: invalid query")
	ErrInvalidWrite          = errors.New("datasource: invalid write")
	ErrUnavailable           = errors.New("datasource: source unavailable")
)

// DefaultPort is used when Source.Port is zero; adapters may override it
// for their own wire default before dialing.
const DefaultPort = 0

// SourceID identifies one tenant's registered source. The tenant is part
// of the key: a source registered under tenant B is unreachable, and
// indistinguishable from absent, to tenant A.
type SourceID struct {
	Tenant string
	ID     string
}

// SecretRef names one entry in a secret backend. It is a reference only;
// the material it points at is resolved at Open time and never stored.
type SecretRef struct {
	// Store names the secret backend (for example "platform").
	Store string
	// Key names the entry inside that backend.
	Key string
}

// Validate rejects empty stores and keys: an unresolvable reference must
// fail at registration, not at first use.
func (r SecretRef) Validate() error {
	if strings.TrimSpace(r.Store) == "" {
		return fmt.Errorf("%w: secret store is empty", ErrInvalidSource)
	}
	if strings.TrimSpace(r.Key) == "" {
		return fmt.Errorf("%w: secret key is empty", ErrInvalidSource)
	}
	return nil
}

// Credential is the resolved secret material for one Open call. Values of
// this type must never be logged, formatted into errors, or retained past
// the dial they authenticate.
type Credential struct {
	Username string
	Password string
}

// Source is one tenant's registered data source.
type Source struct {
	// Tenant owns the source; Open binds the connector to it.
	Tenant string
	// ID is unique per tenant.
	ID string
	// AdapterID names the registered adapter ("mongodb").
	AdapterID string
	// Host is a bare DNS name or IP literal: no scheme, no port, no
	// userinfo, no path. The egress policy allow-lists it at Open.
	Host string
	// Port is the wire port; zero selects the adapter default.
	Port int
	// Database is the tenant's database on that host.
	Database string
	// Secret refers to the credentials; the material is resolved at Open.
	Secret SecretRef
	// TLSRequired must be true. Sources that would dial without
	// transport encryption are refused at registration.
	TLSRequired bool
	// ReadOnly refuses every Write when true.
	ReadOnly bool
	// WritableCollections is the closed set Write may touch. A source
	// that allows writes must declare at least one collection.
	WritableCollections []string
}

// ID of the source as a registry key.
func (s Source) SourceID() SourceID {
	return SourceID{Tenant: s.Tenant, ID: s.ID}
}

// Validate enforces the deny-by-default shape: every field that could
// widen access must be explicit and well-formed.
func (s Source) Validate() error {
	if strings.TrimSpace(s.Tenant) == "" {
		return fmt.Errorf("%w: tenant is empty", ErrInvalidSource)
	}
	if strings.TrimSpace(s.ID) == "" {
		return fmt.Errorf("%w: id is empty", ErrInvalidSource)
	}
	if strings.TrimSpace(s.AdapterID) == "" {
		return fmt.Errorf("%w: adapter is empty", ErrInvalidSource)
	}
	if err := validateHost(s.Host); err != nil {
		return err
	}
	if s.Port < 0 || s.Port > 65535 {
		return fmt.Errorf("%w: port %d out of range", ErrInvalidSource, s.Port)
	}
	if strings.TrimSpace(s.Database) == "" {
		return fmt.Errorf("%w: database is empty", ErrInvalidSource)
	}
	if err := s.Secret.Validate(); err != nil {
		return err
	}
	if !s.TLSRequired {
		return fmt.Errorf("%w: source %s/%s", ErrInsecureTransport, s.Tenant, s.ID)
	}
	if !s.ReadOnly && len(s.WritableCollections) == 0 {
		return fmt.Errorf("%w: writable source %s/%s declares no collections", ErrInvalidSource, s.Tenant, s.ID)
	}
	for _, c := range s.WritableCollections {
		if strings.TrimSpace(c) == "" {
			return fmt.Errorf("%w: writable collection is empty", ErrInvalidSource)
		}
	}
	return nil
}

// Writable reports whether writes to collection are allowed.
func (s Source) Writable(collection string) bool {
	if s.ReadOnly {
		return false
	}
	for _, c := range s.WritableCollections {
		if c == collection {
			return true
		}
	}
	return false
}

// validateHost refuses anything that is not a bare host: URIs smuggle
// schemes, userinfo and paths past an allow-list that expects a name.
func validateHost(host string) error {
	if strings.TrimSpace(host) == "" {
		return fmt.Errorf("%w: host is empty", ErrInvalidSource)
	}
	if strings.ContainsAny(host, ":/@?#[]") {
		return fmt.Errorf("%w: host %q must be a bare name or IP, without scheme, port, userinfo or path", ErrInvalidSource, host)
	}
	if strings.ContainsAny(host, " \t\r\n") {
		return fmt.Errorf("%w: host %q contains whitespace", ErrInvalidSource, host)
	}
	return nil
}

// SecretsPort resolves credential material at Open time. Production
// wires this to the platform secret store; tests substitute a fake.
type SecretsPort interface {
	ResolveSecret(ctx context.Context, tenant string, ref SecretRef) (Credential, error)
}

// EgressPolicy allow-lists outbound destinations. Production wires this
// to destination trust plus DNS; the adapter calls it before dialing.
type EgressPolicy interface {
	AllowHost(ctx context.Context, host string, port int) error
}

// OpenDeps are the per-Open seams an adapter factory needs.
type OpenDeps struct {
	Secrets SecretsPort
	Egress  EgressPolicy
}

// AdapterFactory builds a tenant-bound Connector for a validated Source.
// The factory must resolve secrets through deps, check egress through
// deps, and refuse to dial when either fails.
type AdapterFactory func(ctx context.Context, src Source, deps OpenDeps) (Connector, error)
