package extract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

// ReviewedOverridesPath is the repository-relative path of the generator's
// reviewed inputs: obligation content the state reviews (LEGAL-ST-*-001)
// carry in the checked-in packs that the heuristics do not reproduce —
// federal-baseline markers, locality rules, multi-obligation kinds,
// review-trimmed bodies and notes, and confirmed absences.
//
// The file is an INPUT, not an output: Generate consumes it and the
// LEGAL-010 golden test proves the checked-in packs equal what the
// generator produces from (research, matrix, reviewed). A pack edit that
// bypasses this file breaks that test. Every entry carries its basis so a
// reviewer can tell a reviewed finding from extractor noise.
const ReviewedOverridesPath = "internal/governance/legal/extract/reviewed_overrides.json"

// ReviewedObligation is one verbatim obligation a review carries: the
// fields that vary by finding. ID, kind, source file and review status are
// filled mechanically (the reviewed packs keep the extractor's ID scheme,
// cite their own research file and stay UNREVIEWED), so this file cannot
// smuggle in a foreign provenance.
type ReviewedObligation struct {
	ID               string         `json:"id"`
	Section          string         `json:"section"`
	Note             string         `json:"note"`
	ConfidenceMarker string         `json:"confidence_marker"`
	Body             legal.BodyJSON `json:"body"`
}

// ReviewedState is one state's reviewed content: per-kind complete
// obligation lists, an optional exact emission order, and an optional
// provenance suffix recording the review itself.
type ReviewedState struct {
	// Basis names the review behind the entries (e.g. LEGAL-ST-MI-001),
	// or records that the bytes are a freeze of reviewed output the
	// current heuristics select differently.
	Basis string `json:"basis"`
	// Kinds maps a kind name to that kind's COMPLETE obligation list.
	// An empty (non-nil) list suppresses the kind: the review confirms
	// the absence (e.g. ST-CO NOTICE, ST-NJ PERSONNEL_FILE). A kind
	// absent from the map takes the mechanical path.
	Kinds map[string][]ReviewedObligation `json:"kinds,omitempty"`
	// Order, when present, is the exact emission order by obligation ID.
	// It covers packs whose reviewed order is not kind-ordinal (review
	// additions ride at the end). It must name every emitted ID exactly
	// once; Generate fails loudly otherwise.
	Order []string `json:"order,omitempty"`
	// ProvenanceAppend, when present, is appended verbatim to the
	// mechanical provenance notes (it starts with its own separator).
	// It records the review attestation the pack carries.
	ProvenanceAppend string `json:"provenance_append,omitempty"`
}

// ReviewedOverrides is the parsed reviewed-inputs file.
type ReviewedOverrides struct {
	States map[string]ReviewedState `json:"states"`
}

// LoadReviewedOverrides reads and validates the reviewed-inputs file under
// root. Structural mistakes (unknown state or kind, empty basis or ID)
// fail here; content mistakes fail louder, when the generated definition
// does not validate or does not match the checked-in pack.
func LoadReviewedOverrides(root string) (*ReviewedOverrides, error) {
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(ReviewedOverridesPath)))
	if err != nil {
		return nil, fmt.Errorf("extract: reading reviewed overrides: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var out ReviewedOverrides
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("extract: parsing reviewed overrides: %w", err)
	}
	if out.States == nil {
		return nil, fmt.Errorf("extract: reviewed overrides carry no states")
	}
	for code, state := range out.States {
		if _, ok := StateByCode(code); !ok {
			return nil, fmt.Errorf("extract: reviewed overrides name unknown state %q", code)
		}
		if state.Basis == "" {
			return nil, fmt.Errorf("extract: reviewed overrides for %s state no basis", code)
		}
		for kindName, list := range state.Kinds {
			if _, err := legal.ParseObligationType(kindName); err != nil {
				return nil, fmt.Errorf("extract: reviewed overrides for %s name unknown kind %q", code, kindName)
			}
			for i := range list {
				if list[i].ID == "" {
					return nil, fmt.Errorf("extract: reviewed overrides for %s/%s carry an empty obligation id", code, kindName)
				}
				if list[i].ConfidenceMarker == "" {
					return nil, fmt.Errorf("extract: reviewed overrides for %s/%s (%s) carry no confidence marker", code, kindName, list[i].ID)
				}
			}
		}
	}
	return &out, nil
}

// forState returns the reviewed entries for a state, or nil when the state
// takes the fully mechanical path.
func (o *ReviewedOverrides) forState(code string) *ReviewedState {
	if o == nil {
		return nil
	}
	state, ok := o.States[code]
	if !ok {
		return nil
	}
	return &state
}

// reviewedObligation renders one verbatim obligation. Source file and
// review status stay mechanical: every extracted draft cites its own
// research file and stays UNREVIEWED.
func reviewedObligation(state State, file *ResearchFile, kind legal.ObligationType, ro ReviewedObligation) legal.ObligationJSON {
	return legal.ObligationJSON{
		Kind: kind.String(),
		ID:   ro.ID,
		Citation: legal.CitationJSON{
			SourceFile:       file.Path,
			Section:          ro.Section,
			Note:             ro.Note,
			ReviewStatus:     legal.ReviewStatusUnreviewed.String(),
			ConfidenceMarker: ro.ConfidenceMarker,
		},
		Body: ro.Body,
	}
}

// applyOrder reorders obligations into the reviewed emission order,
// failing loudly when the order does not name every emitted ID exactly
// once. A silent drop or duplication here would be a lost rule.
func applyOrder(code string, obligations []legal.ObligationJSON, order []string) ([]legal.ObligationJSON, error) {
	byID := make(map[string]legal.ObligationJSON, len(obligations))
	for _, o := range obligations {
		if _, dup := byID[o.ID]; dup {
			return nil, fmt.Errorf("extract: %s emits duplicate obligation id %q", code, o.ID)
		}
		byID[o.ID] = o
	}
	if len(order) != len(obligations) {
		return nil, fmt.Errorf("extract: %s reviewed order names %d obligations for %d emitted",
			code, len(order), len(obligations))
	}
	out := make([]legal.ObligationJSON, 0, len(order))
	seen := make(map[string]bool, len(order))
	for _, id := range order {
		o, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("extract: %s reviewed order names unemitted obligation %q", code, id)
		}
		if seen[id] {
			return nil, fmt.Errorf("extract: %s reviewed order names %q twice", code, id)
		}
		seen[id] = true
		out = append(out, o)
	}
	return out, nil
}
