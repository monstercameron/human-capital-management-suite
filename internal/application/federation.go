package application

// Federation-backed credential verification (REV-005-01).
//
// TRUST-002 built tested OIDC/SAML assertion validation
// (internal/trust/federation) and a governed issuer registry
// (internal/authn/issuerregistry), but the serve composition root only ever
// built the development HMAC verifier, so no deployment could authenticate
// against a tenant IdP. This file is the thin adapter that closes that gap:
// when `-federation-issuers` and `-federation-keys-file` are present,
// composeVerifier builds a trust.Verifier over the federation Validator
// whose keys resolve through the governed registry (Publish then Activate,
// so suspension/retirement still refuse at runtime). The development HMAC
// verifier remains available only when the operator passes -dev-hmac-key
// explicitly or runs -profile=local-dev (which supplies the default key);
// a federation deployment never acquires an implicit dev issuer.

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/authn/issuerregistry"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/federation"
)

// Federation configuration identities recorded in the registry's permanent
// event history. The pinned-keys file is the trust root: whoever could write
// it chose the keys, so the publisher names the pinned issuer and a distinct
// activator applies it, preserving the registry's dual-control shape.
const (
	federationPublisherPrefix = "serve-config:keys-sha256:"
	federationActivator       = "serve-config:federation-activator"
	federationAuthority       = "serve-config"
	// federationMetadataStaleness bounds how long serve trusts one pinned
	// snapshot before the operator must re-pin it with a new revision.
	federationMetadataStaleness = 24 * time.Hour
)

// federationConfigured reports whether the operator supplied tenant IdP
// configuration.
func federationConfigured(cfg ServeConfig) bool {
	return strings.TrimSpace(cfg.FederationIssuers) != "" || strings.TrimSpace(cfg.FederationKeysFile) != ""
}

// validateFederation rejects half-supplied IdP configuration and unparsable
// issuers or keys at startup, so a listener never starts on a federation
// promise it cannot keep.
func (c ServeConfig) validateFederation() error {
	issuersSet := strings.TrimSpace(c.FederationIssuers) != ""
	keysSet := strings.TrimSpace(c.FederationKeysFile) != ""
	if !issuersSet && !keysSet {
		return nil
	}
	if !issuersSet || !keysSet {
		return fmt.Errorf("application: -%s and -%s are set together or not at all",
			FieldFederationIssuers, FieldFederationKeysFile)
	}
	pairs, err := parseFederationIssuers(c.FederationIssuers)
	if err != nil {
		return err
	}
	pinned, err := loadFederationKeysFile(c.FederationKeysFile)
	if err != nil {
		return err
	}
	for tenant, issuers := range pairs {
		for _, issuerURL := range issuers {
			entry, ok := pinned[issuerURL]
			if !ok {
				return fmt.Errorf("application: allow-listed issuer %q for tenant %q has no pinned keys in -%s",
					issuerURL, tenant, FieldFederationKeysFile)
			}
			if values.TenantId(entry.Tenant) != tenant {
				return fmt.Errorf("application: pinned keys for issuer %q name tenant %q, not allow-listed tenant %q",
					issuerURL, entry.Tenant, tenant)
			}
		}
	}
	if strings.TrimSpace(c.Audience) == "" {
		return fmt.Errorf("application: -%s is required with tenant IdP configuration", FieldAudience)
	}
	return nil
}

// parseFederationIssuers parses -federation-issuers: comma-separated
// tenant=issuer pairs. A tenant may name several issuers by repeating its
// pair; every pair names exactly one.
func parseFederationIssuers(raw string) (map[values.TenantId][]string, error) {
	out := map[values.TenantId][]string{}
	seen := map[values.TenantId]map[string]bool{}
	for _, pair := range strings.Split(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		tenant, issuer, ok := strings.Cut(pair, "=")
		tenant = strings.TrimSpace(tenant)
		issuer = strings.TrimSpace(issuer)
		if !ok || tenant == "" || issuer == "" {
			return nil, fmt.Errorf("application: -%s entry %q must be tenant=issuer", FieldFederationIssuers, pair)
		}
		tenantID := values.TenantId(tenant)
		if err := tenantID.Validate(); err != nil {
			return nil, fmt.Errorf("application: -%s entry %q: %v", FieldFederationIssuers, pair, err)
		}
		if _, err := url.ParseRequestURI(issuer); err != nil {
			return nil, fmt.Errorf("application: -%s entry %q: issuer does not parse: %v", FieldFederationIssuers, pair, err)
		}
		if seen[tenantID] == nil {
			seen[tenantID] = map[string]bool{}
		}
		if seen[tenantID][issuer] {
			return nil, fmt.Errorf("application: -%s names %q twice", FieldFederationIssuers, pair)
		}
		seen[tenantID][issuer] = true
		out[tenantID] = append(out[tenantID], issuer)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("application: -%s names no tenant issuer", FieldFederationIssuers)
	}
	return out, nil
}

// federationFileKey is one pinned verification key: base64 DER
// SubjectPublicKeyInfo copied from the issuer's JWKS at publish time, with
// an optional rotation window.
type federationFileKey struct {
	KeyID     string `json:"kid"`
	Algorithm string `json:"alg"`
	PublicKey string `json:"public_key"`
	NotBefore string `json:"not_before,omitempty"`
	NotAfter  string `json:"not_after,omitempty"`
}

// federationFileIssuer is one tenant issuer's pinned snapshot.
type federationFileIssuer struct {
	Tenant       string              `json:"tenant"`
	Issuer       string              `json:"issuer"`
	DiscoveryURL string              `json:"discovery_url,omitempty"`
	Keys         []federationFileKey `json:"keys"`
}

// federationKeysFile is the whole -federation-keys-file document.
type federationKeysFile struct {
	Issuers []federationFileIssuer `json:"issuers"`
}

// loadFederationKeysFile reads and validates the pinned-keys document: one
// entry per issuer, every key parseable, every algorithm in the federation
// closed vocabulary.
func loadFederationKeysFile(path string) (map[string]federationFileIssuer, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("application: read -%s: %w", FieldFederationKeysFile, err)
	}
	var file federationKeysFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("application: parse -%s: %w", FieldFederationKeysFile, err)
	}
	out := make(map[string]federationFileIssuer, len(file.Issuers))
	for _, entry := range file.Issuers {
		entry.Issuer = strings.TrimSpace(entry.Issuer)
		entry.Tenant = strings.TrimSpace(entry.Tenant)
		if entry.Issuer == "" || entry.Tenant == "" {
			return nil, fmt.Errorf("application: -%s entry needs a tenant and an issuer", FieldFederationKeysFile)
		}
		if _, err := url.ParseRequestURI(entry.Issuer); err != nil {
			return nil, fmt.Errorf("application: -%s issuer %q does not parse: %v", FieldFederationKeysFile, entry.Issuer, err)
		}
		if err := values.TenantId(entry.Tenant).Validate(); err != nil {
			return nil, fmt.Errorf("application: -%s tenant %q: %v", FieldFederationKeysFile, entry.Tenant, err)
		}
		if _, duplicate := out[entry.Issuer]; duplicate {
			return nil, fmt.Errorf("application: -%s names issuer %q twice", FieldFederationKeysFile, entry.Issuer)
		}
		if len(entry.Keys) == 0 {
			return nil, fmt.Errorf("application: -%s issuer %q pins no keys", FieldFederationKeysFile, entry.Issuer)
		}
		seen := map[string]bool{}
		for _, key := range entry.Keys {
			if strings.TrimSpace(key.KeyID) == "" {
				return nil, fmt.Errorf("application: -%s issuer %q has a key with no id", FieldFederationKeysFile, entry.Issuer)
			}
			if seen[key.KeyID] {
				return nil, fmt.Errorf("application: -%s issuer %q repeats key id %q", FieldFederationKeysFile, entry.Issuer, key.KeyID)
			}
			seen[key.KeyID] = true
			if !federation.Algorithm(key.Algorithm).Valid() {
				return nil, fmt.Errorf("application: -%s issuer %q key %q names unsupported algorithm %q",
					FieldFederationKeysFile, entry.Issuer, key.KeyID, key.Algorithm)
			}
			der, err := base64.StdEncoding.DecodeString(key.PublicKey)
			if err != nil || len(der) == 0 {
				return nil, fmt.Errorf("application: -%s issuer %q key %q has no decodable public key",
					FieldFederationKeysFile, entry.Issuer, key.KeyID)
			}
			if _, err := x509.ParsePKIXPublicKey(der); err != nil {
				return nil, fmt.Errorf("application: -%s issuer %q key %q public key does not parse: %v",
					FieldFederationKeysFile, entry.Issuer, key.KeyID, err)
			}
			for _, bound := range []string{key.NotBefore, key.NotAfter} {
				if bound != "" {
					if _, err := time.Parse(time.RFC3339, bound); err != nil {
						return nil, fmt.Errorf("application: -%s issuer %q key %q window %q is not RFC3339: %v",
							FieldFederationKeysFile, entry.Issuer, key.KeyID, bound, err)
					}
				}
			}
		}
		out[entry.Issuer] = entry
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("application: -%s pins no issuers", FieldFederationKeysFile)
	}
	return out, nil
}

// federationVerifier adapts the federation Validator to trust.Verifier: the
// Bearer [REDACTED] case the listener accepts, validated against the tenant's
// allow-listed issuers.
type federationVerifier struct {
	validator *federation.Validator
}

var _ trust.Verifier = (*federationVerifier)(nil)

// Verify implements trust.Verifier.
func (v *federationVerifier) Verify(ctx context.Context, cred trust.Credential) (*trust.Principal, error) {
	if v == nil || v.validator == nil {
		return nil, fmt.Errorf("%w: federation verifier is not composed", trust.ErrInvalidCredential)
	}
	if strings.TrimSpace(cred.Token) == "" {
		return nil, trust.ErrNoCredential
	}
	if cred.Scheme != "" && !strings.EqualFold(cred.Scheme, "bearer") {
		return nil, fmt.Errorf("%w: %q", trust.ErrUnsupportedScheme, cred.Scheme)
	}
	return v.validator.Validate(ctx, cred.Token)
}

// composeFederationVerifier seeds the governed issuer registry from the
// operator's pinned configuration (Publish, then Activate under a distinct
// principal so the dual-control event history holds) and returns the
// federation-backed trust.Verifier the listener authenticates with.
func composeFederationVerifier(cfg ServeConfig, now func() time.Time) (trust.Verifier, error) {
	pairs, err := parseFederationIssuers(cfg.FederationIssuers)
	if err != nil {
		return nil, err
	}
	pinned, err := loadFederationKeysFile(cfg.FederationKeysFile)
	if err != nil {
		return nil, err
	}
	at := time.Now
	if now != nil {
		at = now
	}
	publishedAt := at().UTC()
	store := issuerregistry.NewMemoryStore()
	for tenant, issuers := range pairs {
		for _, issuerURL := range issuers {
			entry, ok := pinned[issuerURL]
			if !ok {
				return nil, fmt.Errorf("application: allow-listed issuer %q for tenant %q has no pinned keys in -%s",
					issuerURL, tenant, FieldFederationKeysFile)
			}
			if values.TenantId(entry.Tenant) != tenant {
				return nil, fmt.Errorf("application: pinned keys for issuer %q name tenant %q, not allow-listed tenant %q",
					issuerURL, entry.Tenant, tenant)
			}
			issuer, err := federationFileIssuerRecord(entry, cfg.Audience, publishedAt)
			if err != nil {
				return nil, err
			}
			if _, err := issuerregistry.Publish(store, issuer); err != nil {
				return nil, fmt.Errorf("application: publish issuer %q: %w", issuerURL, err)
			}
			if _, err := issuerregistry.Activate(store, issuer.Ref(), issuerregistry.Evidence{
				ActedBy:   federationActivator,
				Authority: federationAuthority,
				Reason:    "pinned IdP configuration applied at serve composition",
				At:        publishedAt,
			}); err != nil {
				return nil, fmt.Errorf("application: activate issuer %q: %w", issuerURL, err)
			}
		}
	}
	resolver, err := issuerregistry.NewResolver(store, nil)
	if err != nil {
		return nil, fmt.Errorf("application: issuer resolver: %w", err)
	}
	validator, err := federation.NewValidator(federation.Config{
		TenantIssuers: pairs,
		Audience:      cfg.Audience,
		Keys:          federation.NewRegistryKeySource(resolver),
		Now:           at,
	})
	if err != nil {
		return nil, fmt.Errorf("application: federation validator: %w", err)
	}
	return &federationVerifier{validator: validator}, nil
}

// federationFileIssuerRecord converts one pinned file entry into the
// immutable issuer revision the registry governs.
func federationFileIssuerRecord(entry federationFileIssuer, audience string, publishedAt time.Time) (issuerregistry.Issuer, error) {
	issuer := issuerregistry.Issuer{
		Tenant:             values.TenantId(entry.Tenant),
		IssuerURL:          entry.Issuer,
		Audience:           audience,
		JWKS:               issuerregistry.JWKSSource{Kind: issuerregistry.JWKSSourcePinnedKeys, DiscoveryURL: entry.DiscoveryURL},
		Revision:           1,
		PublisherPrincipal: federationPublisherPrefix + entry.Issuer,
		PublishedAt:        publishedAt,
		MetadataStaleness:  federationMetadataStaleness,
	}
	seenAlg := map[federation.Algorithm]bool{}
	for _, key := range entry.Keys {
		der, err := base64.StdEncoding.DecodeString(key.PublicKey)
		if err != nil {
			return issuerregistry.Issuer{}, fmt.Errorf("decode pinned key %q: %w", key.KeyID, err)
		}
		pinned := issuerregistry.PinnedKey{
			KeyID:        key.KeyID,
			Algorithm:    federation.Algorithm(key.Algorithm),
			PublicKeyDER: der,
		}
		if key.NotBefore != "" {
			notBefore, err := time.Parse(time.RFC3339, key.NotBefore)
			if err != nil {
				return issuerregistry.Issuer{}, fmt.Errorf("pinned key %q not_before: %w", key.KeyID, err)
			}
			pinned.NotBefore = notBefore
		}
		if key.NotAfter != "" {
			notAfter, err := time.Parse(time.RFC3339, key.NotAfter)
			if err != nil {
				return issuerregistry.Issuer{}, fmt.Errorf("pinned key %q not_after: %w", key.KeyID, err)
			}
			pinned.NotAfter = notAfter
		}
		issuer.JWKS.PinnedKeys = append(issuer.JWKS.PinnedKeys, pinned)
		if !seenAlg[pinned.Algorithm] {
			seenAlg[pinned.Algorithm] = true
			issuer.Algorithms = append(issuer.Algorithms, pinned.Algorithm)
		}
	}
	return issuer, nil
}
