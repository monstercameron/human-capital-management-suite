package scopefidelity

import (
	"fmt"
	"strings"
)

// CodeSelfAdmittingOpen flags a ticked item whose latest evidence text
// admits the work is not actually complete.
const CodeSelfAdmittingOpen = "SELF_ADMITTING_OPEN"

// EvidenceClaim is one backlog item's latest evidence state as seen by the
// audit.
type EvidenceClaim struct {
	TodoID string
	Ticked bool
	Text   string
}

// openMarkers are evidence-text inflections that admit outstanding work.
// They are matched case-insensitively against the latest evidence line only:
// a green live-run record contains none of them.
var openMarkers = []string{
	"remains non-green",
	"remains unrepaired",
	"remains open",
	"remains an",
	"cannot close",
	"still open",
	"follow-up",
	"followup",
	"not yet",
	"unresolved",
}

// FlagSelfAdmittingEvidence returns one finding per ticked claim whose
// evidence text contains an open-work marker, citing the first marker in
// policy order. Unticked items never flag.
func FlagSelfAdmittingEvidence(claims []EvidenceClaim) []Finding {
	var out []Finding
	for _, c := range claims {
		if !c.Ticked {
			continue
		}
		lower := strings.ToLower(c.Text)
		for _, m := range openMarkers {
			if strings.Contains(lower, m) {
				out = append(out, Finding{
					TodoID:  c.TodoID,
					Code:    CodeSelfAdmittingOpen,
					Message: fmt.Sprintf("ticked item's latest evidence admits open work (%q); repair the findings or untick/re-scope the item", m),
				})
				break
			}
		}
	}
	sortFindings(out)
	return out
}
