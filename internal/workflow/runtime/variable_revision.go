package runtime

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

// variableRevisionSchema tags the canonical stream a revision digest covers.
const variableRevisionSchema = "hcmnext.workflow.runtime.WorkflowVariableRevision"

// VariableWrite is one request to change a shared workflow variable. The
// runtime exposes no global mutable map: a value that genuinely evolves
// changes only through a write that names its schema, its writer, its reason
// and its causation (specs/workflow-runtime.md, WF-COMP-007).
type VariableWrite struct {
	VariableName string
	SchemaRef    string
	// Value is the new value. It must be a JSON object; numbers in exponent
	// form are refused because the store's jsonb rendering would not
	// round-trip them to the same digest.
	Value json.RawMessage
	// WriterNodeID is the compiled node whose completion wrote the value.
	WriterNodeID string
	// WriterRef is the execution identity that performed the write (a
	// capability execution, decision or human task reference).
	WriterRef   string
	Reason      string
	CausationID string
}

// VariableRevision is one append-only WorkflowVariableRevision record.
//
// Revisions are dense per instance (1, 2, 3, ...) and hash-chained: each
// carries the digest of the instance's previous revision, and its own digest
// covers every field. PreviousRevision names the prior revision of the same
// variable, so the value of any variable as of any revision is recoverable
// without a second table.
type VariableRevision struct {
	TenantID   uuid.UUID
	InstanceID uuid.UUID
	Revision   int64

	VariableName string
	SchemaRef    string
	Value        json.RawMessage
	ValueDigest  string

	WriterNodeID string
	WriterRef    string
	Reason       string
	CausationID  string

	PreviousRevision int64
	PriorDigest      string
	RevisionDigest   string

	WrittenAt time.Time
}

// canonicalVariableValue renders v as sorted-key compact JSON, refusing
// anything but a JSON object and any number in exponent form.
func canonicalVariableValue(v json.RawMessage) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(v))
	dec.UseNumber()
	var parsed any
	if err := dec.Decode(&parsed); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, errTrailingJSON
	}
	if _, ok := parsed.(map[string]any); !ok {
		return nil, errNotObject
	}
	if hasExponent(parsed) {
		return nil, errExponentNumber
	}
	return json.Marshal(parsed)
}

type variableValueError string

func (e variableValueError) Error() string { return string(e) }

const (
	errTrailingJSON   variableValueError = "variable value carries trailing JSON"
	errNotObject      variableValueError = "variable value must be a JSON object"
	errExponentNumber variableValueError = "variable value numbers must not use exponent form"
)

func hasExponent(v any) bool {
	switch x := v.(type) {
	case json.Number:
		return strings.ContainsAny(string(x), "eE")
	case map[string]any:
		for _, e := range x {
			if hasExponent(e) {
				return true
			}
		}
	case []any:
		for _, e := range x {
			if hasExponent(e) {
				return true
			}
		}
	}
	return false
}

// digest computes the revision digest over every field but the digest itself.
func (r VariableRevision) digest() (string, error) {
	return canonicalbytes.New(variableRevisionSchema, 1).
		String("tenant_id", r.TenantID.String()).
		String("instance_id", r.InstanceID.String()).
		Int("revision", r.Revision).
		String("variable_name", r.VariableName).
		String("schema_ref", r.SchemaRef).
		String("value_digest", r.ValueDigest).
		String("writer_node_id", r.WriterNodeID).
		String("writer_ref", r.WriterRef).
		String("reason", r.Reason).
		String("causation_id", r.CausationID).
		Int("previous_revision", r.PreviousRevision).
		String("prior_digest", r.PriorDigest).
		String("written_at", r.WrittenAt.UTC().Format(time.RFC3339Nano)).
		Digest()
}

// Validate reports whether the revision is complete and its value and
// revision digests still match its content.
func (r VariableRevision) Validate() error {
	id := r.InstanceID.String()
	switch {
	case r.TenantID == uuid.Nil || r.InstanceID == uuid.Nil:
		return refuse(CodeInvalidRecord, id, r.WriterNodeID, "variable revision names no tenant or instance")
	case r.Revision < 1:
		return refuse(CodeInvalidRecord, id, r.WriterNodeID, "variable revision starts at 1")
	case strings.TrimSpace(r.VariableName) == "" || strings.TrimSpace(r.SchemaRef) == "":
		return refuse(CodeInvalidRecord, id, r.WriterNodeID, "a typed variable revision names its variable and schema")
	case strings.TrimSpace(r.WriterNodeID) == "" || strings.TrimSpace(r.WriterRef) == "":
		return refuse(CodeInvalidRecord, id, r.WriterNodeID, "a variable revision names its writer")
	case strings.TrimSpace(r.Reason) == "" || strings.TrimSpace(r.CausationID) == "":
		return refuse(CodeInvalidRecord, id, r.WriterNodeID, "a variable revision names its reason and causation")
	case r.PreviousRevision < 0 || r.PreviousRevision >= r.Revision:
		return refuse(CodeInvalidRecord, id, r.WriterNodeID,
			"previous revision %d does not precede revision %d", r.PreviousRevision, r.Revision)
	case (r.Revision == 1) != (r.PriorDigest == ""):
		return refuse(CodeInvalidRecord, id, r.WriterNodeID, "only revision 1 starts the digest chain")
	case r.WrittenAt.IsZero():
		return refuse(CodeInvalidRecord, id, r.WriterNodeID,
			"written_at must be supplied; this package never reads a wall clock")
	}
	canonical, err := canonicalVariableValue(r.Value)
	if err != nil {
		return wrap(CodeInvalidRecord, id, r.WriterNodeID, err, "variable %s value", r.VariableName)
	}
	if canonicalbytes.Digest(canonical) != r.ValueDigest {
		return refuse(CodeInvalidRecord, id, r.WriterNodeID, "variable %s value no longer matches its digest", r.VariableName)
	}
	want, err := r.digest()
	if err != nil {
		return wrap(CodeInvalidRecord, id, r.WriterNodeID, err, "digest variable revision %d", r.Revision)
	}
	if want != r.RevisionDigest {
		return refuse(CodeInvalidRecord, id, r.WriterNodeID, "variable revision %d no longer matches its digest", r.Revision)
	}
	return nil
}

// VariableRevisionLog is one instance's append-only revision history. It is
// a value owned by its caller: Append only ever extends it, and nothing
// rewrites a revision already held.
type VariableRevisionLog struct {
	tenantID   uuid.UUID
	instanceID uuid.UUID
	revisions  []VariableRevision
}

// NewVariableRevisionLog rebuilds a log from stored revisions, refusing any
// history that is not a dense, correctly chained sequence for exactly this
// instance: a gap, a reorder, a foreign row or an edited row all fail.
func NewVariableRevisionLog(tenantID, instanceID uuid.UUID, stored []VariableRevision) (*VariableRevisionLog, error) {
	l := &VariableRevisionLog{tenantID: tenantID, instanceID: instanceID}
	for _, r := range stored {
		if err := l.admit(r); err != nil {
			return nil, err
		}
	}
	return l, nil
}

// admit appends r after proving it extends the log.
func (l *VariableRevisionLog) admit(r VariableRevision) error {
	id := l.instanceID.String()
	if r.TenantID != l.tenantID || r.InstanceID != l.instanceID {
		return refuse(CodeInvalidRecord, id, r.WriterNodeID, "variable revision belongs to another instance")
	}
	if err := r.Validate(); err != nil {
		return err
	}
	if r.Revision != l.Head()+1 {
		return refuse(CodeInvalidRecord, id, r.WriterNodeID,
			"variable revision %d does not extend head %d", r.Revision, l.Head())
	}
	if r.PriorDigest != l.headDigest() {
		return refuse(CodeInvalidRecord, id, r.WriterNodeID,
			"variable revision %d does not chain to revision %d", r.Revision, l.Head())
	}
	prev, _ := l.Current(r.VariableName)
	if r.PreviousRevision != prev.Revision {
		return refuse(CodeInvalidRecord, id, r.WriterNodeID,
			"variable %s revision %d names previous revision %d, the log holds %d",
			r.VariableName, r.Revision, r.PreviousRevision, prev.Revision)
	}
	l.revisions = append(l.revisions, cloneRevision(r))
	return nil
}

// Head is the instance's variable revision head: 0 before any write.
func (l *VariableRevisionLog) Head() int64 { return int64(len(l.revisions)) }

func (l *VariableRevisionLog) headDigest() string {
	if len(l.revisions) == 0 {
		return ""
	}
	return l.revisions[len(l.revisions)-1].RevisionDigest
}

// Next builds, without appending, the revision w would become at head + 1.
func (l *VariableRevisionLog) Next(w VariableWrite, writtenAt time.Time) (VariableRevision, error) {
	id := l.instanceID.String()
	canonical, err := canonicalVariableValue(w.Value)
	if err != nil {
		return VariableRevision{}, wrap(CodeInvalidRecord, id, w.WriterNodeID, err, "variable %s value", w.VariableName)
	}
	prev, _ := l.Current(w.VariableName)
	r := VariableRevision{
		TenantID:         l.tenantID,
		InstanceID:       l.instanceID,
		Revision:         l.Head() + 1,
		VariableName:     w.VariableName,
		SchemaRef:        w.SchemaRef,
		Value:            json.RawMessage(canonical),
		ValueDigest:      canonicalbytes.Digest(canonical),
		WriterNodeID:     w.WriterNodeID,
		WriterRef:        w.WriterRef,
		Reason:           w.Reason,
		CausationID:      w.CausationID,
		PreviousRevision: prev.Revision,
		PriorDigest:      l.headDigest(),
		// The store keeps microseconds; truncating here keeps the digest a
		// function of what a read returns.
		WrittenAt: writtenAt.UTC().Truncate(time.Microsecond),
	}
	if r.RevisionDigest, err = r.digest(); err != nil {
		return VariableRevision{}, wrap(CodeInvalidRecord, id, w.WriterNodeID, err, "digest variable revision")
	}
	if err := r.Validate(); err != nil {
		return VariableRevision{}, err
	}
	return r, nil
}

// Append builds the next revision for w and appends it.
func (l *VariableRevisionLog) Append(w VariableWrite, writtenAt time.Time) (VariableRevision, error) {
	r, err := l.Next(w, writtenAt)
	if err != nil {
		return VariableRevision{}, err
	}
	if err := l.admit(r); err != nil {
		return VariableRevision{}, err
	}
	return cloneRevision(r), nil
}

// Current returns the latest revision of name.
func (l *VariableRevisionLog) Current(name string) (VariableRevision, bool) {
	return l.AsOf(name, l.Head())
}

// AsOf returns the revision of name in force at instance revision at.
func (l *VariableRevisionLog) AsOf(name string, at int64) (VariableRevision, bool) {
	for i := len(l.revisions) - 1; i >= 0; i-- {
		r := l.revisions[i]
		if r.Revision <= at && r.VariableName == name {
			return cloneRevision(r), true
		}
	}
	return VariableRevision{}, false
}

// Revisions returns a copy of the whole history in revision order.
func (l *VariableRevisionLog) Revisions() []VariableRevision {
	out := make([]VariableRevision, len(l.revisions))
	for i, r := range l.revisions {
		out[i] = cloneRevision(r)
	}
	return out
}

func cloneRevision(r VariableRevision) VariableRevision {
	r.Value = append(json.RawMessage(nil), r.Value...)
	return r
}
