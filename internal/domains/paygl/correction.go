package paygl

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

// ErrCorrectionRejected is the PAYGL-005 refusal boundary. A correction
// creates a reversal or adjustment with exact prior-line links; it never
// overwrites exported journal history.
var ErrCorrectionRejected = errors.New("PAYGL_005_REJECTED")

// ReversalLine names one exact prior line to reverse in full.
type ReversalLine struct {
	CorrectsOrdinal int
}

// CorrectionRequest asks for an append-only correction of one journal.
type CorrectionRequest struct {
	CorrectionID string
	Reason       string
	Reversals    []ReversalLine
	Adjustments  []JournalLine
}

// JournalCorrection is the immutable correction record. OriginalLines is the
// snapshot that was corrected from; Corrected is the new balanced journal.
// The original journal value is never edited.
type JournalCorrection struct {
	CorrectionID   string
	OriginalID     string
	OriginalDigest string
	Reason         string
	OriginalLines  []JournalLine
	Reversals      []JournalLine
	Adjustments    []JournalLine
	Corrected      Journal
	Digest         string
}

// CorrectJournal appends a reversal/adjustment correction to one journal.
// The returned correction links the exact original digest; the input journal
// is returned unchanged by value semantics.
func CorrectJournal(original Journal, req CorrectionRequest) (JournalCorrection, error) {
	fail := func(format string, args ...any) (JournalCorrection, error) {
		return JournalCorrection{}, errors.Join(ErrCorrectionRejected, fmt.Errorf(format, args...))
	}
	if err := original.Validate(); err != nil {
		return fail("original: %v", err)
	}
	if strings.TrimSpace(req.CorrectionID) == "" {
		return fail("correction id is required")
	}
	if strings.TrimSpace(req.Reason) == "" {
		return fail("correction reason is required")
	}
	if len(req.Reversals) == 0 && len(req.Adjustments) == 0 {
		return fail("at least one reversal or adjustment is required")
	}
	byOrdinal := map[int]JournalLine{}
	for _, line := range original.Lines {
		byOrdinal[line.Ordinal] = line
	}
	seen := map[int]struct{}{}
	reversals := make([]JournalLine, 0, len(req.Reversals))
	for i, reversal := range req.Reversals {
		linked, ok := byOrdinal[reversal.CorrectsOrdinal]
		if !ok {
			return fail("reversal %d: no prior line with ordinal %d", i, reversal.CorrectsOrdinal)
		}
		if _, ok := seen[reversal.CorrectsOrdinal]; ok {
			return fail("reversal %d: prior line %d already reversed", i, reversal.CorrectsOrdinal)
		}
		seen[reversal.CorrectsOrdinal] = struct{}{}
		mirror := linked
		if linked.Side == SideDebit {
			mirror.Side = SideCredit
		} else {
			mirror.Side = SideDebit
		}
		reversals = append(reversals, mirror)
	}
	combined := make([]JournalLine, 0, len(reversals)+len(req.Adjustments))
	combined = append(combined, reversals...)
	combined = append(combined, req.Adjustments...)
	for i := range combined {
		combined[i].Ordinal = i + 1
	}
	corrected, err := NewJournal(JournalRequest{
		JournalID: req.CorrectionID, SourceRunID: original.SourceRunID,
		SourceRunRevision: original.SourceRunRevision, SourceRunDigest: original.SourceRunDigest,
		Lines: combined,
	})
	if err != nil {
		return fail("corrected journal: %v", err)
	}
	out := JournalCorrection{
		CorrectionID: req.CorrectionID, OriginalID: original.JournalID, OriginalDigest: original.Digest,
		Reason: req.Reason, OriginalLines: append([]JournalLine(nil), original.Lines...),
		Reversals: combined[:len(reversals)], Adjustments: combined[len(reversals):],
		Corrected: corrected,
	}
	out.Digest = canonicalbytes.Digest(out.body())
	return out, nil
}

func (c JournalCorrection) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.paygl.JournalCorrection", 1).
		String("correction_id", c.CorrectionID).String("original_id", c.OriginalID).
		String("original_digest", c.OriginalDigest).String("reason", c.Reason).
		String("corrected_digest", c.Corrected.Digest).
		Count("original_lines", len(c.OriginalLines))
	for _, line := range c.OriginalLines {
		w.Int("original_ordinal", int64(line.Ordinal)).String("original_account", line.Account).
			String("original_side", string(line.Side)).Value("original_amount", line.Amount)
	}
	w.Count("reversals", len(c.Reversals))
	for _, line := range c.Reversals {
		w.Int("reversal_ordinal", int64(line.Ordinal)).String("reversal_account", line.Account).
			String("reversal_side", string(line.Side)).Value("reversal_amount", line.Amount)
	}
	w.Count("adjustments", len(c.Adjustments))
	for _, line := range c.Adjustments {
		w.Int("adjustment_ordinal", int64(line.Ordinal)).String("adjustment_account", line.Account).
			String("adjustment_side", string(line.Side)).Value("adjustment_amount", line.Amount).
			Value("adjustment_dimension", line.Dimension)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Validate rechecks exact prior-line links, correction membership in the
// corrected journal, balance and the digest binding.
func (c JournalCorrection) Validate() error {
	fail := func(format string, args ...any) error {
		return errors.Join(ErrCorrectionRejected, fmt.Errorf(format, args...))
	}
	if strings.TrimSpace(c.CorrectionID) == "" || strings.TrimSpace(c.Reason) == "" {
		return fail("correction id and reason are required")
	}
	if strings.TrimSpace(c.OriginalDigest) == "" {
		return fail("original digest link is required")
	}
	if len(c.Reversals) == 0 && len(c.Adjustments) == 0 {
		return fail("at least one reversal or adjustment is required")
	}
	if err := c.Corrected.Validate(); err != nil {
		return fail("corrected journal: %v", err)
	}
	if c.Corrected.JournalID != c.CorrectionID {
		return fail("corrected journal %q is not this correction", c.Corrected.JournalID)
	}
	byOrdinal := map[int]JournalLine{}
	for _, line := range c.OriginalLines {
		byOrdinal[line.Ordinal] = line
	}
	corrected := map[string]int{}
	for _, line := range c.Corrected.Lines {
		corrected[correctionLineKey(line)]++
	}
	seen := map[int]struct{}{}
	for i, reversal := range c.Reversals {
		if _, ok := seen[reversal.Ordinal]; ok {
			return fail("reversal %d duplicates a corrected ordinal", i)
		}
		seen[reversal.Ordinal] = struct{}{}
		if corrected[correctionLineKey(reversal)] == 0 {
			return fail("reversal %d is not in the corrected journal", i)
		}
		corrected[correctionLineKey(reversal)]--
		// A reversal mirrors exactly one original line with flipped side.
		matched := false
		for _, prior := range c.OriginalLines {
			mirror := prior
			if prior.Side == SideDebit {
				mirror.Side = SideCredit
			} else {
				mirror.Side = SideDebit
			}
			if mirror.Account == reversal.Account && mirror.Side == reversal.Side &&
				mirror.Amount.Equal(reversal.Amount) && mirror.Currency == reversal.Currency &&
				mirror.Entity == reversal.Entity && mirror.Ledger == reversal.Ledger &&
				mirror.Dimension == reversal.Dimension {
				matched = true
				break
			}
		}
		if !matched {
			return fail("reversal %d links no exact prior line", i)
		}
	}
	for i, adjustment := range c.Adjustments {
		if corrected[correctionLineKey(adjustment)] == 0 {
			return fail("adjustment %d is not in the corrected journal", i)
		}
		corrected[correctionLineKey(adjustment)]--
	}
	for key, remaining := range corrected {
		if remaining != 0 {
			return fail("corrected journal has unlinked line %q", key)
		}
	}
	if c.Digest != canonicalbytes.Digest(c.body()) {
		return fail("canonical digest mismatch")
	}
	return nil
}

func correctionLineKey(line JournalLine) string {
	return strings.Join([]string{
		line.Account, string(line.Side), line.Amount.String(),
		line.Currency, line.Entity, string(line.Ledger),
		line.Dimension.Kind.String(), line.Dimension.Value, line.Dimension.Version,
	}, "\x00")
}
