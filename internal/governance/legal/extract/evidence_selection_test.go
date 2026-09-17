package extract

// Mechanism tests for the state-pack evidence selection: every rule the
// LEGAL-ST-IA/ID/IL/IN/KS/KY-001 reviews depend on, pinned at the unit
// level so a later matcher edit cannot silently reintroduce a wrong pick.

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestEvidenceSelection_SectionCodes(t *testing.T) {
	cases := []struct{ text, want string }{
		{"Payroll Record Retention: IC 22-2-8 requires 4 years", "IC 22-2-8"},
		{"paid semimonthly or biweekly, IC 22-2-5-1 requires", "IC 22-2-5-1"},
		{"Record Retention and Notice: K.S.A. 44-320 requires written notice", "K.S.A. 44-320"},
		{"ranges read whole: K.S.A. 44-313 to 44-327 govern", "K.S.A. 44-313 to 44-327"},
		{"lettered suffixes read whole: K.S.A. 50-7a01 requires", "K.S.A. 50-7a01"},
		{"regulations read: per K.A.R. 49-20-1", "K.A.R. 49-20-1"},
		// A co-cited federal section never stands in for the state
		// authority that precedes it in the pattern order.
		{"K.S.A. 44-320 requires notice; records per 29 U.S.C. § 211", "K.S.A. 44-320"},
		{"IC 22-2-8 requires 4 years; FLSA (29 U.S.C. § 211) requires 3", "IC 22-2-8"},
		// A federal-only cite keeps the honest gap.
		{"records per federal FLSA (29 U.S.C. § 211)", SectionNotStated},
		{"nothing cited here", SectionNotStated},
	}
	for _, c := range cases {
		if got := ExtractSection(c.text); got != c.want {
			t.Errorf("ExtractSection(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}

func TestEvidenceSelection_SummarizeSkipsAbbreviations(t *testing.T) {
	cases := []struct{ text, want string }{
		// "No." is Senate Enrolled Act No., not a sentence end.
		{"Physician non-competes: IC 25-22.5-5.5, as amended by Senate Enrolled Act No. 475 (effective July 1, 2025), restricts them.",
			"Physician non-competes: IC 25-22.5-5.5, as amended by Senate Enrolled Act No. 475 (effective July 1, 2025), restricts them."},
		// "K.S.A." is a cite, not a sentence end.
		{"Record Retention and Notice: K.S.A. 44-320 requires written notice of pay rate at hire.",
			"Record Retention and Notice: K.S.A. 44-320 requires written notice of pay rate at hire."},
		// "v." is versus, not a sentence end.
		{"Non-compete agreements are enforced solely under common-law reasonableness (Weber v. Tillman).",
			"Non-compete agreements are enforced solely under common-law reasonableness (Weber v. Tillman)."},
		// A real sentence boundary still cuts.
		{"First sentence carries the rule. Second sentence is detail.",
			"First sentence carries the rule."},
	}
	for _, c := range cases {
		if got := Summarize(c.text, 220); got != c.want {
			t.Errorf("Summarize(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}

func TestEvidenceSelection_DayCounts(t *testing.T) {
	if n, basis := ExtractDays("Model a 3-business-day response window"); n != 3 || basis != "BUSINESS" {
		t.Errorf("hyphenated business days = (%d,%q), want (3,BUSINESS)", n, basis)
	}
	if n, _ := ExtractDays("trigger 30-day notice to employees"); n != 30 {
		t.Errorf("hyphenated day notice = %d, want 30", n)
	}
	if n, basis := ExtractDays("paid within 30 days of separation"); n != 30 || basis != "" {
		t.Errorf("plain day count = (%d,%q), want (30,\"\")", n, basis)
	}
	// The notice period is the count attached to "notice", not the window.
	if n := ExtractNoticeDays("layoffs affecting 50+ employees in 30 days require 60-day notice"); n != 60 {
		t.Errorf("notice days = %d, want 60", n)
	}
	if n := ExtractNoticeDays("no notice period stated"); n != 0 {
		t.Errorf("notice days = %d, want 0", n)
	}
}

func TestEvidenceSelection_PayFrequencyDenial(t *testing.T) {
	if !payFrequencyDenied("Idaho Code § 45-606 does not mandate a specific pay frequency (weekly, bi-weekly, monthly)") {
		t.Error("explicit no-mandate finding not recognized")
	}
	if payFrequencyDenied("Wages must be paid at least semimonthly on designated paydays") {
		t.Error("semimonthly mandate misread as denial")
	}
}

func TestEvidenceSelection_AgeIsAWord(t *testing.T) {
	if bases := protectedBases("peers in substantially similar roles at comparable wages"); hasStr(bases, "age") {
		t.Errorf("bases=%v, \"wage\" is not the protected basis \"age\"", bases)
	}
	if bases := protectedBases("discrimination based on sex, race and age is prohibited"); !hasStr(bases, "age") {
		t.Errorf("bases=%v, standalone age lost", bases)
	}
}

func TestEvidenceSelection_SeventhDayDimension(t *testing.T) {
	if got := classificationDimension("Apply seventh-day premium for seven consecutive days (KRS 337.285)"); got != "OVERTIME_THRESHOLD" {
		t.Errorf("dimension=%q, want OVERTIME_THRESHOLD", got)
	}
	if got := classificationDimension("Contractor vs. Employee: Kansas recognizes the ABC test"); got != "CONTRACTOR" {
		t.Errorf("dimension=%q, want CONTRACTOR", got)
	}
}

func TestEvidenceSelection_NoticeReaders(t *testing.T) {
	if got := noticeDirection("inform employees of any wage reductions before the affected work is performed"); got != "BEFORE" {
		t.Errorf("direction=%q, want BEFORE", got)
	}
	if got := noticeDirection("flag 1-pay-period advance-notice requirement"); got != "BEFORE" {
		t.Errorf("direction=%q, want BEFORE", got)
	}
	fields := noticeContentFields("must inform employees of any wage reductions")
	if !hasStr(fields, "pay_rate") {
		t.Errorf("content=%v, want pay_rate for a wage reduction", fields)
	}
}

// TestEvidenceSelection_Ranking pins the cited-evidence ranking: cited
// beats uncited, earliest statement beats late mention, a Summary rule
// statement beats an Implications paraphrase on an exact tie, and a
// kind-level identity signal beats position.
func TestEvidenceSelection_Ranking(t *testing.T) {
	file := func(items ...Item) *ResearchFile {
		for i := range items {
			items[i].detailOf = -1
		}
		return &ResearchFile{Items: items}
	}
	impl := func(text string) Item {
		return Item{Topic: TopicImplications, Text: text}
	}
	sum := func(text string) Item {
		return Item{Topic: TopicSummary, Text: text}
	}

	t.Run("earliest cited statement wins", func(t *testing.T) {
		f := file(
			impl("Paid leave accrual preservation (820 ILCS 192): on termination include payout in final pay calculation."),
			impl("Final pay calculation (§ 115/5): on termination calculate final pay as gross wages plus accrued vacation."),
		)
		got, found := findEvidence(f, legal.ObligationTypeFinalPayDeadline)
		if !found || !strings.Contains(got.Text, "§ 115/5") {
			t.Fatalf("picked %q, want the final-pay calculation item", got.Text)
		}
	})

	t.Run("summary beats implications on an exact tie", func(t *testing.T) {
		f := file(
			impl("Final Pay and Leave on Potential Termination (§ 91A.4): if the system models termination, model final pay timing."),
			sum("Final pay timing mandated: Iowa Code § 91A.4 requires final wages paid by the next regular payday or within 5 days."),
		)
		got, found := findEvidence(f, legal.ObligationTypeFinalPayDeadline)
		if !found || !strings.Contains(got.Text, "mandated") {
			t.Fatalf("picked %q, want the Summary rule statement", got.Text)
		}
	})

	t.Run("uncited never beats cited", func(t *testing.T) {
		f := file(
			impl("Final pay timing: flag that final payment deadline is the earlier of payday or 10 days."),
			sum("Final pay timing strict (10 days or regular payday): Idaho Code § 45-606 requires wages early."),
		)
		got, found := findEvidence(f, legal.ObligationTypeFinalPayDeadline)
		if !found || !strings.Contains(got.Text, "§ 45-606") {
			t.Fatalf("picked %q, want the cited item", got.Text)
		}
	})

	t.Run("form identity beats position for filings", func(t *testing.T) {
		f := file(
			impl("Retain termination records: preserve separation notice and final-pay documentation for 4 years per IC 22-2-8."),
			impl("Separation and Final-Pay Records: retain separation notices (Form UI-14) as part of the cycle per IC 22-2-8."),
		)
		got, found := findEvidence(f, legal.ObligationTypeSeparationFiling)
		if !found || !strings.Contains(got.Text, "UI-14") {
			t.Fatalf("picked %q, want the UI-14 item", got.Text)
		}
	})
}

// TestEvidenceSelection_Merge pins the Implications detail merge: a
// numbered head absorbs its adjacent bullets (which stay searchable), while
// a blank line, a group change, or a non-Implications section breaks
// ownership.
func TestEvidenceSelection_Merge(t *testing.T) {
	md := "## 11. Implications for P1A/P1B\n\n1. **Iowa WARN (§ 84C) - Layoff Scenario**: If the system models a future layoff:\n   - For 25+ employees, trigger 30-day notice.\n   - Distinguish from federal WARN (60 days for 50+ employees).\n\n2. **Pay-Rate Change Notice (§ 91A.3)**: Mandatory.\n\n**Previously unverified items:**\n\n- **820 ILCS 70 (Credit)**: Prohibits credit checks.\n\n## 3. Wages\n\n1. **Numbered elsewhere**: not merged.\n   - Detail bullet elsewhere stays separate.\n"
	f := ParseResearch("test.md", md)
	var head *Item
	bullets := 0
	for i := range f.Items {
		it := &f.Items[i]
		if strings.HasPrefix(it.Text, "Iowa WARN") {
			head = it
		}
		if strings.HasPrefix(it.Text, "For 25+ employees") || strings.HasPrefix(it.Text, "Distinguish from federal") {
			bullets++
		}
		if strings.HasPrefix(it.Text, "820 ILCS 70") && strings.Contains(it.Text, "Pay-Rate Change Notice") {
			t.Error("resolved bullet merged into the numbered head across a group change")
		}
	}
	if head == nil {
		t.Fatal("WARN head missing")
	}
	if !strings.Contains(head.Text, "25+ employees") || !strings.Contains(head.Text, "federal WARN") {
		t.Errorf("head did not absorb its detail bullets: %q", head.Text)
	}
	if bullets != 2 {
		t.Errorf("detail bullets not kept searchable: found %d", bullets)
	}
	for _, it := range f.Items {
		if strings.HasPrefix(it.Text, "Numbered elsewhere") && strings.Contains(it.Text, "Detail bullet elsewhere") {
			t.Errorf("non-Implications head absorbed bullets: %q", it.Text)
		}
	}
}

func hasStr(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
