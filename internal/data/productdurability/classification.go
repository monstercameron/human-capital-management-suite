package productdurability

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// ALIGN-038: storage honors classification. Every stored field is
// classified before it lands; the classification dictates the protection
// the store must provide. Confidential and restricted fields must be
// encrypted at rest, restricted fields must additionally be audit-logged on
// access, and an unclassified field is refused rather than stored with a
// guessed policy.

// Classification is the sensitivity of a stored field. The set is closed.
type Classification string

// Storage classifications.
const (
	ClassificationPublic       Classification = "public"
	ClassificationInternal     Classification = "internal"
	ClassificationConfidential Classification = "confidential"
	ClassificationRestricted   Classification = "restricted"
)

// Classification errors.
var (
	ErrClassificationInvalid = errors.New("productdurability: storage classification is invalid")
	ErrClassificationBreach  = errors.New("productdurability: stored field misses its required protection")
)

// Protection is the storage protection a classification requires.
type Protection struct {
	EncryptedAtRest bool `json:"encrypted_at_rest"`
	AuditOnAccess   bool `json:"audit_on_access"`
}

// PolicyFor returns the required protection for a classification.
func PolicyFor(class Classification) (Protection, error) {
	switch class {
	case ClassificationPublic:
		return Protection{}, nil
	case ClassificationInternal:
		return Protection{AuditOnAccess: true}, nil
	case ClassificationConfidential:
		return Protection{EncryptedAtRest: true, AuditOnAccess: true}, nil
	case ClassificationRestricted:
		return Protection{EncryptedAtRest: true, AuditOnAccess: true}, nil
	default:
		return Protection{}, fmt.Errorf("%w: %q", ErrClassificationInvalid, string(class))
	}
}

// restricted reports whether the classification additionally forbids
// derived copies outside the encrypted store. Restricted fields must be
// encrypted; the audit requirement is shared with confidential.
func (c Classification) restricted() bool { return c == ClassificationRestricted }

// StoredField is one field as stored: its classification plus the
// protections the store actually applied.
type StoredField struct {
	Field           string         `json:"field"`
	Classification  Classification `json:"classification"`
	EncryptedAtRest bool           `json:"encrypted_at_rest"`
	AuditOnAccess   bool           `json:"audit_on_access"`
}

// ClassificationRegistry maps field names to their classification. The
// zero value is not usable; build one with NewClassificationRegistry.
type ClassificationRegistry struct {
	mu     sync.Mutex
	fields map[string]Classification
}

// NewClassificationRegistry builds an empty registry.
func NewClassificationRegistry() *ClassificationRegistry {
	return &ClassificationRegistry{fields: make(map[string]Classification)}
}

// Register classifies a field name. Re-registering with the same class is
// idempotent; changing a field's class is refused so policy cannot drift
// under stored data.
func (r *ClassificationRegistry) Register(field string, class Classification) error {
	if strings.TrimSpace(field) == "" {
		return fmt.Errorf("%w: field name is required", ErrClassificationInvalid)
	}
	if _, err := PolicyFor(class); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.fields[field]; ok {
		if existing != class {
			return fmt.Errorf("%w: %q is %s, not %s", ErrClassificationInvalid, field, existing, class)
		}
		return nil
	}
	r.fields[field] = class
	return nil
}

// Enforce verifies that every stored field is classified and carries at
// least its required protection. The first breach names its field.
func (r *ClassificationRegistry) Enforce(stored []StoredField) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, field := range stored {
		class, ok := r.fields[field.Field]
		if !ok {
			return fmt.Errorf("%w: %q is not classified", ErrClassificationInvalid, field.Field)
		}
		if field.Classification != class {
			return fmt.Errorf("%w: %q stored as %s, classified %s", ErrClassificationBreach, field.Field, field.Classification, class)
		}
		required, err := PolicyFor(class)
		if err != nil {
			return err
		}
		if required.EncryptedAtRest && !field.EncryptedAtRest {
			return fmt.Errorf("%w: %q requires encryption at rest", ErrClassificationBreach, field.Field)
		}
		if required.AuditOnAccess && !field.AuditOnAccess {
			return fmt.Errorf("%w: %q requires access audit", ErrClassificationBreach, field.Field)
		}
		if class.restricted() && !field.EncryptedAtRest {
			return fmt.Errorf("%w: restricted field %q is not encrypted", ErrClassificationBreach, field.Field)
		}
	}
	return nil
}

// Digest returns the content digest over the sorted registrations.
func (r *ClassificationRegistry) Digest() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, 0, len(r.fields))
	for name := range r.fields {
		names = append(names, name)
	}
	sort.Strings(names)
	registrations := make([]struct {
		Field string         `json:"field"`
		Class Classification `json:"class"`
	}, 0, len(names))
	for _, name := range names {
		registrations = append(registrations, struct {
			Field string         `json:"field"`
			Class Classification `json:"class"`
		}{name, r.fields[name]})
	}
	b, err := json.Marshal(registrations)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}
