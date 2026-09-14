// Sealed key-service recovery: TRUST-032 proves key/secret service
// outage and sealed recovery.
//
// Outage modes are explicit: a reachable service is live, an
// unreachable service with a fresh reference cache serves bounded
// cached references, and an expired cache seals the service — only
// sealed reference bundles restore, and only into isolated cells whose
// trust allowlist names each key. The bundle carries references (see
// [secrets.SecretReference]), never secret material: there is no field
// material could travel in, and every reference revalidates on restore.
package outage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/secrets"
)

// KeyOutageMode is the closed set of key-service outage modes.
type KeyOutageMode string

// The explicit key-service outage modes.
const (
	// KeyModeLive serves references from the reachable key service.
	KeyModeLive KeyOutageMode = "LIVE"
	// KeyModeCachedBounded serves cached references inside the policy window.
	KeyModeCachedBounded KeyOutageMode = "CACHED_BOUNDED"
	// KeyModeSealedOnly restores sealed references into isolated cells only.
	KeyModeSealedOnly KeyOutageMode = "SEALED_ONLY"
)

// Key-service outage reason tokens.
const (
	ReasonKeyServiceLive        = "key_service_live"
	ReasonKeyCacheWithinWindow  = "key_reference_cache_within_window"
	ReasonKeyCacheWindowExpired = "key_reference_cache_expired_sealed_only"
)

// KeyServiceHealth is the key-service health snapshot.
type KeyServiceHealth struct {
	// Reachable reports whether the last key-service call succeeded.
	Reachable bool
	// ReferenceCacheAge is how stale the local reference cache is. A
	// negative value is malformed input.
	ReferenceCacheAge time.Duration
}

// SealedPolicy bounds key-reference cache staleness. The bound must be
// strictly positive; a non-positive policy is refused rather than read
// as "unbounded".
type SealedPolicy struct {
	MaxReferenceCacheAge time.Duration
}

// ErrInvalidSealedPolicy is returned for a non-positive sealed policy.
var ErrInvalidSealedPolicy = errors.New("outage: invalid sealed policy")

// ClassifyKeyOutage maps key-service health to its explicit mode. The
// cache never silently outlives policy: a cache older than the bound —
// including exactly at inputs the policy cannot order — seals service.
func ClassifyKeyOutage(policy SealedPolicy, health KeyServiceHealth) (KeyOutageMode, string) {
	if policy.MaxReferenceCacheAge <= 0 || health.ReferenceCacheAge < 0 {
		return KeyModeSealedOnly, ReasonKeyCacheWindowExpired
	}
	if health.Reachable {
		return KeyModeLive, ReasonKeyServiceLive
	}
	if health.ReferenceCacheAge <= policy.MaxReferenceCacheAge {
		return KeyModeCachedBounded, ReasonKeyCacheWithinWindow
	}
	return KeyModeSealedOnly, ReasonKeyCacheWindowExpired
}

// SealedBundle is a sealed set of key references for isolated recovery.
// It carries identities and envelope digests only.
type SealedBundle struct {
	References []secrets.SecretReference
	Digest     string
}

// SealReferences validates each reference and seals the bundle.
func SealReferences(references []secrets.SecretReference) (SealedBundle, error) {
	if len(references) == 0 {
		return SealedBundle{}, errors.New("outage: sealed bundle requires at least one reference")
	}
	seen := map[string]bool{}
	sorted := append([]secrets.SecretReference(nil), references...)
	for _, ref := range sorted {
		if err := ref.Validate(); err != nil {
			return SealedBundle{}, err
		}
		qualified := ref.ID + "@" + ref.Version
		if seen[qualified] {
			return SealedBundle{}, fmt.Errorf("outage: duplicate sealed reference %s", qualified)
		}
		seen[qualified] = true
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].ID != sorted[j].ID {
			return sorted[i].ID < sorted[j].ID
		}
		return sorted[i].Version < sorted[j].Version
	})
	return SealedBundle{References: sorted, Digest: sealedDigest(sorted)}, nil
}

func sealedDigest(references []secrets.SecretReference) string {
	parts := []string{"trust032-sealed-bundle"}
	for _, ref := range references {
		parts = append(parts, strings.Join([]string{
			ref.ID, string(ref.Kind), ref.Version, ref.Provider,
			ref.ProviderPath, ref.Tenant, ref.Region, string(ref.State),
		}, "\x00"))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// IsolatedCell names a recovery cell and the exact key references its
// trust store may resolve, as id@version pairs.
type IsolatedCell struct {
	CellID     string
	AllowedIDs []string
}

// RestoreReceipt proves which references one isolated restore admitted.
type RestoreReceipt struct {
	CellID   string
	Restored []string
	Digest   string
}

// RestoreIsolated restores sealed references into an isolated cell. The
// bundle digest must verify, every reference must revalidate, and every
// reference must be allowlisted by the cell: untrusted references and
// tampered bundles refuse, and forbidden secret material has no field
// to travel in.
func RestoreIsolated(bundle SealedBundle, cell IsolatedCell) (RestoreReceipt, error) {
	if strings.TrimSpace(cell.CellID) == "" {
		return RestoreReceipt{}, errors.New("outage: isolated cell id is required")
	}
	if sealedDigest(bundle.References) != bundle.Digest {
		return RestoreReceipt{}, errors.New("outage: sealed bundle digest mismatch")
	}
	allowed := map[string]bool{}
	for _, id := range cell.AllowedIDs {
		allowed[id] = true
	}
	restored := []string{}
	for _, ref := range bundle.References {
		if err := ref.Validate(); err != nil {
			return RestoreReceipt{}, err
		}
		qualified := ref.ID + "@" + ref.Version
		if !allowed[qualified] {
			return RestoreReceipt{}, fmt.Errorf("outage: untrusted key reference %s", qualified)
		}
		restored = append(restored, qualified)
	}
	sort.Strings(restored)
	receipt := RestoreReceipt{CellID: cell.CellID, Restored: restored}
	sum := sha256.Sum256([]byte("trust032-restore\n" + cell.CellID + "\n" + strings.Join(restored, "\n") + "\n" + bundle.Digest))
	receipt.Digest = "sha256:" + hex.EncodeToString(sum[:])
	return receipt, nil
}
