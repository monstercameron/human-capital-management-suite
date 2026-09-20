// MANAGED publish: a registry accepts a new Definition version only after
// the evolution compatibility check clears it.
//
// The compatibility checker lives behind the CompatibilityChecker function
// type rather than a direct call into internal/intent/evolution: evolution
// already imports this package, so a direct call would be an import cycle.
// The canonical checker is evolution.ManagedChecker; tests and harnesses
// may inject a stub. Either way the registry never accepts a version the
// check did not clear: incompatible versions are refused with
// ErrIncompatibleDefinitionVersion, and the check evidence is recorded on
// the ManagedReceipt, whose canonical bytes and digest pin the acceptance.
package intent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

var (
	// ErrIncompatibleDefinitionVersion reports a MANAGED publish whose
	// candidate fails the evolution compatibility check against its
	// predecessor. Match it with errors.Is rather than parsing the reason.
	ErrIncompatibleDefinitionVersion = errors.New("intent: incompatible definition version")

	// ErrManagedVersionNotAdvancing reports a MANAGED publish whose
	// candidate version does not advance the published tip of its intent
	// type. A published version is never rewritten and the tip never moves
	// backwards.
	ErrManagedVersionNotAdvancing = errors.New("intent: managed publish does not advance the published version")
)

// CompatibilityChecker is the evolution gate a MANAGED publish runs before
// accepting a new Definition version. It reports whether current may
// succeed previous, the deterministic evidence behind that verdict, and any
// caller mistake (different intent type, non-advancing version, invalid
// definition) as an error rather than a verdict.
type CompatibilityChecker func(previous, current Definition) (compatible bool, evidence string, err error)

// ManagedReceipt is the byte-stable acceptance record of one MANAGED
// publish. PreviousVersion is zero for a genesis publish, which has no
// predecessor to check against.
type ManagedReceipt struct {
	IntentTypeID    string `json:"intent_type_id"`
	PreviousVersion uint32 `json:"previous_version"`
	Version         uint32 `json:"version"`
	Profile         string `json:"profile"`
	Compatible      bool   `json:"compatible"`
	Evidence        string `json:"evidence"`
	Digest          string `json:"digest"`
}

// managedReceiptBody is the digestable content of a receipt: every field but
// the digest itself, in declaration order, so the digest never hashes
// itself.
type managedReceiptBody struct {
	IntentTypeID    string `json:"intent_type_id"`
	PreviousVersion uint32 `json:"previous_version"`
	Version         uint32 `json:"version"`
	Profile         string `json:"profile"`
	Compatible      bool   `json:"compatible"`
	Evidence        string `json:"evidence"`
}

// CanonicalBytes returns the deterministic encoding of the receipt content.
// It pins the acceptance: the GOLDEN test compares these bytes against the
// checked-in vector.
func (r ManagedReceipt) CanonicalBytes() []byte {
	b, err := json.Marshal(managedReceiptBody{
		IntentTypeID:    r.IntentTypeID,
		PreviousVersion: r.PreviousVersion,
		Version:         r.Version,
		Profile:         r.Profile,
		Compatible:      r.Compatible,
		Evidence:        r.Evidence,
	})
	if err != nil {
		return nil
	}
	return b
}

// computeDigest hashes the canonical content.
func (r ManagedReceipt) computeDigest() string {
	sum := sha256.Sum256(r.CanonicalBytes())
	return "sha256:" + hex.EncodeToString(sum[:])
}

// VerifyDigest recomputes the receipt digest from its canonical content and
// reports whether the recorded digest matches. A receipt is never trusted
// on the word of the digest it carries.
func (r ManagedReceipt) VerifyDigest() error {
	if got := r.computeDigest(); got != r.Digest {
		return newError("VerifyDigest", "digest", ErrInvalidDefinition,
			"managed receipt for %s/v%d records digest %q but its content hashes to %q",
			r.IntentTypeID, r.Version, r.Digest, got)
	}
	return nil
}

// PublishManaged accepts a new Definition version under the MANAGED
// lifecycle: the candidate validates on its own terms, must not reuse a
// published (intent_type_id, version) pair, must advance the published tip
// of its intent type, and must clear the compatibility check against its
// predecessor. The registry is immutable, so acceptance returns a new
// registry; the source registry is unchanged whether the publish succeeds
// or is refused.
func (r *Registry) PublishManaged(candidate Definition, check CompatibilityChecker) (*Registry, ManagedReceipt, error) {
	if r == nil {
		return nil, ManagedReceipt{}, newError("PublishManaged", "registry", ErrInvalidDefinition,
			"no registry to publish into")
	}
	if err := candidate.Validate(); err != nil {
		return nil, ManagedReceipt{}, err
	}
	if _, dup := r.byRef[candidate.Ref]; dup {
		return nil, ManagedReceipt{}, newError("PublishManaged", "definition_ref", ErrDuplicateDefinition,
			"%s is already published; a published version is immutable", candidate.Ref)
	}
	previous, latest, found := r.predecessor(candidate.Ref.TypeID)
	if found && candidate.Ref.Version <= latest {
		return nil, ManagedReceipt{}, newError("PublishManaged", "definition_ref", ErrManagedVersionNotAdvancing,
			"%s does not advance published %s/v%d", candidate.Ref, candidate.Ref.TypeID, latest)
	}
	if check == nil {
		return nil, ManagedReceipt{}, newError("PublishManaged", "compatibility_checker", ErrInvalidDefinition,
			"no compatibility checker supplied")
	}
	receipt := ManagedReceipt{
		IntentTypeID:    candidate.Ref.TypeID,
		PreviousVersion: latest,
		Version:         candidate.Ref.Version,
		Profile:         string(r.profile),
	}
	if !found {
		receipt.Compatible = true
		receipt.Evidence = fmt.Sprintf("genesis: %s is the first published version; definition validated", candidate.Ref)
	} else {
		compatible, evidence, err := check(previous, candidate)
		if err != nil {
			return nil, ManagedReceipt{}, fmt.Errorf("intent: managed compatibility check: %w", err)
		}
		if !compatible {
			return nil, ManagedReceipt{}, newError("PublishManaged", "definition_ref", ErrIncompatibleDefinitionVersion,
				"%s is incompatible with %s: %s", candidate.Ref, previous.Ref, evidence)
		}
		receipt.Compatible = true
		receipt.Evidence = evidence
	}
	next, err := NewRegistry(r.profile, append(r.Definitions(), candidate), r.policiesSlice(), r.catalog)
	if err != nil {
		return nil, ManagedReceipt{}, err
	}
	receipt.Digest = receipt.computeDigest()
	return next, receipt, nil
}

// predecessor returns the highest published version of one intent type.
func (r *Registry) predecessor(typeID string) (Definition, uint32, bool) {
	var previous Definition
	var latest uint32
	found := false
	for ref, d := range r.byRef {
		if ref.TypeID != typeID {
			continue
		}
		if !found || ref.Version > latest {
			previous, latest, found = d, ref.Version, true
		}
	}
	return previous, latest, found
}

// policiesSlice renders the published policies in a stable order for
// registry rebuilds.
func (r *Registry) policiesSlice() []NegativeStatePolicy {
	out := make([]NegativeStatePolicy, 0, len(r.policies))
	for _, p := range r.policies {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref() < out[j].Ref() })
	return out
}
