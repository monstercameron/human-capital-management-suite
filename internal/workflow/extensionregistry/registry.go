// Package extensionregistry owns the common admission contract for immutable
// workflow extensions. Kind-specific packages provide fixture executors; this
// package owns version, digest, dependency, review and safety proof semantics.
package extensionregistry

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

type Kind string

const (
	KindBlock      Kind = "BLOCK"
	KindFragment   Kind = "FRAGMENT"
	KindReducer    Kind = "REDUCER"
	KindFunction   Kind = "FUNCTION"
	KindTrigger    Kind = "TRIGGER"
	KindForm       Kind = "FORM"
	KindBinding    Kind = "BINDING"
	KindCapability Kind = "CAPABILITY"
)

func (k Kind) Valid() bool {
	switch k {
	case KindBlock, KindFragment, KindReducer, KindFunction, KindTrigger, KindForm, KindBinding, KindCapability:
		return true
	default:
		return false
	}
}

type Effect string

const (
	EffectPure                         Effect = "PURE"
	EffectReadOnly                     Effect = "READ_ONLY"
	EffectInternalMutation             Effect = "INTERNAL_MUTATION"
	EffectExternalMutation             Effect = "EXTERNAL_MUTATION"
	EffectIrreversibleExternalMutation Effect = "IRREVERSIBLE_EXTERNAL_MUTATION"
)

func (e Effect) rank() (int, bool) {
	switch e {
	case EffectPure:
		return 0, true
	case EffectReadOnly:
		return 1, true
	case EffectInternalMutation:
		return 2, true
	case EffectExternalMutation:
		return 3, true
	case EffectIrreversibleExternalMutation:
		return 4, true
	default:
		return 0, false
	}
}

type Ref struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

func (r Ref) valid() bool { return strings.TrimSpace(r.ID) != "" && strings.TrimSpace(r.Version) != "" }
func (r Ref) key() string { return r.ID + "@" + r.Version }

type Fixture struct {
	ID            string          `json:"id"`
	Input         json.RawMessage `json:"input"`
	Expected      json.RawMessage `json:"expected,omitempty"`
	ExpectedError string          `json:"expected_error,omitempty"`
}

type ReviewRecord struct {
	ID         string `json:"id"`
	Reviewer   string `json:"reviewer"`
	Authority  string `json:"authority"`
	ReviewedAt string `json:"reviewed_at"`
	Decision   string `json:"decision"`
	Rationale  string `json:"rationale"`
}

type SafetyContract struct {
	MinimumEffect     Effect `json:"minimum_effect"`
	RequiresSafePoint bool   `json:"requires_safe_point"`
}

// CompilerProof records the compiler's verified minimum effect and safe-point
// facts. Admission rejects a claimed proof weaker than the reviewed contract.
type CompilerProof struct {
	Effect    Effect `json:"effect"`
	SafePoint bool   `json:"safe_point"`
}

// Manifest is the shared, versioned admission shape for every extension kind.
// Digest is computed over all fields except Digest itself.
type Manifest struct {
	ID           string         `json:"id"`
	Version      string         `json:"version"`
	Kind         Kind           `json:"kind"`
	Owner        string         `json:"owner"`
	Dependencies []Ref          `json:"dependencies,omitempty"`
	Fixtures     []Fixture      `json:"fixtures"`
	Review       ReviewRecord   `json:"review"`
	Safety       SafetyContract `json:"safety"`
	Proof        CompilerProof  `json:"proof"`
	Digest       string         `json:"digest,omitempty"`
}

type Record struct {
	Manifest Manifest
	Digest   string
}

var (
	ErrInvalid        = errors.New("workflow extension: invalid manifest")
	ErrDuplicate      = errors.New("workflow extension: version already registered")
	ErrProofWeakened  = errors.New("workflow extension: compiler proof weakens reviewed safety contract")
	ErrDigestMismatch = errors.New("workflow extension: supplied digest does not match manifest")
)

type Registry struct {
	mu      sync.RWMutex
	records map[Ref]Record
}

func New() *Registry { return &Registry{records: map[Ref]Record{}} }

func (r *Registry) Register(m Manifest) error {
	normalized, err := normalize(m)
	if err != nil {
		return err
	}
	computed, err := Digest(normalized)
	if err != nil {
		return err
	}
	if m.Digest != "" && m.Digest != computed {
		return fmt.Errorf("%w: got %s, computed %s", ErrDigestMismatch, m.Digest, computed)
	}
	normalized.Digest = computed
	key := Ref{ID: normalized.ID, Version: normalized.Version}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.records[key]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicate, key)
	}
	for _, dependency := range normalized.Dependencies {
		if _, ok := r.records[dependency]; !ok {
			return fmt.Errorf("%w: unresolved dependency %s", ErrInvalid, dependency.key())
		}
	}
	r.records[key] = Record{Manifest: cloneManifest(normalized), Digest: computed}
	return nil
}

func (r *Registry) Resolve(ref Ref) (Record, bool) {
	if r == nil {
		return Record{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	got, ok := r.records[ref]
	if !ok {
		return Record{}, false
	}
	return Record{Manifest: cloneManifest(got.Manifest), Digest: got.Digest}, true
}

func (r *Registry) List() []Record {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Record, 0, len(r.records))
	for _, record := range r.records {
		out = append(out, Record{Manifest: cloneManifest(record.Manifest), Digest: record.Digest})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Manifest.ID != out[j].Manifest.ID {
			return out[i].Manifest.ID < out[j].Manifest.ID
		}
		return out[i].Manifest.Version < out[j].Manifest.Version
	})
	return out
}

func Digest(m Manifest) (string, error) {
	m.Digest = ""
	canonical, err := normalize(m)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	h := sha256.Sum256(append([]byte("hcmnext.workflow.ExtensionManifest/v1\x00"), b...))
	return "sha256:" + hex.EncodeToString(h[:]), nil
}

func normalize(m Manifest) (Manifest, error) {
	if strings.TrimSpace(m.ID) == "" || strings.TrimSpace(m.Version) == "" || !m.Kind.Valid() || strings.TrimSpace(m.Owner) == "" {
		return Manifest{}, fmt.Errorf("%w: identity, kind and owner are required", ErrInvalid)
	}
	minimum, minOK := m.Safety.MinimumEffect.rank()
	actual, actualOK := m.Proof.Effect.rank()
	if !minOK || !actualOK {
		return Manifest{}, fmt.Errorf("%w: unknown effect class", ErrInvalid)
	}
	if actual < minimum || (m.Safety.RequiresSafePoint && !m.Proof.SafePoint) {
		return Manifest{}, ErrProofWeakened
	}
	if m.Review.ID == "" || m.Review.Reviewer == "" || m.Review.Authority == "" || m.Review.ReviewedAt == "" || m.Review.Decision != "APPROVED" || m.Review.Rationale == "" {
		return Manifest{}, fmt.Errorf("%w: approved review record is incomplete", ErrInvalid)
	}
	if _, err := time.Parse(time.RFC3339, m.Review.ReviewedAt); err != nil {
		return Manifest{}, fmt.Errorf("%w: review timestamp must be RFC3339", ErrInvalid)
	}
	if len(m.Fixtures) == 0 {
		return Manifest{}, fmt.Errorf("%w: at least one data fixture is required", ErrInvalid)
	}
	for i := range m.Fixtures {
		f := &m.Fixtures[i]
		if strings.TrimSpace(f.ID) == "" || !json.Valid(f.Input) || (f.ExpectedError == "" && !json.Valid(f.Expected)) || (f.ExpectedError != "" && len(f.Expected) != 0) {
			return Manifest{}, fmt.Errorf("%w: fixture %q has invalid input or oracle", ErrInvalid, f.ID)
		}
		f.Input, _ = canonicalJSON(f.Input)
		if len(f.Expected) != 0 {
			f.Expected, _ = canonicalJSON(f.Expected)
		}
	}
	seenDependencies := map[Ref]bool{}
	for _, d := range m.Dependencies {
		if !d.valid() {
			return Manifest{}, fmt.Errorf("%w: incomplete dependency", ErrInvalid)
		}
		if seenDependencies[d] {
			return Manifest{}, fmt.Errorf("%w: duplicate dependency %s", ErrInvalid, d.key())
		}
		seenDependencies[d] = true
	}
	sort.Slice(m.Dependencies, func(i, j int) bool { return m.Dependencies[i].key() < m.Dependencies[j].key() })
	sort.Slice(m.Fixtures, func(i, j int) bool { return m.Fixtures[i].ID < m.Fixtures[j].ID })
	for i := 1; i < len(m.Fixtures); i++ {
		if m.Fixtures[i-1].ID == m.Fixtures[i].ID {
			return Manifest{}, fmt.Errorf("%w: duplicate fixture %q", ErrInvalid, m.Fixtures[i].ID)
		}
	}
	return cloneManifest(m), nil
}

func canonicalJSON(in json.RawMessage) (json.RawMessage, error) {
	var value any
	if err := json.Unmarshal(in, &value); err != nil {
		return nil, err
	}
	out, err := json.Marshal(value)
	return json.RawMessage(out), err
}
func cloneManifest(m Manifest) Manifest {
	m.Dependencies = append([]Ref(nil), m.Dependencies...)
	m.Fixtures = append([]Fixture(nil), m.Fixtures...)
	for i := range m.Fixtures {
		m.Fixtures[i].Input = bytes.Clone(m.Fixtures[i].Input)
		m.Fixtures[i].Expected = bytes.Clone(m.Fixtures[i].Expected)
	}
	return m
}

type FixtureExecutor interface {
	Execute(context.Context, json.RawMessage) (json.RawMessage, error)
}
type FixtureExecutorFunc func(context.Context, json.RawMessage) (json.RawMessage, error)

func (f FixtureExecutorFunc) Execute(ctx context.Context, in json.RawMessage) (json.RawMessage, error) {
	return f(ctx, in)
}

type ConformanceRunner struct{ Executors map[Kind]FixtureExecutor }

func (runner ConformanceRunner) Run(ctx context.Context, registry *Registry) (retErr error) {
	ctx, op := observe.Begin(ctx, "workflow.extension.conformance_run")
	defer func() { observe.Done(op, retErr) }()
	if registry == nil {
		return fmt.Errorf("%w: registry is required", ErrInvalid)
	}
	for _, record := range registry.List() {
		executor := runner.Executors[record.Manifest.Kind]
		if executor == nil {
			return fmt.Errorf("%w: no fixture executor for %s", ErrInvalid, record.Manifest.Kind)
		}
		for _, fixture := range record.Manifest.Fixtures {
			got, err := executor.Execute(ctx, bytes.Clone(fixture.Input))
			if fixture.ExpectedError != "" {
				if err == nil || err.Error() != fixture.ExpectedError {
					return fmt.Errorf("%s fixture %s: expected error %q, got %v", record.Manifest.ID, fixture.ID, fixture.ExpectedError, err)
				}
				continue
			}
			if err != nil {
				return fmt.Errorf("%s fixture %s: %w", record.Manifest.ID, fixture.ID, err)
			}
			want, _ := canonicalJSON(fixture.Expected)
			actual, normErr := canonicalJSON(got)
			if normErr != nil || !bytes.Equal(want, actual) {
				return fmt.Errorf("%s fixture %s: expected %s, got %s", record.Manifest.ID, fixture.ID, want, actual)
			}
		}
	}
	return nil
}
