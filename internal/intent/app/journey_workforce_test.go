package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// workerInputFixture is a complete, valid create form placed on a job code,
// grade and pay zone the fixture catalog really covers.
func workerInputFixture() workspace.WorkerInput {
	return workspace.WorkerInput{
		LegalName:     "Ada Lovelace",
		PreferredName: "Ada",
		JobCode:       "OPS-HRBP2",
		Grade:         "P2",
		OrgUnit:       "people-ops",
		PositionID:    "POS-HRBP-204",
		Location:      "Boston, MA",
		PayZone:       "US-EAST",
		BasePay:       "90000.00",
		Currency:      "USD",
		BonusTarget:   "0.0500",
		HireDate:      "2021-04-05",
	}
}

// TestWorkforceOptionsAreDerivedFromTheCatalogAndTheCorpus proves the options
// are the closed set the RED clause requires rather than a hand-maintained
// list: every job code, grade and pay zone in them comes from a real pay band,
// and every org unit and position from a real corpus worker.
func TestWorkforceOptionsAreDerivedFromTheCatalogAndTheCorpus(t *testing.T) {
	options, err := workforceOptions()
	if err != nil {
		t.Fatalf("workforceOptions: %v", err)
	}
	if options.Currency != "USD" {
		t.Fatalf("currency = %q, want the catalog's single declared currency", options.Currency)
	}

	scopes, err := fixtures.BandScopes()
	if err != nil {
		t.Fatalf("fixtures.BandScopes: %v", err)
	}
	// PROMOUX-001: workforceOptions now also publishes the demo company's own
	// career ladder (internal/data/demoworkforce), so job codes, grades,
	// placements and promotion paths are the UNION of the fixed four-worker
	// corpus's catalog and the demoworkforce ladder -- one governed
	// job-architecture model instead of the corpus catalog alone, which
	// never mentioned any of the seeded workers' real job codes.
	demoEdges := demoworkforce.PromotionPaths()
	jobCodes, grades, payZones := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, s := range scopes {
		jobCodes[s.JobCode], grades[s.Grade], payZones[s.PayZone] = true, true, true
	}
	for _, edge := range demoEdges {
		jobCodes[edge.SourceJobCode], jobCodes[edge.TargetJobCode] = true, true
		grades[edge.SourceGrade], grades[edge.TargetGrade] = true, true
	}
	for _, tc := range []struct {
		name  string
		got   []string
		valid map[string]bool
	}{
		{"job codes", options.JobCodes, jobCodes},
		{"grades", options.Grades, grades},
		{"pay zones", options.PayZones, payZones},
	} {
		if len(tc.got) != len(tc.valid) {
			t.Errorf("%s = %v, want the catalog's own %d distinct values", tc.name, tc.got, len(tc.valid))
		}
		for _, v := range tc.got {
			if !tc.valid[v] {
				t.Errorf("%s offers %q, which no pay band or demo ladder edge declares", tc.name, v)
			}
		}
		for i := 1; i < len(tc.got); i++ {
			if tc.got[i-1] >= tc.got[i] {
				t.Errorf("%s is not sorted: %v", tc.name, tc.got)
				break
			}
		}
	}
	if len(options.Placements) < len(scopes) {
		t.Fatalf("placements = %d, want at least one for each of %d catalog scopes", len(options.Placements), len(scopes))
	}
	for i, scope := range scopes {
		placement := options.Placements[i]
		if placement.JobCode != scope.JobCode || placement.Grade != scope.Grade || placement.PayZone != scope.PayZone || placement.Currency != scope.Currency {
			t.Errorf("placements[%d] = %+v, want exact scope %+v", i, placement, scope)
		}
	}
	demoZones := demoworkforce.PayZones()
	wantDemoPlacements := map[string]bool{}
	for _, edge := range demoEdges {
		for _, zone := range demoZones {
			wantDemoPlacements[edge.TargetJobCode+"|"+edge.TargetGrade+"|"+zone] = true
		}
	}
	for _, placement := range options.Placements[len(scopes):] {
		key := placement.JobCode + "|" + placement.Grade + "|" + placement.PayZone
		if !wantDemoPlacements[key] || placement.Currency != "USD" {
			t.Errorf("unexpected demo placement %+v", placement)
		}
	}
	if got, want := len(options.Placements)-len(scopes), len(wantDemoPlacements); got != want {
		t.Fatalf("demo placements = %d, want exactly %d (one per target job/grade/zone, deduplicated)", got, want)
	}

	paths, err := fixtures.PromotionPaths()
	if err != nil {
		t.Fatalf("fixtures.PromotionPaths: %v", err)
	}
	if len(options.PromotionPaths) != len(paths)+len(demoEdges) {
		t.Fatalf("promotion paths = %d, want the published %d fixture paths plus %d demo ladder edges", len(options.PromotionPaths), len(paths), len(demoEdges))
	}
	if got := options.PromotionPaths[0]; got.SourceJobCode != "OPS-HRBP2" || got.SourceGrade != "P2" || got.TargetJobCode != "OPS-HRBP3" || got.TargetGrade != "P3" || got.MinimumBaseIncrease != "0.0500" || got.MaximumBaseIncrease != "0.1500" {
		t.Fatalf("first promotion path lost its governed identity or rules: %+v", got)
	}
	for _, edge := range demoEdges {
		found := false
		for _, path := range options.PromotionPaths[len(paths):] {
			// The published kind is the edge's own: a step inside the
			// source's organization unit is UPWARD, a move into another one
			// is CROSS_FAMILY. Asserting the edge's kind rather than a fixed
			// "UPWARD" is what keeps this test honest now that a job
			// publishes several targets of different kinds.
			if path.SourceJobCode == edge.SourceJobCode && path.SourceGrade == edge.SourceGrade &&
				path.TargetJobCode == edge.TargetJobCode && path.TargetGrade == edge.TargetGrade && path.Kind == edge.Kind {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("demo ladder edge %+v is not published in workforceOptions", edge)
		}
	}

	profiles, err := fixtures.Workers()
	if err != nil {
		t.Fatalf("fixtures.Workers: %v", err)
	}
	orgUnits, positions := map[string]bool{}, map[string]bool{}
	for _, p := range profiles {
		orgUnits[p.OrgUnit], positions[p.PositionID] = true, true
	}
	for _, v := range options.OrgUnits {
		if !orgUnits[v] {
			t.Errorf("org units offer %q, which no corpus worker occupies", v)
		}
	}
	for _, v := range options.Positions {
		if !positions[v] {
			t.Errorf("positions offer %q, which no corpus worker occupies", v)
		}
	}
}

// TestValidatePlacementRefusesAPlacementNoBandCovers is the RED clause of the
// create surface: a worker placed outside every band would have every
// promotion refused later for a reason that has nothing to do with the
// promotion, so the placement is refused now, naming the field.
func TestValidatePlacementRefusesAPlacementNoBandCovers(t *testing.T) {
	for name, mutate := range map[string]func(*workspace.WorkerInput){
		"job code no band declares": func(in *workspace.WorkerInput) { in.JobCode = "OPS-NOSUCH9" },
		"grade the band does not":   func(in *workspace.WorkerInput) { in.Grade = "P7" },
		"zone the band does not":    func(in *workspace.WorkerInput) { in.PayZone = "EU-WEST" },
		"a real code in the wrong zone": func(in *workspace.WorkerInput) {
			// ENG-SWE3/P3 exists, but only in US-WEST. Crossing them is the
			// mistake a free-text form makes and a closed option set does not.
			in.JobCode, in.Grade, in.PayZone = "ENG-SWE3", "P3", "US-EAST"
		},
	} {
		t.Run(name, func(t *testing.T) {
			in := workerInputFixture()
			mutate(&in)
			_, err := validatePlacement(in, "USD")
			if !errors.Is(err, workspace.ErrJourneyInput) {
				t.Fatalf("validatePlacement(%s) = %v, want ErrJourneyInput", name, err)
			}
			if !strings.Contains(err.Error(), "job_code") {
				t.Errorf("refusal %q does not name the field to fix", err)
			}
		})
	}
}

// TestValidatePlacementNamesEveryRequiredField walks the presence rules.
func TestValidatePlacementNamesEveryRequiredField(t *testing.T) {
	for field, mutate := range map[string]func(*workspace.WorkerInput){
		"job_code":    func(in *workspace.WorkerInput) { in.JobCode = "  " },
		"grade":       func(in *workspace.WorkerInput) { in.Grade = "" },
		"org_unit":    func(in *workspace.WorkerInput) { in.OrgUnit = "" },
		"position_id": func(in *workspace.WorkerInput) { in.PositionID = "" },
		"pay_zone":    func(in *workspace.WorkerInput) { in.PayZone = "" },
	} {
		t.Run(field, func(t *testing.T) {
			in := workerInputFixture()
			mutate(&in)
			_, err := validatePlacement(in, "USD")
			if !errors.Is(err, workspace.ErrJourneyInput) {
				t.Fatalf("validatePlacement = %v, want ErrJourneyInput", err)
			}
			if !strings.Contains(err.Error(), field) {
				t.Errorf("refusal %q does not name %s", err, field)
			}
		})
	}

	// A blank location falls back to the org unit rather than being refused:
	// no rule reads it, and refusing a display fact would be theatre.
	in := workerInputFixture()
	in.Location = ""
	placement, err := validatePlacement(in, "USD")
	if err != nil {
		t.Fatalf("validatePlacement(no location): %v", err)
	}
	if placement.Location != in.OrgUnit {
		t.Errorf("location = %q, want the org unit %q", placement.Location, in.OrgUnit)
	}
}

// TestNewWorkerRowDerivesTheRecordTheFormDoesNotSupply proves the identity and
// evidence halves of a created worker are the engine's, not the form's.
func TestNewWorkerRowDerivesTheRecordTheFormDoesNotSupply(t *testing.T) {
	e, principal := workforceEngineFixture(t)
	options, err := workforceOptions()
	if err != nil {
		t.Fatalf("workforceOptions: %v", err)
	}
	row, err := e.newWorkerRow(principal, workerInputFixture(), options)
	if err != nil {
		t.Fatalf("newWorkerRow: %v", err)
	}

	if row.WorkerID == uuid.Nil {
		t.Fatal("the created worker has no identity")
	}
	short := row.WorkerID.String()[:8]
	if row.WorkerKey != "ada-"+short {
		t.Errorf("worker key = %q, want the name slug plus the short id", row.WorkerKey)
	}
	if row.RevisionStream != "people.worker."+row.WorkerID.String() || row.RevisionSequence != 1 {
		t.Errorf("revision = %s@%d, want the worker's own stream at 1", row.RevisionStream, row.RevisionSequence)
	}
	if row.EmploymentID != "emp_"+short || row.AssignmentID != "asg_"+short {
		t.Errorf("employment/assignment = %s/%s, want them derived from the identity",
			row.EmploymentID, row.AssignmentID)
	}
	if row.ManagerRelationshipRef != "rel_mgr_"+short {
		t.Errorf("manager relation = %q, want a derived reference", row.ManagerRelationshipRef)
	}
	if row.LifecycleStatus != journeyWorkerLifecycleActive || row.WorkerType != journeyWorkerType {
		t.Errorf("lifecycle/type = %s/%s, want the corpus vocabulary the preflight compares against",
			row.LifecycleStatus, row.WorkerType)
	}
	if row.FTE != journeyWorkerFTE || row.PayBasis != journeyWorkerPayBasis {
		t.Errorf("fte/pay basis = %s/%s", row.FTE, row.PayBasis)
	}
	if row.HireDate != "2021-04-05" || row.EffectiveFrom != "2021-04-05" {
		t.Errorf("hire/effective = %s/%s, want the form's own hire date", row.HireDate, row.EffectiveFrom)
	}
	if !row.KnownAt.Equal(row.RecordedAt) || row.KnownAt.IsZero() {
		t.Errorf("knowledge coordinates = %s/%s, want the engine's own clock", row.KnownAt, row.RecordedAt)
	}
	if row.CreatedBy != principal.Subject() {
		t.Errorf("created_by = %q, want the signed-in principal", row.CreatedBy)
	}
	if row.Source != workforce.SourceCreated {
		t.Errorf("source = %q, want CREATED", row.Source)
	}
	if err := row.Validate(); err != nil {
		t.Fatalf("the derived row is not insertable: %v", err)
	}

	// The derived halves that fill in for an incomplete form.
	sparse := workerInputFixture()
	sparse.PreferredName, sparse.Currency, sparse.BonusTarget, sparse.ManagerRef = "", "", "", ""
	filled, err := e.newWorkerRow(principal, sparse, options)
	if err != nil {
		t.Fatalf("newWorkerRow(sparse): %v", err)
	}
	if filled.PreferredName != "Ada" {
		t.Errorf("preferred name = %q, want it derived from the legal name", filled.PreferredName)
	}
	if filled.Currency != options.Currency {
		t.Errorf("currency = %q, want the options' own %q", filled.Currency, options.Currency)
	}
	if filled.BonusTarget != journeyWorkerDefaultBonusTarget {
		t.Errorf("bonus target = %q, want the default %q", filled.BonusTarget, journeyWorkerDefaultBonusTarget)
	}
}

// TestNewWorkerRowRefusesAMalformedForm walks every input rule, asserting the
// refusal names the field somebody has to fix.
func TestNewWorkerRowRefusesAMalformedForm(t *testing.T) {
	e, principal := workforceEngineFixture(t)
	options, err := workforceOptions()
	if err != nil {
		t.Fatalf("workforceOptions: %v", err)
	}
	for field, mutate := range map[string]func(*workspace.WorkerInput){
		"legal_name":   func(in *workspace.WorkerInput) { in.LegalName = "   " },
		"base_pay":     func(in *workspace.WorkerInput) { in.BasePay = "not-a-number" },
		"bonus_target": func(in *workspace.WorkerInput) { in.BonusTarget = "half" },
		"hire_date":    func(in *workspace.WorkerInput) { in.HireDate = "05/04/2021" },
	} {
		t.Run(field, func(t *testing.T) {
			in := workerInputFixture()
			mutate(&in)
			_, err := e.newWorkerRow(principal, in, options)
			if !errors.Is(err, workspace.ErrJourneyInput) {
				t.Fatalf("newWorkerRow = %v, want ErrJourneyInput", err)
			}
			if !strings.Contains(err.Error(), field) {
				t.Errorf("refusal %q does not name %s", err, field)
			}
		})
	}

	t.Run("hire_date missing", func(t *testing.T) {
		in := workerInputFixture()
		in.HireDate = ""
		if _, err := e.newWorkerRow(principal, in, options); !errors.Is(err, workspace.ErrJourneyInput) {
			t.Fatalf("newWorkerRow(no hire date) = %v, want ErrJourneyInput", err)
		}
	})

	t.Run("base pay must be positive", func(t *testing.T) {
		for _, amount := range []string{"0.00", "-1000.00"} {
			in := workerInputFixture()
			in.BasePay = amount
			_, err := e.newWorkerRow(principal, in, options)
			if !errors.Is(err, workspace.ErrJourneyInput) || !strings.Contains(err.Error(), "base_pay") {
				t.Fatalf("newWorkerRow(base %s) = %v, want an ErrJourneyInput naming base_pay", amount, err)
			}
		}
	})
}

// TestCorpusWorkersCarryOnlyDeclaredCompensation proves the listing rule: the
// two reference workers state their own declared promotion baselines, while
// the rest of the corpus never inherits a salary from somebody else's
// scenario.
func TestCorpusWorkersCarryOnlyDeclaredCompensation(t *testing.T) {
	listed, err := corpusWorkers()
	if err != nil {
		t.Fatalf("corpusWorkers: %v", err)
	}
	profiles, err := fixtures.Workers()
	if err != nil {
		t.Fatalf("fixtures.Workers: %v", err)
	}
	if len(listed) != len(profiles) {
		t.Fatalf("listed %d corpus workers, want %d", len(listed), len(profiles))
	}
	declared := map[string]struct {
		base, currency, bonus string
	}{
		"jane-doe":   {fixtures.JanePromotionBase, "USD", fixtures.JanePromotionBonus},
		"omar-reyes": {"93000.00", "USD", "0.0500"},
	}
	for i, w := range listed {
		if w.Source != workspace.WorkerSourceCorpus {
			t.Errorf("corpus worker %d source = %q, want CORPUS", i, w.Source)
		}
		if want, ok := declared[w.WorkerRef]; ok {
			if w.BasePay != want.base || w.Currency != want.currency || w.BonusTarget != want.bonus {
				t.Errorf("corpus worker %s baseline = %s/%s/%s, want %s/%s/%s", w.WorkerRef,
					w.BasePay, w.Currency, w.BonusTarget, want.base, want.currency, want.bonus)
			}
		} else if w.BasePay != "" || w.Currency != "" || w.BonusTarget != "" {
			t.Errorf("corpus worker %s carries an undeclared baseline: %+v", w.WorkerRef, w)
		}
		if !w.CreatedAt.IsZero() {
			t.Errorf("corpus worker %s claims a creation instant", w.WorkerRef)
		}
		if w.WorkerRef != profiles[i].Key || w.WorkerID != profiles[i].ID {
			t.Errorf("corpus worker %d = %s/%s, want %s/%s", i,
				w.WorkerRef, w.WorkerID, profiles[i].Key, profiles[i].ID)
		}
	}
}

// TestCreatedWorkerSummaryProjectsTheDurableRow is the other half of the
// listing rule: a created worker states the baseline it really carries.
func TestCreatedWorkerSummaryProjectsTheDurableRow(t *testing.T) {
	row := createdRowFixture()
	got := createdWorkerSummary(row)
	if got.Source != workspace.WorkerSourceCreated {
		t.Errorf("source = %q, want CREATED", got.Source)
	}
	if got.WorkerRef != row.WorkerKey || got.WorkerID != row.WorkerID.String() {
		t.Errorf("identity = %s/%s, want %s/%s", got.WorkerRef, got.WorkerID, row.WorkerKey, row.WorkerID)
	}
	if got.BasePay != row.BasePay || got.Currency != row.Currency || got.BonusTarget != row.BonusTarget {
		t.Errorf("baseline = %s/%s/%s, want the row's own", got.BasePay, got.Currency, got.BonusTarget)
	}
	if got.JobTitle != row.JobTitle || got.ManagerRef != row.ManagerRelationshipRef || got.ProfilePhotoURL != row.ProfilePhotoProxyRef {
		t.Errorf("display projection lost title, manager, or photo: %+v", got)
	}
	if !got.CreatedAt.Equal(row.RecordedAt) {
		t.Errorf("created at = %s, want the row's recorded instant %s", got.CreatedAt, row.RecordedAt)
	}
}

func TestCreatedWorkerSummaryTreatsBoardAuthorityAsEmployeeTreeRoot(t *testing.T) {
	row := createdRowFixture()
	row.ManagerRelationshipRef = "board:harborcare"
	got := createdWorkerSummary(row)
	if got.ManagerDisposition != workspace.ManagerRelationshipRoot {
		t.Fatalf("manager disposition = %q, want ROOT", got.ManagerDisposition)
	}
	if got.ManagerRef != row.ManagerRelationshipRef {
		t.Fatalf("manager relationship ref = %q, want governed source ref %q", got.ManagerRef, row.ManagerRelationshipRef)
	}
}

// TestJourneyBaselineTakesACreatedWorkersOwnPay is the baseline-selection
// rule: the ported legacy scenario describes exactly one worker, so an
// employee somebody just made must not inherit that person's salary.
func TestJourneyBaselineTakesACreatedWorkersOwnPay(t *testing.T) {
	in := journeyProposalFixture()
	corpus, err := journeyBaseline(in, journeyCorpusSubject(t))
	if err != nil {
		t.Fatalf("journeyBaseline(corpus): %v", err)
	}

	row := createdRowFixture()
	created, err := journeyBaseline(in, WorkerLocation{Key: row.WorkerKey, Created: &row})
	if err != nil {
		t.Fatalf("journeyBaseline(created): %v", err)
	}
	if created.currentBase != row.BasePay || created.currency != row.Currency ||
		created.bonusTarget != row.BonusTarget {
		t.Fatalf("created baseline = %s/%s/%s, want the row's own %s/%s/%s",
			created.currentBase, created.currency, created.bonusTarget,
			row.BasePay, row.Currency, row.BonusTarget)
	}
	if created.currentBase == corpus.currentBase {
		t.Fatal("a created worker inherited the ported scenario's salary")
	}
	// REV-006-01: the created coordinate keeps the row's full intraday
	// precision. Truncating it to a date moved the knowledge cut-off to
	// midnight and made every fact the row records later that day read as
	// stale, refusing every journey propose for a worker created that day.
	if created.knownAtCutoff() != row.KnownAt.UTC().Format(time.RFC3339Nano) {
		t.Errorf("known-at = %q, want the row's own %s",
			created.knownAtCutoff(), row.KnownAt.UTC().Format(time.RFC3339Nano))
	}
	// The budget authority is a finance fact about the org unit, not about
	// the worker, so it still comes from the corpus either way.
	if created.budgetAvailabe != corpus.budgetAvailabe || created.budgetAvailabe == "" {
		t.Errorf("budget = %q, want the corpus authority %q", created.budgetAvailabe, corpus.budgetAvailabe)
	}
	// A corpus worker's knowledge coordinate is unchanged.
	if corpus.knownAtCutoff() != corpus.evaluationDate {
		t.Errorf("corpus known-at = %q, want the scenario's evaluation date %q",
			corpus.knownAtCutoff(), corpus.evaluationDate)
	}
}

// TestJourneyWorkerKeyDisambiguatesSharedNames proves two people with one name
// get two keys: a creation that collided on a name would be a person the
// surface refused to admit exists.
func TestJourneyWorkerKeyDisambiguatesSharedNames(t *testing.T) {
	first := journeyWorkerKey("Ada", "Ada Lovelace", "1a2b3c4d")
	second := journeyWorkerKey("Ada", "Ada Lovelace", "9f8e7d6c")
	if first == second {
		t.Fatalf("two workers named Ada share the key %q", first)
	}
	if first != "ada-1a2b3c4d" {
		t.Fatalf("key = %q, want the name slug plus the short id", first)
	}
	// A name with nothing sluggable in it still produces a usable key from
	// the legal name, and failing that from the fallback token.
	if got := journeyWorkerKey("", "Ada Lovelace", "1a2b3c4d"); got != "ada-lovelace-1a2b3c4d" {
		t.Errorf("key with no preferred name = %q", got)
	}
	if got := journeyWorkerKey("", "", "1a2b3c4d"); got != "worker-1a2b3c4d" {
		t.Errorf("key with no name at all = %q, want the fallback", got)
	}
	// Punctuation-only names sanitize to separators alone; the key must not
	// open with a bare "-" but fall through to the next readable name.
	if got := journeyWorkerKey("**", "Ada Lovelace", "1a2b3c4d"); got != "ada-lovelace-1a2b3c4d" {
		t.Errorf("key with an unsluggable preferred name = %q", got)
	}
	if got := journeyWorkerKey("**", "??", "1a2b3c4d"); got != "worker-1a2b3c4d" {
		t.Errorf("key with no sluggable name at all = %q, want the fallback", got)
	}
	// A person literally called Worker keeps their name; only the empty
	// name is the fallback token.
	if got := journeyWorkerKey("Worker", "Worker Bee", "1a2b3c4d"); got != "worker-1a2b3c4d" {
		t.Errorf("key for a person named Worker = %q", got)
	}
}

// TestWorkforceSubjectNamesTheEvidenceSubject proves a refused creation still
// records who it was about, even before a key exists.
func TestWorkforceSubjectNamesTheEvidenceSubject(t *testing.T) {
	if got := workforceSubject(workspace.WorkerInput{PreferredName: "Ada"}); got != "ada" {
		t.Errorf("subject = %q, want the preferred name slug", got)
	}
	if got := workforceSubject(workspace.WorkerInput{LegalName: "Ada Lovelace"}); got != "ada-lovelace" {
		t.Errorf("subject = %q, want the legal name slug", got)
	}
	if got := workforceSubject(workspace.WorkerInput{}); got != "worker" {
		t.Errorf("subject = %q, want the fallback token", got)
	}
}

// TestWorkforceRefusalClassificationIsByTypedError proves the store's two
// typed refusals are told apart by errors.Is rather than by message text.
func TestWorkforceRefusalClassificationIsByTypedError(t *testing.T) {
	if !isWorkforceDuplicate(workforceWrap(workforce.ErrDuplicate)) {
		t.Error("a duplicate was not recognised")
	}
	if !isWorkforceInvalidRow(workforceWrap(workforce.ErrInvalidRow)) {
		t.Error("an invalid row was not recognised")
	}
	if isWorkforceDuplicate(errors.New("workforce: a worker with that key already exists in this tenant")) {
		t.Error("a look-alike message was classified as a duplicate")
	}
}

func workforceWrap(err error) error { return errors.Join(errors.New("app: journey: create"), err) }

// createdRowFixture is one durable worker row with a baseline deliberately
// different from the ported scenario's, so a baseline that came from the wrong
// place is visible.
func createdRowFixture() workforce.WorkerRow {
	id := uuid.MustParse("7f3b1c22-0000-4000-8000-0000000000aa")
	return workforce.WorkerRow{
		WorkerID:                id,
		WorkerKey:               "ada-7f3b1c22",
		LegalName:               "Ada Lovelace",
		PreferredName:           "Ada",
		WorkerNumber:            "W-J7F3B1C22",
		WorkerType:              journeyWorkerType,
		LifecycleStatus:         journeyWorkerLifecycleActive,
		JobCode:                 "OPS-HRBP2",
		JobTitle:                "Senior People Partner",
		Grade:                   "P2",
		OrgUnit:                 "people-ops",
		PositionID:              "POS-HRBP-204",
		Location:                "Boston, MA",
		PayZone:                 "US-EAST",
		FTE:                     journeyWorkerFTE,
		ManagerRelationshipRef:  "manager-ada",
		ProfilePhotoOriginalRef: "profile-originals/ada.png",
		ProfilePhotoProxyRef:    "/workspace/assets/person-ada-small.jpg",
		HireDate:                "2021-04-05",
		EffectiveFrom:           "2021-04-05",
		BasePay:                 "81500.00",
		Currency:                "USD",
		PayBasis:                journeyWorkerPayBasis,
		BonusTarget:             "0.0700",
		RevisionStream:          "people.worker." + id.String(),
		RevisionSequence:        1,
		KnownAt:                 time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC),
		RecordedAt:              time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC),
		CreatedBy:               "user-0191f3c4",
		Source:                  workforce.SourceCreated,
	}
}

// workforceEngineFixture builds the smallest engine the derivation rules need:
// an id source, a tenant mapping and a pinned clock. It touches no database,
// because everything under test here happens before the row is written.
func workforceEngineFixture(t *testing.T) (*journeyEngine, *trust.Principal) {
	t.Helper()
	svc := &IntentService{
		ids:        intent.UUIDv7Source,
		tenantUUID: func(values.TenantId) uuid.UUID { return uuid.MustParse("11111111-2222-4333-8444-555555555555") },
	}
	engine := newJourneyEngine(svc, nil, "", func() time.Time {
		return time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	}, nil)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant:               fixtures.Tenant,
		Subject:              "user-0191f3c4",
		SubjectKind:          trust.SubjectKindHuman,
		Roles:                []string{"intent_author"},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance:            trust.AssuranceHigh,
		SessionRef:           "session-workforce",
		Purposes:             []string{"hcm_operations"},
		IssuedAt:             time.Now().Add(-time.Minute),
		ExpiresAt:            time.Now().Add(time.Hour),
		CredentialDigest:     "digest:workforce-fixture",
	})
	if err != nil {
		t.Fatalf("trust.NewPrincipal: %v", err)
	}
	return engine, principal
}
