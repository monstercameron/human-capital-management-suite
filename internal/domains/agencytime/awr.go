package agencytime

// QualifyingWeekCount is the caller-supplied count of qualifying weeks this
// worker has accrued with this hirer, in this role, under the UK Agency
// Workers Regulations 2010 (breaks of six weeks or less do not reset the
// count). The counting itself — tracking weeks, applying the six-week
// break rule, and resetting on a genuine break — lives in worktimerules as
// a WTIME-007 rolling window; this package only decides what the count
// means once it has it.
const QualifyingWeeksForParity = 12

// ParityObligation is whether the UK AWR equal-treatment (parity) rate must
// now apply to this agency worker's pay and basic working conditions.
type ParityObligation struct {
	Applies         bool
	QualifyingWeeks int
	Reason          string
}

// EvaluateParity reports the parity obligation from a supplied qualifying-week
// count. It performs no counting of its own and takes no clock; a negative
// count is rejected as a caller error rather than treated as zero.
func EvaluateParity(qualifyingWeeks int) (ParityObligation, error) {
	if qualifyingWeeks < 0 {
		return ParityObligation{}, reject("QualifyingWeeks", "negative", "qualifying week count cannot be negative")
	}
	if qualifyingWeeks >= QualifyingWeeksForParity {
		return ParityObligation{
			Applies: true, QualifyingWeeks: qualifyingWeeks,
			Reason: "worker has reached the twelfth qualifying week under the UK Agency Workers Regulations 2010; the parity rate applies",
		}, nil
	}
	return ParityObligation{
		Applies: false, QualifyingWeeks: qualifyingWeeks,
		Reason: "worker has not yet reached the twelfth qualifying week",
	}, nil
}
