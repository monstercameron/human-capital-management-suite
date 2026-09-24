package hipaa

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrBreachMatrixInvalid reports a missing, changed, or contradictory HIPAA
// clock in the extension attached to a PRIV-009 matrix version.
var ErrBreachMatrixInvalid = errors.New("hipaa: breach clock matrix is invalid")

// BreachMatrixVersion pins the modeled statutory clock set.
const BreachMatrixVersion = "hipaa-breach-v1"

// BreachRecipient names who must receive a HIPAA breach notice.
type BreachRecipient string

const (
	RecipientIndividuals   BreachRecipient = "INDIVIDUALS"
	RecipientMedia         BreachRecipient = "MEDIA"
	RecipientSecretary     BreachRecipient = "HHS_SECRETARY"
	RecipientCoveredEntity BreachRecipient = "COVERED_ENTITY"
)

// BreachClock is one recipient's condition and timing in the HIPAA matrix.
type BreachClock struct {
	Recipient        BreachRecipient `json:"recipient"`
	Threshold        int64           `json:"threshold"`
	ThresholdMeaning string          `json:"threshold_meaning"`
	DeadlineDays     int             `json:"deadline_days"`
	DeadlineFrom     string          `json:"deadline_from"`
	Authority        string          `json:"authority"`
}

// BreachMatrixExtension pins HIPAA clocks beside an existing PRIV-009 matrix.
// The rule set is statutory; changing it requires a new extension version and
// review against 45 CFR 164.400-414.
type BreachMatrixExtension struct {
	Version              string        `json:"version"`
	PRIV009MatrixVersion string        `json:"priv009_matrix_version"`
	Exception            string        `json:"exception"`
	Rules                []BreachClock `json:"rules"`
}

// StandardBreachMatrixExtension returns the current program's HIPAA extension.
func StandardBreachMatrixExtension(priv009Version string) BreachMatrixExtension {
	return BreachMatrixExtension{Version: BreachMatrixVersion, PRIV009MatrixVersion: priv009Version, Exception: "45 CFR 164.412 law-enforcement delay", Rules: []BreachClock{
		{Recipient: RecipientIndividuals, Threshold: 1, ThresholdMeaning: "each affected individual", DeadlineDays: 60, DeadlineFrom: "discovery", Authority: "45 CFR 164.404(b)"},
		{Recipient: RecipientMedia, Threshold: 500, ThresholdMeaning: "more than 500 residents of a state or jurisdiction", DeadlineDays: 60, DeadlineFrom: "discovery", Authority: "45 CFR 164.406(a)-(b)"},
		{Recipient: RecipientSecretary, Threshold: 500, ThresholdMeaning: "500 or more affected individuals", DeadlineDays: 60, DeadlineFrom: "discovery; contemporaneous with individual notice", Authority: "45 CFR 164.408(a)-(b)"},
		{Recipient: RecipientSecretary, Threshold: 499, ThresholdMeaning: "fewer than 500 affected individuals", DeadlineDays: 60, DeadlineFrom: "end of calendar year of discovery", Authority: "45 CFR 164.408(c)"},
		{Recipient: RecipientCoveredEntity, Threshold: 1, ThresholdMeaning: "business-associate breach of unsecured PHI", DeadlineDays: 60, DeadlineFrom: "discovery, without unreasonable delay", Authority: "45 CFR 164.410(b)"},
	}}
}

// Validate requires exact coverage of the statutory recipient/threshold
// combinations and an attached PRIV-009 version. It rejects extra rules too.
func (m BreachMatrixExtension) Validate() error {
	if m.Version != BreachMatrixVersion || strings.TrimSpace(m.PRIV009MatrixVersion) == "" || m.PRIV009MatrixVersion != strings.TrimSpace(m.PRIV009MatrixVersion) || m.Exception != "45 CFR 164.412 law-enforcement delay" {
		return fmt.Errorf("%w: recognized extension and PRIV-009 matrix versions are required", ErrBreachMatrixInvalid)
	}
	want := StandardBreachMatrixExtension(m.PRIV009MatrixVersion)
	if len(m.Rules) != len(want.Rules) {
		return fmt.Errorf("%w: got %d rules, want %d", ErrBreachMatrixInvalid, len(m.Rules), len(want.Rules))
	}
	canonical := func(rules []BreachClock) []string {
		out := make([]string, len(rules))
		for i, r := range rules {
			out[i] = fmt.Sprintf("%s|%d|%s|%d|%s|%s", r.Recipient, r.Threshold, r.ThresholdMeaning, r.DeadlineDays, r.DeadlineFrom, r.Authority)
		}
		sort.Strings(out)
		return out
	}
	got, expected := canonical(m.Rules), canonical(want.Rules)
	for i := range got {
		if got[i] != expected[i] {
			return fmt.Errorf("%w: rule set differs from statutory version", ErrBreachMatrixInvalid)
		}
	}
	return nil
}

// Canonical renders a stable, golden-test representation of the extension.
func (m BreachMatrixExtension) Canonical() string {
	rules := append([]BreachClock(nil), m.Rules...)
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].Recipient != rules[j].Recipient {
			return rules[i].Recipient < rules[j].Recipient
		}
		return rules[i].Threshold < rules[j].Threshold
	})
	lines := []string{"version=" + m.Version + "; priv009=" + m.PRIV009MatrixVersion + "; exception=" + m.Exception}
	for _, r := range rules {
		lines = append(lines, fmt.Sprintf("%s :: %d %s :: %d days from %s :: %s", r.Recipient, r.Threshold, r.ThresholdMeaning, r.DeadlineDays, r.DeadlineFrom, r.Authority))
	}
	return strings.Join(lines, "\n")
}
