package extract

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

// repoRoot resolves the repository root for every test in this package.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := legal.RepoRoot()
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}
	return root
}

// --- TestTodo_LEGAL_010_Golden ----------------------------------------------

// TestTodo_LEGAL_010_Golden is the second half of LEGAL-010's GOLDEN row (the
// first half, in package legal, pins the canonical release encoding): running
// the extractor again over the same research corpus and the same contract
// reproduces the fifty checked-in definition files byte for byte.
//
// That is what makes the checked-in files reviewable. If regeneration drifted,
// a reviewer could not tell an intentional rule change from extractor noise,
// and the "mechanical extraction, no invention" claim in the contract's
// section 7.1 would be unverifiable.
func TestTodo_LEGAL_010_Golden(t *testing.T) {
	root := repoRoot(t)
	files, err := Generate(root)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(files) != 50 {
		t.Fatalf("generated %d definition files, want 50", len(files))
	}

	for _, f := range files {
		abs := filepath.Join(root, filepath.FromSlash(f.RelPath))
		onDisk, err := os.ReadFile(abs)
		if err != nil {
			t.Errorf("%s: %v", f.RelPath, err)
			continue
		}
		if string(onDisk) == string(f.Contents) {
			continue
		}
		t.Errorf("%s is not byte-identical to a fresh extraction (%d bytes on disk, %d generated); "+
			"regenerate with WriteAll and review the diff",
			f.RelPath, len(onDisk), len(f.Contents))
		if diff := firstDifference(string(onDisk), string(f.Contents)); diff != "" {
			t.Errorf("%s: %s", f.RelPath, diff)
		}
	}

	// Determinism within one process: two runs of the extractor over the same
	// inputs must agree, or a map iteration has leaked into the output.
	second, err := Generate(root)
	if err != nil {
		t.Fatalf("Generate (second pass): %v", err)
	}
	for i := range files {
		if files[i].RelPath != second[i].RelPath || string(files[i].Contents) != string(second[i].Contents) {
			t.Fatalf("two extractor runs disagreed at %s; the extraction is not deterministic", files[i].RelPath)
		}
	}
}

func firstDifference(a, b string) string {
	aLines := strings.Split(a, "\n")
	bLines := strings.Split(b, "\n")
	for i := 0; i < len(aLines) && i < len(bLines); i++ {
		if aLines[i] != bLines[i] {
			return fmt.Sprintf("first difference at line %d:\n  on disk:   %s\n  generated: %s",
				i+1, aLines[i], bLines[i])
		}
	}
	return fmt.Sprintf("files agree for %d lines but differ in length (%d vs %d lines)",
		min(len(aLines), len(bLines)), len(aLines), len(bLines))
}

// --- TestTodo_LEGAL_CFG_Conformance ------------------------------------------

// TestTodo_LEGAL_CFG_Conformance is the contract's section 8.3 completeness
// oracle. It is what keeps section 5 and the code from drifting apart: every
// definition file loads, every citation names a research file that exists, and
// every matrix cell agrees with the pack it describes.
func TestTodo_LEGAL_CFG_Conformance(t *testing.T) {
	root := repoRoot(t)
	matrix, err := LoadMatrix(root)
	if err != nil {
		t.Fatalf("LoadMatrix: %v", err)
	}

	t.Run("the matrix parses to fifty rows keyed to the fifty research files", func(t *testing.T) {
		if len(matrix.States) != 50 {
			t.Fatalf("matrix has %d rows, want 50", len(matrix.States))
		}
		if len(States) != 50 {
			t.Fatalf("the extractor knows %d states, want 50", len(States))
		}
		for _, state := range States {
			if _, ok := matrix.Cells[state.Code]; !ok {
				t.Errorf("%s has a research file but no matrix row", state.Code)
			}
			path := filepath.Join(root, filepath.FromSlash(legal.ResearchDir), state.File)
			if _, err := os.Stat(path); err != nil {
				t.Errorf("%s names a research file that does not exist: %v", state.Code, err)
			}
		}
		for _, code := range matrix.States {
			if _, ok := StateByCode(code); !ok {
				t.Errorf("matrix row %s has no research file", code)
			}
		}
	})

	t.Run("every cell holds a legend value", func(t *testing.T) {
		legend := map[Cell]bool{
			CellStateRule: true, CellLocalOnly: true, CellPreempted: true,
			CellFederalBaseline: true, CellUncertain: true,
		}
		for _, code := range matrix.States {
			for kind, cell := range matrix.Cells[code] {
				if !legend[cell.Value] {
					t.Errorf("%s/%s holds %q, which is outside the section 5 legend", code, kind, cell.Value)
				}
			}
		}
	})

	t.Run("MONITORING_CONSENT's five states are still the five the contract names", func(t *testing.T) {
		// The kind is stated in prose rather than as a column, so the list is
		// transcribed in matrix.go. This re-reads the sentence.
		contract, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(ContractPath)))
		if err != nil {
			t.Fatalf("reading the contract: %v", err)
		}
		text := string(contract)
		idx := strings.Index(text, "`MONITORING_CONSENT` is not a column")
		if idx < 0 {
			t.Fatal("the contract no longer states MONITORING_CONSENT in prose; the transcription in matrix.go is stale")
		}
		paragraph := text[idx:]
		if end := strings.Index(paragraph, "\n\n"); end > 0 {
			paragraph = paragraph[:end]
		}
		for _, want := range []string{"Illinois", "Colorado", "New York", "California", "Texas"} {
			if !strings.Contains(paragraph, want) {
				t.Errorf("the MONITORING_CONSENT paragraph no longer names %s", want)
			}
		}
	})

	// The completeness oracle proper.
	for _, state := range States {
		t.Run(state.Code, func(t *testing.T) {
			path := filepath.Join(root, filepath.FromSlash(legal.PackDefinitionDir),
				StatesDir, DefinitionFileName(state.Code))
			def, err := legal.LoadPackDefinitionFile(path)
			if err != nil {
				t.Fatalf("loading the %s draft: %v", state.Code, err)
			}
			candidate, err := def.Candidate()
			if err != nil {
				t.Fatalf("validating the %s draft: %v", state.Code, err)
			}
			pack := candidate.Pack()

			// Every citation resolves to a file that exists under the research
			// directory. A citation pointing at nothing is a rule with no
			// provenance.
			for _, o := range def.Obligations {
				if !strings.HasPrefix(o.Citation.SourceFile, legal.ResearchDir+"/") {
					t.Errorf("%s: obligation %q cites %q, outside %s",
						state.Code, o.ID, o.Citation.SourceFile, legal.ResearchDir)
					continue
				}
				abs := filepath.Join(root, filepath.FromSlash(o.Citation.SourceFile))
				if _, err := os.Stat(abs); err != nil {
					t.Errorf("%s: obligation %q cites a missing file: %v", state.Code, o.ID, err)
				}
				if strings.TrimSpace(o.Citation.Section) == "" {
					t.Errorf("%s: obligation %q carries an empty section", state.Code, o.ID)
				}
				if o.Citation.ReviewStatus != legal.ReviewStatusUnreviewed.String() {
					t.Errorf("%s: obligation %q is %s; every extracted draft rule stays UNREVIEWED",
						state.Code, o.ID, o.Citation.ReviewStatus)
				}
				if o.Citation.ConfidenceMarker == "" {
					t.Errorf("%s: obligation %q carries no confidence marker", state.Code, o.ID)
				}
			}

			// The completeness oracle: Y has the kind, F has none.
			//
			// Three reviewed resolutions shared with l16Agree in
			// internal/governance/legal/legal_016_test.go, each stricter
			// than a skip — the two oracles must agree cell by cell, so a
			// resolution recorded there applies here unchanged:
			// (1) a federal-baseline (F) WAGE_FLOOR may be carried as a
			// marker with no state amount, per the registered Alabama
			// precedent; a state figure above federal under an F cell
			// still fails;
			// (2) the cells in fPresentOverrides expect the obligation
			// present — the research states a retrieved statute and the
			// lane GREEN requires it, so the matrix F is stale and
			// agreement is Y-like until the matrix is fixed (correcting
			// the cell itself belongs to the contract's owning lane);
			// (3) an L cell accepts subdivision carriage only when every
			// carried obligation cites the annotated locality — the
			// stopgap LEGAL-ST-NY-001 GREEN requires while locality packs
			// remain a separate, out-of-scope family (contract section
			// 12), gated so an invented statewide rule under an L cell
			// still fails.
			counts := pack.KindCounts()
			cites := packKindCitations(pack)
			for _, kind := range legal.AllObligationTypes() {
				cell := matrix.Cell(state.Code, kind)
				switch cell.Value {
				case CellStateRule:
					if counts[kind] == 0 {
						t.Errorf("%s/%s is Y in the matrix and the pack carries no such obligation",
							state.Code, kind)
					}
				case CellFederalBaseline:
					if kind == legal.ObligationTypeWageFloor {
						for _, floor := range pack.WageFloors {
							if got := floor.FloorAmount.String(); got != "" {
								t.Errorf("%s/%s is F in the matrix and the pack sets a state amount %q",
									state.Code, kind, got)
							}
						}
						continue
					}
					if want, ok := fPresentOverrides[state.Code][kind]; ok {
						if counts[kind] == 0 {
							t.Errorf("%s/%s is F in the matrix but the reviewed resolution requires it present (want %s)",
								state.Code, kind, want)
						}
						continue
					}
					if counts[kind] != 0 {
						t.Errorf("%s/%s is F in the matrix and the pack carries %d such obligation(s)",
							state.Code, kind, counts[kind])
					}
				case CellLocalOnly:
					if counts[kind] == 0 {
						continue
					}
					if cell.Annotation == "" {
						t.Errorf("%s/%s is L (locality rule only) and the subdivision pack carries %d",
							state.Code, kind, counts[kind])
						continue
					}
					for _, citation := range cites[kind] {
						if !strings.Contains(citation.Section, cell.Annotation) {
							t.Errorf("%s/%s is L (%s) and the subdivision pack carries a rule citing %q, want the annotated locality's own rule",
								state.Code, kind, cell.Annotation, citation.Section)
						}
					}
				case CellPreempted:
					if counts[kind] != 0 {
						t.Errorf("%s/%s is P (state preempts locality rules) and the pack carries %d "+
							"obligation(s); a preemption is an assertion, never an obligation",
							state.Code, kind, counts[kind])
					}
				case CellUncertain:
					// Either way is allowed. What is not allowed is shipping
					// it: the pack must stay at UNREVIEWED while the cell is `?`.
					if pack.ReviewStatus.Releasable() {
						t.Errorf("%s/%s is `?` and the pack claims a releasable review status %s",
							state.Code, kind, pack.ReviewStatus)
					}
				}
			}

			// A `?` anywhere in a kind the flow consumes blocks release, per
			// section 5. Every checked-in draft is UNREVIEWED, so this is a
			// standing invariant rather than a per-state exception.
			if pack.ReviewStatus != legal.ReviewStatusUnreviewed {
				t.Errorf("%s draft review status = %s, want UNREVIEWED", state.Code, pack.ReviewStatus)
			}
			if pack.ReviewStatus.Releasable() {
				t.Errorf("%s draft claims a releasable status", state.Code)
			}
			if err := pack.ValidateForRelease(); err != nil {
				t.Errorf("%s draft fails its own release validation: %v", state.Code, err)
			}

			// The preemption assertions the contract's section 6.4 records.
			wantPreemptions := map[legal.ObligationType]bool{}
			for _, row := range matrix.Preemptions[state.Code] {
				wantPreemptions[row.Kind] = true
			}
			for _, a := range pack.PreemptionAssertions {
				if !wantPreemptions[a.Kind] {
					t.Errorf("%s asserts preemption of %s, which section 6.4 does not record",
						state.Code, a.Kind)
				}
				delete(wantPreemptions, a.Kind)
			}
			if len(wantPreemptions) != 0 {
				t.Errorf("%s is missing preemption assertions the contract records: %v",
					state.Code, sortedKindNames(wantPreemptions))
			}
		})
	}
}

// fPresentOverrides mirrors l16FPresentOverrides in
// internal/governance/legal/legal_016_test.go: matrix cells whose F value
// the research has overtaken. A retrieved state statute plus a lane GREEN
// clause require the obligation, so the matrix F is stale (correcting the
// cell itself belongs to the contract's owning lane). Agreement stays
// Y-like — presence is still required — rather than going silent.
var fPresentOverrides = map[string]map[legal.ObligationType]string{
	// Michigan NON_COMPETE: MCL 445.774a reasonableness test with
	// blue-pencil reformation, retrieved 2026-09-03; LEGAL-ST-MI-001
	// GREEN requires the pack to carry it. Statute states are Y down
	// the column; this F is the outlier.
	"MI": {
		legal.ObligationTypeNonCompete: "MCL 445.774a",
	},
}

// packKindCitations collects the citations a pack carries per kind, so the
// L branch can gate subdivision carriage on the annotated locality. It
// mirrors l16KindCitations in internal/governance/legal/legal_016_test.go.
func packKindCitations(pack legal.RulePack) map[legal.ObligationType][]legal.Citation {
	out := map[legal.ObligationType][]legal.Citation{}
	collect := func(kind legal.ObligationType, citations []legal.Citation) {
		out[kind] = append(out[kind], citations...)
	}
	for _, o := range pack.Notices {
		collect(legal.ObligationTypeNotice, []legal.Citation{o.Citation})
	}
	for _, o := range pack.FieldRestrictions {
		collect(legal.ObligationTypeFieldRestriction, []legal.Citation{o.Citation})
	}
	for _, o := range pack.RetentionRules {
		collect(legal.ObligationTypeRetention, []legal.Citation{o.Citation})
	}
	for _, o := range pack.LeaveInteractions {
		collect(legal.ObligationTypeLeaveInteraction, []legal.Citation{o.Citation})
	}
	for _, o := range pack.PayFrequencyConstraints {
		collect(legal.ObligationTypePayFrequency, []legal.Citation{o.Citation})
	}
	for _, o := range pack.FinalPayDeadlines {
		collect(legal.ObligationTypeFinalPayDeadline, []legal.Citation{o.Citation})
	}
	for _, o := range pack.PayTransparencyDuties {
		collect(legal.ObligationTypePayTransparency, []legal.Citation{o.Citation})
	}
	for _, o := range pack.NonCompeteThresholds {
		collect(legal.ObligationTypeNonCompete, []legal.Citation{o.Citation})
	}
	for _, o := range pack.EVerifyChecks {
		collect(legal.ObligationTypeEVerify, []legal.Citation{o.Citation})
	}
	for _, o := range pack.MiniWARNTriggers {
		collect(legal.ObligationTypeMiniWARN, []legal.Citation{o.Citation})
	}
	for _, o := range pack.WageFloors {
		collect(legal.ObligationTypeWageFloor, []legal.Citation{o.Citation})
	}
	for _, o := range pack.PayEquityReviews {
		collect(legal.ObligationTypePayEquityReview, []legal.Citation{o.Citation})
	}
	for _, o := range pack.PayStatements {
		collect(legal.ObligationTypePayStatement, []legal.Citation{o.Citation})
	}
	for _, o := range pack.Classifications {
		collect(legal.ObligationTypeClassification, []legal.Citation{o.Citation})
	}
	for _, o := range pack.PersonnelFileRules {
		collect(legal.ObligationTypePersonnelFile, []legal.Citation{o.Citation})
	}
	for _, o := range pack.AntiRetaliationRules {
		collect(legal.ObligationTypeAntiRetaliation, []legal.Citation{o.Citation})
	}
	for _, o := range pack.JobSecurityRules {
		collect(legal.ObligationTypeJobSecurity, []legal.Citation{o.Citation})
	}
	for _, o := range pack.SeparationFilings {
		collect(legal.ObligationTypeSeparationFiling, []legal.Citation{o.Citation})
	}
	for _, o := range pack.DrugTestingRules {
		collect(legal.ObligationTypeDrugTesting, []legal.Citation{o.Citation})
	}
	for _, o := range pack.BreachNotifications {
		collect(legal.ObligationTypeBreachNotification, []legal.Citation{o.Citation})
	}
	for _, o := range pack.AutomatedDecisions {
		collect(legal.ObligationTypeAutomatedDecision, []legal.Citation{o.Citation})
	}
	for _, o := range pack.MonitoringConsents {
		collect(legal.ObligationTypeMonitoringConsent, []legal.Citation{o.Citation})
	}
	return out
}

func sortedKindNames(set map[legal.ObligationType]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k.String())
	}
	sort.Strings(out)
	return out
}

// --- TestExtractionNeverBroadensARecommendation ------------------------------

// TestExtractionNeverBroadensARecommendation is the contract's section 7.1
// one-way rule, checked directly against the extractor's own output: a
// research item written as advice never becomes a statutory requirement,
// and a rule the extractor could not evidence never claims a section it
// does not have.
//
// The check runs over the fully mechanical extraction (no reviewed
// inputs), because the one-way rule binds the extractor, not the reviews:
// a review may carry advisory detail under a confirmed duty — Montana's
// breach rule confirms the statutory notice duty while its 3-5-day
// timeframe stays advisory — and that reviewed pairing (pinned by the
// LEGAL-ST-MT-001 golden) is not an item written as advice. Reviewed
// content answers to the state goldens; this guard answers for the
// extractor.
func TestExtractionNeverBroadensARecommendation(t *testing.T) {
	root := repoRoot(t)
	matrix, err := LoadMatrix(root)
	if err != nil {
		t.Fatalf("LoadMatrix: %v", err)
	}

	recommendations, unevidenced := 0, 0
	for _, state := range States {
		relPath := filepath.ToSlash(filepath.Join(legal.ResearchDir, state.File))
		file, err := ParseResearchFile(root, relPath)
		if err != nil {
			t.Fatalf("ParseResearchFile(%s): %v", state.Code, err)
		}
		extraction, err := ExtractState(matrix, file, state)
		if err != nil {
			t.Fatalf("ExtractState(%s): %v", state.Code, err)
		}
		for _, o := range extraction.Definition.Obligations {
			if o.Body.Standard == legal.RuleStandardRecommended.String() {
				recommendations++
				if o.Citation.ConfidenceMarker != legal.ConfidenceMarkerVerify.String() {
					t.Errorf("%s: %q is RECOMMENDED but marked %s; a recommendation is never confirmed law",
						state.Code, o.ID, o.Citation.ConfidenceMarker)
				}
			}
			if o.Citation.Section == SectionNotStated {
				unevidenced++
				if o.Citation.ConfidenceMarker != legal.ConfidenceMarkerVerify.String() {
					t.Errorf("%s: %q names no statutory section but is marked %s",
						state.Code, o.ID, o.Citation.ConfidenceMarker)
				}
			}
		}
	}
	if recommendations == 0 {
		t.Error("no rule in the corpus was extracted as a recommendation; the section 7.1 guard is not exercised")
	}
	t.Logf("extracted %d RECOMMENDED rules and %d rules with no statutory section, all marked VERIFY",
		recommendations, unevidenced)
}

// --- TestExtractionIsPureOverItsInputs ---------------------------------------

// TestExtractionIsPureOverItsInputs proves the parser reads content, not
// layout: reflowing a research file's whitespace and emphasis must not change
// what comes out.
func TestExtractionIsPureOverItsInputs(t *testing.T) {
	root := repoRoot(t)
	matrix, err := LoadMatrix(root)
	if err != nil {
		t.Fatalf("LoadMatrix: %v", err)
	}
	state, _ := StateByCode("MN")
	relPath := filepath.ToSlash(filepath.Join(legal.ResearchDir, state.File))
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relPath)))
	if err != nil {
		t.Fatalf("reading %s: %v", relPath, err)
	}

	baseline, err := ExtractState(matrix, ParseResearch(relPath, string(raw)), state)
	if err != nil {
		t.Fatalf("ExtractState: %v", err)
	}
	// Windows line endings and doubled interior spaces are layout, not
	// content.
	reflowed := strings.ReplaceAll(string(raw), "\n", "\r\n")
	reflowed = strings.ReplaceAll(reflowed, ": ", ":  ")
	got, err := ExtractState(matrix, ParseResearch(relPath, reflowed), state)
	if err != nil {
		t.Fatalf("ExtractState(reflowed): %v", err)
	}

	wantBytes, err := legal.MarshalPackDefinition(baseline.Definition)
	if err != nil {
		t.Fatalf("MarshalPackDefinition: %v", err)
	}
	gotBytes, err := legal.MarshalPackDefinition(got.Definition)
	if err != nil {
		t.Fatalf("MarshalPackDefinition: %v", err)
	}
	if string(wantBytes) != string(gotBytes) {
		t.Errorf("reflowing whitespace changed the extraction: %s",
			firstDifference(string(wantBytes), string(gotBytes)))
	}
}

// --- TestMatrixTotalsMatchTheContract ----------------------------------------

// TestMatrixTotalsMatchTheContract recomputes the column totals the contract
// states beneath each table. A total that no longer adds up means a cell was
// edited without the summary being updated, and the completeness oracle above
// would then be checking the code against a stale table.
func TestMatrixTotalsMatchTheContract(t *testing.T) {
	matrix, err := LoadMatrix(repoRoot(t))
	if err != nil {
		t.Fatalf("LoadMatrix: %v", err)
	}
	stated := map[legal.ObligationType]int{
		legal.ObligationTypeNotice:             22,
		legal.ObligationTypePayTransparency:    16,
		legal.ObligationTypeFieldRestriction:   20,
		legal.ObligationTypeWageFloor:          30,
		legal.ObligationTypePayFrequency:       31,
		legal.ObligationTypePayStatement:       18,
		legal.ObligationTypeLeaveInteraction:   28,
		legal.ObligationTypeNonCompete:         35,
		legal.ObligationTypeClassification:     10,
		legal.ObligationTypePayEquityReview:    36,
		legal.ObligationTypeRetention:          31,
		legal.ObligationTypePersonnelFile:      18,
		legal.ObligationTypeFinalPayDeadline:   44,
		legal.ObligationTypeMiniWARN:           17,
		legal.ObligationTypeSeparationFiling:   9,
		legal.ObligationTypeEVerify:            17,
		legal.ObligationTypeDrugTesting:        13,
		legal.ObligationTypeAntiRetaliation:    50,
		legal.ObligationTypeJobSecurity:        4,
		legal.ObligationTypeBreachNotification: 49,
		legal.ObligationTypeAutomatedDecision:  3,
		legal.ObligationTypeMonitoringConsent:  5,
	}
	counted := matrix.KindTotals()
	for kind, want := range stated {
		if got := counted[kind]; got != want {
			t.Errorf("%s: the contract states a column total of %d, the cells count %d", kind, want, got)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
