// ATTEND-003: detect meal, break and overtime exceptions.
//
// Detection composes the applicable legal, CBA and company rule pins with
// waiver and attestation evidence: an exception is raised only under a known
// composition, and excused only by a waiver carrying attestation evidence.
// An unknown jurisdiction or rule revision never reports compliant — it
// reports UNKNOWN. Findings are evidence only; routing belongs to a later
// workflow.
package attendance

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"
)

// ATTEND-003 exception kinds.
const (
	MealException     ExceptionKind = "MEAL"
	BreakException    ExceptionKind = "BREAK"
	OvertimeException ExceptionKind = "OVERTIME"
)

// MealBreakPolicy pins the applicable legal/CBA/company composition and the
// thresholds it imposes. Every ref must be versioned: an unversioned pin is
// an unknown rule, not a lenient one.
type MealBreakPolicy struct {
	JurisdictionCode   string
	JurisdictionRef    VersionedRef
	RulesRef           VersionedRef
	LegalRef           VersionedRef
	CBARef             VersionedRef
	CompanyRef         VersionedRef
	MealAfter          time.Duration
	MealMinutes        time.Duration
	RestEvery          time.Duration
	RestMinutes        time.Duration
	DailyOvertimeAfter time.Duration
	WaiverAllowed      bool
}

// PremiumRule is an explicitly versioned jurisdiction rule for statutory
// meal/rest premium pay. WorkdayIDs must identify each finding's local legal
// workday; an absent mapping makes the premium calculation unknown.
type PremiumRule struct {
	JurisdictionCode  string
	RuleRef           VersionedRef
	MealHours         int
	RestHours         int
	MaxMealPerWorkday int
	MaxRestPerWorkday int
}

// PremiumLine records capped premium units for one workday and violation kind.
type PremiumLine struct {
	WorkdayID string
	Kind      ExceptionKind
	Count     int
	Hours     int
}

// CalculateBreakPremiums applies versioned per-occurrence hours and independent
// meal and rest caps per workday. Unknown jurisdiction, incomplete workday evidence, or an
// invalid rule returns unknown rather than zero premium.
func CalculateBreakPremiums(res Result, jurisdiction string, rule PremiumRule, workdayIDs map[string]string) ([]PremiumLine, Outcome, error) {
	if rule.JurisdictionCode == "" || rule.JurisdictionCode != jurisdiction || rule.RuleRef.ID == "" || rule.RuleRef.Version == "" {
		return nil, Unknown, nil
	}
	if rule.MealHours < 0 || rule.RestHours < 0 || rule.MaxMealPerWorkday < 0 || rule.MaxRestPerWorkday < 0 ||
		(rule.MealHours > 0 && rule.MaxMealPerWorkday == 0) || (rule.RestHours > 0 && rule.MaxRestPerWorkday == 0) || (rule.MealHours == 0 && rule.RestHours == 0) {
		return nil, Unknown, fmt.Errorf("%w: invalid premium rule", ErrInvalidEvidence)
	}
	if err := res.Validate(); err != nil {
		return nil, Unknown, fmt.Errorf("%w: premium source evaluation: %v", ErrInvalidEvidence, err)
	}
	type key struct {
		day  string
		kind ExceptionKind
	}
	counts := make(map[key]int)
	for _, finding := range res.Exceptions {
		if finding.Kind != MealException && finding.Kind != BreakException {
			continue
		}
		day := workdayIDs[finding.ShiftID]
		if day == "" {
			return nil, Unknown, nil
		}
		counts[key{day, finding.Kind}]++
	}
	lines := make([]PremiumLine, 0, len(counts))
	keys := make([]key, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].day != keys[j].day {
			return keys[i].day < keys[j].day
		}
		return keys[i].kind < keys[j].kind
	})
	for _, k := range keys {
		cap := rule.MaxMealPerWorkday
		per := rule.MealHours
		if k.kind == BreakException {
			cap = rule.MaxRestPerWorkday
			per = rule.RestHours
		}
		capLeft := cap
		count := counts[k]
		if count > capLeft {
			count = capLeft
		}
		if count < 0 {
			count = 0
		}
		if count > 0 && per > 0 {
			lines = append(lines, PremiumLine{WorkdayID: k.day, Kind: k.kind, Count: count, Hours: count * per})
		}
	}
	return lines, res.Outcome, nil
}

func (p MealBreakPolicy) knownFor(req Request) bool {
	if p.JurisdictionCode == "" || p.JurisdictionCode != req.Jurisdiction.Code {
		return false
	}
	for _, r := range []VersionedRef{p.JurisdictionRef, p.RulesRef, p.LegalRef, p.CBARef, p.CompanyRef} {
		if r.ID == "" || r.Version == "" {
			return false
		}
	}
	return true
}

func (p MealBreakPolicy) validate() error {
	if p.MealAfter < 0 || p.MealMinutes < 0 || p.RestEvery < 0 || p.RestMinutes < 0 || p.DailyOvertimeAfter < 0 {
		return fmt.Errorf("%w: meal/break/overtime thresholds must not be negative", ErrInvalidEvidence)
	}
	return nil
}

// WaiverKind names which exception a waiver may excuse. Overtime is a
// measured fact, never a waivable exception.
type WaiverKind string

// Waiver kinds.
const (
	WaiverMeal WaiverKind = "MEAL"
	WaiverRest WaiverKind = "REST"
)

// Waiver excuses one shift's meal or rest exception only when the policy
// allows waivers and the waiver carries both a waiver record and a distinct
// attestation evidence ref.
type Waiver struct {
	ShiftID        string
	Kind           WaiverKind
	WaiverRef      VersionedRef
	AttestationRef VersionedRef
}

func (w Waiver) validate() error {
	if w.ShiftID == "" {
		return fmt.Errorf("%w: waiver shift is required", ErrInvalidEvidence)
	}
	if w.Kind != WaiverMeal && w.Kind != WaiverRest {
		return fmt.Errorf("%w: waiver kind %q is not waivable", ErrInvalidEvidence, w.Kind)
	}
	if w.WaiverRef.ID == "" || w.WaiverRef.Version == "" || w.AttestationRef.ID == "" || w.AttestationRef.Version == "" {
		return fmt.Errorf("%w: waiver requires a waiver record and attestation evidence", ErrInvalidEvidence)
	}
	return nil
}

// MealBreakResult is the bounded detection answer for one request.
type MealBreakResult struct {
	Outcome    Outcome
	Exceptions []Finding
	Digest     string
}

// DetectMealBreakOvertime detects meal, rest-break and overtime exceptions
// for every shift with determinate attendance evidence. Shifts the base
// evaluation already flags missing are skipped: an absence is not a missed
// meal. Unknown jurisdiction or rule composition yields UNKNOWN, never
// COMPLIANT. A malformed waiver fails closed with invalid evidence.
func DetectMealBreakOvertime(req Request, policy MealBreakPolicy, waivers []Waiver) (MealBreakResult, error) {
	if err := policy.validate(); err != nil {
		return MealBreakResult{Outcome: Unknown}, err
	}
	base, err := Evaluate(req)
	if err != nil {
		return MealBreakResult{Outcome: Unknown}, err
	}
	if !policy.knownFor(req) {
		return MealBreakResult{Outcome: Unknown, Digest: mealBreakDigest(req, policy, nil)}, nil
	}
	for i, w := range waivers {
		if err := w.validate(); err != nil {
			return MealBreakResult{Outcome: Unknown}, fmt.Errorf("%w: waiver %d: %v", ErrInvalidEvidence, i, err)
		}
	}
	waived := make(map[string]map[WaiverKind]Waiver)
	if policy.WaiverAllowed {
		for _, w := range waivers {
			if waived[w.ShiftID] == nil {
				waived[w.ShiftID] = make(map[WaiverKind]Waiver)
			}
			waived[w.ShiftID][w.Kind] = w
		}
	}
	missing := make(map[string]bool)
	for _, e := range base.Exceptions {
		if e.Kind == MissingException {
			missing[e.ShiftID] = true
		}
	}
	byID := make(map[string]Shift, len(req.Schedule.Shifts))
	for _, s := range req.Schedule.Shifts {
		byID[s.ID] = s
	}
	ordered := append([]ShiftResult(nil), base.Shifts...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].ShiftID < ordered[j].ShiftID })
	out := MealBreakResult{Outcome: Compliant}
	for _, sr := range ordered {
		if missing[sr.ShiftID] {
			continue
		}
		shift, ok := byID[sr.ShiftID]
		if !ok {
			continue
		}
		worked := shift.Interval.End.Sub(shift.Interval.Start)
		rest := breakMinutesIn(req.Breaks, shift.Interval)
		if policy.DailyOvertimeAfter > 0 && worked > policy.DailyOvertimeAfter {
			over := worked - policy.DailyOvertimeAfter
			out.Exceptions = append(out.Exceptions, exceptionFor(sr.ShiftID, OvertimeException, "", Interval{Start: shift.Interval.End.Add(-over), End: shift.Interval.End}, "hours exceed the daily overtime threshold"))
		}
		if policy.MealAfter > 0 && worked > policy.MealAfter && rest < policy.MealMinutes {
			if _, ok := waived[sr.ShiftID][WaiverMeal]; !ok {
				short := policy.MealMinutes - rest
				out.Exceptions = append(out.Exceptions, exceptionFor(sr.ShiftID, MealException, "", Interval{Start: shift.Interval.End.Add(-short), End: shift.Interval.End}, "meal break below the required minutes"))
			}
		}
		if policy.RestEvery > 0 {
			required := time.Duration(int64(worked) / int64(policy.RestEvery) * int64(policy.RestMinutes))
			if rest < required {
				if _, ok := waived[sr.ShiftID][WaiverRest]; !ok {
					short := required - rest
					out.Exceptions = append(out.Exceptions, exceptionFor(sr.ShiftID, BreakException, "", Interval{Start: shift.Interval.End.Add(-short), End: shift.Interval.End}, "rest breaks below the required minutes"))
				}
			}
		}
	}
	sort.SliceStable(out.Exceptions, func(i, j int) bool {
		if out.Exceptions[i].ShiftID != out.Exceptions[j].ShiftID {
			return out.Exceptions[i].ShiftID < out.Exceptions[j].ShiftID
		}
		return exceptionRank(out.Exceptions[i].Kind) < exceptionRank(out.Exceptions[j].Kind)
	})
	if len(out.Exceptions) > 0 {
		out.Outcome = Exception
	}
	out.Digest = mealBreakDigest(req, policy, out.Exceptions)
	return out, nil
}

// exceptionRank fixes the canonical finding order: meal, rest, overtime.
func exceptionRank(k ExceptionKind) int {
	switch k {
	case MealException:
		return 0
	case BreakException:
		return 1
	case OvertimeException:
		return 2
	}
	return 3
}

func breakMinutesIn(breaks []Break, iv Interval) time.Duration {
	total := time.Duration(0)
	for _, b := range breaks {
		start := b.Interval.Start
		if iv.Start.After(start) {
			start = iv.Start
		}
		end := b.Interval.End
		if iv.End.Before(end) {
			end = iv.End
		}
		if end.After(start) {
			total += end.Sub(start)
		}
	}
	return total
}

func mealBreakDigest(req Request, policy MealBreakPolicy, exceptions []Finding) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%s|%s|%s|%s|%d|%d|%d|%d|%d",
		req.Jurisdiction.Code, policy.RulesRef.Version, policy.LegalRef.Version,
		policy.CBARef.Version, policy.CompanyRef.Version,
		policy.JurisdictionCode, policy.MealAfter, policy.MealMinutes,
		policy.RestEvery, policy.RestMinutes, policy.DailyOvertimeAfter)
	for _, e := range exceptions {
		fmt.Fprintf(h, "|%s=%s:%d", e.ShiftID, e.Kind, e.Minutes)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
