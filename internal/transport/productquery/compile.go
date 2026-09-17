package productquery

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/queryenvelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// Compile-time errors. A compiled query never widens the envelope it was
// built from: denied and withheld fields are dropped uniformly so the plan
// cannot become an existence oracle, and an envelope without an authorized
// subject compiles to nothing.
var (
	// ErrCompileUnauthorized is returned when the envelope carries no
	// authorized subject: there is no grant to compile into a query.
	ErrCompileUnauthorized = errors.New("productquery: envelope authorizes no subject to compile")
	// ErrCompileInvalid is returned when the envelope or projection would
	// make the compiled query ambiguous, unbounded, or unexecutable.
	ErrCompileInvalid = errors.New("productquery: cannot compile field dispositions into a product query")
)

// CompiledQuery is the bounded executable plan a repository adapter runs for
// one authorized envelope. ReadFields name the fields the adapter may read
// as values; RedactedFields name the fields the adapter may consult for
// presence only and must never read as values. Denied and withheld fields
// are absent entirely. The plan carries no row values.
type CompiledQuery struct {
	ContractVersion int                `json:"contract_version"`
	SliceID         string             `json:"slice_id"`
	SliceVersion    int                `json:"slice_version"`
	QueryID         string             `json:"query_id"`
	Resource        string             `json:"resource"`
	Tenant          values.TenantId    `json:"tenant"`
	Purpose         string             `json:"purpose"`
	ReadAt          values.Instant     `json:"read_at"`
	MaxRows         int                `json:"max_rows"`
	Subjects        []values.EntityRef `json:"subjects"`
	ReadFields      []authz.FieldID    `json:"read_fields"`
	RedactedFields  []authz.FieldID    `json:"redacted_fields"`
	PolicyVersion   string             `json:"policy_version"`
	EvidenceID      string             `json:"evidence_id"`
	Projection      Projection         `json:"projection"`
}

// Compile turns the field dispositions of one authorized envelope into the
// bounded query an adapter executes against the named projection. The
// projection supplies the watermark and sequence the reads are bound to;
// every other authority input stays with the envelope.
func Compile(env queryenvelope.Envelope, proj Projection) (CompiledQuery, error) {
	if err := env.Validate(); err != nil {
		return CompiledQuery{}, fmt.Errorf("%w: envelope: %v", ErrCompileInvalid, err)
	}
	if !env.Authorized() {
		return CompiledQuery{}, fmt.Errorf("%w: slice %s query %s", ErrCompileUnauthorized, env.SliceID, env.QueryID)
	}
	if err := proj.validate(); err != nil {
		return CompiledQuery{}, fmt.Errorf("%w: projection: %v", ErrCompileInvalid, err)
	}
	if len(env.Subjects) > MaxCandidates {
		return CompiledQuery{}, fmt.Errorf("%w: %d authorized subjects exceed %d", ErrCompileInvalid, len(env.Subjects), MaxCandidates)
	}
	subjects := append([]values.EntityRef(nil), env.Subjects...)
	sort.SliceStable(subjects, func(i, j int) bool { return subjects[i].String() < subjects[j].String() })
	var reads, redacted []authz.FieldID
	for _, disposition := range env.Fields {
		switch disposition.Effect {
		case authz.EffectAllow:
			reads = append(reads, disposition.Field)
		case authz.EffectRedacted:
			redacted = append(redacted, disposition.Field)
		default:
			// Denied, withheld, and any future negative effect are
			// dropped uniformly: the plan never names them.
		}
	}
	sort.Slice(reads, func(i, j int) bool { return reads[i] < reads[j] })
	sort.Slice(redacted, func(i, j int) bool { return redacted[i] < redacted[j] })
	if len(reads)+len(redacted) > MaxFields {
		return CompiledQuery{}, fmt.Errorf("%w: %d compiled fields exceed %d", ErrCompileInvalid, len(reads)+len(redacted), MaxFields)
	}
	q := CompiledQuery{
		ContractVersion: contractVersion,
		SliceID:         env.SliceID,
		SliceVersion:    env.SliceVersion,
		QueryID:         env.QueryID,
		Resource:        env.Resource,
		Tenant:          env.Tenant,
		Purpose:         env.Purpose,
		ReadAt:          env.ReadAt,
		MaxRows:         env.MaxRows,
		Subjects:        subjects,
		ReadFields:      reads,
		RedactedFields:  redacted,
		PolicyVersion:   env.PolicyVersion,
		EvidenceID:      env.EvidenceID,
		Projection:      proj,
	}
	if err := q.Validate(); err != nil {
		return CompiledQuery{}, err
	}
	return q, nil
}

// Validate checks the executable shape before an adapter is allowed to run
// it. It re-applies the envelope invariants that matter at execution time so
// a decoded or stored plan cannot drift past them.
func (q CompiledQuery) Validate() error {
	if q.ContractVersion != contractVersion {
		return fmt.Errorf("%w: unsupported compiled version %d", ErrCompileInvalid, q.ContractVersion)
	}
	if !boundedToken(q.SliceID, MaxProjectionNameSize) || q.SliceVersion < 1 || !boundedToken(q.QueryID, MaxProjectionNameSize) || !boundedToken(q.Resource, MaxProjectionNameSize) {
		return fmt.Errorf("%w: slice, query, or resource identity is invalid", ErrCompileInvalid)
	}
	if err := q.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %v", ErrCompileInvalid, err)
	}
	if !boundedToken(q.Purpose, MaxProjectionNameSize) || !boundedToken(q.PolicyVersion, MaxProjectionNameSize) || !boundedToken(q.EvidenceID, MaxProjectionNameSize) {
		return fmt.Errorf("%w: authorization evidence is incomplete", ErrCompileInvalid)
	}
	if err := q.ReadAt.Validate(); err != nil {
		return fmt.Errorf("%w: read instant: %v", ErrCompileInvalid, err)
	}
	if q.MaxRows < 1 || q.MaxRows > 1000 {
		return fmt.Errorf("%w: max_rows %d is outside [1,1000]", ErrCompileInvalid, q.MaxRows)
	}
	if len(q.Subjects) == 0 || len(q.Subjects) > q.MaxRows || len(q.Subjects) > MaxCandidates {
		return fmt.Errorf("%w: %d compiled subjects are outside executable bounds", ErrCompileInvalid, len(q.Subjects))
	}
	previous := ""
	for _, subject := range q.Subjects {
		if err := subject.Validate(); err != nil {
			return fmt.Errorf("%w: subject: %v", ErrCompileInvalid, err)
		}
		if subject.Tenant != q.Tenant {
			return fmt.Errorf("%w: subject crosses tenant boundary", ErrCompileInvalid)
		}
		key := subject.String()
		if previous != "" && key <= previous {
			return fmt.Errorf("%w: compiled subjects are not unique and sorted", ErrCompileInvalid)
		}
		previous = key
	}
	if len(q.ReadFields)+len(q.RedactedFields) == 0 || len(q.ReadFields)+len(q.RedactedFields) > MaxFields {
		return fmt.Errorf("%w: compiled field count is outside executable bounds", ErrCompileInvalid)
	}
	seen := make(map[authz.FieldID]bool, len(q.ReadFields)+len(q.RedactedFields))
	for _, id := range q.ReadFields {
		if id == "" || seen[id] {
			return fmt.Errorf("%w: compiled read field %q is invalid", ErrCompileInvalid, id)
		}
		seen[id] = true
	}
	for _, id := range q.RedactedFields {
		if id == "" || seen[id] {
			return fmt.Errorf("%w: compiled redacted field %q is invalid", ErrCompileInvalid, id)
		}
		seen[id] = true
	}
	if !sortedFieldIDs(q.ReadFields) || !sortedFieldIDs(q.RedactedFields) {
		return fmt.Errorf("%w: compiled fields are not canonical", ErrCompileInvalid)
	}
	if err := q.Projection.validate(); err != nil {
		return fmt.Errorf("%w: projection: %v", ErrCompileInvalid, err)
	}
	return nil
}

func sortedFieldIDs(ids []authz.FieldID) bool {
	for i := 1; i < len(ids); i++ {
		if ids[i-1] >= ids[i] {
			return false
		}
	}
	return true
}

// CanonicalBytes is the cross-surface encoding of the executable plan. It
// carries references and dispositions only, never row values.
func (q CompiledQuery) CanonicalBytes() ([]byte, error) {
	clone := q
	clone.Subjects = append([]values.EntityRef(nil), q.Subjects...)
	sort.SliceStable(clone.Subjects, func(i, j int) bool { return clone.Subjects[i].String() < clone.Subjects[j].String() })
	if clone.Subjects == nil {
		clone.Subjects = []values.EntityRef{}
	}
	clone.ReadFields = append([]authz.FieldID(nil), q.ReadFields...)
	sort.Slice(clone.ReadFields, func(i, j int) bool { return clone.ReadFields[i] < clone.ReadFields[j] })
	if clone.ReadFields == nil {
		clone.ReadFields = []authz.FieldID{}
	}
	clone.RedactedFields = append([]authz.FieldID(nil), q.RedactedFields...)
	sort.Slice(clone.RedactedFields, func(i, j int) bool { return clone.RedactedFields[i] < clone.RedactedFields[j] })
	if clone.RedactedFields == nil {
		clone.RedactedFields = []authz.FieldID{}
	}
	return json.Marshal(clone)
}

// Digest identifies the executable plan independent of input ordering.
func (q CompiledQuery) Digest() string {
	b, err := q.CanonicalBytes()
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Explain returns a bounded, value-free summary of the executable plan. It
// names no field value, subject payload, or storage relation.
func (q CompiledQuery) Explain() string {
	return fmt.Sprintf("compiled product query v%d %s@%d with %d subject(s), %d read field(s), %d redacted field(s)", q.ContractVersion, q.SliceID, q.SliceVersion, len(q.Subjects), len(q.ReadFields), len(q.RedactedFields))
}
