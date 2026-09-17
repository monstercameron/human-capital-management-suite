package extract

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

// This file holds the text-reading half of the extractor: which words make an
// item evidence for a kind, how a statutory section is lifted out of it, and
// how a day count, a year count or a money amount is read.
//
// Everything here is a pure function of the item's text. Nothing infers: a
// value that is not in the text does not come out of these functions, and the
// caller records the absence rather than substituting a default.

// KindMatcher is the evidence rule for one obligation kind: which topic
// section owns it, and which words in an item make that item evidence.
type KindMatcher struct {
	Kind legal.ObligationType
	// Owning is the research-file section searched after the Implications and
	// Summary sections.
	Owning Topic
	// Pattern is the evidence regex. It is deliberately narrow: a false
	// negative leaves a matrix Y cell with a thin, honestly-marked rule; a
	// false positive would attach a duty to a state that has none.
	Pattern *regexp.Regexp
}

// mustMatch is called only from the fixed kindMatchers table during package
// initialisation, so each expression compiles once.
func mustMatch(expr string) *regexp.Regexp { return regexp.MustCompile(`(?i)` + expr) } // regexhoist:dynamic

// kindMatchers is the evidence table, in kind-ordinal order.
var kindMatchers = []KindMatcher{
	{legal.ObligationTypeNotice, TopicRelationship, mustMatch(
		`wage notice|wage theft prevention|notice of (?:a )?(?:pay|wage|rate)|` +
			`wage reductions?|pay reductions?|reduction in (?:pay|wages?)|` +
			`pay[- ]change notice|written notice[^.]{0,60}(?:pay|wage|rate|compensation)|` +
			`notif(?:y|ication)[^.]{0,50}(?:pay|wage|rate) chang|change of pay|` +
			`§ ?2810\.5|195\(1\)|195\(2\)`)},
	{legal.ObligationTypeFieldRestriction, TopicTransparency, mustMatch(
		`salary[- ]history|wage history|pay history|prior salary|previous salary|` +
			`compensation history|credit history|salary-history ban`)},
	{legal.ObligationTypeRetention, TopicRecords, mustMatch(
		`retain|retention|recordkeeping|record[- ]keeping|preserve[^.]{0,40}record|` +
			`records? for (?:at least )?\d|\d[- ]year record`)},
	{legal.ObligationTypeLeaveInteraction, TopicLeave, mustMatch(
		`sick leave|paid leave|leave accrual|accru\w+[^.]{0,30}leave|leave balance|` +
			`family (?:and medical )?leave|paid family|ESST|PLAWA|PSL\b|carryover|carry over`)},
	// A bare "payday" is deliberately not evidence: the corpus writes "by the
	// next regular payday" about final pay in almost every file, and matching
	// it would give thirty states a pay-frequency rule cited to a final-pay
	// statute.
	{legal.ObligationTypePayFrequency, TopicWages, mustMatch(
		`pay frequency|pay period(?:s)? (?:must|shall|may)|semi-?monthly|bi-?weekly|paid weekly|` +
			`paid at least (?:once|twice)|wage payment interval|designated paydays?|` +
			`regular paydays?[^.]{0,40}(?:designat|establish|at least)|payday schedule|pay schedule`)},
	{legal.ObligationTypeFinalPayDeadline, TopicWages, mustMatch(
		`final[- ]pay|final[- ]wage|final[- ]check|final paycheck|last paycheck|` +
			`wages[^.]{0,40}(?:due|paid)[^.]{0,40}(?:termination|separation|discharge|resignation)`)},
	{legal.ObligationTypePayTransparency, TopicTransparency, mustMatch(
		`pay transparency|salary range|wage range|pay scale|pay range|pay band|` +
			`salary[- ]range disclosure|pay secrecy|job posting[^.]{0,40}(?:salary|wage|range)`)},
	{legal.ObligationTypeNonCompete, TopicSeparation, mustMatch(
		`non-?compete|noncompete|restrictive covenant|non-?solicit`)},
	{legal.ObligationTypeEVerify, TopicHiring, mustMatch(
		`e-?verify|work authorization|employment eligibility|form i-9|\bi-9\b`)},
	{legal.ObligationTypeMiniWARN, TopicSeparation, mustMatch(
		`\bwarn\b|mass layoff|plant closing|plant closure|reduction in force|` +
			`layoff notice|group termination`)},
	{legal.ObligationTypeWageFloor, TopicWages, mustMatch(
		`minimum wage|wage floor|tipped wage|subminimum`)},
	{legal.ObligationTypePayEquityReview, TopicTransparency, mustMatch(
		`equal pay|pay equity|pay-equity|comparable work|substantially similar work|` +
			`wage discrimination|equal work|pay disparit`)},
	{legal.ObligationTypePayStatement, TopicWages, mustMatch(
		`pay statement|wage statement|itemized statement|pay stub|paystub|` +
			`earnings statement|statement of (?:earnings|wages)`)},
	{legal.ObligationTypeClassification, TopicClassification, mustMatch(
		`abc test|right[- ]of[- ]control|20-factor|classification (?:test|audit|review)|` +
			`exempt(?:ion)? (?:test|threshold|status)|overtime threshold|` +
			`seventh[- ]day|independent contractor|misclassif`)},
	{legal.ObligationTypePersonnelFile, TopicRecords, mustMatch(
		`personnel file|personnel record|employee file|inspect[^.]{0,40}(?:file|record)|` +
			`access to (?:their |his or her )?(?:own )?(?:personnel |employment )?(?:file|record)`)},
	{legal.ObligationTypeAntiRetaliation, TopicRelationship, mustMatch(
		`retaliat|whistleblower|whistle-blower|protected activity|` +
			`no longer covers|no longer a protected class|` +
			`adverse action[^.]{0,50}(?:complaint|claim|report)`)},
	{legal.ObligationTypeJobSecurity, TopicRelationship, mustMatch(
		`good cause|wrongful discharge|wrongful termination|handbook|implied contract|` +
			`constructive discharge|at-will|probationary period`)},
	{legal.ObligationTypeSeparationFiling, TopicSeparation, mustMatch(
		`separation notice|separation form|separation report|unemployment[^.]{0,60}` +
			`(?:notice|form|filing|report|insurance notice)|form (?:bc-10|dol-800|le-1|uc-)`)},
	{legal.ObligationTypeDrugTesting, TopicHiring, mustMatch(
		`drug test|drug-free|drug and alcohol|substance abuse test|cannabis|marijuana|` +
			`safety-sensitive`)},
	{legal.ObligationTypeBreachNotification, TopicPrivacy, mustMatch(
		`breach notification|data breach|security breach|breach[^.]{0,30}notif`)},
	{legal.ObligationTypeAutomatedDecision, TopicPrivacy, mustMatch(
		`automated (?:decision|employment|tool)|artificial intelligence|\bai act\b|` +
			`\baedt\b|bias audit|algorithmic`)},
	{legal.ObligationTypeMonitoringConsent, TopicPrivacy, mustMatch(
		`biometric|electronic monitoring|surveillance|\bbipa\b|gps track|` +
			`monitoring[^.]{0,30}(?:consent|notice)`)},
}

// kindPrefer holds the identity signals that outrank position among cited
// matches. A separation filing IS its state form: an item naming the UI-14
// (or BC-10, DOL-800, ...) is the filing duty even when a vaguer
// "preserve separation notices" item trips the evidence words first.
var kindPrefer = map[legal.ObligationType]*regexp.Regexp{
	legal.ObligationTypeSeparationFiling: mustMatch(
		`form [A-Z]+-?\d+|unemployment insurance (?:separation )?notice`),
	// A notice duty names its form: "written notice of any change",
	// "wage reduction notice", "notice before any reduction". A heading
	// that trips the evidence words ("Pay Frequency and Wage Notices") or
	// a comparison item ("unlike California's § 2810.5 model") does not.
	legal.ObligationTypeNotice: mustMatch(
		`wage reduction notice|written notice (?:of (?:any|pay rate|the)|required)|` +
			`before any reduction|K\.A\.R\.`),
	// A pay-frequency duty states the mandate's measure ("requires
	// payment at least semimonthly ... within 10 business days"), not a
	// passing payday reference.
	legal.ObligationTypePayFrequency: mustMatch(
		`requires payment at least`),
	// A pay-statement duty states itemized content ("must provide itemized
	// statement of deductions"), not a bare format disclaimer.
	legal.ObligationTypePayStatement: mustMatch(
		`itemized`),
}

// preferFor returns the identity signal for a kind, or nil when position
// alone decides among cited matches.
func preferFor(kind legal.ObligationType) *regexp.Regexp {
	return kindPrefer[kind]
}

// MatcherFor returns the evidence rule for a kind.
func MatcherFor(kind legal.ObligationType) (KindMatcher, bool) {
	for _, m := range kindMatchers {
		if m.Kind == kind {
			return m, true
		}
	}
	return KindMatcher{}, false
}

// --- citation section -------------------------------------------------------

var sectionPatterns = []*regexp.Regexp{
	// A section-sign citation with up to four capitalised words of code name
	// in front of it: "Ala. Code § 25-1-30", "Labor Code §§ 201-203".
	regexp.MustCompile(`(?:[A-Z][A-Za-z.'&]*\.?\s+){0,4}§§?\s?\d[\w.\-:/()]*(?:\s*[-–]\s*\d[\w.\-:/()]*)?`),
	regexp.MustCompile(`\b\d+\s+ILCS\s+[\d/]+`),
	regexp.MustCompile(`\bKRS\s+[\d.]+`),
	regexp.MustCompile(`\bNRS\s+[\d.]+`),
	regexp.MustCompile(`\bRCW\s+[\d.]+`),
	regexp.MustCompile(`\bORS\s+[\d.]+`),
	regexp.MustCompile(`\bMCA\s+[\d\-]+`),
	regexp.MustCompile(`\bA\.R\.S\.\s*[\d\-.]+`),
	regexp.MustCompile(`\bR\.C\.\s*[\d.]+`),
	regexp.MustCompile(`\bO\.C\.G\.A\.\s*[\d\-.]+`),
	regexp.MustCompile(`\bN\.J\.S\.A\.\s*[\d:\-.]+`),
	regexp.MustCompile(`\bM\.G\.L\.\s*c\.\s*[\dA-Za-z]+(?:,\s*§+\s*[\d\w]+)?`),
	regexp.MustCompile(`\b(?:Tex|Cal|Md|Va|Wis|Tenn|Okla|Minn|Iowa|Neb|Kan|Miss|Ark|Ala|Del|Haw|Ida|Ind|La|Me|Mich|Mo|Mont|Nev|Ohio|Ore|Utah|Vt|Wash)\.\s+[A-Z][\w.&\s]{0,30}Code\s+(?:Ann\.\s+)?§?\s*[\d\-.]+`),
	regexp.MustCompile(`\b(?:SB|HB|AB|LB|SF|HF)\s?\d+[\-\d]*`),
	// Section-sign-less state codes: Indiana ("IC 22-2-8"), Kansas
	// ("K.S.A. 44-320", ranges such as "K.S.A. 44-313 to 44-327") and Kansas
	// regulations ("K.A.R. 49-20-1"). They sit after the bill pattern so a
	// "SB 241"-style cite keeps winning where both appear, and before the
	// generic section-sign pattern so a co-cited "29 U.S.C. § 211" never
	// stands in for the operative state authority.
	regexp.MustCompile(`\bIC\s+\d+[\dA-Za-z.\-]*(?:\s*(?:to|through|[-–])\s*\d+[\dA-Za-z.\-]*)?`),
	regexp.MustCompile(`\bK\.S\.A\.\s*\d+[\dA-Za-z.\-]*(?:\s*(?:to|through|[-–])\s*\d+[\dA-Za-z.\-]*)?(?:\s*et seq\.?)?`),
	regexp.MustCompile(`\bK\.A\.R\.\s*\d+[\dA-Za-z.\-]*`),
	regexp.MustCompile(`\bAct\s+\d{4}-\d+`),
	regexp.MustCompile(`\bChapter\s+\d+[\w.\-]*`),
	regexp.MustCompile(`\bTitle\s+\d+[\w.\-]*`),
}

// SectionNotStated is the placeholder a citation carries when the research
// item asserts a duty but names no statutory section. It is deliberately
// unmistakable: a citation that reads like a real section but is not would be
// worse than one that says it is missing.
const SectionNotStated = "(statutory section not stated in the research file)"

// federalCiteRE recognizes a federal citation ("29 U.S.C. § 211"). The
// extractor builds state packs, so a federal cite is never the operative
// state authority: when an item cites both ("K.S.A. 44-320 ... 29 U.S.C.
// § 211"), the state cite wins no matter which pattern would have matched
// first. An item citing only federal law keeps the honest gap.
var federalCiteRE = regexp.MustCompile(`U\.?\s*S\.?\s*C\.?|C\.?\s*F\.?\s*R\.?|F\.?L\.?S\.?A\.?`)

// ExtractSection lifts the first statutory citation out of an item's text.
func ExtractSection(text string) string {
	for _, re := range sectionPatterns {
		for _, m := range re.FindAllString(text, -1) {
			if federalCiteRE.MatchString(m) {
				continue
			}
			return cleanSection(m)
		}
	}
	return SectionNotStated
}

var sectionTrimRE = regexp.MustCompile(`^(?:and|or|the|per|under|see|in|of|a|an)\s+`)

func cleanSection(s string) string {
	s = strings.TrimSpace(s)
	for {
		trimmed := sectionTrimRE.ReplaceAllString(s, "")
		if trimmed == s {
			break
		}
		s = trimmed
	}
	s = strings.Trim(s, " ,;:.")
	// A citation lifted out of "... (MCA § 28-2-703): flag ..." carries the
	// closing bracket of the surrounding prose. Drop whichever bracket has no
	// partner rather than leaving "§ 28-2-703)" in a citation field.
	for strings.Count(s, "(") > strings.Count(s, ")") && strings.HasSuffix(s, "(") {
		s = strings.TrimSpace(strings.TrimSuffix(s, "("))
	}
	for strings.Count(s, ")") > strings.Count(s, "(") && strings.HasSuffix(s, ")") {
		s = strings.TrimSpace(strings.TrimSuffix(s, ")"))
	}
	s = strings.Trim(s, " ,;:.")
	if s == "" {
		return SectionNotStated
	}
	return s
}

// --- confidence and standard ------------------------------------------------

var (
	uncertainRE = regexp.MustCompile(`(?i)\bverify\b|\bconfirm\b|\bunclear\b|\bunconfirmed\b|` +
		`\btbd\b|\buncertain\b|\bnot confirmed\b|\bcheck current status\b|\bappears? to\b`)
	// recommendationRE deliberately does not match a bare "should". The
	// research writes "system should flag ..." about the platform's behaviour
	// on hundreds of statutory duties; treating that as a recommendation
	// about the law would mislabel the corpus. What it matches is language
	// about the rule itself being advisory.
	recommendationRE = regexp.MustCompile(`(?i)\bbest practice\b|\brecommend(?:ed|s|ation)?\b|` +
		`\badvisable\b|\bvoluntar(?:y|ily)\b|\bno (?:state )?(?:law|statute|statutory requirement)\b|` +
		`\bnot required\b|\bper employer policy\b`)
)

// ReadsUncertain reports whether the research hedges the claim.
func ReadsUncertain(text string) bool { return uncertainRE.MatchString(text) }

// ReadsRecommendation reports whether the research states the rule as advice
// rather than as a requirement.
func ReadsRecommendation(text string) bool { return recommendationRE.MatchString(text) }

// --- numbers ----------------------------------------------------------------

var (
	dayRE             = regexp.MustCompile(`(?i)\b(\d{1,3})[- ](?:(calendar|business|working)[- ])?days?\b`)
	yearRE            = regexp.MustCompile(`(?i)\b(\d{1,2})[- ]?(?:\+\s*)?years?\b`)
	moneyRE           = regexp.MustCompile(`\$\s?(\d{1,3}(?:,\d{3})*(?:\.\d{1,2})?)`)
	countRE           = regexp.MustCompile(`\b(\d{1,4})\+?\s*(?:or more\s*)?(?:employees|workers)\b`)
	annotationDaysRE  = regexp.MustCompile(`^(\d+)d$`)
	annotationCountRE = regexp.MustCompile(`^(\d+)\+?$`)
)

// ExtractDays returns the first day count and its basis, or (0, "").
func ExtractDays(text string) (int, string) {
	m := dayRE.FindStringSubmatch(text)
	if m == nil {
		return 0, ""
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, ""
	}
	switch strings.TrimSpace(strings.ToLower(m[2])) {
	case "business", "working":
		return n, "BUSINESS"
	case "calendar":
		return n, "CALENDAR"
	default:
		return n, ""
	}
}

// noticeDaysRE matches a day count attached to the word "notice"
// ("60-day notice", "60 days notice"). A bare day count elsewhere in the
// item is as often the layoff window the notice is measured over as the
// notice period itself.
var noticeDaysRE = regexp.MustCompile(`(?i)\b(\d{1,3})\s*-days?(?:'|’s)?\s+notice\b`)

// ExtractNoticeDays returns the day count attached to the word "notice",
// or 0 when the item states no notice period of its own.
func ExtractNoticeDays(text string) int {
	m := noticeDaysRE.FindStringSubmatch(text)
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	return n
}

// ExtractYears returns the first year count, or 0.
func ExtractYears(text string) int {
	m := yearRE.FindStringSubmatch(text)
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	return n
}

// ExtractMoney returns the first dollar amount as a decimal string, or "".
func ExtractMoney(text string) string {
	m := moneyRE.FindStringSubmatch(text)
	if m == nil {
		return ""
	}
	amount := strings.ReplaceAll(m[1], ",", "")
	if !strings.Contains(amount, ".") {
		return amount + ".00"
	}
	if len(amount)-strings.Index(amount, ".") == 2 {
		return amount + "0"
	}
	return amount
}

// ExtractEmployeeCount returns the first employee-count threshold, or 0.
func ExtractEmployeeCount(text string) int {
	m := countRE.FindStringSubmatch(text)
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	return n
}

// ExtractAnnotationDays reads a matrix annotation such as "45d" or "48h".
// Hours are not days and are not silently converted; an hour annotation
// returns zero and leaves the day count unstated.
func ExtractAnnotationDays(annotation string) int {
	m := annotationDaysRE.FindStringSubmatch(strings.TrimSpace(annotation))
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	return n
}

// ExtractAnnotationCount reads a matrix annotation such as "25+" or "11+".
func ExtractAnnotationCount(annotation string) int {
	m := annotationCountRE.FindStringSubmatch(strings.TrimSpace(annotation))
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	return n
}

// summarizeAbbrevRE recognizes the tokens a sentence never ends with:
// initialisms ("K.S.A", "U.S.C", "U.S"), courtesy and reference
// abbreviations ("No", "ch", "Sec", "Fig", "cf", "al", "Dr"). Cutting a note
// at "Senate Enrolled Act No." or "K.S.A." would keep the citation and drop
// the rule it cites.
var summarizeAbbrevRE = regexp.MustCompile(`(?i)^(?:no|ch|st|vs|v|eg|ie|sec|secs|art|arts|fig|cfs?|al|etc|jr|sr|mr|mrs|ms|dr|inc|co|corp|dept|est|[a-z](?:\.[a-z])+)\.?$`)

// endsAbbrev reports whether the period ending at s (inclusive, so s ends
// with ".") belongs to an abbreviation rather than to a sentence.
func endsAbbrev(s string) bool {
	tok := s
	if i := strings.LastIndexAny(tok, " \t\n([\""); i >= 0 {
		tok = tok[i+1:]
	}
	tok = strings.TrimSuffix(tok, ".")
	return summarizeAbbrevRE.MatchString(tok)
}

// Summarize trims an item's text down to a citation note: the first sentence,
// capped so a definition file stays readable. It is a excerpt of the
// repository's own research prose, never of a statute.
func Summarize(text string, max int) string {
	s := strings.TrimSpace(text)
	for start := 0; ; {
		rel := strings.Index(s[start:], ". ")
		if rel < 0 {
			break
		}
		idx := start + rel
		if idx >= max {
			break
		}
		if idx > 0 && !endsAbbrev(s[:idx+1]) {
			s = s[:idx+1]
			break
		}
		start = idx + 2
	}
	if len(s) > max {
		cut := strings.LastIndex(s[:max], " ")
		if cut < max/2 {
			cut = max
		}
		s = strings.TrimRight(s[:cut], " ,;:") + "..."
	}
	return s
}
