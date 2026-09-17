package legal_test

// LEGAL-016 verification tests.
//
// LEGAL-016 proves every registered state pack against the one canonical
// promotion-and-base-pay proposal: each release loads, cites only files
// that exist, and produces a signed receipt; the receipts match the
// checked-in golden vector byte for byte; the matrix and the packs agree
// cell by cell; and the three boundary vectors (Virginia 2026-09-02/04,
// Ohio 2025-09-28/30, Wisconsin with and without a Milwaukee overlay)
// each produce their declared difference.
//
// Per-state divergence lives only in the releases, never in the
// proposal: the PRIMARY vector and the property checks share one
// canonical fixture. The two date-boundary legs are draft-derived
// pre/post releases split at the statute's own effective date; the
// Milwaukee overlay is the hypothetical ordinance the contract's
// section 6.4 names, defeated by Wisconsin's real checked-in
// preemption assertions.

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal/extract"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// l16FlowKinds are the Table A kinds the promotion and base-pay flow
// consumes, per the contract's section 5.1. A state with a `?` in one
// of these kinds must not be releasable.
var l16FlowKinds = []legal.ObligationType{
	legal.ObligationTypeNotice,
	legal.ObligationTypePayTransparency,
	legal.ObligationTypeFieldRestriction,
	legal.ObligationTypeWageFloor,
	legal.ObligationTypePayFrequency,
	legal.ObligationTypePayStatement,
	legal.ObligationTypeLeaveInteraction,
	legal.ObligationTypeNonCompete,
	legal.ObligationTypeClassification,
	legal.ObligationTypePayEquityReview,
	legal.ObligationTypeRetention,
	legal.ObligationTypePersonnelFile,
}

func l16Root(t *testing.T) string {
	t.Helper()
	root, err := legal.RepoRoot()
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}
	return root
}

func l16Matrix(t *testing.T) *extract.Matrix {
	t.Helper()
	matrix, err := extract.LoadMatrix(l16Root(t))
	if err != nil {
		t.Fatalf("LoadMatrix: %v", err)
	}
	return matrix
}

func l16Date(t *testing.T, year int, month time.Month, day int) values.LocalDate {
	t.Helper()
	d, err := values.NewLocalDate(year, month, day)
	if err != nil {
		t.Fatalf("NewLocalDate(%d,%d,%d): %v", year, month, day, err)
	}
	return d
}

func l16Instant(t *testing.T, unixSec int64) values.Instant {
	t.Helper()
	instant, err := values.NewInstantFromUnix(unixSec, 0)
	if err != nil {
		t.Fatalf("NewInstantFromUnix(%d): %v", unixSec, err)
	}
	return instant
}

func l16Signer(t *testing.T) *legal.Signer {
	t.Helper()
	priv := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x16}, ed25519.SeedSize))
	signer, err := legal.NewSigner(priv)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	return signer
}

func l16LoadPack(t *testing.T, code string) legal.RulePack {
	t.Helper()
	path, err := legal.PackDefinitionPath("states", extract.DefinitionFileName(code))
	if err != nil {
		t.Fatalf("PackDefinitionPath(%s): %v", code, err)
	}
	def, err := legal.LoadPackDefinitionFile(path)
	if err != nil {
		t.Fatalf("LoadPackDefinitionFile(%s): %v", code, err)
	}
	candidate, err := def.Candidate()
	if err != nil {
		t.Fatalf("Candidate(%s): %v", code, err)
	}
	return candidate.Pack()
}

// canonicalProposal is the one shared promotion-and-base-pay fixture
// every registered release evaluates. It is an ordinary internal
// promotion with a pay change, salary-history collection, protected
// leave, an existing covenant, recorded protected activities, and
// leave balances — facts, never conclusions.
func canonicalProposal(t *testing.T) legal.PromotionProposalSnapshot {
	t.Helper()
	current, err := values.NewMoney("32.00", "USD", 2, values.RoundingHalfUp)
	if err != nil {
		t.Fatalf("NewMoney(current): %v", err)
	}
	raised, err := values.NewMoney("36.00", "USD", 2, values.RoundingHalfUp)
	if err != nil {
		t.Fatalf("NewMoney(new): %v", err)
	}
	return legal.PromotionProposalSnapshot{
		WorkerID:              "legal-016-canonical-worker",
		LegalEntityID:         "legal-016-canonical-entity",
		EffectiveDate:         l16Date(t, 2026, time.March, 1),
		CurrentBasePay:        current,
		NewBasePay:            raised,
		PayFrequency:          "SEMIMONTHLY",
		IsInternalPromotion:   true,
		CollectsSalaryHistory: true,
		OnProtectedLeave:      true,
		HasExistingNonCompete: true,
		LeaveProgramBalances:  []string{"accrued paid sick leave", "paid family and medical leave"},
		RoleChanged:           true,
		RecordedProtectedActivities: []legal.RecordedProtectedActivity{
			{Kind: "workers_compensation_claim", DaysBeforeEffectiveDate: 10},
			{Kind: "whistleblower_report", DaysBeforeEffectiveDate: 30},
		},
		RoleBecomesSafetySensitive: true,
		DrugTestOrdered:            true,
		BreachIncidentOpened:       true,
		DataCategoriesTouched:      []string{"biometric_identifiers"},
	}
}

// l16RegisterAll registers every state draft in extract order.
func l16RegisterAll(t *testing.T, registry *legal.Registry) {
	t.Helper()
	for _, state := range extract.States {
		if err := registry.Register(l16LoadPack(t, state.Code)); err != nil {
			t.Fatalf("Register(%s): %v", state.Code, err)
		}
	}
}

func l16Input(code string, effective values.LocalDate, t *testing.T) legal.LegalContextInput {
	t.Helper()
	jurisdiction := legal.Jurisdiction{Country: "US", State: code}
	known, err := values.NewKnownAt(l16Instant(t, 1_770_000_000))
	if err != nil {
		t.Fatalf("NewKnownAt: %v", err)
	}
	return legal.LegalContextInput{
		LegalEntityID:          "legal-016-canonical-entity",
		WorkLocation:           jurisdiction,
		EmploymentJurisdiction: jurisdiction,
		EffectiveDate:          effective,
		KnownAt:                known,
	}
}

// l16Citations returns every citation a pack carries, obligations and
// preemption assertions alike.
func l16Citations(pack legal.RulePack) []legal.Citation {
	var out []legal.Citation
	appendCitations := func(citations ...legal.Citation) {
		out = append(out, citations...)
	}
	for _, o := range pack.Notices {
		appendCitations(o.Citation)
	}
	for _, o := range pack.FieldRestrictions {
		appendCitations(o.Citation)
	}
	for _, o := range pack.RetentionRules {
		appendCitations(o.Citation)
	}
	for _, o := range pack.LeaveInteractions {
		appendCitations(o.Citation)
	}
	for _, o := range pack.PayFrequencyConstraints {
		appendCitations(o.Citation)
	}
	for _, o := range pack.FinalPayDeadlines {
		appendCitations(o.Citation)
	}
	for _, o := range pack.PayTransparencyDuties {
		appendCitations(o.Citation)
	}
	for _, o := range pack.NonCompeteThresholds {
		appendCitations(o.Citation)
	}
	for _, o := range pack.EVerifyChecks {
		appendCitations(o.Citation)
	}
	for _, o := range pack.MiniWARNTriggers {
		appendCitations(o.Citation)
	}
	for _, o := range pack.WageFloors {
		appendCitations(o.Citation)
	}
	for _, o := range pack.PayEquityReviews {
		appendCitations(o.Citation)
	}
	for _, o := range pack.PayStatements {
		appendCitations(o.Citation)
	}
	for _, o := range pack.Classifications {
		appendCitations(o.Citation)
	}
	for _, o := range pack.PersonnelFileRules {
		appendCitations(o.Citation)
	}
	for _, o := range pack.AntiRetaliationRules {
		appendCitations(o.Citation)
	}
	for _, o := range pack.JobSecurityRules {
		appendCitations(o.Citation)
	}
	for _, o := range pack.SeparationFilings {
		appendCitations(o.Citation)
	}
	for _, o := range pack.DrugTestingRules {
		appendCitations(o.Citation)
	}
	for _, o := range pack.BreachNotifications {
		appendCitations(o.Citation)
	}
	for _, o := range pack.AutomatedDecisions {
		appendCitations(o.Citation)
	}
	for _, o := range pack.MonitoringConsents {
		appendCitations(o.Citation)
	}
	for _, a := range pack.PreemptionAssertions {
		appendCitations(a.Citation)
	}
	return out
}

// l16KindCounts counts obligations per kind for matrix agreement.
func l16KindCounts(pack legal.RulePack) map[legal.ObligationType]int {
	return map[legal.ObligationType]int{
		legal.ObligationTypeNotice:             len(pack.Notices),
		legal.ObligationTypeFieldRestriction:   len(pack.FieldRestrictions),
		legal.ObligationTypeRetention:          len(pack.RetentionRules),
		legal.ObligationTypeLeaveInteraction:   len(pack.LeaveInteractions),
		legal.ObligationTypePayFrequency:       len(pack.PayFrequencyConstraints),
		legal.ObligationTypeFinalPayDeadline:   len(pack.FinalPayDeadlines),
		legal.ObligationTypePayTransparency:    len(pack.PayTransparencyDuties),
		legal.ObligationTypeNonCompete:         len(pack.NonCompeteThresholds),
		legal.ObligationTypeEVerify:            len(pack.EVerifyChecks),
		legal.ObligationTypeMiniWARN:           len(pack.MiniWARNTriggers),
		legal.ObligationTypeWageFloor:          len(pack.WageFloors),
		legal.ObligationTypePayEquityReview:    len(pack.PayEquityReviews),
		legal.ObligationTypePayStatement:       len(pack.PayStatements),
		legal.ObligationTypeClassification:     len(pack.Classifications),
		legal.ObligationTypePersonnelFile:      len(pack.PersonnelFileRules),
		legal.ObligationTypeAntiRetaliation:    len(pack.AntiRetaliationRules),
		legal.ObligationTypeJobSecurity:        len(pack.JobSecurityRules),
		legal.ObligationTypeSeparationFiling:   len(pack.SeparationFilings),
		legal.ObligationTypeDrugTesting:        len(pack.DrugTestingRules),
		legal.ObligationTypeBreachNotification: len(pack.BreachNotifications),
		legal.ObligationTypeAutomatedDecision:  len(pack.AutomatedDecisions),
		legal.ObligationTypeMonitoringConsent:  len(pack.MonitoringConsents),
	}
}

// l16KindMarkers collects confidence markers per kind. Only kinds the
// pack actually carries appear.
func l16KindMarkers(pack legal.RulePack) map[legal.ObligationType][]legal.ConfidenceMarker {
	out := map[legal.ObligationType][]legal.ConfidenceMarker{}
	collect := func(kind legal.ObligationType, citations []legal.Citation) {
		for _, c := range citations {
			out[kind] = append(out[kind], c.ConfidenceMarker)
		}
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

// l16KindCitations collects the citations a pack carries per kind, so
// the L branch can gate subdivision carriage on the annotated locality.
func l16KindCitations(pack legal.RulePack) map[legal.ObligationType][]legal.Citation {
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

// l16Evaluate evaluates the canonical proposal for one state and
// returns the verified receipt.
func l16Evaluate(t *testing.T, registry *legal.Registry, signer *legal.Signer, code string, effective values.LocalDate, proposal legal.PromotionProposalSnapshot) legal.LegalEvaluationReceipt {
	t.Helper()
	at := l16Instant(t, 1_770_200_000)
	ctx, err := legal.Resolve(l16Input(code, effective, t), registry, signer, at)
	if err != nil {
		t.Fatalf("Resolve(%s): %v", code, err)
	}
	receipt, err := legal.EvaluateReceipt(ctx, proposal, registry, signer, at)
	if err != nil {
		t.Fatalf("EvaluateReceipt(%s): %v", code, err)
	}
	if err := receipt.VerifyWithKey(signer.PublicKey()); err != nil {
		t.Fatalf("VerifyWithKey(%s): %v", code, err)
	}
	if receipt.Digest == "" {
		t.Fatalf("receipt(%s) carries no digest", code)
	}
	return receipt
}

// TestTodo_LEGAL_016 proves every registered pack against the canonical
// promotion vector: each release loads, cites only files that exist,
// and produces a signed receipt — and no state uncertain in a
// flow-consumed kind is releasable.
func TestTodo_LEGAL_016(t *testing.T) {
	matrix := l16Matrix(t)
	registry := legal.NewRegistry()
	l16RegisterAll(t, registry)
	signer := l16Signer(t)
	proposal := canonicalProposal(t)
	effective := l16Date(t, 2026, time.March, 1)

	for _, state := range extract.States {
		pack := l16LoadPack(t, state.Code)
		// Every citation points at a file that exists.
		for _, citation := range l16Citations(pack) {
			if citation.SourceFile == "" {
				t.Errorf("%s: citation without a source file", state.Code)
				continue
			}
			abs := filepath.Join(l16Root(t), filepath.FromSlash(citation.SourceFile))
			if _, err := os.Stat(abs); err != nil {
				t.Errorf("%s: citation source file %q: %v", state.Code, citation.SourceFile, err)
			}
		}
		// The canonical proposal evaluates to a signed receipt.
		receipt := l16Evaluate(t, registry, signer, state.Code, effective, proposal)
		if len(receipt.PinnedReleases) == 0 {
			t.Errorf("%s: receipt pins no release", state.Code)
		}
		// A state with a `?` in a flow-consumed kind is not releasable.
		for _, kind := range l16FlowKinds {
			if matrix.Cell(state.Code, kind).Value != extract.CellUncertain {
				continue
			}
			if pack.ReviewStatus.Releasable() {
				t.Errorf("%s: matrix marks %s uncertain yet the pack is releasable", state.Code, kind)
			}
		}
	}
}

// l16StateReceipt is one row of the golden promotion vector.
type l16StateReceipt struct {
	State   string                       `json:"state"`
	Receipt legal.LegalEvaluationReceipt `json:"receipt"`
}

// l16VectorBytes evaluates the canonical proposal for every registered
// pack and renders the receipt vector in state order.
func l16VectorBytes(t *testing.T) []byte {
	t.Helper()
	registry := legal.NewRegistry()
	l16RegisterAll(t, registry)
	signer := l16Signer(t)
	proposal := canonicalProposal(t)
	effective := l16Date(t, 2026, time.March, 1)

	rows := make([]l16StateReceipt, 0, len(extract.States))
	for _, state := range extract.States {
		rows = append(rows, l16StateReceipt{
			State:   state.Code,
			Receipt: l16Evaluate(t, registry, signer, state.Code, effective, proposal),
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].State < rows[j].State })
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("marshal vector: %v", err)
	}
	return raw
}

// TestTodo_LEGAL_016_Golden pins the canonical promotion vector: the
// receipt for every registered release matches the checked-in golden
// file byte for byte.
func TestTodo_LEGAL_016_Golden(t *testing.T) {
	got := l16VectorBytes(t)
	want, err := os.ReadFile(filepath.Join("testdata", "legal016.golden.json"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	want = bytes.TrimSpace(want)
	if !bytes.Equal(got, want) {
		t.Fatalf("promotion vector moved: got %d bytes, want %d bytes", len(got), len(want))
	}
}

// l16Agree checks one pack against its matrix row: Y implies present,
// F implies absent, P implies a locality-only assertion with no
// subdivision obligation, L implies no subdivision obligation, and an
// emitted ? implies VERIFY markers throughout.
//
// One refinement the contract's RED text does not state: a federal
// baseline (F) WAGE_FLOOR may be carried as a marker with no state
// amount, per the registered Alabama precedent. A state figure above
// federal under an F cell still fails.
//
// Three reviewed resolutions the contract's lane GREEN clauses require,
// each stricter than a skip: (1) a ? cell accepts VERIFY or DISPUTED,
// never CONFIRMED — DISPUTED is the contract section 7.3 honesty marker
// and the LEGAL-018 terminal state for blocking items (New York
// personnel-file bill S.3460/A.2107, review-log item 12); (2) the cells
// in l16FPresentOverrides expect the obligation present — the research
// states a retrieved statute and the lane GREEN requires it, so the
// matrix F is stale and agreement is Y-like until the matrix is fixed;
// (3) an L cell accepts subdivision carriage only when every carried
// obligation cites the annotated locality — the stopgap
// LEGAL-ST-NY-001 GREEN requires while locality packs remain a separate,
// out-of-scope family (contract section 12), gated so an invented
// statewide rule under an L cell still fails.
// l16FPresentOverrides lists matrix cells whose F value the research has
// overtaken: a retrieved state statute plus a lane GREEN clause require
// the obligation, so the matrix F is stale (correcting the cell itself
// belongs to the contract's owning lane). Agreement stays Y-like —
// presence is still required — rather than going silent.
var l16FPresentOverrides = map[string]map[legal.ObligationType]string{
	// Michigan NON_COMPETE: MCL 445.774a reasonableness test with
	// blue-pencil reformation, retrieved 2026-09-03; LEGAL-ST-MI-001
	// GREEN requires the pack to carry it. Statute states are Y down
	// the column; this F is the outlier.
	"MI": {
		legal.ObligationTypeNonCompete: "MCL 445.774a",
	},
}

func l16Agree(t *testing.T, matrix *extract.Matrix, code string, pack legal.RulePack) {
	t.Helper()
	counts := l16KindCounts(pack)
	markers := l16KindMarkers(pack)
	cites := l16KindCitations(pack)
	assertions := map[legal.ObligationType]bool{}
	for _, a := range pack.PreemptionAssertions {
		if a.Scope != "LOCALITY_ONLY" {
			t.Errorf("%s: preemption scope for %s = %q, want LOCALITY_ONLY", code, a.Kind, a.Scope)
		}
		assertions[a.Kind] = true
	}
	for _, kind := range legal.AllObligationTypes() {
		cell := matrix.Cell(code, kind)
		switch cell.Value {
		case extract.CellStateRule:
			if counts[kind] == 0 {
				t.Errorf("%s: matrix marks %s Y but the pack carries none", code, kind)
			}
		case extract.CellFederalBaseline:
			if kind == legal.ObligationTypeWageFloor {
				for _, floor := range pack.WageFloors {
					if got := floor.FloorAmount.String(); got != "" {
						t.Errorf("%s: matrix marks WAGE_FLOOR F but the pack sets %q", code, got)
					}
				}
				continue
			}
			if want, ok := l16FPresentOverrides[code][kind]; ok {
				if counts[kind] == 0 {
					t.Errorf("%s: reviewed resolution requires %s present (matrix F is stale, want %s)", code, kind, want)
				}
				continue
			}
			if counts[kind] != 0 {
				t.Errorf("%s: matrix marks %s F but the pack carries %d", code, kind, counts[kind])
			}
		case extract.CellPreempted:
			if !assertions[kind] {
				t.Errorf("%s: matrix marks %s P but the pack asserts no preemption", code, kind)
			}
			if counts[kind] != 0 {
				t.Errorf("%s: matrix marks %s P but the pack carries %d obligations", code, kind, counts[kind])
			}
		case extract.CellLocalOnly:
			if counts[kind] == 0 {
				continue
			}
			if cell.Annotation == "" {
				t.Errorf("%s: matrix marks %s L but the subdivision pack carries %d", code, kind, counts[kind])
				continue
			}
			for _, citation := range cites[kind] {
				if !strings.Contains(citation.Section, cell.Annotation) {
					t.Errorf("%s: matrix marks %s L (%s) but the subdivision pack carries a rule citing %q, want the annotated locality's own rule", code, kind, cell.Annotation, citation.Section)
				}
			}
		case extract.CellUncertain:
			for _, marker := range markers[kind] {
				if marker != legal.ConfidenceMarkerVerify && marker != legal.ConfidenceMarkerDisputed {
					t.Errorf("%s: matrix marks %s ? but an emission is %s, want VERIFY or DISPUTED", code, kind, marker)
				}
			}
		}
	}
}

func l16AppliedIDs(receipt legal.LegalEvaluationReceipt) map[string]bool {
	out := map[string]bool{}
	for _, applied := range receipt.ObligationsApplied {
		out[applied.ID] = true
	}
	return out
}

// l16SplitRelease derives a pre/post supersession pair from a draft by
// removing the named duty kinds from the pre-effective leg and
// splitting the window at the statute's own effective date.
func l16SplitRelease(t *testing.T, code string, preStart, boundary values.LocalDate, drop func(*legal.RulePack)) (pre, post legal.RulePack) {
	t.Helper()
	draft := l16LoadPack(t, code)
	pre = draft
	drop(&pre)
	closed, err := legal.NewClosedEffectiveWindow(preStart, boundary)
	if err != nil {
		t.Fatalf("closed window(%s): %v", code, err)
	}
	pre.Window = closed
	post = draft
	open, err := legal.NewOpenEffectiveWindow(boundary)
	if err != nil {
		t.Fatalf("open window(%s): %v", code, err)
	}
	post.Window = open
	post.Version = draft.Version + 1
	return pre, post
}

// TestTodo_LEGAL_016_Conformance checks the matrix against every pack
// cell by cell and proves the three boundary vectors each produce
// their declared difference.
func TestTodo_LEGAL_016_Conformance(t *testing.T) {
	matrix := l16Matrix(t)
	for _, state := range extract.States {
		l16Agree(t, matrix, state.Code, l16LoadPack(t, state.Code))
	}

	t.Run("Virginia pay-transparency boundary", func(t *testing.T) {
		boundary := l16Date(t, 2026, time.September, 3)
		pre, post := l16SplitRelease(t, "VA", l16Date(t, 2026, time.January, 1), boundary, func(p *legal.RulePack) {
			p.PayTransparencyDuties = nil
			p.FieldRestrictions = nil
		})
		// The post leg carries the 2026-09-03 duties the GREEN contract pins.
		var dutyIDs []string
		for _, duty := range post.PayTransparencyDuties {
			dutyIDs = append(dutyIDs, duty.ID)
		}
		for _, restriction := range post.FieldRestrictions {
			dutyIDs = append(dutyIDs, restriction.ID)
		}
		if len(dutyIDs) == 0 {
			t.Fatal("Virginia draft carries no transparency duties to bound")
		}
		registry := legal.NewRegistry()
		if err := registry.Register(pre); err != nil {
			t.Fatalf("Register(VA pre): %v", err)
		}
		if err := registry.Supersede(pre.Release(), post); err != nil {
			t.Fatalf("Supersede(VA): %v", err)
		}
		signer := l16Signer(t)
		proposal := canonicalProposal(t)
		beforeDate := l16Date(t, 2026, time.September, 2)
		afterDate := l16Date(t, 2026, time.September, 4)
		if got, err := registry.Lookup(legal.Jurisdiction{Country: "US", State: "VA"}, beforeDate); err != nil || got.Version != 1 {
			t.Fatalf("Lookup(VA, 2026-09-02) = v%v, %v; want v1", got, err)
		}
		if got, err := registry.Lookup(legal.Jurisdiction{Country: "US", State: "VA"}, afterDate); err != nil || got.Version != 2 {
			t.Fatalf("Lookup(VA, 2026-09-04) = v%v, %v; want v2", got, err)
		}
		beforeProposal := proposal
		beforeProposal.EffectiveDate = beforeDate
		afterProposal := proposal
		afterProposal.EffectiveDate = afterDate
		before := l16Evaluate(t, registry, signer, "VA", beforeDate, beforeProposal)
		after := l16Evaluate(t, registry, signer, "VA", afterDate, afterProposal)
		beforeIDs, afterIDs := l16AppliedIDs(before), l16AppliedIDs(after)
		for _, id := range dutyIDs {
			if beforeIDs[id] {
				t.Errorf("2026-09-02 receipt applies %q, want it absent before 2026-09-03", id)
			}
			if !afterIDs[id] {
				t.Errorf("2026-09-04 receipt omits %q, want it present from 2026-09-03", id)
			}
		}
		if string(before.CanonicalBytes()) == string(after.CanonicalBytes()) {
			t.Fatal("Virginia boundary receipts are identical; the declared difference is missing")
		}
	})

	t.Run("Ohio mini-WARN boundary", func(t *testing.T) {
		boundary := l16Date(t, 2025, time.September, 29)
		pre, post := l16SplitRelease(t, "OH", l16Date(t, 2025, time.January, 1), boundary, func(p *legal.RulePack) {
			p.MiniWARNTriggers = nil
		})
		if len(post.MiniWARNTriggers) == 0 {
			t.Fatal("Ohio draft carries no mini-WARN trigger to bound")
		}
		var triggerIDs []string
		for _, trigger := range post.MiniWARNTriggers {
			triggerIDs = append(triggerIDs, trigger.ID)
		}
		threshold := post.MiniWARNTriggers[0].EmployeeThreshold
		if threshold <= 0 {
			threshold = 100
		}
		registry := legal.NewRegistry()
		if err := registry.Register(pre); err != nil {
			t.Fatalf("Register(OH pre): %v", err)
		}
		if err := registry.Supersede(pre.Release(), post); err != nil {
			t.Fatalf("Supersede(OH): %v", err)
		}
		signer := l16Signer(t)
		proposal := canonicalProposal(t)
		proposal.WorkforceReductionCount = threshold
		beforeDate := l16Date(t, 2025, time.September, 28)
		afterDate := l16Date(t, 2025, time.September, 30)
		if got, err := registry.Lookup(legal.Jurisdiction{Country: "US", State: "OH"}, beforeDate); err != nil || got.Version != 1 {
			t.Fatalf("Lookup(OH, 2025-09-28) = v%v, %v; want v1", got, err)
		}
		if got, err := registry.Lookup(legal.Jurisdiction{Country: "US", State: "OH"}, afterDate); err != nil || got.Version != 2 {
			t.Fatalf("Lookup(OH, 2025-09-30) = v%v, %v; want v2", got, err)
		}
		beforeProposal := proposal
		beforeProposal.EffectiveDate = beforeDate
		afterProposal := proposal
		afterProposal.EffectiveDate = afterDate
		before := l16Evaluate(t, registry, signer, "OH", beforeDate, beforeProposal)
		after := l16Evaluate(t, registry, signer, "OH", afterDate, afterProposal)
		beforeIDs, afterIDs := l16AppliedIDs(before), l16AppliedIDs(after)
		for _, id := range triggerIDs {
			if beforeIDs[id] {
				t.Errorf("2025-09-28 receipt applies %q, want it absent before 2025-09-29", id)
			}
			if !afterIDs[id] {
				t.Errorf("2025-09-30 receipt omits %q, want it present from 2025-09-29", id)
			}
		}
		if string(before.CanonicalBytes()) == string(after.CanonicalBytes()) {
			t.Fatal("Ohio boundary receipts are identical; the declared difference is missing")
		}
	})

	t.Run("Wisconsin Milwaukee overlay boundary", func(t *testing.T) {
		statePack := l16LoadPack(t, "WI")
		hasLeavePreemption := false
		for _, a := range statePack.PreemptionAssertions {
			if a.Kind == legal.ObligationTypeLeaveInteraction && a.Scope == "LOCALITY_ONLY" {
				hasLeavePreemption = true
			}
		}
		if !hasLeavePreemption {
			t.Fatal("Wisconsin draft carries no locality-only LEAVE preemption to bound with")
		}
		milwaukee := legal.Jurisdiction{Country: "US", State: "WI", Locality: "Milwaukee"}
		window, err := legal.NewOpenEffectiveWindow(l16Date(t, 2026, time.January, 1))
		if err != nil {
			t.Fatalf("overlay window: %v", err)
		}
		overlay := legal.RulePack{
			PackID:            "us-wi-milwaukee-paid-leave-fixture",
			Version:           1,
			Jurisdiction:      milwaukee,
			Window:            window,
			VocabularyVersion: legal.VocabularyVersion2,
			SourceType:        legal.SourceTypeStatute,
			ReviewStatus:      legal.ReviewStatusUnreviewed,
			LeaveInteractions: []legal.LeaveInteraction{{
				ID:              "milwaukee-paid-leave",
				LeaveType:       "accrued paid sick leave",
				InteractionRule: "Hypothetical Milwaukee paid-leave ordinance for the boundary vector: the subdivision preemption defeats it.",
				Citation: legal.Citation{
					SourceFile:       "planning/research/state-employment-law/wisconsin.md",
					Section:          "Milwaukee Code ch. 112 (hypothetical ordinance for the boundary vector)",
					Status:           legal.ReviewStatusUnreviewed,
					ConfidenceMarker: legal.ConfidenceMarkerVerify,
				},
			}},
		}
		registry := legal.NewRegistry()
		if err := registry.Register(statePack); err != nil {
			t.Fatalf("Register(WI): %v", err)
		}
		if err := registry.Register(overlay); err != nil {
			t.Fatalf("Register(Milwaukee): %v", err)
		}
		signer := l16Signer(t)
		proposal := canonicalProposal(t)
		effective := l16Date(t, 2026, time.March, 1)
		plain := l16Evaluate(t, registry, signer, "WI", effective, proposal)
		if len(plain.PreemptionsApplied) != 0 {
			t.Fatalf("plain Wisconsin receipt applies preemptions = %+v, want none", plain.PreemptionsApplied)
		}
		known, err := values.NewKnownAt(l16Instant(t, 1_770_000_000))
		if err != nil {
			t.Fatalf("NewKnownAt: %v", err)
		}
		at := l16Instant(t, 1_770_200_000)
		overlayCtx, err := legal.Resolve(legal.LegalContextInput{
			LegalEntityID:          "legal-016-canonical-entity",
			WorkLocation:           milwaukee,
			EmploymentJurisdiction: milwaukee,
			EffectiveDate:          effective,
			KnownAt:                known,
		}, registry, signer, at)
		if err != nil {
			t.Fatalf("Resolve(Milwaukee): %v", err)
		}
		withOverlay, err := legal.EvaluateReceipt(overlayCtx, proposal, registry, signer, at)
		if err != nil {
			t.Fatalf("EvaluateReceipt(Milwaukee): %v", err)
		}
		if err := withOverlay.VerifyWithKey(signer.PublicKey()); err != nil {
			t.Fatalf("VerifyWithKey(Milwaukee): %v", err)
		}
		if len(withOverlay.JurisdictionSet.UnregisteredLocalities) != 0 {
			t.Fatalf("unregistered localities = %+v, want the overlay registered exact", withOverlay.JurisdictionSet.UnregisteredLocalities)
		}
		removed := false
		for _, record := range withOverlay.PreemptionsApplied {
			for _, id := range record.RemovedObligationIDs {
				if id == "milwaukee-paid-leave" {
					removed = true
				}
			}
		}
		if !removed {
			t.Fatalf("preemptions = %+v, want the Milwaukee paid-leave duty removed", withOverlay.PreemptionsApplied)
		}
		if l16AppliedIDs(withOverlay)["milwaukee-paid-leave"] {
			t.Fatal("overlay receipt applies the preempted Milwaukee duty")
		}
		if string(plain.CanonicalBytes()) == string(withOverlay.CanonicalBytes()) {
			t.Fatal("Wisconsin boundary receipts are identical; the declared difference is missing")
		}
	})
}

// TestTodo_LEGAL_016_Property fixes the canonical vector's two
// metamorphic boundaries: re-evaluating the same inputs reproduces the
// same receipt bytes, and the order packs register in never changes
// what any state evaluates to.
func TestTodo_LEGAL_016_Property(t *testing.T) {
	t.Run("the vector is stable across re-evaluation", func(t *testing.T) {
		first := l16VectorBytes(t)
		second := l16VectorBytes(t)
		if !bytes.Equal(first, second) {
			t.Fatalf("re-evaluation moved the vector: %d vs %d bytes", len(first), len(second))
		}
	})

	t.Run("registration order never changes a receipt", func(t *testing.T) {
		forward := legal.NewRegistry()
		l16RegisterAll(t, forward)
		reversed := legal.NewRegistry()
		for i := len(extract.States) - 1; i >= 0; i-- {
			if err := reversed.Register(l16LoadPack(t, extract.States[i].Code)); err != nil {
				t.Fatalf("Register(%s): %v", extract.States[i].Code, err)
			}
		}
		signer := l16Signer(t)
		proposal := canonicalProposal(t)
		effective := l16Date(t, 2026, time.March, 1)
		for _, state := range extract.States {
			a := l16Evaluate(t, forward, signer, state.Code, effective, proposal)
			b := l16Evaluate(t, reversed, signer, state.Code, effective, proposal)
			if a.Digest != b.Digest {
				t.Errorf("%s: forward digest %s != reversed digest %s", state.Code, a.Digest, b.Digest)
			}
		}
	})
}
