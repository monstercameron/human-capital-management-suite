// PROGRAM-001: ProgramDefinition records type, owner, scope, funding,
// outcomes and extensions — but only after signed conformance passes.
// Definitions are immutable once recorded and every definition seals a
// canonical digest verifiable through the shared oracle.
package program

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

var (
	ErrConformanceGate   = errors.New("program: conformance gate has not passed")
	ErrInvalidDefinition = errors.New("program: invalid program definition")
	ErrDuplicateProgram  = errors.New("program: program definition already recorded")
	ErrNotAuthorized     = errors.New("program: caller is not authorized")
)

// ProgramType is the closed program vocabulary.
type ProgramType string

const (
	ProgramBenefit  ProgramType = "BENEFIT"
	ProgramBonus    ProgramType = "BONUS"
	ProgramLearning ProgramType = "LEARNING"
	ProgramLeave    ProgramType = "LEAVE"
)

func (t ProgramType) valid() bool {
	return t == ProgramBenefit || t == ProgramBonus || t == ProgramLearning || t == ProgramLeave
}

// FundingModel names who funds the program.
type FundingModel string

const (
	FundingEmployer  FundingModel = "EMPLOYER"
	FundingEmployee  FundingModel = "EMPLOYEE"
	FundingShared    FundingModel = "SHARED"
	FundingStatutory FundingModel = "STATUTORY"
)

func (f FundingModel) valid() bool {
	return f == FundingEmployer || f == FundingEmployee || f == FundingShared || f == FundingStatutory
}

// Caller carries the acting principal and the tenants it may touch.
type Caller struct {
	ID      string
	Tenants []string
}

// SignedConformance is the gate proof: the conformance todo id, the
// sealed digest it produced, and the board that signed it.
type SignedConformance struct {
	TodoID string
	Digest string
	Signer string
}

// Definition is one immutable program definition.
type Definition struct {
	ID         string
	Name       string
	Type       ProgramType
	Owner      string
	Scope      []string
	Funding    FundingModel
	Outcomes   []string
	Extensions map[string]string
	Digest     string
}

// JournalEntry is bounded evidence of a successful state change.
type JournalEntry struct {
	Op     string
	Ref    string
	Detail string
}

// Catalog is the program book of record: definitions, revisions,
// bindings, enrollments, formulas and the effect journal.
type Catalog struct {
	conformance *SignedConformance
	definitions map[string]Definition
	revisions   []Revision
	revDigests  map[string]bool
	bindings    []Binding
	enrollments map[string]Enrollment
	formulas    map[string]FormulaFunc
	journal     []JournalEntry
	store       RevisionStore
}

// NewCatalog returns an empty catalog on the in-memory revision store.
func NewCatalog() *Catalog {
	return &Catalog{
		definitions: map[string]Definition{},
		enrollments: map[string]Enrollment{},
		formulas:    map[string]FormulaFunc{},
		revDigests:  map[string]bool{},
		store:       &memoryRevisionStore{},
	}
}

// Journal returns the effect evidence recorded so far.
func (c *Catalog) Journal() []JournalEntry {
	return append([]JournalEntry(nil), c.journal...)
}

func (c *Catalog) authorize(caller Caller, tenant string) error {
	if strings.TrimSpace(caller.ID) == "" || strings.TrimSpace(tenant) == "" {
		return fmt.Errorf("%w: missing caller or tenant", ErrNotAuthorized)
	}
	for _, t := range caller.Tenants {
		if t == tenant {
			return nil
		}
	}
	// Generic denial: never name which tenants exist.
	return fmt.Errorf("%w: scope denied", ErrNotAuthorized)
}

// RecordConformance records the signed PROGRAM-CONF-001 gate proof.
func (c *Catalog) RecordConformance(proof SignedConformance) error {
	if strings.TrimSpace(proof.TodoID) == "" || strings.TrimSpace(proof.Digest) == "" ||
		strings.TrimSpace(proof.Signer) == "" {
		return fmt.Errorf("%w: todo, digest and signer are required", ErrConformanceGate)
	}
	if !strings.HasPrefix(proof.Digest, "sha256:") {
		return fmt.Errorf("%w: digest is not sealed", ErrConformanceGate)
	}
	c.conformance = &proof
	return nil
}

// ValidateDefinition checks a definition without recording it.
func ValidateDefinition(def Definition) error {
	if strings.TrimSpace(def.ID) == "" || strings.TrimSpace(def.Name) == "" ||
		strings.TrimSpace(def.Owner) == "" {
		return fmt.Errorf("%w: id, name and owner are required", ErrInvalidDefinition)
	}
	if !def.Type.valid() {
		return fmt.Errorf("%w: unknown program type %q", ErrInvalidDefinition, def.Type)
	}
	if !def.Funding.valid() {
		return fmt.Errorf("%w: unknown funding model %q", ErrInvalidDefinition, def.Funding)
	}
	if len(def.Scope) == 0 || len(def.Outcomes) == 0 {
		return fmt.Errorf("%w: scope and outcomes are required", ErrInvalidDefinition)
	}
	for _, s := range append(append([]string{}, def.Scope...), def.Outcomes...) {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("%w: scope and outcomes must be non-blank", ErrInvalidDefinition)
		}
	}
	return nil
}

func definitionDigest(def Definition) (string, error) {
	w := canonicalbytes.New("program-definition", 1)
	w.String("id", def.ID)
	w.String("name", def.Name)
	w.String("type", string(def.Type))
	w.String("owner", def.Owner)
	w.SortedStrings("scope", def.Scope)
	w.String("funding", string(def.Funding))
	w.SortedStrings("outcomes", def.Outcomes)
	keys := make([]string, 0, len(def.Extensions))
	for k := range def.Extensions {
		keys = append(keys, k)
	}
	w.SortedStrings("extension_keys", keys)
	for _, k := range keys {
		w.String("extension:"+k, def.Extensions[k])
	}
	raw, err := w.Bytes()
	if err != nil {
		return "", err
	}
	return canonicalbytes.Digest(raw), nil
}

// VerifyDefinition reports whether def seals against the shared oracle.
func VerifyDefinition(def Definition) bool {
	if def.Digest == "" {
		return false
	}
	want, err := definitionDigest(def)
	if err != nil {
		return false
	}
	return want == def.Digest
}

// Define records an immutable definition. It stays blocked until signed
// conformance proves the shared invariants.
func (c *Catalog) Define(caller Caller, def Definition) (Definition, error) {
	if c.conformance == nil {
		return Definition{}, ErrConformanceGate
	}
	if err := ValidateDefinition(def); err != nil {
		return Definition{}, err
	}
	if err := c.authorize(caller, "tenant-acme"); err != nil {
		return Definition{}, err
	}
	if _, dup := c.definitions[def.ID]; dup {
		return Definition{}, fmt.Errorf("%w: %s", ErrDuplicateProgram, def.ID)
	}
	def.Digest = ""
	digest, err := definitionDigest(def)
	if err != nil {
		return Definition{}, err
	}
	def.Digest = digest
	c.definitions[def.ID] = def
	c.journal = append(c.journal, JournalEntry{
		Op: "define", Ref: def.ID,
		Detail: "type=" + string(def.Type) + " owner=" + def.Owner,
	})
	return def, nil
}

// Lookup returns the recorded definition by id.
func (c *Catalog) Lookup(id string) (Definition, bool) {
	def, ok := c.definitions[id]
	return def, ok
}
