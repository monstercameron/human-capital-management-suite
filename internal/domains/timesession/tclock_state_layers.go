package timesession

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// StateLayerSchemaVersion identifies the wire shape used by a correction layer.
const StateLayerSchemaVersion = 1

// Layer errors are stable for callers that need to route a correction failure.
var (
	ErrLayerInvalid       = errors.New("timesession: correction layer is invalid")
	ErrLayerCAS           = errors.New("timesession: correction layer compare-and-swap failed")
	ErrLayerDuplicate     = errors.New("timesession: correction layer revision already exists")
	ErrLayerExactReplay   = errors.New("timesession: correction layer was already applied")
	ErrLayerScope         = errors.New("timesession: correction layer scope does not match state")
	ErrLayerUnapproved    = errors.New("timesession: correction layer is not approved")
	ErrLayerPathForbidden = errors.New("timesession: correction path is forbidden")
	ErrLayerPathConflict  = errors.New("timesession: correction contains a conflicting path")
)

// LayerValueKind is the small typed value vocabulary supported by state paths.
type LayerValueKind string

const (
	LayerValueTime   LayerValueKind = "time"
	LayerValueString LayerValueKind = "string"
	LayerValueNull   LayerValueKind = "null"
)

// LayerValue is a typed value. Null explicitly removes an optional field.
type LayerValue struct {
	Kind   LayerValueKind
	Time   time.Time
	String string
}

// TimeValue creates a time-valued patch.
func TimeValue(v time.Time) LayerValue { return LayerValue{Kind: LayerValueTime, Time: v.UTC()} }

// StringValue creates a string-valued patch.
func StringValue(v string) LayerValue { return LayerValue{Kind: LayerValueString, String: v} }

// NullValue creates a deletion patch for an optional field.
func NullValue() LayerValue { return LayerValue{Kind: LayerValueNull} }

func (v LayerValue) valid() bool {
	switch v.Kind {
	case LayerValueTime:
		return !v.Time.IsZero() && v.String == ""
	case LayerValueString:
		return v.Time.IsZero()
	case LayerValueNull:
		return v.Time.IsZero() && v.String == ""
	default:
		return false
	}
}

// StateLayerPatch changes one versioned field path.
type StateLayerPatch struct {
	Path  string
	Value LayerValue
}

// WorkflowProof binds a correction to an exact workflow execution point.
type WorkflowProof struct {
	InstanceID string
	PlanID     string // legacy source identifier; PlanDigest is authoritative.
	PlanDigest string
	TraceID    string
	NodeID     string
	Attempt    uint64
	Version    uint64
}

// LayerProvenance identifies who approved a layer and the workflow proof used.
type LayerProvenance struct {
	Actor         string
	Reason        string
	WorkflowProof WorkflowProof
}

// StateCorrectionLayer is an immutable, approved overlay over base facts.
type StateCorrectionLayer struct {
	Tenant         string
	Subject        string
	BaseRevision   uint64
	ParentRevision uint64
	Revision       uint64
	Approved       bool
	Provenance     LayerProvenance
	Patches        []StateLayerPatch
	Digest         string
}

// TimeSessionBaseFacts are immutable facts supplied by the clock source.
type TimeSessionBaseFacts struct {
	Tenant       string
	Subject      string
	BaseRevision uint64
	ClockOutAt   time.Time
	Reason       string
}

// LayeredState retains immutable base facts and append-only correction layers.
type LayeredState struct {
	Base   TimeSessionBaseFacts
	Layers []StateCorrectionLayer
}

// ResolvedTimeSession is a state view at a requested layer revision.
type ResolvedTimeSession struct {
	Tenant     string
	Subject    string
	Revision   uint64
	ClockOutAt time.Time
	Reason     string
	Extensions map[string]LayerValue
}

// FieldProvenance records the layer that supplied a resolved path.
type FieldProvenance struct {
	Path          string
	LayerRevision uint64
	Digest        string
	Actor         string
	Reason        string
	WorkflowProof WorkflowProof
}

// NewLayeredState validates and creates a state with no correction overlays.
func NewLayeredState(base TimeSessionBaseFacts) (LayeredState, error) {
	if strings.TrimSpace(base.Tenant) == "" || strings.TrimSpace(base.Subject) == "" || base.BaseRevision == 0 {
		return LayeredState{}, fmt.Errorf("%w: tenant, subject and positive base revision are required", ErrLayerInvalid)
	}
	return LayeredState{Base: base}, nil
}

// Digest returns the canonical digest binding schema, base version and patches.
func (l StateCorrectionLayer) DigestValue() string { return layerDigest(l) }

// AppendLayer appends one approved layer after checking scope, digest and CAS.
// The receiver is unchanged on every error.
func (s LayeredState) AppendLayer(layer StateCorrectionLayer) (LayeredState, error) {
	if err := s.validate(); err != nil {
		return s, err
	}
	if err := validateLayer(layer); err != nil {
		return s, err
	}
	if !layer.Approved {
		return s, ErrLayerUnapproved
	}
	if layer.Tenant != s.Base.Tenant || layer.Subject != s.Base.Subject || layer.BaseRevision != s.Base.BaseRevision {
		return s, ErrLayerScope
	}
	if layer.Digest != layerDigest(layer) {
		return s, fmt.Errorf("%w: digest mismatch", ErrLayerInvalid)
	}
	current := uint64(len(s.Layers))
	if layer.ParentRevision != current || layer.Revision != current+1 {
		if layer.Revision > 0 && layer.Revision <= current && s.Layers[layer.Revision-1].Digest == layer.Digest {
			return s, ErrLayerExactReplay
		}
		return s, fmt.Errorf("%w: parent=%d current=%d revision=%d", ErrLayerCAS, layer.ParentRevision, current, layer.Revision)
	}
	next := s
	next.Layers = append(append([]StateCorrectionLayer(nil), s.Layers...), cloneLayer(layer))
	return next, nil
}

// Resolve returns the approved state as of revision. Revision zero means base.
func (s LayeredState) Resolve(revision uint64) (ResolvedTimeSession, error) {
	if err := s.validate(); err != nil {
		return ResolvedTimeSession{}, err
	}
	if revision > uint64(len(s.Layers)) {
		return ResolvedTimeSession{}, ErrLayerCAS
	}
	r := ResolvedTimeSession{Tenant: s.Base.Tenant, Subject: s.Base.Subject, Revision: revision, ClockOutAt: s.Base.ClockOutAt, Reason: s.Base.Reason, Extensions: map[string]LayerValue{}}
	for _, layer := range s.Layers[:revision] {
		for _, patch := range layer.Patches {
			applyPatch(&r, patch)
		}
	}
	return r, nil
}

func (s LayeredState) validate() error {
	if strings.TrimSpace(s.Base.Tenant) == "" || strings.TrimSpace(s.Base.Subject) == "" || s.Base.BaseRevision == 0 {
		return ErrLayerInvalid
	}
	for i, layer := range s.Layers {
		if err := validateLayer(layer); err != nil {
			return err
		}
		if !layer.Approved || layer.Tenant != s.Base.Tenant || layer.Subject != s.Base.Subject || layer.BaseRevision != s.Base.BaseRevision || layer.Revision != uint64(i+1) || layer.ParentRevision != uint64(i) || layer.Digest != layerDigest(layer) {
			return ErrLayerInvalid
		}
	}
	return nil
}

// Get returns a typed value and its provenance at the requested revision.
func (s LayeredState) Get(path string, revision uint64) (LayerValue, FieldProvenance, error) {
	if !validPath(path) {
		return LayerValue{}, FieldProvenance{}, ErrLayerPathForbidden
	}
	r, err := s.Resolve(revision)
	if err != nil {
		return LayerValue{}, FieldProvenance{}, err
	}
	value, found := valueAt(r, path)
	var p FieldProvenance
	if path == "clock_out_at" && !s.Base.ClockOutAt.IsZero() {
		p = FieldProvenance{Path: path}
	}
	if path == "reason" && s.Base.Reason != "" {
		p = FieldProvenance{Path: path}
	}
	for _, layer := range s.Layers[:revision] {
		for _, patch := range layer.Patches {
			if patch.Path == path {
				p = FieldProvenance{Path: path, LayerRevision: layer.Revision, Digest: layer.Digest, Actor: layer.Provenance.Actor, Reason: layer.Provenance.Reason, WorkflowProof: layer.Provenance.WorkflowProof}
			}
		}
	}
	if !found {
		return NullValue(), p, nil
	}
	return value, p, nil
}

func validateLayer(l StateCorrectionLayer) error {
	proof := l.Provenance.WorkflowProof
	if l.Revision == 0 || l.BaseRevision == 0 || strings.TrimSpace(l.Tenant) == "" || strings.TrimSpace(l.Subject) == "" || strings.TrimSpace(l.Provenance.Actor) == "" || strings.TrimSpace(l.Provenance.Reason) == "" || !validUUID(proof.InstanceID) || strings.TrimSpace(proof.PlanDigest) == "" || strings.TrimSpace(proof.TraceID) == "" || strings.TrimSpace(proof.NodeID) == "" || proof.Attempt == 0 || proof.Version == 0 || len(l.Patches) == 0 {
		return ErrLayerInvalid
	}
	seen := map[string]bool{}
	for _, p := range l.Patches {
		if !validPath(p.Path) {
			return ErrLayerPathForbidden
		}
		if !p.Value.valid() {
			return ErrLayerInvalid
		}
		if !valueAllowed(p.Path, p.Value) {
			return fmt.Errorf("%w: value kind %q is not valid for %s", ErrLayerInvalid, p.Value.Kind, p.Path)
		}
		if seen[p.Path] {
			return ErrLayerPathConflict
		}
		seen[p.Path] = true
	}
	return nil
}

func valueAllowed(path string, value LayerValue) bool {
	if value.Kind == LayerValueNull {
		return true
	}
	if path == "clock_out_at" {
		return value.Kind == LayerValueTime
	}
	if path == "reason" {
		return value.Kind == LayerValueString
	}
	return true
}

func validPath(path string) bool {
	if path == "clock_out_at" || path == "reason" {
		return true
	}
	if !strings.HasPrefix(path, "x.") || len(path) <= 2 || strings.ContainsAny(path, " /\\") {
		return false
	}
	for _, forbidden := range []string{"tenant", "subject", "workflow", "actor", "proof", "revision", "digest"} {
		if strings.Contains(strings.ToLower(path), forbidden) {
			return false
		}
	}
	return true
}

func validUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, r := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if r != '-' {
				return false
			}
			continue
		}
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

func applyPatch(r *ResolvedTimeSession, p StateLayerPatch) {
	switch p.Path {
	case "clock_out_at":
		if p.Value.Kind == LayerValueNull {
			r.ClockOutAt = time.Time{}
		} else {
			r.ClockOutAt = p.Value.Time.UTC()
		}
	case "reason":
		if p.Value.Kind == LayerValueNull {
			r.Reason = ""
		} else {
			r.Reason = p.Value.String
		}
	default:
		if p.Value.Kind == LayerValueNull {
			delete(r.Extensions, p.Path)
		} else {
			r.Extensions[p.Path] = p.Value
		}
	}
}

func valueAt(r ResolvedTimeSession, path string) (LayerValue, bool) {
	switch path {
	case "clock_out_at":
		if r.ClockOutAt.IsZero() {
			return LayerValue{}, false
		}
		return TimeValue(r.ClockOutAt), true
	case "reason":
		if r.Reason == "" {
			return LayerValue{}, false
		}
		return StringValue(r.Reason), true
	default:
		v, ok := r.Extensions[path]
		return v, ok
	}
}

func cloneLayer(l StateCorrectionLayer) StateCorrectionLayer {
	l.Patches = append([]StateLayerPatch(nil), l.Patches...)
	return l
}

func layerDigest(l StateCorrectionLayer) string {
	type item struct {
		Path  string
		Value LayerValue
	}
	items := make([]item, len(l.Patches))
	for i, p := range l.Patches {
		items[i] = item{p.Path, p.Value}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	h := sha256.New()
	write := func(v string) { fmt.Fprintf(h, "%d:%s;", len(v), v) }
	write(fmt.Sprint(StateLayerSchemaVersion))
	write(fmt.Sprint(l.BaseRevision))
	write(l.Tenant)
	write(l.Subject)
	write(l.Provenance.Actor)
	write(l.Provenance.Reason)
	write(l.Provenance.WorkflowProof.InstanceID)
	write(l.Provenance.WorkflowProof.PlanID)
	write(l.Provenance.WorkflowProof.PlanDigest)
	write(l.Provenance.WorkflowProof.TraceID)
	write(l.Provenance.WorkflowProof.NodeID)
	write(fmt.Sprint(l.Provenance.WorkflowProof.Attempt))
	write(fmt.Sprint(l.Provenance.WorkflowProof.Version))
	for _, p := range items {
		write(p.Path)
		write(string(p.Value.Kind))
		switch p.Value.Kind {
		case LayerValueTime:
			write(p.Value.Time.UTC().Format(time.RFC3339Nano))
		case LayerValueString:
			write(p.Value.String)
		case LayerValueNull:
			write("null")
		}
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
