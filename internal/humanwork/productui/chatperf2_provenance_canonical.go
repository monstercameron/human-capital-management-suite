//go:build !(js && wasm)

package productui

import (
	"time"

	evidencev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/evidence/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// CHATBUG-014: this file is not part of the browser client. Filling a
// projection from the canonical evidence messages happens where those messages
// are, on the server; in the client it was the only use of the evidence
// messages, and it kept the whole generated package (about 100 KB of code and
// its descriptors) in a bundle that was over its size ceiling.

// ProjectAuthorizedCanonicalProvenance copies only displayable fields from
// canonical service messages that have already passed authorization. PolicyRef
// and EvidenceRef are deliberately not copied, so this presentation type cannot
// accidentally render them later.
func ProjectAuthorizedCanonicalProvenance(authority *evidencev1.SourceAuthority, evidence *evidencev1.Provenance, status ProvenanceLineage) ProvenanceProjection {
	projection := ProvenanceProjection{
		Bound:           true,
		AuthorityKind:   values.Absent[ProvenanceAuthority](),
		AuthoritySystem: values.Absent[string](),
		EvidenceSource:  values.Absent[string](),
		LineageStatus:   values.Value(status),
		SourceVersion:   values.Absent[string](),
		EffectiveAt:     values.Absent[time.Time](),
		RecordedAt:      values.Absent[time.Time](),
	}
	if authority != nil {
		projection.AuthorityKind = values.Value(ProvenanceAuthority(authority.GetKind()))
		projection.AuthoritySystem = values.Value(authority.GetSystem())
	}
	if evidence != nil {
		projection.EvidenceSource = values.Value(evidence.GetSource())
		if recorded := evidence.GetRecordedAt(); recorded != nil {
			if recorded.CheckValid() == nil {
				projection.RecordedAt = values.Value(recorded.AsTime())
			} else {
				// Preserve invalid canonical input as an invalid display value;
				// presentation must not repair or invent a timestamp.
				projection.RecordedAt = values.Value(time.Time{})
			}
		}
	}
	return projection
}
