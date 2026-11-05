package payroll

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/balance"
)

// RecalculationVersion is the BAL-006 manifest version pin this
// implementation satisfies. A manifest pinning any other payroll version
// fails closed: a stale or drifted recalculator must never satisfy a newer
// manifest.
const RecalculationVersion = "payroll/1"

// ErrRecalculationIncompatible reports a corrected balance the payroll
// recalculator cannot consume: the ending is not exact cent money or the
// caller supplied no sink for the recomputed output.
var ErrRecalculationIncompatible = errors.New("payroll: corrected balance is not a usable tax accumulator")

// RecalculatedPayRun is the observable recomputed payroll output: the
// effective pay-run input (with the corrected year-to-date accumulator)
// and the governed result re-run from it. Callers pass one per correction;
// sharing a sink across concurrent corrections is a data race.
type RecalculatedPayRun struct {
	Input  PayRunInput
	Result PayRunResult
}

// RecalculatorForPayRun returns the BAL-006 dependent recalculator for the
// payroll owner: it re-runs the governed pay-run with the corrected
// balance ending as its year-to-date accumulator, records the recomputed
// output in sink, and returns evidence bound to the corrected balance
// digest and the manifest-pinned version. Any failure — a nil sink, a
// version the manifest does not pin, a non-cent ending, or a refused
// pay-run leg — returns an error so the correction fails closed.
func RecalculatorForPayRun(base PayRunInput, sink *RecalculatedPayRun) balance.DependentRecalculator {
	return func(ctx balance.RecalculationContext) (balance.RecalculationEvidence, error) {
		if sink == nil {
			return balance.RecalculationEvidence{}, fmt.Errorf("%w: no sink captures the recomputed pay run", ErrRecalculationIncompatible)
		}
		if ctx.Dependent.Version != RecalculationVersion {
			return balance.RecalculationEvidence{}, fmt.Errorf("%w: manifest pins %q, implementation satisfies %q", ErrRecalculationIncompatible, ctx.Dependent.Version, RecalculationVersion)
		}
		ending := ctx.Balance.Ending
		if ending.Scale() != 2 || ending.Sign() < 0 {
			return balance.RecalculationEvidence{}, fmt.Errorf("%w: ending %s is not exact cent money", ErrRecalculationIncompatible, ending)
		}
		input := base
		input.PriorAccumulator = ending
		result, err := RunPayrollPeriod(input)
		if err != nil {
			return balance.RecalculationEvidence{}, err
		}
		*sink = RecalculatedPayRun{Input: input, Result: result}
		return balance.RecalculationEvidence{
			InputDigest: ctx.Balance.Digest,
			Version:     ctx.Dependent.Version,
			Digest:      recalculationDigest(ctx.Balance.Digest, result),
		}, nil
	}
}

// recalculationDigest identifies the recomputed artifact: the corrected
// balance it was recomputed from plus the wage, tax and filing digests of
// the rerun that produced it.
func recalculationDigest(balanceDigest string, result PayRunResult) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{"payroll-recalculation", balanceDigest, result.Wage.Digest, result.Tax.Digest, result.Package.Digest}, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}
