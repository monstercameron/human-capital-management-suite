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
		`final pay|final wage|final check|final paycheck|last paycheck|` +
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
			`independent contractor|misclassif`)},
	{legal.ObligationTypePersonnelFile, TopicRecords, mustMatch(
		`personnel file|personnel record|employee file|inspect[^.]{0,40}(?:file|record)|` +
			`access to (?:their |his or her )?(?:own )?(?:personnel |employment )?(?:file|record)`)},
	{legal.ObligationTypeAntiRetaliation, TopicRelationship, mustMatch(
		`retaliat|whistleblower|whistle-blower|protected activity|` +
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
	regexp.MustCompile(`\bAct\s+\d{4}-\d+`),
	regexp.MustCompile(`\bChapter\s+\d+[\w.\-]*`),
	regexp.MustCompile(`\bTitle\s+\d+[\w.\-]*`),
}

// SectionNotStated is the placeholder a citation carries when the research
// item asserts a duty but names no statutory section. It is deliberately
// unmistakable: a citation that reads like a real section but is not would be
// worse than one that says it is missing.
const SectionNotStated = "(statutory section not stated in the research file)"

// ExtractSection lifts the first statutory citation out of an item's text.
func ExtractSection(text string) string {
	for _, re := range sectionPatterns {
		if m := re.FindString(text); m != "" {
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
	dayRE             = regexp.MustCompile(`(?i)\b(\d{1,3})[- ](calendar |business |working |)days?\b`)
	wordDayRE         = regexp.MustCompile(`(?i)\b(two|three|four|five|six|seven|eight|nine|ten|eleven|twelve|thirteen|fourteen|fifteen|twenty|thirty|forty|fifty|sixty|seventy|eighty|ninety|twenty-one|twenty-eight|twenty-four|twenty-seven|forty-five|sixty-five|ninety-six)[- ](calendar |business |working |)days?\b`)
	hourRE            = regexp.MustCompile(`(?i)\b(\d{1,3})[- ]hours?\b`)
	yearRE            = regexp.MustCompile(`(?i)\b(\d{1,2})[- ]?(?:\+\s*)?years?\b`)
	moneyRE           = regexp.MustCompile(`\$\s?(\d{1,3}(?:,\d{3})*(?:\.\d{1,2})?)`)
	countRE           = regexp.MustCompile(`\b(\d{1,4})\+?\s*(?:or more\s*)?(?:employees|workers)\b`)
	annotationDaysRE  = regexp.MustCompile(`^(\d+)d$`)
	annotationCountRE = regexp.MustCompile(`^(\d+)\+?$`)
)

// dayWords maps the number words the research corpus uses for day counts.
var dayWords = map[string]int{
	"two": 2, "three": 3, "four": 4, "five": 5, "six": 6, "seven": 7,
	"eight": 8, "nine": 9, "ten": 10, "eleven": 11, "twelve": 12,
	"thirteen": 13, "fourteen": 14, "fifteen": 15, "twenty": 20,
	"twenty-one": 21, "twenty-four": 24, "twenty-seven": 27,
	"twenty-eight": 28, "thirty": 30, "forty": 40, "forty-five": 45,
	"fifty": 50, "sixty": 60, "sixty-five": 65, "seventy": 70,
	"eighty": 80, "ninety": 90, "ninety-six": 96,
}

func dayBasis(qualifier string) string {
	switch strings.TrimSpace(strings.ToLower(qualifier)) {
	case "business", "working":
		return "BUSINESS"
	case "calendar":
		return "CALENDAR"
	default:
		return ""
	}
}

// ExtractDays returns the first day count and its basis, or (0, ""). A count
// stated in words ("seven calendar days' notice") or hours ("24 hours'
// notice") converts to days — rounding a partial day up, the narrower
// reading for the platform.
func ExtractDays(text string) (int, string) {
	if m := dayRE.FindStringSubmatch(text); m != nil {
		n, err := strconv.Atoi(m[1])
		if err == nil {
			return n, dayBasis(m[2])
		}
	}
	if m := wordDayRE.FindStringSubmatch(text); m != nil {
		if n, ok := dayWords[strings.ToLower(m[1])]; ok {
			return n, dayBasis(m[2])
		}
	}
	if m := hourRE.FindStringSubmatch(text); m != nil &&
		strings.Contains(strings.ToLower(text), "notice") {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			return (n + 23) / 24, ""
		}
	}
	return 0, ""
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

// legalAbbrev is the set of tokens whose trailing period is an abbreviation,
// not a sentence break: "Ala. Code § 25-1-30" must not cut a citation note in
// half at "Ala.".
var legalAbbrev = map[string]bool{
	"ala": true, "ann": true, "app": true, "ariz": true, "ark": true,
	"cal": true, "civ": true, "colo": true, "conn": true, "del": true,
	"fig": true, "fla": true, "ga": true, "gen": true, "haw": true,
	"idaho": true, "ill": true, "ind": true, "iowa": true, "kan": true,
	"ky": true, "la": true, "lab": true, "mass": true, "md": true,
	"me": true, "mich": true, "minn": true, "miss": true, "mo": true,
	"mont": true, "n": true, "neb": true, "nev": true, "no": true,
	"nos": true, "okla": true, "or": true, "ore": true, "pa": true,
	"rev": true, "s": true, "seq": true, "stat": true, "tenn": true,
	"tex": true, "u": true, "utah": true, "v": true, "va": true,
	"vt": true, "w": true, "wash": true, "wis": true, "wyo": true,
}

// sentenceBreakAt reports whether the ". " at s[idx:idx+2] ends a sentence.
// A period closing a legal citation abbreviation does not.
func sentenceBreakAt(s string, idx int) bool {
	word := s[:idx]
	if cut := strings.LastIndexAny(word, " \t"); cut >= 0 {
		word = word[cut+1:]
	}
	word = strings.TrimRight(word, ".,;:)]")
	return !legalAbbrev[strings.ToLower(word)]
}

// Summarize trims an item's text down to a citation note: the first sentence,
// capped so a definition file stays readable. It is a excerpt of the
// repository's own research prose, never of a statute.
func Summarize(text string, max int) string {
	s := strings.TrimSpace(text)
	for idx := 0; ; {
		next := strings.Index(s[idx:], ". ")
		if next < 0 {
			break
		}
		idx += next
		if idx > 0 && idx < max && sentenceBreakAt(s, idx) {
			s = s[:idx+1]
			break
		}
		idx += 2
		if idx >= len(s) {
			break
		}
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
