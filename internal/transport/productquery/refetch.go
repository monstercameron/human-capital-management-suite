package productquery

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ALIGN-024: an invalidation is a hint, never data. A surface holding an
// authorized projection view reacts to one by marking the named subjects dirty
// and refusing to present the view as current until an authoritative refetch
// through Project has caught up with the source sequence the hint announced.
// Nothing in a hint -- not a revision, not a subject -- ever becomes a
// displayed value, and a refetch that has not yet applied the announced source
// position is refused rather than silently accepted as fresh.

// DisplayState is how a surface may present a view.
type DisplayState string

const (
	// DisplayCurrent: the view reflects every invalidation it has received.
	DisplayCurrent DisplayState = "CURRENT"
	// DisplayRefetchRequired: an invalidation arrived that the view has not
	// yet caught up with; its values must not be presented as current.
	DisplayRefetchRequired DisplayState = "REFETCH_REQUIRED"
)

// Refetch refusals.
var (
	ErrRefetchStale    = errors.New("productquery: refetched projection has not applied the invalidated source position")
	ErrRefetchMismatch = errors.New("productquery: refetched envelope belongs to another tenant or projection")
)

// View is a surface's retained copy of one authorized envelope plus what it
// has been told since.
type View struct {
	tenant     values.TenantId
	projection string
	envelope   Envelope
	// required is the highest source sequence an accepted invalidation
	// announced; a refetch must reach it.
	required uint64
	dirty    map[string]uint64
}

// NewView retains an authorized envelope.
func NewView(e Envelope) (*View, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	return &View{tenant: e.Tenant, projection: e.Projection.Name, envelope: e, required: e.Projection.Watermark, dirty: map[string]uint64{}}, nil
}

// RefetchPlan is what a surface must do after an invalidation: refetch
// Projection until its watermark reaches MinWatermark. Subjects lists the
// dirty subject references in canonical order.
type RefetchPlan struct {
	Projection   string   `json:"projection"`
	MinWatermark uint64   `json:"min_watermark"`
	Subjects     []string `json:"subjects"`
}

// Required reports whether the plan demands a refetch.
func (p RefetchPlan) Required() bool { return len(p.Subjects) > 0 }

// Apply records an invalidation hint. A hint for another tenant or projection,
// or one no newer than what the view already reflects, changes nothing. Apply
// never touches the retained rows.
func (v *View) Apply(m InvalidationMessage) (RefetchPlan, error) {
	if err := m.Validate(); err != nil {
		return RefetchPlan{}, err
	}
	if m.Tenant == v.tenant && m.Projection == v.projection && m.SourceSequence > v.envelope.Projection.Watermark {
		for _, item := range m.Items {
			key := item.Subject.String()
			if item.Revision > v.dirty[key] {
				v.dirty[key] = item.Revision
			}
		}
		if m.SourceSequence > v.required {
			v.required = m.SourceSequence
		}
	}
	return v.Plan(), nil
}

// Plan returns the outstanding refetch obligation.
func (v *View) Plan() RefetchPlan {
	plan := RefetchPlan{Projection: v.projection, Subjects: []string{}}
	if len(v.dirty) == 0 {
		return plan
	}
	plan.MinWatermark = v.required
	for key := range v.dirty {
		plan.Subjects = append(plan.Subjects, key)
	}
	sort.Strings(plan.Subjects)
	return plan
}

// State reports how the view may be presented.
func (v *View) State() DisplayState {
	if len(v.dirty) > 0 {
		return DisplayRefetchRequired
	}
	return DisplayCurrent
}

// Envelope returns the retained envelope and its display state. A caller that
// presents rows while the state is REFETCH_REQUIRED must mark them stale.
func (v *View) Envelope() (Envelope, DisplayState) {
	return v.envelope, v.State()
}

// Accept replaces the retained envelope with an authoritative refetch. The
// refetch must be for the same tenant and projection, must itself validate,
// and must have applied at least the source position every received
// invalidation announced; otherwise it is refused and the view stays dirty.
func (v *View) Accept(e Envelope) error {
	if err := e.Validate(); err != nil {
		return err
	}
	if e.Tenant != v.tenant || e.Projection.Name != v.projection {
		return ErrRefetchMismatch
	}
	if e.Projection.Watermark < v.required {
		return fmt.Errorf("%w: watermark %d < required %d", ErrRefetchStale, e.Projection.Watermark, v.required)
	}
	v.envelope, v.required, v.dirty = e, e.Projection.Watermark, map[string]uint64{}
	return nil
}

// CanonicalBytes is the stable encoding of a refetch plan.
func (p RefetchPlan) CanonicalBytes() ([]byte, error) {
	cp := p
	cp.Subjects = append([]string{}, p.Subjects...)
	sort.Strings(cp.Subjects)
	return json.Marshal(cp)
}
